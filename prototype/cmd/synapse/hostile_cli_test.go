package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

// The hostile-conditions suite (docs/cli-breaking.md, Part 1): the real
// program under conditions a script, a pipe, a busy machine or a hostile
// model creates, each checked against what the contract requires.

func journalEntries(t *testing.T, home string) []undo.Entry {
	t.Helper()
	f, err := os.Open(filepath.Join(home, ".synapse", "undo.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []undo.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<24)
	for sc.Scan() {
		var e undo.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("journal line is not an entry: %q", sc.Text())
		}
		out = append(out, e)
	}
	return out
}

// A closed stdout must not kill synapse between running a command and
// journaling its undo. Step 1 runs and prints with stdout open; stdout is then
// closed before the model answers for step 2, so losing stdout happens after a
// step really ran (an earlier version closed it at once, ran nothing, and
// passed on zero files and zero entries; volkswagen audit, finding 6).
func TestStdoutClosedEarlyNeverLeavesAStepUnjournaled(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	release := make(chan struct{})
	var steps atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "fake answer", Done: true})
			return
		}
		d := stepDecision{Action: "done"}
		switch steps.Add(1) {
		case 1:
			d = runStep("touch a.txt")
		case 2:
			<-release // step 2 is chosen only after stdout is gone
			d = runStep("touch b.txt")
		}
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: string(raw), Done: true})
	}))
	defer srv.Close()

	cmd := exec.Command(os.Args[0], "run", "make two files")
	cmd.Env = append(os.Environ(), runMainEnv+"=1", "SYNAPSE_OLLAMA="+srv.URL, "HOME="+home)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "exit code:") {
			break // step 1 has run
		}
	}
	stdout.Close()
	close(release)
	err = cmd.Wait()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	if !fileExists(filepath.Join(dir, "a.txt")) {
		t.Fatal("setup: step 1 did not run")
	}
	if fileExists(filepath.Join(dir, "b.txt")) {
		t.Error("step 2 ran after stdout was gone")
	}
	journaled := false
	for _, e := range journalEntries(t, home) {
		for _, c := range e.Created {
			journaled = journaled || strings.HasSuffix(c, "a.txt")
		}
	}
	if !journaled {
		t.Error("step 1 ran but was not journaled")
	}
	if code != exitInterrupted {
		t.Errorf("exit %d, want %d; stderr:\n%s", code, exitInterrupted, errb.String())
	}
}

// Catching SIGPIPE in synapse must not change the commands it runs: a child
// still gets the default SIGPIPE, so `yes | head -1` ends cleanly instead of
// yes failing with "Broken pipe".
func TestCommandsStillGetTheDefaultSIGPIPE(t *testing.T) {
	m := newFakeModel(t, runStep("yes | head -1"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "say yes once"}, env: m.env()})
	for _, rec := range readJSONLines(t, r.stdout) {
		if rec.Type == "result" {
			if rec.ExitCode == nil || *rec.ExitCode != 0 || strings.Contains(rec.Stderr, "Broken pipe") {
				t.Fatalf("yes | head -1 under synapse: exit %v, stderr %q", rec.ExitCode, rec.Stderr)
			}
			return
		}
	}
	t.Fatal("no result record")
}

// A command that prints hundreds of megabytes must not make synapse hold
// them all in memory.
func TestHugeCommandOutputIsBounded(t *testing.T) {
	m := newFakeModel(t, runStep("head -c 300000000 /dev/zero | tr '\\0' a"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "print a lot"}, env: m.env()})
	if r.code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", r.code, r.stderr)
	}
	if r.maxRSSKiB > 150*1024 {
		t.Errorf("synapse peaked at %d MiB for a 300 MB output", r.maxRSSKiB/1024)
	}
	recs := readJSONLines(t, r.stdout)
	var saw bool
	for _, rec := range recs {
		if rec.Type == "result" {
			saw = true
			if !strings.Contains(rec.Stdout, "bytes not shown") {
				t.Errorf("a truncated result does not say so (%d bytes kept)", len(rec.Stdout))
			}
		}
	}
	if !saw {
		t.Fatal("no result record")
	}
}

