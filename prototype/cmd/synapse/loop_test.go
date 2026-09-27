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
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"synapseos/internal/loopevent"
	"sync/atomic"
	"testing"

	"synapseos/internal/executor"
	"synapseos/internal/ollama"
	"synapseos/internal/session"
	"synapseos/internal/undo"
)

// testAnswerText is what the mock returns for the D31 summarising call.
// Distinctive so a test can assert the reply reached the user.
const testAnswerText = "here is what happened, in plain language."

// runGit runs a git subcommand in dir for test setup, failing the test on
// error — used by the git-reset/git-clean integration tests below to build
// a real (throwaway) repository rather than mocking git's behavior.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// scriptedOllamaServer returns an httptest.Server whose /api/generate
// handler replies with responses[call], one per call, in order — a stand-in
// for the live model so runLoop's orchestration (gating, execution,
// feedback, step limit) can be tested without Ollama running.
func scriptedOllamaServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var call int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The natural-language reply (D31) is a second kind of call with its
		// own system prompt. It is served without consuming a scripted
		// response so that a test's script stays a script of *commands* and
		// the "called too many times" guard keeps its meaning.
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: testAnswerText, EvalCount: 1})
			return
		}
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
	if !strings.Contains(out.String(), "Task complete in 1 step(s).") {
		t.Errorf("stdout missing completion message, got:\n%s", out.String())
	}
}

