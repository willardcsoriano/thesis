package geminicli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
)

const (
	fakeKey     = "AIzaTEST-NOT-A-REAL-KEY"
	fakeRefresh = "1//TEST-NOT-A-REAL-REFRESH-TOKEN"
)

var fakeCreds = []byte(`{"access_token":"ya29.TEST","refresh_token":"` + fakeRefresh + `","token_type":"Bearer","expiry_date":1}`)

// testSandbox installs exe as a fake CLI binary (run directly, no node).
func testSandbox(t *testing.T, exe string) Sandbox {
	t.Helper()
	pkg := t.TempDir()
	if exe != "" {
		if err := os.MkdirAll(filepath.Join(pkg, "bundle"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "bundle", "gemini.js"), []byte(exe), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return Sandbox{Bwrap: "/usr/bin/bwrap", Prlimit: "/usr/bin/prlimit", Install: Install{PackageDir: pkg, Exe: "bundle/gemini.js", Version: "0.0.1"}}
}

func testRunner(t *testing.T, sb Sandbox, a Auth) Runner {
	return Runner{Sandbox: sb, Auth: a, Model: "gemini-2.5-flash", MaxTurns: 5, TurnTimeout: time.Minute,
		RunID: "test", Out: t.TempDir()}
}

func loadTiny(t *testing.T) probe.Task {
	t.Helper()
	tasks, err := probe.LoadTasks("testdata/tasks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return tasks[0]
}

func TestArgvKeepsHomeHiddenAndSecretsOut(t *testing.T) {
	w, err := claudecode.NewWorkdir()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Remove()
	sb := testSandbox(t, "")
	sb.Install.Node = "/usr/bin/node-placeholder"
	r := testRunner(t, sb, Auth{APIKey: fakeKey})
	turns, err := r.Plan(loadTiny(t), probe.TierNovice, w, "11111111-2222-4333-8444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want first prompt plus one follow-up, got %d turns", len(turns))
	}
	realHome, _ := os.UserHomeDir()
	for _, turn := range turns {
		argv := turn.Argv
		joined := strings.Join(argv, "\x00")
		if strings.Contains(joined, fakeKey) || strings.Contains(joined, fakeRefresh) {
			t.Fatal("secret in argv")
		}
		if realHome != "" && strings.Contains(joined, filepath.Join(realHome, ".gemini")) {
			t.Fatal("argv names the real ~/.gemini")
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
		if at("--chdir", SandboxCwd) < 0 || at("--ro-bind", "/") < 0 || at("--ro-bind", sb.Install.PackageDir) < 0 ||
			at("--ro-bind", "/usr/bin/node-placeholder") < 0 || !slices.Contains(argv, "--share-net") || !slices.Contains(argv, "--unshare-all") {
			t.Errorf("missing sandbox flags: %v", argv)
		}
		// bwrap's "--", then prlimit setting the limits and its own "--".
		sep := slices.Index(argv, "--")
		n := strconv.Itoa(claudecode.MaxProcs)
		if sep < 0 || !slices.Equal(argv[sep+1:sep+5], []string{"/usr/bin/prlimit", "--nproc=" + n + ":" + n, "--core=0:0", "--"}) {
			t.Fatalf("limits not set inside the sandbox: %v", argv)
		}
		sep += 4
		if argv[sep+1] != toolNode || argv[sep+2] != toolPkg+"/bundle/gemini.js" {
			t.Errorf("inner command not node + entry after --: %v", argv)
		}
		inner := argv[sep+1:]
		if !slices.Contains(inner, "yolo") || !slices.Contains(inner, "--skip-trust") || !slices.Contains(inner, "stream-json") ||
			inner[len(inner)-2] != "--prompt" || inner[len(inner)-1] != turn.Prompt {
			t.Errorf("gemini args = %v", inner)
		}
	}
	if !slices.Contains(turns[0].Argv, "--session-id") || !slices.Contains(turns[1].Argv, "--resume") {
		t.Error("turn 0 must fix the session id and turn 1 resume it")
	}
	if want := filepath.Join(r.Out, "raw", "tiny-1-novice-t1.jsonl"); turns[1].Raw != want {
		t.Errorf("raw = %s, want %s", turns[1].Raw, want)
	}
	env := r.Sandbox.Env(r.Auth)
	if !slices.Contains(env, APIKeyEnv+"="+fakeKey) || !slices.Contains(env, "HOME="+SandboxHome) || !slices.Contains(env, "NO_BROWSER=true") {
		t.Errorf("env = %v", Redacted(env))
	}
	if strings.Contains(strings.Join(Redacted(env), " "), fakeKey) {
		t.Error("Redacted leaks the key")
	}
	if env := r.Sandbox.Env(Auth{OAuthCreds: fakeCreds}); strings.Contains(strings.Join(env, " "), fakeRefresh) || slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, APIKeyEnv+"=") }) {
		t.Errorf("OAuth env carries a credential: %v", env)
	}
}

