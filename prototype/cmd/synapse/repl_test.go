package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"synapseos/internal/ollama"
)

// TestRunREPLProcessesMultipleTasksInOneProcess verifies M4's core claim:
// several distinct tasks run through runLoop in a single runREPL call,
// without the process restarting between them.
func TestRunREPLProcessesMultipleTasksInOneProcess(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", a),
		"DONE",
		fmt.Sprintf("touch %q", b),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("create a.txt\ncreate b.txt\n")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(a); err != nil {
		t.Errorf("expected first task's effect (%s) to exist: %v", a, err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Errorf("expected second task's effect (%s) to exist: %v", b, err)
	}
}

// TestRunREPLExitsCleanlyOnExitCommand verifies an explicit "exit" line ends
// the session immediately, without running any task that comes after it —
// scriptedOllamaServer's own t.Fatalf-on-overcall guards this: if the loop
// kept going, it would call the model a second time with no response
// scripted for it.
func TestRunREPLExitsCleanlyOnExitCommand(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", target),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("create a.txt\nexit\nthis line must never be reached\n")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected the task before exit to have run: %v", err)
	}
}

// TestRunREPLExitsCleanlyOnEOF verifies Ctrl+D (an immediate EOF with no
// "exit" line typed) ends the session cleanly rather than hanging or
// erroring — the primary way a real terminal user leaves the session.
func TestRunREPLExitsCleanlyOnEOF(t *testing.T) {
	server := scriptedOllamaServer(t, nil)
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
}

// TestRunREPLRunsFinalTaskEvenWithoutTrailingNewlineBeforeEOF verifies a
// task on the last line of input still runs even when that line has no
// trailing newline (ReadString returns the partial line together with
// io.EOF in that case) — a real terminal session ending with Ctrl+D right
// after typing a task, with no final Enter, must still process it.
func TestRunREPLRunsFinalTaskEvenWithoutTrailingNewlineBeforeEOF(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", target),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("create a.txt") // no trailing \n

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected the final, newline-less task to have run: %v", err)
	}
}

// TestRunREPLContinuesSessionAfterAFailedTask verifies a task that fails
// (here: the model reports UNSUPPORTED, which makes runLoop return a
// nonzero code) does not end the session — the next task in the input must
// still be attempted, since forcing a restart to try another task would
// defeat the entire point of a persistent loop.
func TestRunREPLContinuesSessionAfterAFailedTask(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "b.txt")

	server := scriptedOllamaServer(t, []string{
		"UNSUPPORTED",
		fmt.Sprintf("touch %q", target),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("do something impossible\ncreate b.txt\n")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected the task after the failed one to still run: %v", err)
	}
}