// TestRunLoopAnnouncesTheExecutionTimeoutUpfront verifies the user is told
// about the per-step time limit before any step runs, not only after one is
// actually killed by it — added 2026-08-24 so a legitimately slow but
// otherwise-fine command (a large find, a slow package mirror) doesn't look
// like the tool silently hung.
// The execution-timeout notice belongs to a session, not to a task. It used
// to print inside runLoop, which meant it repeated before every task in the
// REPL and leaked into the TUI transcript, where the surrounding hint lines
// already cover it. It is now emitted once by whichever entry point owns the
// session.
func TestSessionAnnouncesTheExecutionTimeoutOnceNotPerTask(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"DONE", "DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runREPL(context.Background(), ollama.New(server.URL), "m", "",
		strings.NewReader("do nothing\ndo nothing again\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	want := fmt.Sprintf("Each step may run for up to %s", stepExecutionTimeout)
	if n := strings.Count(out.String(), want); n != 1 {
		t.Errorf("timeout notice appeared %d times across two tasks, want exactly 1:\n%s", n, out.String())
	}
}

// runLoop itself must stay silent about it, so the notice cannot reappear in
// the TUI transcript by way of the shared execution path.
func TestRunLoopItselfEmitsNoSessionPreamble(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do nothing", neverConfirm(t), &out, &errOut, "")
	if strings.Contains(out.String(), "Each step may run for up to") {
		t.Errorf("runLoop printed the session preamble:\n%s", out.String())
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
	if !strings.Contains(out.String(), "blocked:") || !strings.Contains(out.String(), "Cancelled.") {
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
	got := out.String()
	// The wording matters more than it looks. This is what a study
	// participant meets on every boundary task, so it must do three things:
	// state the limit, say what the system is for, and give a way forward.
	// A response that only reports an internal verdict leaves the user
	// stuck and depresses the usability scores for a reason that has
	// nothing to do with the interface paradigm being measured.
	if !strings.Contains(got, "couldn't work out a command") {
		t.Errorf("the limit itself is not stated, got:\n%s", got)
	}
	if !strings.Contains(got, "files and folders, disk usage, processes, packages") {
		t.Errorf("the response does not say what the system can do, got:\n%s", got)
	}
	// Fourth requirement, added 2026-09-15 after this message was hit in live
	// testing: it must not assert *why* the request failed. UNSUPPORTED is
	// emitted for at least three different reasons and distinguishes none of
	// them, so naming one — the old wording claimed "visual tasks like
	// editing images" every time — turns a failed `du` into a non-sequitur.
	if strings.Contains(got, "not visual tasks like") {
		t.Errorf("the response asserts a specific reason UNSUPPORTED was emitted; it cannot know one:\n%s", got)
	}
	if !strings.Contains(got, "for example") {
		t.Errorf("the response offers no worked example to move forward with, got:\n%s", got)
	}
	if strings.Contains(got, "model reported") {
		t.Errorf("the response reports an internal verdict rather than addressing the user, got:\n%s", got)
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
	if !strings.Contains(out.String(), "Nothing needs to be done") {
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

// TestRunLoopBlocksCpOntoExistingDestination confirms the cp-gap fix
// (classifier.ClassifyForDir, Session 23) is actually wired into runLoop
// end-to-end, not just correct in isolation — runLoop must resolve the real
// working directory and pass it through, so this chdirs into a scratch
// directory the same way the undo-recording test does.
func TestRunLoopBlocksCpOntoExistingDestination(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("source.txt", []byte("new"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile("backup.txt", []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	server := scriptedOllamaServer(t, []string{"cp source.txt backup.txt"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	var confirmPrompted bool
	confirmFn := func(prompt string) bool {
		confirmPrompted = true
		return false // decline — the point is to prove it blocked, not to actually run it
	}

	code := runLoop(context.Background(), client, "m", "back up source.txt", confirmFn, &out, &errOut, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a declined confirmation is a clean cancel); stderr:\n%s", code, errOut.String())
	}
	if !confirmPrompted {
		t.Fatal("expected cp onto an existing destination to block on confirmation, but confirmFn was never called")
	}
	if !strings.Contains(out.String(), "cp overwrites an existing file") {
		t.Errorf("stdout missing the cp-overwrite reason, got:\n%s", out.String())
	}
	got, err := os.ReadFile("backup.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "old" {
		t.Errorf("backup.txt = %q, want unchanged %q — the declined cp must not have run", got, "old")
	}
}

// TestRunLoopBackUpsContentBeforeConfirmedInPlaceEdit is the "guiltless"
// content-backup extension (Session 24) wired end-to-end: a confirmed
// sed -i gets its target file backed up before it runs, the journal records
// it, and undo.Apply actually restores the pre-edit content — not just that
// classifier.ContentMutationTargets and undo.BackupContent are individually
// correct in isolation.
func TestRunLoopBackUpsContentBeforeConfirmedInPlaceEdit(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // BackupContent's DefaultContentBackupDir must not touch the real ~/.synapse

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("config.yaml", []byte("port: 8080"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{
		"sed -i 's/8080/9090/' config.yaml",
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	var confirmPrompted bool
	confirmFn := func(prompt string) bool {
		confirmPrompted = true
		return true // confirm — this is the "wrong yes" the guiltless extension exists to protect against
	}

	code := runLoop(context.Background(), client, "m", "fix the port", confirmFn, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !confirmPrompted {
		t.Fatal("expected sed -i to block on confirmation, but confirmFn was never called")
	}

	mutated, err := os.ReadFile("config.yaml")
	if err != nil || string(mutated) != "port: 9090" {
		t.Fatalf("config.yaml after the confirmed edit = %q (err %v), want %q — the sed must actually have run", mutated, err, "port: 9090")
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		t.Fatalf("PeekLastJournal: %v", err)
	}
	if !ok {
		t.Fatal("expected a content-backup undo entry to have been recorded, journal is empty")
	}
	if len(entry.ContentBackups) != 1 {
		t.Fatalf("entry.ContentBackups = %+v, want exactly 1", entry.ContentBackups)
	}
	wantPath := filepath.Join(dir, "config.yaml")
	if entry.ContentBackups[0].Path != wantPath {
		t.Errorf("ContentBackups[0].Path = %q, want %q", entry.ContentBackups[0].Path, wantPath)
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("config.yaml")
	if err != nil || string(restored) != "port: 8080" {
		t.Errorf("config.yaml after undo = %q (err %v), want the original %q restored", restored, err, "port: 8080")
	}
}

// TestRunLoopTrashesFileBeforeConfirmedRm is the hardlink-based trash
// mechanism (Session 25) wired end-to-end: a confirmed rm gets its target
// preserved via TrashPreserve before it runs, the journal records it, and
// undo.Apply actually restores the file — the file must genuinely be gone
// from its original path after the confirmed rm (unlike the content-backup
// case, rm's whole point is that the original no longer exists there).
func TestRunLoopTrashesFileBeforeConfirmedRm(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // TrashPreserve's DefaultTrashDir must not touch the real ~/.synapse

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("doomed.txt", []byte("keep me"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{
		"rm doomed.txt",
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	var confirmPrompted bool
	confirmFn := func(prompt string) bool { confirmPrompted = true; return true }

	code := runLoop(context.Background(), client, "m", "clean up", confirmFn, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !confirmPrompted {
		t.Fatal("expected rm to block on confirmation, but confirmFn was never called")
	}

	if _, err := os.Stat("doomed.txt"); !os.IsNotExist(err) {
		t.Errorf("expected doomed.txt actually removed after the confirmed rm, stat err = %v", err)
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		t.Fatalf("PeekLastJournal: %v", err)
	}
	if !ok {
		t.Fatal("expected a trash undo entry to have been recorded, journal is empty")
	}
	if len(entry.Trashed) != 1 {
		t.Fatalf("entry.Trashed = %+v, want exactly 1", entry.Trashed)
	}
	wantPath := filepath.Join(dir, "doomed.txt")
	if entry.Trashed[0].OriginalPath != wantPath {
		t.Errorf("Trashed[0].OriginalPath = %q, want %q", entry.Trashed[0].OriginalPath, wantPath)
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("doomed.txt")
	if err != nil || string(restored) != "keep me" {
		t.Errorf("doomed.txt after undo = %q (err %v), want the original %q restored", restored, err, "keep me")
	}
}

// TestRunLoopBacksUpContentBeforeConfirmedCpOverwrite closes the
// previously-flagged gap: cp onto an existing destination is Irreversible
// via ClassifyForDir but had no backup at all until now. Content-copy, not
// trash — cp overwrites its destination's existing inode in place (verified
// empirically, Session 25), so a hardlink taken beforehand would share the
// very data being overwritten and protect nothing.
func TestRunLoopBacksUpContentBeforeConfirmedCpOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("source.txt", []byte("new"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile("backup.txt", []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{
		"cp source.txt backup.txt",
		"DONE",
	})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "back up then overwrite", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	overwritten, err := os.ReadFile("backup.txt")
	if err != nil || string(overwritten) != "new" {
		t.Fatalf("backup.txt after the confirmed cp = %q (err %v), want %q — the cp must actually have run", overwritten, err, "new")
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		t.Fatalf("PeekLastJournal: %v", err)
	}
	if !ok {
		t.Fatal("expected a content-backup undo entry to have been recorded, journal is empty")
	}
	if len(entry.ContentBackups) != 1 {
		t.Fatalf("entry.ContentBackups = %+v, want exactly 1", entry.ContentBackups)
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("backup.txt")
	if err != nil || string(restored) != "old" {
		t.Errorf("backup.txt after undo = %q (err %v), want the original %q restored", restored, err, "old")
	}
}

// TestRunLoopDeclinedInPlaceEditTakesNoBackup confirms the backup only
// happens after an explicit confirmation, not merely because the command
// was classified as a content-mutation risk — a declined command never
// runs, so there is nothing to protect against and nothing should be
// journaled.
func TestRunLoopDeclinedInPlaceEditTakesNoBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("config.yaml", []byte("port: 8080"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{"sed -i 's/8080/9090/' config.yaml"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "fix the port", func(string) bool { return false }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a declined confirmation is a clean cancel); stderr:\n%s", code, errOut.String())
	}

	unchanged, err := os.ReadFile("config.yaml")
	if err != nil || string(unchanged) != "port: 8080" {
		t.Errorf("config.yaml = %q (err %v), want unchanged %q — the declined sed must not have run", unchanged, err, "port: 8080")
	}
	if _, ok, err := undo.PeekLastJournal(journalPath); err != nil || ok {
		t.Errorf("PeekLastJournal: ok=%v err=%v, want ok=false — nothing should be journaled for a declined command", ok, err)
	}
}

// TestBackupBeforeIrreversibleWarnsOnContentBackupFailure exercises each of
// backupBeforeIrreversible's five independent warning paths directly —
// these are failure-injection scenarios (an unwritable backup location, a
// cancelled context, a non-git directory) that don't fit the scripted
// happy-path shape of the runLoop integration tests above.
func TestBackupBeforeIrreversibleWarnsOnContentBackupFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	backupHome := t.TempDir()
	t.Setenv("HOME", backupHome)
	backupDir := filepath.Join(backupHome, ".synapse", "content-backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	if err := os.Chmod(backupDir, 0o555); err != nil {
		t.Fatalf("chmod backupDir: %v", err)
	}
	defer os.Chmod(backupDir, 0o755)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var errOut bytes.Buffer
	backupBeforeIrreversible(context.Background(), "sed -i 's/x/y/' "+filepath.Join(dir, "f.txt"), dir, &errOut)
	if !strings.Contains(errOut.String(), "could not back up a file") {
		t.Errorf("errOut = %q, want a content-backup warning", errOut.String())
	}
}

func TestBackupBeforeIrreversibleWarnsOnGitCleanDryRunFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled up front: the dry-run subprocess must fail to launch

	var errOut bytes.Buffer
	backupBeforeIrreversible(ctx, "git clean -f", t.TempDir(), &errOut)
	if !strings.Contains(errOut.String(), "could not preview git clean's effect") {
		t.Errorf("errOut = %q, want a git-clean-dry-run warning", errOut.String())
	}
}

func TestBackupBeforeIrreversibleWarnsOnTrashFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	trashHome := t.TempDir()
	t.Setenv("HOME", trashHome)
	trashDir := filepath.Join(trashHome, ".synapse", "trash")
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatalf("mkdir trashDir: %v", err)
	}
	if err := os.Chmod(trashDir, 0o555); err != nil {
		t.Fatalf("chmod trashDir: %v", err)
	}
	defer os.Chmod(trashDir, 0o755)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var errOut bytes.Buffer
	backupBeforeIrreversible(context.Background(), "rm "+filepath.Join(dir, "f.txt"), dir, &errOut)
	if !strings.Contains(errOut.String(), "could not preserve a file") {
		t.Errorf("errOut = %q, want a trash-preservation warning", errOut.String())
	}
}

func TestBackupBeforeIrreversibleWarnsOnGitHeadCaptureFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	var errOut bytes.Buffer
	backupBeforeIrreversible(context.Background(), "git reset --hard", t.TempDir(), &errOut) // not a git repo
	if !strings.Contains(errOut.String(), "could not capture git HEAD") {
		t.Errorf("errOut = %q, want a git-HEAD-capture warning", errOut.String())
	}
}

func TestBackupBeforeIrreversibleWarnsOnMetadataBackupFailure(t *testing.T) {
	var errOut bytes.Buffer
	backupBeforeIrreversible(context.Background(), "chmod -R 777 does-not-exist", t.TempDir(), &errOut)
	if !strings.Contains(errOut.String(), "could not back up permissions") {
		t.Errorf("errOut = %q, want a metadata-backup warning", errOut.String())
	}
}

// TestRunLoopWarnsWhenJournalWriteFailsForABackupEntry confirms a failure
// writing the journal itself (as opposed to a failure taking the backup)
// is surfaced as a warning and doesn't fail the step — the command the
// user confirmed already ran; losing the safety net's own record of it is
// unfortunate but must never look like the command itself failed.
func TestRunLoopWarnsWhenJournalWriteFailsForABackupEntry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)
	if err := os.WriteFile("doomed.txt", []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalDir := t.TempDir()
	if err := os.Chmod(journalDir, 0o555); err != nil { // read+execute only: creating the journal file fails
		t.Fatalf("chmod journalDir: %v", err)
	}
	defer os.Chmod(journalDir, 0o755)
	journalPath := filepath.Join(journalDir, "undo.log")

	server := scriptedOllamaServer(t, []string{"rm doomed.txt", "DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "clean up", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a lost safety-net record must not fail the step); stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "could not record undo entry") {
		t.Errorf("errOut = %q, want a could-not-record-undo-entry warning", errOut.String())
	}
	if _, err := os.Stat("doomed.txt"); !os.IsNotExist(err) {
		t.Error("expected doomed.txt actually removed — the confirmed rm must still have run despite the journal-write failure")
	}
}

// TestRunLoopCapturesGitHeadBeforeConfirmedReset is the git reset --hard
// SHA-capture mechanism (Session 26) wired end-to-end against a real
// throwaway repository: the reset actually runs (HEAD moves), the journal
// records the pre-reset SHA, and undo.Apply genuinely brings the original
// commit's content back.
func TestRunLoopCapturesGitHeadBeforeConfirmedReset(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	runGit(t, dir, "init", "-q")
	if err := os.WriteFile("f.txt", []byte("v1"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "first")
	if err := os.WriteFile("f.txt", []byte("v2"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "second")

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{"git reset --hard HEAD~1", "DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "undo last commit", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	reverted, err := os.ReadFile("f.txt")
	if err != nil || string(reverted) != "v1" {
		t.Fatalf("f.txt after the confirmed reset = %q (err %v), want %q — the reset must actually have run", reverted, err, "v1")
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil || !ok {
		t.Fatalf("PeekLastJournal: ok=%v err=%v", ok, err)
	}
	if entry.GitReset == "" {
		t.Fatal("entry.GitReset is empty, want the pre-reset commit SHA captured")
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("f.txt")
	if err != nil || string(restored) != "v2" {
		t.Errorf("f.txt after undo = %q (err %v), want the pre-undo %q restored", restored, err, "v2")
	}
}

// TestRunLoopTrashesUntrackedFileBeforeConfirmedGitClean is the git
// clean -f dry-run-then-trash mechanism (Session 26) wired end-to-end: the
// dry-run correctly identifies the untracked file, it's hardlinked into
// trash before the real clean runs, the file is genuinely gone afterward,
// and undo.Apply restores it.
func TestRunLoopTrashesUntrackedFileBeforeConfirmedGitClean(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	runGit(t, dir, "init", "-q")
	if err := os.WriteFile("tracked.txt", []byte("tracked"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")
	if err := os.WriteFile("untracked.txt", []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{"git clean -f", "DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "clean up untracked files", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	if _, err := os.Stat("untracked.txt"); !os.IsNotExist(err) {
		t.Fatalf("expected untracked.txt actually removed by the confirmed git clean, stat err = %v", err)
	}
	if _, err := os.Stat("tracked.txt"); err != nil {
		t.Errorf("tracked.txt should not have been touched: %v", err)
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil || !ok {
		t.Fatalf("PeekLastJournal: ok=%v err=%v", ok, err)
	}
	if len(entry.Trashed) != 1 {
		t.Fatalf("entry.Trashed = %+v, want exactly 1", entry.Trashed)
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("untracked.txt")
	if err != nil || string(restored) != "keep me" {
		t.Errorf("untracked.txt after undo = %q (err %v), want %q restored", restored, err, "keep me")
	}
}

// TestRunLoopBacksUpMetadataBeforeConfirmedChmodRecursive is the recursive
// chmod metadata-backup mechanism (Session 26) wired end-to-end: the real
// chmod -R runs (permissions actually change), the journal records the
// original mode, and undo.Apply restores it.
func TestRunLoopBacksUpMetadataBeforeConfirmedChmodRecursive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.Mkdir("target", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join("target", "file.txt"), []byte("x"), 0o640); err != nil {
		t.Fatalf("write: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{"chmod -R 777 target", "DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "open up permissions", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	changed, err := os.Stat(filepath.Join("target", "file.txt"))
	if err != nil || changed.Mode().Perm() != 0o777 {
		t.Fatalf("file.txt mode after the confirmed chmod = %v (err %v), want 0777 — the chmod must actually have run", changed.Mode(), err)
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil || !ok {
		t.Fatalf("PeekLastJournal: ok=%v err=%v", ok, err)
	}
	if len(entry.MetadataBackups) == 0 {
		t.Fatal("entry.MetadataBackups is empty, want at least the target dir and file recorded")
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.Stat(filepath.Join("target", "file.txt"))
	if err != nil || restored.Mode().Perm() != 0o640 {
		t.Errorf("file.txt mode after undo = %v (err %v), want the original 0640 restored", restored.Mode(), err)
	}
}

// TestRunLoopBacksUpContentBeforeConfirmedDdOverwrite closes the dd/mkfs
// known gap (Session 26) for the regular-file-target case: dd overwriting
// an existing regular file gets content-backup, the same mechanism cp's
// overwrite already uses.
func TestRunLoopBacksUpContentBeforeConfirmedDdOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origWD)

	if err := os.WriteFile("src.img", []byte("NEWDATA"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile("dst.img", []byte("OLDDATA"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	server := scriptedOllamaServer(t, []string{"dd if=src.img of=dst.img", "DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "overwrite the image", func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}

	overwritten, err := os.ReadFile("dst.img")
	if err != nil || string(overwritten) != "NEWDATA" {
		t.Fatalf("dst.img after the confirmed dd = %q (err %v), want %q — the dd must actually have run", overwritten, err, "NEWDATA")
	}

	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil || !ok {
		t.Fatalf("PeekLastJournal: ok=%v err=%v", ok, err)
	}
	if len(entry.ContentBackups) != 1 {
		t.Fatalf("entry.ContentBackups = %+v, want exactly 1", entry.ContentBackups)
	}

	if errs := undo.Apply(entry); len(errs) != 0 {
		t.Fatalf("undo.Apply returned errors: %v", errs)
	}
	restored, err := os.ReadFile("dst.img")
	if err != nil || string(restored) != "OLDDATA" {
		t.Errorf("dst.img after undo = %q (err %v), want the original %q restored", restored, err, "OLDDATA")
	}
}

// streamingOllamaServer replies to /api/generate with an NDJSON stream,
// emitting each fragment as its own flushed chunk so a caller genuinely
// receives them progressively rather than as one buffered blob.
func streamingOllamaServer(t *testing.T, fragments []string) *httptest.Server {
	t.Helper()
	var call int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}
		i := int(atomic.AddInt32(&call, 1)) - 1
		// First call streams the command; every later call ends the loop.
		if i > 0 {
			io.WriteString(w, `{"response":"DONE","done":true}`+"\n")
			flusher.Flush()
			return
		}
		for _, f := range fragments {
			fmt.Fprintf(w, `{"response":%q,"done":false}`+"\n", f)
			flusher.Flush()
		}
		io.WriteString(w, `{"response":"","done":true,"eval_count":3}`+"\n")
		flusher.Flush()
	}))
}

// TestRunLoopStreamsTokensWhenEnabled verifies M5 step 4's wiring: with
// withTokenStreaming, model output reaches the sink in fragments as it is
// generated, and the command still executes correctly — proving streaming
// changed only when text appears, not what gets run.
func TestRunLoopStreamsTokensWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "streamed.txt")

	server := streamingOllamaServer(t, []string{"touch ", strconv.Quote(target)})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "make a file", neverConfirm(t),
		&out, &errOut, "", withTokenStreaming(&out))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	// The streamed fragments must be visible in the sink before the
	// summary line the loop prints once the command is known.
	if !strings.Contains(out.String(), "touch ") {
		t.Errorf("streamed tokens missing from the sink; got:\n%s", out.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("streamed command did not execute correctly: %v", err)
	}
}

// TestRunLoopDoesNotStreamByDefault pins the documented CLI/REPL
// behavior: without the option the client must use the non-streaming
// endpoint, so a server that only speaks single-object replies still
// works. Guards against streaming silently becoming the default for
// every mode.
func TestRunLoopDoesNotStreamByDefault(t *testing.T) {
	var sawStream bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if s, _ := body["stream"].(bool); s {
			sawStream = true
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "DONE", Done: true})
	}))
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	if code := runLoop(context.Background(), client, "m", "noop", neverConfirm(t), &out, &errOut, ""); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if sawStream {
		t.Error("runLoop requested a streaming response without withTokenStreaming — CLI/REPL mode must stay non-streaming")
	}
}

// --- session context wiring (M6 step 3) ------------------------------

// TestRunLoopRecordsTheTurnIntoSessionContext verifies a completed task
// enters memory, which is the precondition for any follow-up resolving
// against it.
func TestRunLoopRecordsTheTurnIntoSessionContext(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.pdf")

	server := scriptedOllamaServer(t, []string{fmt.Sprintf("touch %q", target), "DONE"})
	defer server.Close()

	sc := session.New()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "create report.pdf",
		neverConfirm(t), &out, &errOut, "", withSessionContext(sc))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if sc.Len() != 1 {
		t.Fatalf("session recorded %d turns, want 1", sc.Len())
	}
	if rendered := sc.Render(); !strings.Contains(rendered, "create report.pdf") {
		t.Errorf("session lost the task text:\n%s", rendered)
	}
}

// TestRunLoopFeedsPriorTurnsIntoTheNextPrompt is the property that makes
// "move it to Downloads" work: the previous turn must actually reach the
// model, not merely be stored.
func TestRunLoopFeedsPriorTurnsIntoTheNextPrompt(t *testing.T) {
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body["prompt"].(string))
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "DONE", Done: true})
	}))
	defer server.Close()

	sc := session.New()
	sc.Append("create report.pdf", []session.Step{{Command: "touch report.pdf", Result: "ok"}})

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "move it to Downloads",
		neverConfirm(t), &out, &errOut, "", withSessionContext(sc))

	if len(prompts) == 0 {
		t.Fatal("model was never called")
	}
	if !strings.Contains(prompts[0], "report.pdf") {
		t.Errorf("prior turn never reached the prompt, so 'it' is unresolvable:\n%s", prompts[0])
	}
	// The current task must still be present and readable as the newest thing.
	if !strings.Contains(prompts[0], "move it to Downloads") {
		t.Errorf("current task missing from prompt:\n%s", prompts[0])
	}
}

// TestRunLoopWithoutSessionStaysStateless pins one-shot CLI mode's
// contract: no option, no memory, no behavior change. Statelessness is
// what makes CLI mode scriptable (D19), not an oversight to fix.
func TestRunLoopWithoutSessionStaysStateless(t *testing.T) {
	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		prompts = append(prompts, body["prompt"].(string))
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "DONE", Done: true})
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "some task",
		neverConfirm(t), &out, &errOut, "")

	if strings.Contains(prompts[0], "Earlier in this session") {
		t.Errorf("stateless run leaked a history section:\n%s", prompts[0])
	}
}

// TestRunLoopRecordsPartialTurnOnFailure guards the deferred-record
// design: a task that failed halfway still did whatever it did, and a
// follow-up like "undo that" needs to see it. A partial turn is memory; a
// missing one misrepresents what happened.
func TestRunLoopRecordsPartialTurnOnFailure(t *testing.T) {
	dir := t.TempDir()
	made := filepath.Join(dir, "made.txt")

	// First step succeeds, then the model errors out on the next propose.
	var call int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(atomic.AddInt32(&call, 1)) == 1 {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: fmt.Sprintf("touch %q", made), Done: true})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sc := session.New()
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "make a file",
		"make a file", neverConfirm(t), &out, &errOut, "", withSessionContext(sc))

	if sc.Len() != 1 {
		t.Fatalf("session recorded %d turns, want 1 — a partially-completed task must still be remembered", sc.Len())
	}
	if !strings.Contains(sc.Render(), "touch") {
		t.Errorf("the step that did run is missing from memory:\n%s", sc.Render())
	}
}

// TestRunLoopReportsDroppedTurns verifies the anti-silence guarantee
// reaches the user, not just the data structure.
func TestRunLoopReportsDroppedTurns(t *testing.T) {
	// The task must actually execute a step: a task that does nothing is
	// deliberately not recorded, so it could never trigger an eviction.
	server := scriptedOllamaServer(t, []string{"echo hello", "DONE"})
	defer server.Close()

	// Tiny budget, pre-filled so this task's turn forces an eviction.
	sc := session.NewWithBudget(15, 20)
	for _, task := range []string{"old one", "old two", "old three"} {
		sc.Append(task, []session.Step{{Command: "echo x", Result: strings.Repeat("y", 200)}})
	}
	sc.TakeDropped() // clear setup noise so we only observe runLoop's report

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "new task",
		neverConfirm(t), &out, &errOut, "", withSessionContext(sc))

	if !strings.Contains(out.String(), "dropped") {
		t.Errorf("eviction was not reported to the user; output was:\n%s", out.String())
	}
}

// Observed live 2026-09-12: the model re-proposed an identical failing command
// until the step cap, producing five identical error blocks and five steps of
// telemetry for one real attempt. The loop now stops the second time it sees a
// command that has already failed.
func TestRepeatedFailingCommandStopsEarly(t *testing.T) {
	t.Chdir(t.TempDir())
	// Five identical failing proposals; only the first should ever run.
	server := scriptedOllamaServer(t, []string{
		"ls /definitely-not-here", "ls /definitely-not-here", "ls /definitely-not-here",
		"ls /definitely-not-here", "ls /definitely-not-here",
	})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "list a missing directory",
		neverConfirm(t), &out, &errOut, "")

	if code == 0 {
		t.Fatal("a task that never succeeded returned success")
	}
	got := out.String()
	if n := strings.Count(got, "exit code: 2"); n != 1 {
		t.Errorf("the failing command ran %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "already failed here") {
		t.Errorf("no explanation given for stopping:\n%s", got)
	}
	if strings.Contains(errOut.String(), "step limit reached") {
		t.Error("burned the whole step cap instead of stopping at the repeat")
	}
}

// The warning has to reach the model, not just the loop — otherwise the only
// defence is the stop, which fires after a wasted step.
func TestFailedCommandsAreNamedInThePrompt(t *testing.T) {
	h := []loopStep{
		{command: "ls /missing", result: executor.Result{ExitCode: 2, Stderr: "No such file"}},
		{command: "true", result: executor.Result{ExitCode: 0}},
	}
	p := buildStepPrompt("do a thing", h, "", "")
	if !strings.Contains(p, "These commands already failed") {
		t.Fatalf("prompt does not warn about failures:\n%s", p)
	}
	if !strings.Contains(p, "- ls /missing") {
		t.Errorf("the failed command is not named:\n%s", p)
	}
	if strings.Contains(p, "- true") {
		t.Error("a command that succeeded was listed as failed")
	}
}

// A lone cd changes nothing that outlasts the shell that ran it. Running it and
// reporting success made the session answer "are we in the root dir?" with "We
// are now in the root directory" while it sat exactly where it started. It must be
// refused as a failed step, say why, and let the next proposal (pwd) go through.
func TestRunLoopBareCdIsRefusedNotReportedAsSuccess(t *testing.T) {
	before, _ := os.Getwd()
	server := scriptedOllamaServer(t, []string{"cd /", "pwd", "DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "are we in the root dir?", neverConfirm(t), &out, &errOut, "")

	got := out.String()
	if !strings.Contains(got, "not run: cd /") {
		t.Errorf("a bare cd should be reported as not run, got:\n%s", got)
	}
	if !strings.Contains(got, "cannot move this session") {
		t.Errorf("the refusal should say why, got:\n%s", got)
	}
	if strings.Contains(got, "exit code: 0\n\nstep 2") && !strings.Contains(got, before) {
		t.Errorf("step 2 (pwd) should have run and printed the real directory %q, got:\n%s", before, got)
	}
	if !strings.Contains(got, before) {
		t.Errorf("the real working directory %q should appear in the output, got:\n%s", before, got)
	}
	if code != 0 {
		t.Errorf("the task should still complete via pwd, exit %d; stderr:\n%s", code, errOut.String())
	}
	if now, _ := os.Getwd(); now != before {
		t.Errorf("the process directory moved from %q to %q", before, now)
	}
}

func TestCdThenPwd(t *testing.T) {
	for cmd, want := range map[string]bool{
		"cd / && pwd": true, "cd /tmp; pwd": true, "cd /x || pwd": true, "cd / && cd /tmp && pwd": true,
		"pwd": false, "cd /": false, "cd logs && ls": false, "pwd && cd /": false,
		"cd / && pwd > out": false, "cd / && pwd | cat": false, "ls && pwd": false, "": false,
	} {
		if got := cdThenPwd(cmd); got != want {
			t.Errorf("cdThenPwd(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestRunLoopCdThenPwdIsRefusedToo(t *testing.T) {
	before, _ := os.Getwd()
	server := scriptedOllamaServer(t, []string{"cd / && pwd", "pwd", "DONE"})
	defer server.Close()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "are we in the root dir?", neverConfirm(t), &out, &errOut, "")
	got := out.String()
	if !strings.Contains(got, "not run: cd / && pwd") {
		t.Errorf("cd-then-pwd should be reported as not run, got:\n%s", got)
	}
	if !strings.Contains(got, before) || code != 0 {
		t.Errorf("the follow-up pwd should report the real directory %q and finish (exit %d), got:\n%s", before, code, got)
	}
}

func TestBareCd(t *testing.T) {
	for cmd, want := range map[string]bool{
		"cd /": true, "cd": true, "cd ~/x": true, "pushd /tmp": true, "popd": true,
		"cd logs && ls": false, "ls": false, "cd /tmp; ls": false, "echo cd": false,
		"cd / > out": false, "cd / | cat": false, "": false, "cd 'unterminated": false,
	} {
		if got := bareCd(cmd); got != want {
			t.Errorf("bareCd(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// With an event consumer the loop reports what happened as typed events and
// writes none of it as text; what ran is the same.
func collectEvents() (*[]loopevent.Event, loopOption) {
	var evs []loopevent.Event
	return &evs, withEvents(func(e loopevent.Event) { evs = append(evs, e) })
}

func kinds(evs []loopevent.Event) []loopevent.Kind {
	out := make([]loopevent.Kind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestRunLoopEventsReplaceTheNarration(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"echo hi", "DONE"})
	defer server.Close()

	evs, opt := collectEvents()
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "", opt)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("the loop still wrote text alongside its events:\nout: %q\nerr: %q", out.String(), errOut.String())
	}
	want := []loopevent.Kind{loopevent.Command, loopevent.Result, loopevent.Answer}
	if got := kinds(*evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	cmd, res := (*evs)[0], (*evs)[1]
	if cmd.Command != "echo hi" || cmd.Step != 1 || cmd.Tokens != 1 {
		t.Errorf("command event = %+v", cmd)
	}
	if res.Stdout != "hi\n" || res.ExitCode != 0 {
		t.Errorf("result event = %+v", res)
	}
}

func TestRunLoopEventsReportAFailureAndRefusalsAsData(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"false", "UNSUPPORTED"})
	defer server.Close()

	evs, opt := collectEvents()
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do it", neverConfirm(t), &out, &errOut, "", opt)
	want := []loopevent.Kind{loopevent.Command, loopevent.Result, loopevent.Notice}
	if got := kinds(*evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	if (*evs)[1].ExitCode != 1 {
		t.Errorf("result exit code = %d, want 1", (*evs)[1].ExitCode)
	}
	if !strings.Contains((*evs)[2].Text, "couldn't work out a command") {
		t.Errorf("notice = %q", (*evs)[2].Text)
	}
}

func TestRunLoopEventsAnnounceAnApprovalBeforeAsking(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "keep.txt")
	os.WriteFile(target, []byte("x"), 0o644)
	server := scriptedOllamaServer(t, []string{fmt.Sprintf("rm %q", target)})
	defer server.Close()

	evs, opt := collectEvents()
	var out, errOut bytes.Buffer
	asked := false
	confirm := func(string) bool {
		asked = true
		if n := len(*evs); n < 2 || (*evs)[n-1].Kind != loopevent.Approval {
			t.Errorf("the approval was not announced before asking: %v", kinds(*evs))
		}
		return false
	}
	runLoop(context.Background(), ollama.New(server.URL), "m", "remove it", confirm, &out, &errOut, "", opt)
	if !asked {
		t.Fatal("the gate never asked")
	}
	ap := (*evs)[1]
	if ap.Command == "" || !strings.Contains(ap.Text, "can't be undone") {
		t.Errorf("approval event = %+v", ap)
	}
	if last := (*evs)[len(*evs)-1]; last.Kind != loopevent.Notice || last.Text != "Cancelled." {
		t.Errorf("last event = %+v, want the cancellation", last)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("the file was removed although the person declined")
	}
}

func TestEventsFromOnlyAttachesToAnEmitter(t *testing.T) {
	if got := eventsFrom(&bytes.Buffer{}); got != nil {
		t.Error("a plain writer must keep the text output")
	}
	if got := eventsFrom(&fakeEmitter{}); len(got) != 1 {
		t.Error("a writer that accepts events must get them")
	}
}

type fakeEmitter struct{ bytes.Buffer }

func (*fakeEmitter) Emit(loopevent.Event) {}

// --- structured step decisions (schema-constrained model replies) ---------

func TestDecodeStepDecisionParsesEachAction(t *testing.T) {
	cases := []struct {
		raw     string
		wantCmd string
	}{
		{`{"action":"run","command":"ls -la"}`, "ls -la"},
		{`{"action":"done","command":""}`, doneSentinel},
		{`{"action":"unsupported","command":""}`, "UNSUPPORTED"},
		// cleanCommand still runs on the extracted command, so a model that
		// wraps it in backticks inside the JSON string is still handled.
		{"{\"action\":\"run\",\"command\":\"`ls -la`\"}", "ls -la"},
	}
	for _, tc := range cases {
		got, ok := decodeStepDecision(tc.raw)
		if !ok {
			t.Errorf("decodeStepDecision(%q) ok = false, want true", tc.raw)
			continue
		}
		if got != tc.wantCmd {
			t.Errorf("decodeStepDecision(%q) = %q, want %q", tc.raw, got, tc.wantCmd)
		}
	}
}

// Anything that is not this exact shape — free text, an unknown action, a
// malformed object — is reported as not decoded, so the caller falls back to
// treating the reply as a plain command. This is the compatibility path that
// keeps every pre-existing scripted test (a bare "DONE" or a bare command
// string) working without being rewritten for this change.
func TestDecodeStepDecisionFallsBackOnAnythingElse(t *testing.T) {
	for _, raw := range []string{
		"DONE",
		"UNSUPPORTED",
		`touch "a.txt"`,
		`{"action":"maybe","command":"ls"}`,
		`not json at all`,
		``,
	} {
		if _, ok := decodeStepDecision(raw); ok {
			t.Errorf("decodeStepDecision(%q) ok = true, want false (fall back to free text)", raw)
		}
	}
}

// scriptedStructuredServer scripts the model's replies as stepFormat JSON
// instead of bare command strings, so runLoop is exercised against exactly
// what a real, schema-constrained server sends.
func scriptedStructuredServer(t *testing.T, decisions ...stepDecision) *httptest.Server {
	t.Helper()
	raw := make([]string, len(decisions))
	for i, d := range decisions {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal scripted decision: %v", err)
		}
		raw[i] = string(b)
	}
	return scriptedOllamaServer(t, raw)
}

func TestRunLoopHandlesAStructuredReplyEndToEnd(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")

	server := scriptedStructuredServer(t,
		stepDecision{Action: "run", Command: fmt.Sprintf("touch %q", target)},
		stepDecision{Action: "done"},
	)
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "create a.txt", neverConfirm(t), &out, &errOut, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected %s to have been created: %v", target, err)
	}
}

func TestRunLoopHandlesAStructuredUnsupportedReply(t *testing.T) {
	server := scriptedStructuredServer(t, stepDecision{Action: "unsupported"})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "edit this image", neverConfirm(t), &out, &errOut, "")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "couldn't work out a command") {
		t.Errorf("stdout missing the unsupported message, got:\n%s", out.String())
	}
}

// proposeStep must ask for the structured format on both the non-streaming
// and the streaming path, so a real server actually constrains the reply.
func TestProposeStepRequestsTheStepFormat(t *testing.T) {
	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&raw)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: `{"action":"done","command":""}`, Done: true, EvalCount: 1})
	}))
	defer server.Close()

	client := ollama.New(server.URL)
	if _, _, err := proposeStep(context.Background(), client, "m", "task", nil, nil, nil); err != nil {
		t.Fatalf("proposeStep: %v", err)
	}
	if raw["format"] == nil {
		t.Error("non-streaming propose call did not request the step format")
	}

	var out bytes.Buffer
	if _, _, err := proposeStep(context.Background(), client, "m", "task", nil, &out, nil); err != nil {
		t.Fatalf("proposeStep (streaming): %v", err)
	}
	if raw["format"] == nil {
		t.Error("streaming propose call did not request the step format")
	}
}