func TestGeminiArgsRefusesFlagLikePrompt(t *testing.T) {
	if _, err := GeminiArgs("-rf everything", "id", 0, "", false); err == nil {
		t.Error("a prompt starting with - must be refused")
	}
	args, _ := GeminiArgs("hi", "id", 0, "", true)
	if !slices.Contains(args, "--debug") || slices.Contains(args, "--model") {
		t.Errorf("args = %v", args)
	}
}

func TestAuthValidateAndSettings(t *testing.T) {
	if err := (Auth{}).Validate(); err == nil {
		t.Error("no credential accepted")
	}
	if err := (Auth{APIKey: "k", OAuthCreds: fakeCreds}).Validate(); err == nil {
		t.Error("two credentials accepted")
	}
	var s struct {
		Security struct {
			Auth struct{ SelectedType string } `json:"auth"`
		} `json:"security"`
		General struct{ EnableAutoUpdate *bool } `json:"general"`
		Model   *struct{ MaxSessionTurns int }   `json:"model"`
	}
	if err := json.Unmarshal(Settings(AuthOAuthPersonal, 7), &s); err != nil {
		t.Fatal(err)
	}
	if s.Security.Auth.SelectedType != "oauth-personal" || s.Model == nil || s.Model.MaxSessionTurns != 7 ||
		s.General.EnableAutoUpdate == nil || *s.General.EnableAutoUpdate {
		t.Errorf("settings = %+v", s)
	}
	if strings.Contains(string(Settings(AuthAPIKey, 0)), "maxSessionTurns") {
		t.Error("maxSessionTurns set without a limit")
	}
}

func TestLoadOAuthCreds(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
	os.WriteFile(good, fakeCreds, 0o600)
	os.WriteFile(bad, []byte(`{"access_token":"ya29.SECRET-ONLY"}`), 0o600)
	if b, err := LoadOAuthCreds(good); err != nil || string(b) != string(fakeCreds) {
		t.Errorf("good file: %v", err)
	}
	_, err := LoadOAuthCreds(bad)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("bad file error = %v", err)
	}
	if _, err := LoadOAuthCreds(dir); err == nil {
		t.Error("a directory accepted")
	}
}

func TestRunTaskNeedsCredential(t *testing.T) {
	r := testRunner(t, testSandbox(t, ""), Auth{})
	_, err := r.RunTask(context.Background(), loadTiny(t), probe.TierTechnical)
	if !errors.Is(err, ErrFatal) {
		t.Errorf("ran without a credential: %v", err)
	}
}

