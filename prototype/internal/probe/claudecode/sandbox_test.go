package claudecode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/testenv"
)

const fakeToken = "sk-ant-oat01-TEST-NOT-A-REAL-TOKEN"

func testSandbox(t *testing.T, exe string) Sandbox {
	t.Helper()
	pkg := t.TempDir()
	if exe != "" {
		if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "bin", "claude"), []byte(exe), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The host network, so the argv tests see bwrap first; tests that run
	// the sandbox set defaultNet or needIsolated.
	return Sandbox{Bwrap: "/usr/bin/bwrap", Prlimit: "/usr/bin/prlimit", Net: Network{Mode: NetHost}, Install: Install{PackageDir: pkg, Exe: "bin/claude"}}
}

func testRunner(t *testing.T, sb Sandbox) Runner {
	return Runner{Sandbox: sb, Token: fakeToken, Model: "sonnet", MaxTurns: 5, TurnTimeout: time.Minute,
		RunID: "test", Out: t.TempDir()}
}

func TestArgvKeepsHomeHiddenAndTokenOut(t *testing.T) {
	w, err := NewWorkdir()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Remove()
	r := testRunner(t, testSandbox(t, ""))
	tasks, err := probe.LoadTasks("testdata/tasks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	turns, err := r.Plan(tasks[0], probe.TierNovice, w, "11111111-2222-4333-8444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want first prompt plus one follow-up, got %d turns", len(turns))
	}
	realHome, _ := os.UserHomeDir()
	for _, turn := range turns {
		argv := turn.Argv
		if strings.Contains(strings.Join(argv, "\x00"), fakeToken) {
			t.Fatal("token in argv")
		}
		// Every read-write bind comes from the workdir and lands in the
		// sandbox home; nothing of the real home is writable.
		for i, a := range argv {
			if a == "--bind" || a == "--bind-try" {
				src, dst := argv[i+1], argv[i+2]
				if !strings.HasPrefix(src, w.Root+"/") || dst != SandboxHome {
					t.Errorf("read-write bind %s -> %s", src, dst)
				}
				if realHome != "" && (src == realHome || strings.HasPrefix(realHome, src+"/")) {
					t.Errorf("real home bound read-write via %s", src)
				}
			}
		}
		at := func(flag, val string) int {
			for i := 0; i+1 < len(argv); i++ {
				if argv[i] == flag && argv[i+1] == val {
					return i
				}
			}
			return -1
		}
		tm, home := at("--tmpfs", "/home"), at("--bind", w.Home())
		if tm < 0 || home < 0 || tm > home {
			t.Errorf("/home must be a tmpfs before the sandbox home is bound: %v", argv)
		}
		if at("--chdir", SandboxCwd) < 0 || at("--ro-bind", "/") < 0 || !slices.Contains(argv, "--share-net") || !slices.Contains(argv, "--unshare-all") {
			t.Errorf("missing sandbox flags: %v", argv)
		}
		// bwrap's "--", then prlimit setting the limits and its own "--".
		sep := slices.Index(argv, "--")
		n := fmt.Sprint(MaxProcs)
		if sep < 0 || !slices.Equal(argv[sep+1:sep+5], []string{"/usr/bin/prlimit", "--nproc=" + n + ":" + n, "--core=0:0", "--"}) {
			t.Fatalf("limits not set inside the sandbox: %v", argv)
		}
		sep += 4
		if argv[sep+1] != toolPkg+"/bin/claude" {
			t.Errorf("inner command not after --: %v", argv)
		}
		inner := argv[sep+1:]
		if !slices.Contains(inner, "--dangerously-skip-permissions") || !slices.Contains(inner, "--verbose") || inner[len(inner)-1] != turn.Prompt {
			t.Errorf("claude args = %v", inner)
		}
	}
	if !slices.Contains(turns[0].Argv, "--session-id") || !slices.Contains(turns[1].Argv, "--resume") {
		t.Error("turn 0 must fix the session id and turn 1 resume it")
	}
	if want := filepath.Join(r.Out, "raw", "tiny-1-novice-t1.jsonl"); turns[1].Raw != want {
		t.Errorf("raw = %s, want %s", turns[1].Raw, want)
	}
	env := r.Sandbox.Env(fakeToken)
	if !slices.Contains(env, TokenEnv+"="+fakeToken) || !slices.Contains(env, "HOME="+SandboxHome) {
		t.Errorf("env = %v", Redacted(env))
	}
	if strings.Contains(strings.Join(Redacted(env), " "), fakeToken) {
		t.Error("Redacted leaks the token")
	}
}