// Text mode must not pass a command's terminal control sequences to the
// screen, where they can clear it, retitle the window, or forge a prompt.
// --json keeps the raw bytes, escaped.
func TestTextModeNeutralisesTerminalEscapes(t *testing.T) {
	const cmd = `printf '\033]0;owned\007\033[2Jvisible\n'`
	m := newFakeModel(t, runStep(cmd))
	r := execSynapse(t, synapseRun{args: []string{"run", "print it"}, env: m.env()})
	if strings.ContainsAny(r.stdout+r.stderr, "\x1b\x07") {
		t.Errorf("terminal control bytes reached the terminal: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "visible") {
		t.Errorf("the printable output was lost: %q", r.stdout)
	}
	m2 := newFakeModel(t, runStep(cmd))
	j := execSynapse(t, synapseRun{args: []string{"run", "--json", "print it"}, env: m2.env()})
	if !strings.Contains(j.stdout, `\u001b`) {
		t.Errorf("--json did not keep the raw escape: %q", j.stdout)
	}
}

func TestConcurrentRunsShareTheJournalSafely(t *testing.T) {
	home := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := newFakeModel(t, runStep(fmt.Sprintf("touch f%d.txt", i)))
			r := execSynapse(t, synapseRun{args: []string{"run", "make a file"}, env: m.env(), home: home})
			if r.code != exitOK {
				t.Errorf("run %d exit %d: %s", i, r.code, r.stderr)
			}
		}(i)
	}
	wg.Wait()
	if got := len(journalEntries(t, home)); got != n {
		t.Fatalf("%d concurrent runs left %d journal entries", n, got)
	}
}

// Concurrent undos must never apply one entry twice or drop one unapplied:
// afterwards, the files still present are exactly those whose entries remain.
func TestConcurrentUndosNeverApplyOrLoseAnEntryTwice(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	const n = 6
	for i := 0; i < n; i++ {
		m := newFakeModel(t, runStep(fmt.Sprintf("touch u%d.txt", i)))
		if r := execSynapse(t, synapseRun{args: []string{"run", "make a file"}, env: m.env(), dir: dir, home: home}); r.code != exitOK {
			t.Fatalf("setup run %d: exit %d %s", i, r.code, r.stderr)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			execSynapse(t, synapseRun{args: []string{"undo"}, home: home, stdin: "y\n"})
		}()
	}
	wg.Wait()
	remaining := map[string]bool{}
	for _, e := range journalEntries(t, home) {
		for _, c := range e.Created {
			remaining[filepath.Base(c)] = true
		}
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("u%d.txt", i)
		if present := fileExists(filepath.Join(dir, name)); present != remaining[name] {
			t.Errorf("%s: present=%v but its journal entry remaining=%v", name, present, remaining[name])
		}
	}
}

// One unreadable journal line, such as an append cut off by a crash, must
// not disable every undo.
func TestCorruptJournalLineDoesNotDisableUndo(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	m := newFakeModel(t, runStep("touch x.txt"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "make x"}, env: m.env(), dir: dir, home: home}); r.code != exitOK {
		t.Fatalf("setup: exit %d %s", r.code, r.stderr)
	}
	jp := filepath.Join(home, ".synapse", "undo.log")
	good, err := os.ReadFile(jp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jp, append([]byte("{\"garbage\n"), good...), 0o644); err != nil {
		t.Fatal(err)
	}
	r := execSynapse(t, synapseRun{args: []string{"undo"}, home: home, stdin: "y\n"})
	if r.code != exitOK || fileExists(filepath.Join(dir, "x.txt")) {
		t.Fatalf("undo exit %d, x.txt exists=%v; stderr:\n%s", r.code, fileExists(filepath.Join(dir, "x.txt")), r.stderr)
	}
	if !strings.Contains(r.stderr, "unreadable") {
		t.Errorf("the skipped line was not reported: %q", r.stderr)
	}
}