// TestRunREPLSharesOneReaderBetweenTasksAndConfirmations is the test that
// actually exercises M4's stated risk: "no leaked confirmation state,
// state carried correctly between iterations." The scripted input
// interleaves an irreversible task, its "y" confirmation answer, and a
// second task, all as separate lines of the same stream — exactly what a
// real user typing into one terminal session produces.
//
// A wrong implementation (e.g. one that opens a fresh bufio.Reader for
// confirm() each time, instead of reusing runREPL's single shared reader)
// would fail this test: bufio.Reader reads ahead into its own buffer on
// first use, so a throwaway confirm-only reader wrapping the same
// underlying stream would greedily consume the second task's line into a
// buffer that then gets discarded when that reader goes out of scope —
// silently losing the next task rather than misrouting it, which is why
// this asserts the second task's effect exists rather than asserting on
// what error occurred.
func TestRunREPLSharesOneReaderBetweenTasksAndConfirmations(t *testing.T) {
	dir := t.TempDir()
	doomed := filepath.Join(dir, "doomed.txt")
	if err := os.WriteFile(doomed, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	second := filepath.Join(dir, "second.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("rm %q", doomed),
		"DONE", // confirming the rm keeps task 1's bounded loop going until it self-reports done
		fmt.Sprintf("touch %q", second),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	// "y" answers the confirmation runLoop raises for the rm step; the
	// second task line follows immediately after, exactly as a user typing
	// into one continuous terminal session would produce it.
	in := strings.NewReader("delete doomed.txt\ny\ncreate second.txt\n")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(doomed); !os.IsNotExist(err) {
		t.Errorf("expected doomed.txt to have been removed after confirming, stat err = %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Errorf("expected the second task (typed right after the confirmation answer) to have run: %v", err)
	}
}

// TestRunREPLDeclinedConfirmationDoesNotLeakIntoNextTask mirrors the test
// above with "n" instead of "y": the declined confirmation must consume
// exactly its own line, leaving the following task line intact for the
// next iteration to read as a task, not as another stray confirmation
// answer.
func TestRunREPLDeclinedConfirmationDoesNotLeakIntoNextTask(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(kept, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	second := filepath.Join(dir, "second.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("rm %q", kept),
		fmt.Sprintf("touch %q", second),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("delete kept.txt\nn\ncreate second.txt\n")

	code := runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("expected kept.txt to survive a declined confirmation: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Errorf("expected the second task to still run as its own task: %v", err)
	}
}

// TestRunREPLPrintsPromptAndGreeting verifies the minimal, plain-text UX
// M4 scopes for (no bubbletea/styling): a startup line explaining how to
// leave, and a "> " prompt before each read.
func TestRunREPLPrintsPromptAndGreeting(t *testing.T) {
	server := scriptedOllamaServer(t, nil)
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("exit\n")

	runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)

	got := errOut.String() // the greeting and prompt are chrome: stderr (atk4-11)
	if !strings.Contains(got, "exit or quit") {
		t.Errorf("expected a greeting explaining how to leave, got:\n%s", got)
	}
	if !strings.Contains(got, "> ") {
		t.Errorf("expected a \"> \" prompt before reading input, got:\n%s", got)
	}
}

// TestRunREPLInterruptCancelsTaskButKeepsSessionAlive verifies the fix for
// the M4 gap found in Session 28's review: an interrupt during a running
// task must cancel that task alone and leave the session accepting further
// tasks, rather than tearing down the whole process.
//
// Real signals are not sent — notifyInterrupts is swapped for a stub that
// fires an interrupt into the task's own channel, the same indirection
// pattern internal/undo uses for os.Link, so the test stays deterministic
// and cannot kill the test runner.
func TestRunREPLInterruptCancelsTaskButKeepsSessionAlive(t *testing.T) {
	dir := t.TempDir()
	after := filepath.Join(dir, "after-interrupt.txt")

	// Task 1 proposes a command that would sleep far longer than the test
	// should ever run; the interrupt must kill it. Task 2 proves the
	// session survived and still works.
	server := scriptedOllamaServer(t, []string{
		"sleep 300",
		fmt.Sprintf("touch %q", after),
		"DONE",
	})
	defer server.Close()

	origNotify, origStop := notifyInterrupts, stopInterrupts
	defer func() { notifyInterrupts, stopInterrupts = origNotify, origStop }()

	var fired bool
	notifyInterrupts = func(c chan<- os.Signal) {
		// Interrupt only the first task, so the second runs normally.
		if fired {
			return
		}
		fired = true
		// Delay before firing: the interrupt has to land while the task's
		// *command* is running, not during the propose call that precedes
		// it. Firing immediately (the first version of this test) cancelled
		// the context before the HTTP propose even completed, which left the
		// scripted response queue misaligned and hung the run — a real bug
		// in the test, caught by the test itself. A local httptest round
		// trip is sub-millisecond, so 1s is an enormous margin, and the
		// command being cancelled sleeps for 300s, so there is no upper-
		// bound race either.
		go func() {
			time.Sleep(1 * time.Second)
			c <- os.Interrupt
		}()
	}
	stopInterrupts = func(c chan<- os.Signal) {}

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("sleep for a long time\ncreate after-interrupt.txt\n")

	done := make(chan int, 1)
	go func() {
		done <- runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runREPL did not return — the interrupt failed to cancel the sleeping task")
	}

	if !strings.Contains(out.String(), "Cancelling this task") {
		t.Errorf("expected the cancellation notice in output, got:\n%s", out.String())
	}
	if _, err := os.Stat(after); err != nil {
		t.Errorf("expected the task after the interrupt to still run (session must survive): %v", err)
	}
}

// --- session memory in the REPL (M6 step 4) --------------------------

// TestRunREPLCarriesContextAcrossTasks is M6's headline claim at the
// session level: the second task's prompt must contain the first task's
// subject, or a follow-up pronoun has nothing to bind to.
func TestRunREPLCarriesContextAcrossTasks(t *testing.T) {
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body["prompt"].(string))
		// Step 1 of task 1 runs a command; everything after is DONE.
		if len(prompts) == 1 {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "echo made-report", Done: true})
			return
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "DONE", Done: true})
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	in := strings.NewReader("create the report\nmove it to Downloads\n")
	runREPL(context.Background(), ollama.New(server.URL), "m", sessionJournal(t), in, &out, &errOut)

	last := prompts[len(prompts)-1]
	if !strings.Contains(last, "create the report") {
		t.Errorf("second task's prompt lost the first task, so 'it' cannot resolve:\n%s", last)
	}
}