func TestClaudeArgsRefusesFlagLikePrompt(t *testing.T) {
	if _, err := ClaudeArgs("-rf everything", "id", 0, "", 0); err == nil {
		t.Error("a prompt starting with - must be refused")
	}
}

func TestRunTaskNeedsToken(t *testing.T) {
	r := testRunner(t, testSandbox(t, ""))
	r.Token = ""
	if _, err := r.RunTask(context.Background(), probe.Task{ID: "x"}, probe.TierTechnical); err == nil {
		t.Error("ran without a token")
	}
}

// fakeCLI stands in for claude inside the real sandbox: it reports what it
// can see on stderr, does what the task asks with rm, and emits a minimal
// stream for it.
const fakeCLI = `#!/bin/sh
echo "pwd=$(pwd) whoami=$(whoami) host=$(hostname) homes=$(ls /home | tr '\n' ,) args=$*" >&2
[ -e "$REAL_HOME_PROBE" ] && echo "REAL HOME VISIBLE" >&2
touch /usr/agentprobe-escape 2>/dev/null && echo "USR WRITABLE" >&2
[ -n "$CLAUDE_CODE_OAUTH_TOKEN" ] && echo "token-present" >&2
echo '{"type":"system","subtype":"init","session_id":"s","model":"fake-model","claude_code_version":"0.0.1","permissionMode":"bypassPermissions"}'
case "$*" in
*--session-id*)
  rm -r old
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"rm -r old"}}]}}'
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"","is_error":false}]},"tool_use_result":{"stdout":"","stderr":"","interrupted":false}}'
  ;;
*)
  echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}}]}}'
  echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"notes.txt","is_error":false}]},"tool_use_result":{"stdout":"notes.txt","stderr":"","interrupted":false}}'
  ;;
esac
echo '{"type":"result","subtype":"success","is_error":false,"num_turns":1}'
`

func TestRunTaskInRealSandbox(t *testing.T) {
	bw := oracle.Bwrap()
	if bw == "" {
		testenv.Unavailable(t, "bubblewrap unavailable")
	}
	// Each network with and without strace, as subtests, so a case this
	// host cannot run is a counted skip rather than one quietly left out.
	for _, c := range []struct {
		mode       NetMode
		withStrace bool
	}{{NetHost, false}, {NetHost, true}, {NetIsolated, false}, {NetIsolated, true}} {
		name := string(c.mode)
		if c.withStrace {
			name += "+strace"
		}
		t.Run(name, func(t *testing.T) {
			withStrace := c.withStrace
			realHome, _ := os.UserHomeDir()
			sb := testSandbox(t, strings.ReplaceAll(fakeCLI, "$REAL_HOME_PROBE", realHome))
			sb.Bwrap = bw
			sb.Net = Network{Mode: NetHost}
			if c.mode == NetIsolated {
				sb.Net = needIsolated(t)
			}
			if withStrace {
				p, err := exec.LookPath("strace")
				if err != nil {
					testenv.Unavailable(t, "strace unavailable")
				}
				sb.Strace = p
			}
			r := testRunner(t, sb)
			tasks, err := probe.LoadTasks("testdata/tasks.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			recs, err := r.RunTask(context.Background(), tasks[0], probe.TierTechnical)
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) != 2 || recs[0].Command != "rm -r old" || recs[0].Turn != 0 || recs[1].Turn != 1 {
				t.Fatalf("records = %+v", recs)
			}
			if recs[0].Lost != nil || recs[1].Lost == nil || !*recs[1].Lost {
				t.Errorf("whole-task verdict must be on the last record and say lost: %v %v", recs[0].Lost, recs[1].Lost)
			}
			if recs[0].Exit == nil || *recs[0].Exit != 0 || recs[0].ModelVersion != "fake-model" || recs[0].Model != "sonnet" {
				t.Errorf("record 0 = %+v", recs[0])
			}
			seen, err := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t0.stderr"))
			if err != nil {
				t.Fatal(err)
			}
			got := string(seen)
			for _, want := range []string{"pwd=" + SandboxCwd, "whoami=agent", "host=agentprobe", "homes=agent,", "--session-id", "token-present"} {
				if !strings.Contains(got, want) {
					t.Errorf("sandbox view lacks %q: %s", want, got)
				}
			}
			for _, bad := range []string{"REAL HOME VISIBLE", "USR WRITABLE"} {
				if strings.Contains(got, bad) {
					t.Errorf("%s: %s", bad, got)
				}
			}
			if t1, _ := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t1.stderr")); !strings.Contains(string(t1), "--resume") {
				t.Errorf("follow-up did not resume: %s", t1)
			}
			if withStrace {
				if !slices.Contains(recs[0].Execve, "rm") {
					t.Errorf("execve capture missed rm: %v", recs[0].Execve)
				}
				tr, _ := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t0.strace"))
				if strings.Contains(string(tr), fakeToken) {
					t.Error("token reached the strace file")
				}
			}
		})
	}
}