// A read-only home means nothing can be journaled; synapse must say so.
func TestUnwritableHomeIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(home, 0o700) })
	m := newFakeModel(t, runStep("touch y.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "make y"}, env: m.env(), home: home})
	// Without undo, a change asks first (finding r2-atk8-08); stdin is
	// closed, so it is declined.
	if r.code != exitDeclined || !strings.Contains(r.stderr, "undoable") || !strings.Contains(r.stderr, "Undo is not available") {
		t.Fatalf("exit %d; want %d, a warning that the run is not undoable, and the plain reason; stderr:\n%s", r.code, exitDeclined, r.stderr)
	}
}

// rawModel answers every step request with the given handler; the answer
// prompt and /api/tags are served normally.
func rawModel(t *testing.T, step func(w http.ResponseWriter)) []string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		var body struct{ System string }
		json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.System, "You are reporting the outcome of a task") {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "fake answer", Done: true})
			return
		}
		step(w)
	}))
	t.Cleanup(srv.Close)
	return []string{"SYNAPSE_OLLAMA=" + srv.URL}
}

func reply(s string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: s, Done: true, EvalCount: 1})
	}
}

// Whatever the model replies, synapse ends with a defined exit code, a valid
// --json stream, and nothing deleted that nobody approved.
func TestHostileModelReplies(t *testing.T) {
	hijack := func(w http.ResponseWriter) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}
	cases := map[string]func(http.ResponseWriter){
		"empty reply":            reply(""),
		"not JSON":               reply("{{{ not json"),
		"run with no command":    reply(`{"action":"run","command":""}`),
		"unknown action":         reply(`{"action":"rm -rf keep.txt","command":""}`),
		"NUL in command":         reply(`{"action":"run","command":"echo a\u0000rm keep.txt"}`),
		"two lines, second rm":   reply(`{"action":"run","command":"echo ok\nrm -f keep.txt"}`),
		"huge command":           reply(`{"action":"run","command":"echo ` + strings.Repeat("a", 2<<20) + `"}`),
		"HTTP 500":               func(w http.ResponseWriter) { http.Error(w, "boom", 500) },
		"empty body":             func(w http.ResponseWriter) {},
		"connection dropped":     hijack,
		"reply that is an array": reply(`[1,2,3]`),
	}
	defined := map[int]bool{exitOK: true, exitFailed: true, exitDeclined: true, exitUnsupported: true, exitStepLimit: true, exitModelUnavailable: true}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			keep := filepath.Join(dir, "keep.txt")
			writeFile(t, keep, "data")
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", "do something"}, env: rawModel(t, h), dir: dir})
			if !defined[r.code] {
				t.Fatalf("exit %d is not a defined outcome; stderr:\n%.2000s", r.code, r.stderr)
			}
			recs := readJSONLines(t, r.stdout)
			if end := recs[len(recs)-1]; end.Type != "end" || end.ExitCode == nil || *end.ExitCode != r.code {
				t.Errorf("stream does not end with the process's exit code: %+v", end)
			}
			if !fileExists(keep) {
				t.Errorf("keep.txt was deleted without approval")
			}
		})
	}
}

// A panic inside the analysis fails closed: the command asks, and synapse
// does not crash.
func TestAnalysisPanicFailsClosed(t *testing.T) {
	orig := decide
	defer func() { decide = orig }()
	decide = func(context.Context, *effects.Analyzer, string, gate.Policy) gate.Decision {
		panic("injected analysis failure")
	}
	t.Setenv("SYNAPSE_ANALYSIS", "strict")
	server := scriptedOllamaServer(t, []string{"ls", "DONE"})
	defer server.Close()
	var asked bool
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "list files",
		func(string) bool { asked = true; return false }, &out, &errOut, "", withoutAnswer())
	if !asked || code != exitDeclined {
		t.Fatalf("asked=%v exit=%d; want the command to ask and be declined; out:\n%s", asked, code, out.String())
	}
}

