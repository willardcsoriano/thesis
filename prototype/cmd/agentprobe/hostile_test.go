package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
)

// readStatus reads OUT/status.jsonl, failing on any line that is not whole.
func readStatus(t *testing.T, out string) []Status {
	t.Helper()
	var st []Status
	for _, line := range jsonLines(t, filepath.Join(out, "status.jsonl")) {
		var s Status
		if err := json.Unmarshal(line, &s); err != nil {
			t.Fatalf("status line %q: %v", line, err)
		}
		st = append(st, s)
	}
	return st
}

// jsonLines returns the lines of a JSONL file and fails unless every one is
// a whole JSON value and the file ends with a newline.
func jsonLines(t *testing.T, p string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		t.Errorf("%s ends mid-line", p)
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<26)
	for sc.Scan() {
		if !json.Valid(sc.Bytes()) {
			t.Errorf("%s holds a line that is not JSON: %.80q", p, sc.Text())
		}
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	return out
}

// A run id is a folder name under -out; one that is a path is refused.
func TestRunIDCannotLeaveTheOutput(t *testing.T) {
	out := t.TempDir()
	for _, id := range []string{"../escape", "a/b", "..", "/abs", ".hidden"} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"claude", "-tasks", suite, "-out", filepath.Join(out, "runs"), "-run-id", id, "-dry-run"},
			env(nil), &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "run id") {
			t.Errorf("run id %q: exit %d: %s", id, code, stderr.String())
		}
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("something was created: %v", ents)
	}
}

// fakeRunner is the real claudecode runner, in the real sandbox, with a
// script for the CLI.
func fakeRunner(t *testing.T, out, script string) claudecode.Runner {
	t.Helper()
	bw := oracle.Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	pl := claudecode.Prlimit()
	if pl == "" {
		t.Skip("prlimit unavailable")
	}
	pkg := t.TempDir()
	os.MkdirAll(filepath.Join(pkg, "bin"), 0o755)
	if err := os.WriteFile(filepath.Join(pkg, "bin", "claude"), []byte("#!/bin/bash\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return claudecode.Runner{Sandbox: claudecode.Sandbox{Bwrap: bw, Prlimit: pl, Install: claudecode.Install{PackageDir: pkg, Exe: "bin/claude"}},
		Token: "sk-ant-oat01-TEST-NOT-A-REAL-TOKEN", TurnTimeout: time.Minute, RunID: "test", Out: out}
}

// The operator presses Ctrl-C while the second task runs. The run stops
// after recording it; events.jsonl stays whole JSON lines, and status.jsonl
// says the first task completed and the second was interrupted, and that
// nothing else ran.
func TestInterruptMidRunSaysWhatCompleted(t *testing.T) {
	tasks, err := probe.LoadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "run")
	r := fakeRunner(t, out, `
echo '{"type":"system","subtype":"init","session_id":"s","model":"m","claude_code_version":"0","permissionMode":"bypassPermissions"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"ls"}}]}}'
case "$*" in *slow*) sleep 300;; esac
echo '{"type":"result","subtype":"success","is_error":false,"num_turns":1}'
`)
	fast := tasks[0]
	fast.ID, fast.FollowUps = "fast", nil
	slow := fast
	slow.ID = "slow"
	slow.Prompts = map[string]string{"technical": "slow please"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	runTask := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		calls++
		if t.ID == "slow" {
			time.AfterFunc(time.Second, cancel)
		}
		return r.RunTask(ctx, t, tier)
	}
	var stderr bytes.Buffer
	start := time.Now()
	code := runTasks(ctx, runTask, out, []probe.Task{fast, slow, fast}, []string{"technical"}, nil, &stderr)
	if code != 1 || calls != 2 {
		t.Errorf("exit %d after %d task runs, want 1 after 2: %s", code, calls, stderr.String())
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("the interrupt took %s to end the run", d)
	}
	if n := len(jsonLines(t, filepath.Join(out, "events.jsonl"))); n != 2 {
		t.Errorf("events: %d records, want 2", n)
	}
	st := readStatus(t, out)
	if len(st) != 2 || st[0].Task != "fast" || st[0].Status != "complete" || st[1].Task != "slow" || st[1].Status != "interrupted" || st[1].Records != 1 {
		t.Errorf("status = %+v", st)
	}
	if !strings.Contains(stderr.String(), "status.jsonl") {
		t.Errorf("the interrupt message does not say where to look: %s", stderr.String())
	}
}

