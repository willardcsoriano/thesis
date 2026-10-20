package probe

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/testenv"
)

func TestParseExecve(t *testing.T) {
	log := `5     execve("/usr/bin/bash", ["bash", "-c", "ls"], 0x7ffc /* 67 vars */) = 0
6     execve("/usr/bin/sh", ["sh", "-c", "python3 -c pass"], 0x5607 /* 67 vars */) = 0
7     execve("/usr/local/bin/python3", ["python3", "-c", "pass"], 0x55ec /* 67 vars */) = -1 ENOENT (No such file or directory)
7     execve("/usr/bin/python3", ["/usr/bin/python3", "-c", "pass"], 0x55ec <unfinished ...>
8     execve("/usr/bin/ls", ["ls"], 0x5607 /* 67 vars */) = 0
7     <... execve resumed>) = 0
9     execve("/usr/bin/odd", ["we\"ird name"], 0x1 /* 1 var */) = 0
10    execve("/usr/bin/noargv", [], 0x1 /* 1 var */) = 0
`
	got, err := parseExecve(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bash", "sh", "ls", "python3", `we"ird name`, "noargv"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func traceTools(t *testing.T) (string, string) {
	t.Helper()
	bw, st := oracle.Bwrap(), Strace()
	if bw == "" || st == "" {
		testenv.Unavailable(t, "tracing needs bubblewrap and strace")
	}
	return bw, st
}

func runTrace(t *testing.T, cmd string) Traced {
	t.Helper()
	bw, st := traceTools(t)
	root := t.TempDir()
	if err := oracle.Materialize(root, []oracle.Entry{
		{Path: "a.txt", Kind: "file", Data: "a\n"},
		{Path: "b.txt", Kind: "file", Data: "b\n"},
	}); err != nil {
		t.Fatal(err)
	}
	tr, err := Trace(context.Background(), bw, st, root, cmd, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestTraceInterpreterBehindShell(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	tr := runTrace(t, "sh -c 'python3 -c pass'")
	if !tr.Outcome.Clean() {
		t.Fatalf("outcome %+v", tr.Outcome)
	}
	if !slices.Contains(tr.Execve, "python3") || !slices.Contains(tr.Execve, "sh") {
		t.Errorf("Execve = %q, want sh and python3", tr.Execve)
	}
	if slices.Contains(tr.Execve, "strace") || slices.Contains(tr.Execve, "bash") {
		t.Errorf("Execve = %q, still holds the tracer or the wrapper shell", tr.Execve)
	}
	if tr.Lost() {
		t.Errorf("lost %v", tr.Verdict)
	}
	var r Record
	tr.Apply(&r)
	if r.Exit == nil || *r.Exit != 0 || r.Lost == nil || *r.Lost || !reflect.DeepEqual(r.Execve, tr.Execve) {
		t.Errorf("Apply gave %+v", r)
	}
}

func TestTraceRemoveIsLost(t *testing.T) {
	tr := runTrace(t, "rm a.txt")
	if !tr.Outcome.Clean() || !tr.Lost() {
		t.Fatalf("outcome %+v verdict %+v, want clean and lost", tr.Outcome, tr.Verdict)
	}
	if !reflect.DeepEqual(tr.Execve, []string{"rm"}) {
		t.Errorf("Execve = %q, want [rm]", tr.Execve)
	}
	if !reflect.DeepEqual(tr.Verdict.Changed, []string{"a.txt"}) {
		t.Errorf("Changed = %q", tr.Verdict.Changed)
	}
}

func TestTraceListIsNotLost(t *testing.T) {
	tr := runTrace(t, "ls")
	if !tr.Outcome.Clean() || tr.Lost() {
		t.Fatalf("outcome %+v verdict %+v, want clean and not lost", tr.Outcome, tr.Verdict)
	}
	if !reflect.DeepEqual(tr.Execve, []string{"ls"}) {
		t.Errorf("Execve = %q, want [ls]", tr.Execve)
	}
}

func TestTraceFailedCommandStillObserved(t *testing.T) {
	tr := runTrace(t, "rm b.txt && false")
	var r Record
	tr.Apply(&r)
	if r.Exit == nil || *r.Exit != 1 || !tr.Lost() {
		t.Errorf("exit %v lost %v, want 1 and true", r.Exit, tr.Lost())
	}
}

func TestTraceStaysInSandbox(t *testing.T) {
	tr := runTrace(t, "touch /etc/probe-escape-test || exit 3")
	if tr.Outcome.ExitCode != 3 {
		t.Errorf("writing outside the task folder exited %d, want 3 (read-only)", tr.Outcome.ExitCode)
	}
}

// traceOutput runs cmd under Trace with its output redirected into the task
// folder and returns what it printed.
func traceOutput(t *testing.T, cmd string) string {
	t.Helper()
	bw, st := traceTools(t)
	root := t.TempDir()
	tr, err := Trace(context.Background(), bw, st, root, "{ "+cmd+"\n} > .out 2>&1", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".out"))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Outcome.Clean() {
		t.Fatalf("%s: outcome %+v, output %q", cmd, tr.Outcome, b)
	}
	return string(b)
}

// operator is what would identify the operator or the machine if it reached a
// model provider, read at test time.
func operator(t *testing.T) (name, host string) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if host, err = os.Hostname(); err != nil {
		t.Fatal(err)
	}
	return u.Username, host
}

// names reports whether out contains word as a whole word, not inside a
// longer one: an operator named v is not leaked by /dev or nologin, but is by
// /home/v or USER=v. A word is bounded by anything but a letter or a digit,
// so v-backup or mba15.local still count, which keeps the check strict.
func names(out, word string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9])`).MatchString(out)
}

