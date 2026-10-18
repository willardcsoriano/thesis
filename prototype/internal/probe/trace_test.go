package probe

import (
	"context"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
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
		t.Skip("tracing needs bubblewrap and strace")
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
	tr := runTrace(t, "touch /tmp/probe-escape-test || exit 3")
	if tr.Outcome.ExitCode != 3 {
		t.Errorf("writing outside the task folder exited %d, want 3 (read-only)", tr.Outcome.ExitCode)
	}
}