// TestRunREPLContextCommandReportsMemory verifies the user can inspect
// what "it" would bind to — the agreement check that has to happen before
// issuing a destructive follow-up.
func TestRunREPLContextCommandReportsMemory(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"echo hi", "DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	in := strings.NewReader("say hi\ncontext\n")
	runREPL(context.Background(), ollama.New(server.URL), "m", sessionJournal(t), in, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, "remembering 1 task(s)") {
		t.Errorf("context command did not report remembered tasks:\n%s", got)
	}
	if !strings.Contains(got, "say hi") {
		t.Errorf("context command did not name the remembered task:\n%s", got)
	}
}

// TestRunREPLClearCommandForgets verifies the escape hatch works, and —
// the part that matters — that it is answered locally rather than sent to
// the model. scriptedOllamaServer fails the test on an unscripted call,
// so a "clear" that leaked through to generation would be caught here.
func TestRunREPLClearCommandForgets(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"echo hi", "DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	in := strings.NewReader("say hi\nclear\ncontext\n")
	runREPL(context.Background(), ollama.New(server.URL), "m", sessionJournal(t), in, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, "Forgot 1 remembered task(s)") {
		t.Errorf("clear did not report what it forgot:\n%s", got)
	}
	if !strings.Contains(got, "no conversation history yet") {
		t.Errorf("context after clear should report an empty session:\n%s", got)
	}
}

// --- scratch mode (D43): REPL's loop, no memory ---------------------------

// The defining property: multiple tasks still run in one process (like
// REPL), but nothing about one task's outcome reaches the next one's prompt.
func TestRunScratchCarriesNoHistoryBetweenTasks(t *testing.T) {
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Prompt string }
		json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body.Prompt)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "DONE", EvalCount: 1})
	}))
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("create report.pdf\nmove it to Downloads\n")

	code := runScratch(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if len(prompts) < 2 {
		t.Fatalf("expected at least 2 prompts (one per task), got %d", len(prompts))
	}
	for i, p := range prompts {
		if strings.Contains(p, "Earlier in this session") {
			t.Errorf("prompt %d carried session history, which scratch mode must never do:\n%s", i, p)
		}
	}
	if !strings.Contains(prompts[1], "move it to Downloads") {
		t.Errorf("the second task's own text should still reach the model:\n%s", prompts[1])
	}
}