// Incomplete and failed task runs are told apart from complete ones.
func TestStatusNamesEveryOutcome(t *testing.T) {
	tasks, err := probe.LoadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []error{nil, fmt.Errorf("x: %w", probe.ErrIncomplete), errors.New("boom"), fmt.Errorf("y: %w", claudecode.ErrFatal)}
	i := 0
	runTask := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		err := outcomes[i]
		i++
		return []probe.Record{{Task: t.ID, Tier: tier, Kind: probe.KindBash, Command: "ls"}}, err
	}
	out := t.TempDir()
	var stderr bytes.Buffer
	stop := func(err error) bool { return errors.Is(err, claudecode.ErrFatal) }
	code := runTasks(context.Background(), runTask, out, []probe.Task{tasks[0], tasks[0], tasks[0], tasks[0], tasks[0]}, []string{"technical"}, stop, &stderr)
	if code != 1 || i != 4 {
		t.Fatalf("exit %d after %d runs", code, i)
	}
	var got []string
	for _, s := range readStatus(t, out) {
		got = append(got, s.Status)
	}
	if strings.Join(got, ",") != "complete,incomplete,error,fatal" {
		t.Errorf("statuses = %v", got)
	}
}

// An output folder that cannot be written ends the run at once with a
// message, and a raw folder that cannot be created stops it after the first
// task instead of failing every task the same way.
func TestUnwritableOutput(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores permissions")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0o755)
	var stderr bytes.Buffer
	never := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		panic("ran a task without anywhere to record it")
	}
	if code := runTasks(context.Background(), never, filepath.Join(parent, "run"), []probe.Task{{ID: "a"}}, []string{"technical"}, nil, &stderr); code != 1 || !strings.Contains(stderr.String(), "permission denied") {
		t.Errorf("exit %d: %s", code, stderr.String())
	}

	tasks, err := probe.LoadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "run")
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(out, "raw"), nil, 0o644) // a file where the raw folder goes
	r := fakeRunner(t, out, "exit 0\n")
	calls := 0
	runTask := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		calls++
		return r.RunTask(ctx, t, tier)
	}
	stderr.Reset()
	stop := func(err error) bool { return errors.Is(err, claudecode.ErrFatal) }
	if code := runTasks(context.Background(), runTask, out, tasks, []string{"technical", "novice"}, stop, &stderr); code != 1 || calls != 1 {
		t.Errorf("exit %d after %d task runs, want 1 after 1: %s", code, calls, stderr.String())
	}
}

// A full disk: events.jsonl is written on a 64 KiB tmpfs until it fills.
// The run stops with an error and the file is still whole JSON lines, not
// one cut mid-record. The test re-runs itself inside bubblewrap, which can
// mount a size-capped tmpfs without root.
func TestFullDiskKeepsEventsWhole(t *testing.T) {
	if dir := os.Getenv("AGENTPROBE_FULL_DISK"); dir != "" {
		fullDiskHelper(t, dir)
		return
	}
	bw := oracle.Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	dir := t.TempDir()
	cmd := exec.Command(bw, "--dev-bind", "/", "/", "--size", "65536", "--tmpfs", dir, "--unshare-user", "--die-with-parent", "--",
		os.Args[0], "-test.run=^TestFullDiskKeepsEventsWhole$", "-test.v")
	cmd.Env = append(os.Environ(), "AGENTPROBE_FULL_DISK="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "FULL-DISK-CHECKED") {
		t.Errorf("full-disk run: %v\n%s", err, out)
	}
}

func fullDiskHelper(t *testing.T, dir string) {
	big := strings.Repeat("x", 5000)
	n := 0
	runTask := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		n++
		return []probe.Record{{Task: t.ID, Tier: tier, Kind: probe.KindBash, Command: big}, {Task: t.ID, Tier: tier, Kind: probe.KindBash, Command: big}}, nil
	}
	tasks := make([]probe.Task, 100)
	for i := range tasks {
		tasks[i] = probe.Task{ID: fmt.Sprintf("t%d", i)}
	}
	var stderr bytes.Buffer
	out := filepath.Join(dir, "run")
	code := runTasks(context.Background(), runTask, out, tasks, []string{"technical"}, nil, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "no space left") {
		t.Fatalf("exit %d after %d tasks: %s", code, n, stderr.String())
	}
	if n >= len(tasks) {
		t.Fatal("the disk never filled")
	}
	recs := jsonLines(t, filepath.Join(out, "events.jsonl"))
	if len(recs) == 0 {
		t.Fatal("nothing was recorded before the disk filled")
	}
	if !t.Failed() {
		fmt.Println("FULL-DISK-CHECKED", len(recs), "records")
	}
}