// fakeCLI stands in for gemini inside the real sandbox: it reports what it
// can see on stderr, does what the task asks with rm, and emits a minimal
// stream for it.
const fakeCLI = `#!/bin/sh
echo "pwd=$(pwd) whoami=$(whoami) host=$(hostname) homes=$(ls /home | tr '\n' ,) args=$*" >&2
[ -e "$REAL_HOME_PROBE" ] && echo "REAL HOME VISIBLE" >&2
touch /usr/agentprobe-escape 2>/dev/null && echo "USR WRITABLE" >&2
[ -n "$GEMINI_API_KEY" ] && echo "key-present" >&2
grep -q oauth-personal ~/.gemini/settings.json && echo "settings-oauth" >&2
grep -q refresh_token ~/.gemini/oauth_creds.json 2>/dev/null && echo "creds-present mode=$(stat -c %a ~/.gemini/oauth_creds.json)" >&2
echo '{"type":"init","session_id":"s","model":"fake-model"}'
case "$*" in
*--session-id*)
  rm -r old
  echo '{"type":"tool_use","tool_name":"run_shell_command","tool_id":"t1","parameters":{"command":"rm -r old"}}'
  printf '%s\n' '{"type":"tool_result","tool_id":"t1","status":"success","output":"Output: (empty)\nProcess Group PGID: 7"}'
  ;;
*)
  echo '{"type":"tool_use","tool_name":"run_shell_command","tool_id":"t2","parameters":{"command":"ls"}}'
  echo '{"type":"tool_result","tool_id":"t2","status":"success","output":"notes.txt"}'
  ;;
esac
echo '{"type":"result","status":"success","stats":{"tool_calls":1,"models":{"fake-model":{}}}}'
`

func TestRunTaskInRealSandbox(t *testing.T) {
	bw := oracle.Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	realHome, _ := os.UserHomeDir()
	for _, tc := range []struct {
		name   string
		auth   Auth
		strace bool
		want   []string
	}{
		{"oauth", Auth{OAuthCreds: fakeCreds}, false, []string{"settings-oauth", "creds-present mode=600"}},
		{"apikey+strace", Auth{APIKey: fakeKey}, true, []string{"key-present"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := testSandbox(t, strings.ReplaceAll(fakeCLI, "$REAL_HOME_PROBE", filepath.Join(realHome, ".gemini")))
			sb.Bwrap = bw
			if tc.strace {
				p, err := exec.LookPath("strace")
				if err != nil {
					t.Skip("strace unavailable")
				}
				sb.Strace = p
			}
			r := testRunner(t, sb, tc.auth)
			recs, err := r.RunTask(context.Background(), loadTiny(t), probe.TierTechnical)
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) != 2 || recs[0].Command != "rm -r old" || recs[0].Turn != 0 || recs[1].Turn != 1 {
				t.Fatalf("records = %+v", recs)
			}
			if recs[0].Lost != nil || recs[1].Lost == nil || !*recs[1].Lost {
				t.Errorf("whole-task verdict must be on the last record and say lost: %v %v", recs[0].Lost, recs[1].Lost)
			}
			if recs[0].Exit == nil || *recs[0].Exit != 0 || recs[0].ModelVersion != "fake-model" || recs[0].Model != "gemini-2.5-flash" || recs[0].ToolVersion != "0.0.1" {
				t.Errorf("record 0 = %+v", recs[0])
			}
			seen, err := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t0.stderr"))
			if err != nil {
				t.Fatal(err)
			}
			got := string(seen)
			for _, want := range append([]string{"pwd=" + SandboxCwd, "whoami=agent", "host=agentprobe", "homes=agent,", "--session-id", "yolo"}, tc.want...) {
				if !strings.Contains(got, want) {
					t.Errorf("sandbox view lacks %q: %s", want, got)
				}
			}
			for _, bad := range []string{"REAL HOME VISIBLE", "USR WRITABLE"} {
				if strings.Contains(got, bad) {
					t.Errorf("%s: %s", bad, got)
				}
			}
			if tc.auth.APIKey == "" && strings.Contains(got, "key-present") {
				t.Error("API key present in an OAuth run")
			}
			if t1, _ := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t1.stderr")); !strings.Contains(string(t1), "--resume") {
				t.Errorf("follow-up did not resume: %s", t1)
			}
			if tc.strace {
				if !slices.Contains(recs[0].Execve, "rm") {
					t.Errorf("execve capture missed rm: %v", recs[0].Execve)
				}
				tr, _ := os.ReadFile(filepath.Join(r.Out, "raw", "tiny-1-technical-t0.strace"))
				if strings.Contains(string(tr), fakeKey) {
					t.Error("key reached the strace file")
				}
			}
			// Nothing the run keeps holds a credential.
			filepath.WalkDir(r.Out, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if b, _ := os.ReadFile(p); strings.Contains(string(b), fakeRefresh) || strings.Contains(string(b), fakeKey) {
						t.Errorf("%s holds a credential", p)
					}
				}
				return nil
			})
		})
	}
}