// SIGTERM and SIGHUP stop the task like Ctrl+C: the change the running
// command already made is journaled, the stream ends with its end record, and
// the exit code is 128 plus the signal (finding atk4-03).
func TestTermAndHangupJournalWhatRanAndExitCleanly(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			m := newFakeModel(t, runStep("touch made.txt && sleep 30"))
			r := execSynapse(t, synapseRun{
				args: []string{"run", "--json", "make a file and wait"}, env: m.env(), dir: dir, home: home,
				started: func(p *os.Process) {
					go func() {
						time.Sleep(1500 * time.Millisecond)
						p.Signal(sig)
					}()
				},
			})
			if r.code != 128+int(sig) {
				t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, 128+int(sig), r.stderr)
			}
			recs := readJSONLines(t, r.stdout)
			if end := recs[len(recs)-1]; end.Type != "end" || end.Outcome != outcomeInterrupted {
				t.Errorf("last record %+v, want end/interrupted", end)
			}
			if !fileExists(filepath.Join(dir, "made.txt")) {
				t.Fatal("setup: the command did not get as far as creating made.txt")
			}
			journaled := false
			for _, e := range journalEntries(t, home) {
				for _, c := range e.Created {
					journaled = journaled || strings.HasSuffix(c, "made.txt")
				}
			}
			if !journaled {
				t.Error("made.txt was created but not journaled, so undo cannot remove it")
			}
		})
	}
}

// stdin from /dev/zero: bounded memory, and the confirmation declines.
func TestEndlessStdinIsBoundedAndDeclines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "x")
	m := newFakeModel(t, runStep("rm keep.txt"))
	cmd := []string{"run", "--json", "delete keep.txt"}
	f, err := os.Open("/dev/zero")
	if err != nil {
		t.Skip("no /dev/zero")
	}
	f.Close()
	r := execSynapse(t, synapseRun{args: cmd, env: m.env(), dir: dir, stdinFile: "/dev/zero"})
	if r.code != exitDeclined || !fileExists(filepath.Join(dir, "keep.txt")) {
		t.Fatalf("exit %d, keep.txt exists=%v; want declined and kept; stderr:\n%.1000s", r.code, fileExists(filepath.Join(dir, "keep.txt")), r.stderr)
	}
	if r.maxRSSKiB > 100*1024 {
		t.Errorf("peak memory %d MiB on endless stdin", r.maxRSSKiB/1024)
	}
}

// A command that failed, followed by the model giving up, is a failure (exit
// 1), not "this has no command-line equivalent" (exit 4; finding atk1-01).
func TestGivingUpAfterAFailedCommandIsAFailure(t *testing.T) {
	m := newFakeModel(t, runStep("cp nothere.txt documents/"), stepDecision{Action: "unsupported"})
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "copy my notes"}, env: m.env()})
	if r.code != exitFailed {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitFailed, r.stderr)
	}
	recs := readJSONLines(t, r.stdout)
	if end := recs[len(recs)-1]; end.Outcome != outcomeGaveUp {
		t.Errorf("outcome %q, want %q", end.Outcome, outcomeGaveUp)
	}
	if strings.Contains(r.stdout, "no command-line equivalent") {
		t.Error("told the person the request is impossible after a command merely failed")
	}
}

// A working folder that no longer exists stops the task before the model is
// asked anything (finding atk4-07).
func TestDeletedWorkingFolderIsReported(t *testing.T) {
	base := t.TempDir()
	gone := filepath.Join(base, "gone")
	os.Mkdir(gone, 0o755)
	chdir(t, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	server := scriptedOllamaServer(t, nil) // any model call fails the test
	defer server.Close()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "list the files", neverConfirm(t), &out, &errOut, "")
	if code != exitFailed || !strings.Contains(errOut.String(), "no longer exists") {
		t.Fatalf("exit %d; stderr:\n%s", code, errOut.String())
	}
}

// Tasks piped into repl: a confirmation declines without consuming the next
// line, so that task still runs (findings atk1-04, atk4-12), and stdout holds
// results only (atk4-11).
func TestPipedREPLNeverSwallowsTheNextTask(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "x")
	m := newFakeModel(t, runStep("rm keep.txt"), runStep("touch b.txt"))
	r := execSynapse(t, synapseRun{args: []string{"repl"}, env: m.env(), dir: dir, stdin: "delete keep.txt\ncreate b.txt\n"})
	if !fileExists(filepath.Join(dir, "keep.txt")) {
		t.Error("keep.txt was deleted although nobody could approve")
	}
	if !fileExists(filepath.Join(dir, "b.txt")) {
		t.Errorf("the task after the confirmation was swallowed; stderr:\n%s", r.stderr)
	}
	for _, chrome := range []string{"[y/N]", "Persistent session", "> "} {
		if strings.Contains(r.stdout, chrome) {
			t.Errorf("stdout carries session chrome %q:\n%s", chrome, r.stdout)
		}
	}
}

