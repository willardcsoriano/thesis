package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

// scriptedOllamaServer returns an httptest.Server whose /api/generate
// handler replies with responses[call], one per call, in order — a stand-in
// for the live model so runLoop's orchestration (gating, execution,
// feedback, step limit) can be tested without Ollama running.
func scriptedOllamaServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var call int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(atomic.AddInt32(&call, 1)) - 1
		if i >= len(responses) {
			t.Fatalf("ollama called %d times, only %d scripted responses", i+1, len(responses))
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: responses[i], EvalCount: 1})
	}))
}

// neverConfirm fails the test if the confirmation gate is ever consulted —
// for scenarios that must stay entirely within reversible commands.
func neverConfirm(t *testing.T) func(string) bool {
	t.Helper()
	return func(prompt string) bool {
		t.Fatalf("confirmFn called unexpectedly with prompt %q — a reversible-only scenario should never gate", prompt)
		return false
	}
}

func TestRunLoopMultiStepReversibleNeverPromptsAndAppliesRealEffects(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", target),
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "create a.txt", neverConfirm(t), &out, &errOut, "")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected %s to have been created by the executed step: %v", target, err)
	}
	if !strings.Contains(out.String(), "task complete in 1 step(s).") {
		t.Errorf("stdout missing completion message, got:\n%s", out.String())
	}
}

func TestRunLoopIrreversibleCancelledNeverExecutes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	server := scriptedOllamaServer(t, []string{fmt.Sprintf("rm %q", target)})
	defer server.Close()

	var confirmCalls int
	confirmFn := func(prompt string) bool {
		confirmCalls++
		return false // decline
	}

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "delete keep.txt", confirmFn, &out, &errOut, "")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a declined confirmation is a clean cancel, not a failure); stderr:\n%s", code, errOut.String())
	}
	if confirmCalls != 1 {
		t.Errorf("confirmFn called %d times, want exactly 1", confirmCalls)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("file should have survived the declined confirmation, but stat failed: %v", err)
	}
	if !strings.Contains(out.String(), "blocked:") || !strings.Contains(out.String(), "cancelled.") {
		t.Errorf("stdout missing blocked/cancelled messaging, got:\n%s", out.String())
	}
}

func TestRunLoopIrreversibleConfirmedExecutes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "delete-me.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("rm %q", target),
		"DONE",
	})
	defer server.Close()

	var confirmCalls int
	confirmFn := func(prompt string) bool {
		confirmCalls++
		return true // approve
	}

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "delete delete-me.txt", confirmFn, &out, &errOut, "")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if confirmCalls != 1 {
		t.Errorf("confirmFn called %d times, want exactly 1", confirmCalls)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("file should have been deleted after confirmation, stat err = %v", err)
	}
}

func TestRunLoopEveryStepReclassifiedNotJustTheFirst(t *testing.T) {
	// A reversible first step must not exempt a later irreversible step from
	// its own gate — this is D21's central safety property.
	dir := t.TempDir()
	target := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	server := scriptedOllamaServer(t, []string{
		fmt.Sprintf("touch %q", filepath.Join(dir, "a.txt")), // reversible, auto-runs
		fmt.Sprintf("rm %q", target),                         // irreversible, must still gate
	})
	defer server.Close()

	var confirmCalls int
	confirmFn := func(prompt string) bool {
		confirmCalls++
		return false
	}

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	runLoop(context.Background(), client, "m", "touch a then remove b", confirmFn, &out, &errOut, "")

	if confirmCalls != 1 {
		t.Fatalf("confirmFn called %d times, want exactly 1 (only the second, irreversible step)", confirmCalls)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Errorf("first (reversible) step should have run: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("b.txt should have survived the declined second step: %v", err)
	}
}

func TestRunLoopStepLimitReachedIsReportedNotSilent(t *testing.T) {
	responses := make([]string, maxLoopSteps)
	for i := range responses {
		responses[i] = "true" // always reversible, never signals DONE
	}
	server := scriptedOllamaServer(t, responses)
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "never-ending task", neverConfirm(t), &out, &errOut, "")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), fmt.Sprintf("step limit reached (%d steps)", maxLoopSteps)) {
		t.Errorf("stderr missing step-limit message, got:\n%s", errOut.String())
	}
}

func TestRunLoopUnsupportedRequest(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"UNSUPPORTED"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "sing me a song", neverConfirm(t), &out, &errOut, "")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "cannot be done with a shell command") {
		t.Errorf("stdout missing the UNSUPPORTED explanation, got:\n%s", out.String())
	}
}

func TestRunLoopDoneOnFirstStepWithNoHistory(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "already done somehow", neverConfirm(t), &out, &errOut, "")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "nothing needs to be done") {
		t.Errorf("stdout missing the no-history DONE message, got:\n%s", out.String())
	}
}

func TestRunLoopProposeErrorIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "anything", neverConfirm(t), &out, &errOut, "")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "error:") {
		t.Errorf("stderr missing an error report, got:\n%s", errOut.String())
	}
}

// TestRunLoopRecordsUndoEntryForReversibleAutoRun verifies runLoop actually
// wires up undo recording (internal/undo has its own thorough tests for the
// snapshot/diff/pairing logic itself — this just confirms runLoop calls it
// correctly). It temporarily chdirs into a scratch directory since undo
// recording snapshots the process's actual working directory, not whatever
// absolute paths a command happens to mention.
func TestRunLoopRecordsUndoEntryForReversibleAutoRun(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("a.log", []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{
		"mkdir dest && mv a.log dest",
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "organize", neverConfirm(t), &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		t.Fatalf("PeekLastJournal: %v", err)
	}
	if !ok {
		t.Fatal("expected an undo entry to have been recorded, journal is empty")
	}
	if entry.Command != "mkdir dest && mv a.log dest" {
		t.Errorf("entry.Command = %q, want the executed command", entry.Command)
	}
	wantMove := undo.Move{OldPath: "a.log", NewPath: filepath.Join("dest", "a.log")}
	if len(entry.Moves) != 1 || entry.Moves[0] != wantMove {
		t.Errorf("entry.Moves = %+v, want [%+v]", entry.Moves, wantMove)
	}
}
