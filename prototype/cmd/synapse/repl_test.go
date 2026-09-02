package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/ollama"
)

// TestRunREPLProcessesMultipleTasksInOneProcess verifies M3a's core claim:
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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected the task after the failed one to still run: %v", err)
	}
}

// TestRunREPLSharesOneReaderBetweenTasksAndConfirmations is the test that
// actually exercises M3a's stated risk: "no leaked confirmation state,
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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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

	code := runREPL(context.Background(), client, "m", "", in, &out, &errOut)

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
// M3a scopes for (no bubbletea/styling): a startup line explaining how to
// leave, and a "> " prompt before each read.
func TestRunREPLPrintsPromptAndGreeting(t *testing.T) {
	server := scriptedOllamaServer(t, nil)
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer
	in := strings.NewReader("exit\n")

	runREPL(context.Background(), client, "m", "", in, &out, &errOut)

	got := out.String()
	if !strings.Contains(got, "exit or quit") {
		t.Errorf("expected a greeting explaining how to leave, got:\n%s", got)
	}
	if !strings.Contains(got, "> ") {
		t.Errorf("expected a \"> \" prompt before reading input, got:\n%s", got)
	}
}

// TestRunREPLInterruptCancelsTaskButKeepsSessionAlive verifies the fix for
// the M3a gap found in Session 28's review: an interrupt during a running
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
		done <- runREPL(context.Background(), client, "m", "", in, &out, &errOut)
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runREPL did not return — the interrupt failed to cancel the sleeping task")
	}

	if !strings.Contains(out.String(), "cancelling this task") {
		t.Errorf("expected the cancellation notice in output, got:\n%s", out.String())
	}
	if _, err := os.Stat(after); err != nil {
		t.Errorf("expected the task after the interrupt to still run (session must survive): %v", err)
	}
}