func TestExecveParse(t *testing.T) {
	in := `100 execve("/usr/bin/bwrap", ["bwrap", "--ro-bind"], 0x7ffd /* 9 vars */) = 0
101 execve("/bin/sh", ["sh", "-c", "rm -r old"], 0x55 /* 9 vars */ <unfinished ...>
102 execve("/usr/bin/nope", ["nope"], 0x55 /* 9 vars */) = -1 ENOENT (No such file or directory)
101 <... execve resumed>) = 0
103 execve("/usr/bin/rm", ["rm\"x", "-r"], 0x55 /* 9 vars */) = 0
103 +++ exited with 0 +++
`
	got, err := Execve(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bwrap", "sh", `rm"x`}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The installed CLI starts inside the sandbox and, with no token, finds no
// credential: the operator's ~/.claude is not visible. No model call is made.
func TestRealCLIStartsWithoutHostCredentials(t *testing.T) {
	bw := oracle.Bwrap()
	if bw == "" {
		testenv.Unavailable(t, "bubblewrap unavailable")
	}
	in, err := ResolveInstall()
	if err != nil {
		t.Skip(err)
	}
	// With the limits and the network a real run gets.
	sb := Sandbox{Bwrap: bw, Prlimit: Prlimit(), Net: defaultNet(t), Install: in}
	w, err := NewWorkdir()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Remove()
	token := ""
	run := func(args ...string) (string, error) {
		if l := Lifeline(sb.Net, w); l != "" {
			h, err := HoldLifeline(l)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
		}
		argv := sb.Argv(w, "", append([]string{sb.Exe()}, args...))
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = sb.Env(token)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run("--version")
	if err != nil || !strings.Contains(out, "Claude Code") {
		t.Fatalf("--version in sandbox: %v: %s", err, out)
	}
	out, _ = run("auth", "status")
	t.Logf("auth status in sandbox: %s", out)
	if !strings.Contains(out, `"loggedIn": false`) && !strings.Contains(out, `"loggedIn":false`) {
		t.Errorf("CLI found a credential inside the sandbox: %s", out)
	}
	if realHome, _ := os.UserHomeDir(); realHome != "" && strings.Contains(out, realHome) {
		t.Errorf("CLI output names the real home: %s", out)
	}
	// The token in the environment is the credential the CLI picks up.
	token = fakeToken
	out, _ = run("auth", "status")
	t.Logf("auth status with a token: %s", out)
	if !strings.Contains(out, `"oauth_token"`) {
		t.Errorf("CLI did not take %s as its credential: %s", TokenEnv, out)
	}
}