// Results that cannot be written are a failure, not a silent success
// (finding atk4-10).
func TestUnwritableStdoutIsAFailure(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full")
	}
	m := newFakeModel(t, runStep("echo result"))
	cmd := exec.Command(os.Args[0], "run", "say something")
	cmd.Env = append(os.Environ(), runMainEnv+"=1", "SYNAPSE_OLLAMA="+m.srv.URL, "HOME="+t.TempDir())
	full, _ := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	defer full.Close()
	cmd.Stdout = full
	var errb strings.Builder
	cmd.Stderr = &errb
	cmd.Dir = t.TempDir()
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != exitFailed {
		t.Fatalf("run with stdout on /dev/full: %v, want exit %d; stderr:\n%s", err, exitFailed, errb.String())
	}
	if !strings.Contains(errb.String(), "could not write the results") {
		t.Errorf("no diagnostic on stderr:\n%s", errb.String())
	}
}

// A reply the token cap cut off is never parsed or run (finding atk2-06): it
// ends the task as a model failure.
func TestACutOffModelReplyIsNeverRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "x")
	env := rawModel(t, func(w http.ResponseWriter) {
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: `{"action":"run","command":"rm -f keep.txt; rm -f ke`, Done: true, DoneReason: "length"})
	})
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "tidy up"}, env: env, dir: dir, stdin: "y\n"})
	if r.code != exitModelUnavailable || !fileExists(filepath.Join(dir, "keep.txt")) {
		t.Fatalf("exit %d, keep.txt exists=%v; want %d and nothing run", r.code, fileExists(filepath.Join(dir, "keep.txt")), exitModelUnavailable)
	}
	if !strings.Contains(r.stdout, "cut off") {
		t.Errorf("the stream does not say the reply was cut off:\n%s", r.stdout)
	}
}

// "undo" typed in a session runs synapse's undo, not a model request.
func TestUndoTypedInASessionRunsUndo(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	m := newFakeModel(t, runStep("touch made.txt"))
	execSynapse(t, synapseRun{args: []string{"run", "make a file"}, env: m.env(), dir: dir, home: home})
	if !fileExists(filepath.Join(dir, "made.txt")) {
		t.Fatal("setup: made.txt not created")
	}
	m2 := newFakeModel(t) // a model request would show as a step
	r := execSynapse(t, synapseRun{args: []string{"repl"}, env: m2.env(), dir: dir, home: home, stdin: "Undo that!\n"})
	if m2.steps.Load() != 0 {
		t.Errorf("\"undo that\" was sent to the model")
	}
	if !strings.Contains(r.stdout, "Undoing: touch made.txt") {
		t.Errorf("undo did not run; stdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
}

func TestIsUndoRequest(t *testing.T) {
	for in, want := range map[string]bool{"undo": true, "Undo that!": true, "  undo   it ": true, "oops, undo that": true,
		"undo the move of report.txt": false, "can you undo everything from yesterday": false, "": false} {
		if got := isUndoRequest(in); got != want {
			t.Errorf("isUndoRequest(%q) = %v, want %v", in, got, want)
		}
	}
}

// The give-up notice points at the failed step's error, even when a later
// step succeeded quietly (Layer 7 log, 2026-10-25: pdftk not found, then ls).
func TestGiveUpNoticeFindsTheFailedStepsError(t *testing.T) {
	m := newFakeModel(t, runStep("definitely-not-installed-tool a b"), runStep("ls"), stepDecision{Action: "unsupported"})
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "merge the files"}, env: m.env(), stdin: "y\n"})
	if strings.Contains(r.stdout, "gave no error message") {
		t.Errorf("the notice says there was no error message, but the first step failed with one:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "Its error message is above") {
		t.Errorf("the notice does not point at the error:\n%s", r.stdout)
	}
}