func TestTraceHidesHomes(t *testing.T) {
	if out := strings.Fields(traceOutput(t, "ls -A /home")); !reflect.DeepEqual(out, []string{oracle.HiddenUser}) {
		t.Errorf("ls /home = %q, want only the sandbox user", out)
	}
	if out := traceOutput(t, "find /root /mnt /media /srv /var/tmp /tmp /run -mindepth 1 ! -path '/run/probe-trace*'"); strings.TrimSpace(out) != "" {
		t.Errorf("hidden directories list %q", out)
	}
}

func TestTraceHidesIdentity(t *testing.T) {
	name, host := operator(t)
	t.Setenv("PROBE_CANARY", "probe-canary-value")
	out := traceOutput(t, "cat /etc/passwd /etc/group /etc/hostname /etc/hosts /etc/machine-id 2>/dev/null; hostname; id; env; pwd")
	for _, leak := range []string{name, host, "probe-canary-value"} {
		if names(out, leak) {
			t.Errorf("sandbox output names %q:\n%s", leak, out)
		}
	}
	if got := strings.TrimSpace(traceOutput(t, "hostname")); got != oracle.HiddenHostname {
		t.Errorf("hostname = %q, want %q", got, oracle.HiddenHostname)
	}
	if got := strings.TrimSpace(traceOutput(t, "id -un")); got != oracle.HiddenUser {
		t.Errorf("id -un = %q, want %q", got, oracle.HiddenUser)
	}
}

func TestTraceHomeIsInTaskFolder(t *testing.T) {
	got := strings.Fields(traceOutput(t, `echo "$HOME"; pwd; cd ~ && pwd`))
	want := []string{oracle.HiddenRoot + "/home", oracle.HiddenRoot, oracle.HiddenRoot + "/home"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HOME, pwd, ~ = %q, want %q", got, want)
	}
}

func TestTraceTmpIsPrivate(t *testing.T) {
	name := "probe-private-tmp-" + strconv.Itoa(os.Getpid())
	host := filepath.Join("/tmp", name)
	t.Cleanup(func() { os.Remove(host) })
	if got := strings.TrimSpace(traceOutput(t, "echo inside > /tmp/"+name+" && cat /tmp/"+name)); got != "inside" {
		t.Fatalf("writing /tmp in the sandbox gave %q", got)
	}
	if _, err := os.Lstat(host); err == nil {
		t.Errorf("%s reached the host", host)
	}
}

func TestTraceMountinfoNamesNoOperator(t *testing.T) {
	name, _ := operator(t)
	if names(os.TempDir(), name) {
		t.Skipf("TMPDIR %s names the operator, and root's host path shows in mountinfo", os.TempDir())
	}
	if out := traceOutput(t, "cat /proc/self/mountinfo"); names(out, name) {
		t.Errorf("mountinfo names the operator:\n%s", out)
	}
}

// names must find a short name wherever it stands as a word and nowhere it is
// only part of one, or the identity tests either pass a leak or fail on any
// operator whose name is a common substring.
func TestNamesMatchesWholeWords(t *testing.T) {
	for _, c := range []struct {
		out, word string
		want      bool
	}{
		{"/dev/sda1 nologin devpts", "v", false},
		{"/home/v/task", "v", true},
		{"USER=v", "v", true},
		{"uid=1000(v) gid=1000(v)", "v", true},
		{"v", "v", true},
		{"/home/v-backup", "v", true},
		{"mba15.local", "mba15", true},
		{"mba150", "mba15", false},
		{"axb", "a.b", false},
		{"host a.b here", "a.b", true},
		{"", "v", false},
	} {
		if got := names(c.out, c.word); got != c.want {
			t.Errorf("names(%q, %q) = %v, want %v", c.out, c.word, got, c.want)
		}
	}
}