// Still one process, several tasks — the one property scratch mode keeps
// from REPL despite having none of its memory.
func TestRunScratchProcessesMultipleTasksInOneProcess(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", a),
		"DONE",
		fmt.Sprintf("touch %q", b),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("create a.txt\ncreate b.txt\n")

	code := runScratch(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(a); err != nil {
		t.Errorf("expected first task's effect (%s) to exist: %v", a, err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Errorf("expected second task's effect (%s) to exist: %v", b, err)
	}
}

// "context" and "clear" still answer truthfully in scratch mode — there is
// just nothing for either of them to report or discard.
func TestRunScratchContextAndClearAnswerTruthfullyWithNoMemory(t *testing.T) {
	client := ollama.New("http://unused.invalid") // must never be called
	var out, errOut bytes.Buffer
	in := strings.NewReader("context\nclear\n")

	code := runScratch(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "no memory is kept in this mode") {
		t.Errorf("context should say plainly that scratch mode keeps nothing, got:\n%s", got)
	}
	if !strings.Contains(got, "nothing to forget") {
		t.Errorf("clear should say there is nothing to forget, got:\n%s", got)
	}
}

// The header must say what mode this is — a user typing "scratch" and
// getting REPL's own "follow-ups can refer back" hint would be told
// something false about the session they are in.
func TestRunScratchHeaderSaysNothingCarriesOver(t *testing.T) {
	client := ollama.New("http://unused.invalid")
	var out, errOut bytes.Buffer
	in := strings.NewReader("")

	runScratch(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	got := errOut.String() // the header is chrome: stderr (atk4-11)
	if !strings.Contains(got, "Scratch mode") {
		t.Errorf("header should name the mode, got:\n%s", got)
	}
	if strings.Contains(got, "Follow-ups can refer back") {
		t.Errorf("scratch mode's header claimed follow-up resolution it does not have:\n%s", got)
	}
}

// REPL's own header and behavior must be completely unaffected by scratch
// mode's addition — this is the regression the shared-loop refactor (D43)
// must not introduce.
func TestRunREPLHeaderIsUnchangedByScratchModesExistence(t *testing.T) {
	client := ollama.New("http://unused.invalid")
	var out, errOut bytes.Buffer
	in := strings.NewReader("")

	runREPL(context.Background(), client, "m", sessionJournal(t), in, &out, &errOut)
	got := errOut.String() // the header is chrome: stderr (atk4-11)
	if !strings.Contains(got, "Follow-ups can refer back") {
		t.Errorf("REPL's memory hint is missing, got:\n%s", got)
	}
	if strings.Contains(got, "Scratch mode") {
		t.Errorf("REPL's header should never mention scratch mode, got:\n%s", got)
	}
}

// Ctrl+C while a task waits at a confirmation prompt cancels that task at
// once, declines, and leaves the session reading: the next line typed goes to
// the session's prompt as a new task, not to the abandoned confirmation.
// Before lineSource, the prompt kept waiting until Enter, and that Enter was
// consumed as the confirmation's answer.
func TestRunREPLInterruptAtAConfirmationPromptIsImmediate(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	after := filepath.Join(dir, "after.txt")
	if err := os.WriteFile(keep, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("rm %q", keep),
		fmt.Sprintf("touch %q", after),
		"DONE",
	})
	defer server.Close()

	origNotify, origStop := notifyInterrupts, stopInterrupts
	defer func() { notifyInterrupts, stopInterrupts = origNotify, origStop }()
	var taskSignals []chan<- os.Signal
	var mu sync.Mutex
	notifyInterrupts = func(c chan<- os.Signal) { mu.Lock(); taskSignals = append(taskSignals, c); mu.Unlock() }
	stopInterrupts = func(c chan<- os.Signal) {}

	in, typing := io.Pipe()
	out, errOut := &lockedBuffer{}, &lockedBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- runREPL(context.Background(), ollama.New(server.URL), "m", sessionJournal(t), in, out, errOut)
	}()

	typing.Write([]byte("delete keep.txt\n"))
	waitFor(t, func() bool { return strings.Contains(errOut.String(), "[y/N]") }, "the confirmation prompt")
	mu.Lock()
	taskSignals[0] <- os.Interrupt
	mu.Unlock()
	waitFor(t, func() bool { return strings.Contains(out.String(), "Cancelling this task") }, "the cancellation notice")

	typing.Write([]byte("create after.txt\n"))
	waitFor(t, func() bool { _, err := os.Stat(after); return err == nil }, "the next task to run")
	typing.Close()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("runREPL did not return after its input closed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep.txt was deleted although the confirmation was interrupted: %v", err)
	}
}

// lockedBuffer is a bytes.Buffer safe to read while the session writes to it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// sessionJournal gives a session test a writable undo journal. A real session
// asks before every change when it has no journal (loopConfig.requireUndo),
// so a test of session mechanics needs one like any real session has.
func sessionJournal(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "undo.log")
}

// "undo" typed in the TUI is synapse's undo, never a request to the model, as
// in the REPL (ship review, 2026-10-27: the TUI sent it to the model).
func TestTUIUndoIsAnsweredLocally(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	journal := filepath.Join(t.TempDir(), "undo.log")
	srv := scriptedOllamaServer(t, []string{"touch made.txt", "DONE"})
	run := tuiRunner(ollama.New(srv.URL), "m", journal, nil, newTaskTracker(), nil)
	yes := func(string) bool { return true }
	var out, errOut bytes.Buffer
	if code := run(context.Background(), "make a file", yes, &out, &errOut); code != exitOK {
		t.Fatalf("task exit %d: %s", code, errOut.String())
	}
	srv.Close()
	// Any request to the model now fails the test: the server is gone, and
	// one that answered would be the model handling "undo".
	asked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the model was asked to handle undo")
	}))
	defer asked.Close()
	run = tuiRunner(ollama.New(asked.URL), "m", journal, nil, newTaskTracker(), nil)
	out.Reset()
	if code := run(context.Background(), "undo", yes, &out, &errOut); code != exitOK {
		t.Fatalf("undo exit %d: %s%s", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "made.txt")); err == nil {
		t.Errorf("made.txt is still there after undo:\n%s", out.String())
	}
}