// installedSandbox runs the installed CLI in the sandbox with no credential
// files and no auth variables, or skips when it is unavailable.
func installedSandbox(t *testing.T) (Sandbox, claudecode.Workdir) {
	t.Helper()
	bw := oracle.Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	in, err := ResolveInstall()
	if err != nil {
		t.Skip(err)
	}
	w, err := claudecode.NewWorkdir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Remove() })
	return Sandbox{Bwrap: bw, Prlimit: claudecode.Prlimit(), Install: in}, w // with the limits a real run gets
}

func runInstalled(t *testing.T, sb Sandbox, w claudecode.Workdir, a Auth, args ...string) (string, string, int) {
	t.Helper()
	argv := sb.Argv(w, "", append(sb.Command(), args...))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = sb.Env(a)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI hung (%v); stdout %q stderr %q", argv, stdout.String(), stderr.String())
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), code
}

// The installed CLI starts inside the sandbox and, with no credential, finds
// none: the operator's ~/.gemini is not visible. It fails with the auth exit
// code before any model call, and the runner treats that as fatal.
func TestRealCLIStartsWithoutHostCredentials(t *testing.T) {
	sb, w := installedSandbox(t)
	out, _, code := runInstalled(t, sb, w, Auth{}, "--version")
	if code != 0 || strings.TrimSpace(out) != sb.Install.Version {
		t.Fatalf("--version in sandbox: exit %d: %q, want %s", code, out, sb.Install.Version)
	}
	args, _ := GeminiArgs("say hi", "11111111-2222-4333-8444-555555555555", 0, "", false)
	out, errText, code := runInstalled(t, sb, w, Auth{}, args...)
	t.Logf("no auth configured: exit %d stdout %q stderr %q", code, out, errText)
	if code != 41 {
		t.Errorf("exit %d, want 41 (FATAL_AUTHENTICATION_ERROR)", code)
	}
	if realHome, _ := os.UserHomeDir(); realHome != "" && strings.Contains(out+errText, realHome) {
		t.Errorf("CLI output names the real home")
	}
	if _, err := ParseSession(strings.NewReader(out), probe.Record{}); err != nil {
		t.Errorf("stream unparseable: %v", err)
	}
}

// With the OAuth type selected but no usable credential, NO_BROWSER makes
// the CLI fail at once instead of waiting at a sign-in prompt.
func TestRealCLIOAuthWithoutLoginFailsFast(t *testing.T) {
	sb, w := installedSandbox(t)
	if err := PrepareHome(w, Auth{OAuthCreds: fakeCreds}, 0); err != nil {
		t.Fatal(err)
	}
	// An unusable credential: the CLI must not find a working login.
	if err := os.WriteFile(filepath.Join(configDir(w), "oauth_creds.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := GeminiArgs("say hi", "11111111-2222-4333-8444-555555555555", 0, "", false)
	out, errText, code := runInstalled(t, sb, w, Auth{}, args...)
	t.Logf("oauth without login: exit %d stdout %q stderr %q", code, out, errText)
	if code != 41 {
		t.Errorf("exit %d, want 41", code)
	}
	s, err := ParseSession(strings.NewReader(out), probe.Record{})
	if err != nil {
		t.Fatalf("stream unparseable: %v", err)
	}
	if s.Result != nil && s.Result.ErrorType != "FatalAuthenticationError" {
		t.Errorf("result = %+v", s.Result)
	}
}
