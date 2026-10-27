package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
)

// runScripted runs one task against a scripted model in a fresh working
// directory and returns the exit code, the outcome, the events and the
// directory. The script fails the test if the model is asked for more steps
// than it holds, so a repeat that ran on would be caught.
func runScripted(t *testing.T, steps []string) (int, string, []loopevent.Event, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	server := scriptedOllamaServer(t, steps)
	defer server.Close()
	evs, opt := collectEvents()
	var outcome string
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "do it", neverConfirm(t), &out, &errOut, "",
		opt, withOutcome(func(o string) { outcome = o }))
	return code, outcome, *evs, dir
}

func countKind(evs []loopevent.Event, k loopevent.Kind) int {
	n := 0
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return n
}

// The step just run, proposed again instead of DONE, is not run again: the
// loop stops, answers, and exits 0 under its own outcome. The side effect
// happens once, which is what not running it is for.
func TestRunLoopStopsWhenTheStepJustRunIsProposedAgain(t *testing.T) {
	code, outcome, evs, dir := runScripted(t, []string{"echo x >> count.txt", "echo x >> count.txt"})
	if code != exitOK || outcome != outcomeRepeatedSuccess {
		t.Errorf("exit %d, outcome %q; want 0 and %q", code, outcome, outcomeRepeatedSuccess)
	}
	b, err := os.ReadFile(dir + "/count.txt")
	if err != nil || string(b) != "x\n" {
		t.Errorf("count.txt = %q (%v), want one line: the repeat must not run", b, err)
	}
	if n := countKind(evs, loopevent.Command); n != 1 {
		t.Errorf("%d Command events, want 1: the repeat was announced as if it would run", n)
	}
	if n := countKind(evs, loopevent.Answer); n != 1 {
		t.Errorf("%d Answer events, want 1: the person is still told the result", n)
	}
	var note bool
	for _, e := range evs {
		note = note || (e.Kind == loopevent.Note && strings.Contains(e.Text, "echo x >> count.txt"))
	}
	if !note {
		t.Errorf("no note names the command that was not run again: %v", kinds(evs))
	}
}

// Text mode says why it stopped and does not claim the model finished.
func TestRunLoopRepeatInTextModeSaysWhyItStopped(t *testing.T) {
	t.Chdir(t.TempDir())
	server := scriptedOllamaServer(t, []string{"echo hi", "echo hi"})
	defer server.Close()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "")
	if code != exitOK {
		t.Fatalf("exit %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "just ran") || strings.Contains(out.String(), "Task complete") {
		t.Errorf("text output should say the repeat was not run, not that the task completed:\n%s", out.String())
	}
}

// A repeat with another command in between runs: ls before and after a change
// is a check, and its second result differs from the first.
func TestRunLoopRunsACommandAgainAfterAnotherRan(t *testing.T) {
	code, outcome, evs, _ := runScripted(t, []string{"ls", "touch made", "ls", "DONE"})
	if code != exitOK || outcome != outcomeComplete {
		t.Errorf("exit %d, outcome %q; want 0 and %q", code, outcome, outcomeComplete)
	}
	var results []loopevent.Event
	for _, e := range evs {
		if e.Kind == loopevent.Result {
			results = append(results, e)
		}
	}
	if len(results) != 3 || !strings.Contains(results[2].Stdout, "made") {
		t.Errorf("results = %+v, want three, the last listing the new file", results)
	}
}

// A failed step proposed again is still a repeated failure, not a success.
func TestRunLoopRepeatOfAFailureIsStillAFailure(t *testing.T) {
	code, outcome, _, _ := runScripted(t, []string{"false", "false"})
	if outcome != outcomeRepeatedFailure || code != exitFailed {
		t.Errorf("exit %d, outcome %q; want %d and %q", code, outcome, exitFailed, outcomeRepeatedFailure)
	}
}

// A refused step (a bare cd is reported as failed, not run) proposed again is
// a repeated failure too: it never succeeded.
func TestRunLoopRepeatOfARefusedStepIsNotASuccess(t *testing.T) {
	_, outcome, _, _ := runScripted(t, []string{"cd /", "cd /"})
	if outcome == outcomeRepeatedSuccess {
		t.Errorf("outcome %q: a step that was refused, not run, counted as succeeding", outcome)
	}
}

// Text mode names the model's command when it refuses to run it again; the
// command is shown, not obeyed, like every other text from outside synapse
// (ship review, 2026-10-27).
func TestRefusalLinesShowTheCommandAsText(t *testing.T) {
	t.Chdir(t.TempDir())
	hostile := "false \x1b]0;owned\x07"
	server := scriptedOllamaServer(t, []string{hostile, hostile})
	defer server.Close()
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do it", neverConfirm(t), &out, &errOut, "")
	if strings.Contains(out.String(), "\x1b]0;owned") || !strings.Contains(out.String(), "^[]0;owned^G") {
		t.Errorf("the refused command was not shown as text:\n%q", out.String())
	}
}
