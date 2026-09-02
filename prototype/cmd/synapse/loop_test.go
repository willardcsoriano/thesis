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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

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

// TestRunLoopAnnouncesTheExecutionTimeoutUpfront verifies the user is told
// about the per-step time limit before any step runs, not only after one is
// actually killed by it — added 2026-08-24 so a legitimately slow but
// otherwise-fine command (a large find, a slow package mirror) doesn't look
// like the tool silently hung.
func TestRunLoopAnnouncesTheExecutionTimeoutUpfront(t *testing.T) {
	server := scriptedOllamaServer(t, []string{"DONE"})
	defer server.Close()

	client := ollama.New(server.URL)
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "do nothing", neverConfirm(t), &out, &errOut, "")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	want := fmt.Sprintf("each step may run for up to %s", stepExecutionTimeout)
	if !strings.Contains(out.String(), want) {
		t.Errorf("stdout missing upfront timeout notice %q, got:\n%s", want, out.String())
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

// TestRunLoopStreamsTokensWhenEnabled verifies M3b step 4's wiring: with
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
