package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/undo"
)

// TestRunUndoNothingToUndo confirms an empty/missing journal is reported
// plainly and never reaches the confirmation gate — there is nothing to
// confirm applying.
func TestRunUndoNothingToUndo(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "does-not-exist.log")
	var out, errOut bytes.Buffer

	code := runUndo(neverConfirm(t), &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "nothing to undo") {
		t.Errorf("stdout = %q, want it to say there's nothing to undo", out.String())
	}
}

// TestRunUndoAppliesMoveOnConfirm is the flagship reversible-undo path:
// preview shows the move, confirming applies it for real, and the journal
// entry is consumed (popped) afterward.
func TestRunUndoAppliesMoveOnConfirm(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dest"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dest", "a.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{
		Timestamp: time.Now(),
		Command:   "mkdir dest && mv a.log dest",
		Dir:       dir,
		Moves:     []undo.Move{{OldPath: "a.log", NewPath: filepath.Join("dest", "a.log")}},
		Created:   []string{"dest"},
	}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	var confirmPrompted bool
	confirmFn := func(prompt string) bool { confirmPrompted = true; return true }

	code := runUndo(confirmFn, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !confirmPrompted {
		t.Fatal("expected the confirmation gate to be consulted")
	}
	if !strings.Contains(out.String(), "move back: "+filepath.Join("dest", "a.log")+" -> a.log") {
		t.Errorf("stdout missing the move preview, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "undo complete.") {
		t.Errorf("stdout missing completion message, got:\n%s", out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "a.log")); err != nil {
		t.Errorf("expected a.log restored to %s: %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dest")); !os.IsNotExist(err) {
		t.Errorf("expected dest/ removed after undo, stat err = %v", err)
	}
	if _, ok, err := undo.PeekLastJournal(journalPath); err != nil || ok {
		t.Errorf("PeekLastJournal after undo: ok=%v err=%v, want ok=false (entry should be popped)", ok, err)
	}
}

// TestRunUndoDeclinedLeavesEverythingIntact confirms a decline touches
// neither the filesystem nor the journal — the entry must still be there
// to try again later.
func TestRunUndoDeclinedLeavesEverythingIntact(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "touch newfile.txt", Dir: dir, Created: []string{"newfile.txt"}}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return false }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (a declined undo is a clean cancel); stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "cancelled.") {
		t.Errorf("stdout missing cancellation message, got:\n%s", out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "newfile.txt")); err != nil {
		t.Errorf("newfile.txt should be untouched by a declined undo: %v", err)
	}
	peeked, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil || !ok {
		t.Fatalf("PeekLastJournal after decline: ok=%v err=%v, want the entry still present", ok, err)
	}
	if peeked.Command != entry.Command {
		t.Errorf("journal entry after decline = %q, want the original %q still there untouched", peeked.Command, entry.Command)
	}
}

// TestRunUndoDisplaysAndRestoresContentBackup covers the "guiltless"
// content-backup entry type end-to-end through the undo subcommand itself,
// not just undo.Apply in isolation.
func TestRunUndoDisplaysAndRestoresContentBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	live := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(live, []byte("port: 8080"), 0o644); err != nil {
		t.Fatalf("write live: %v", err)
	}
	backups, errs := undo.BackupContent([]string{live})
	if len(errs) != 0 || len(backups) != 1 {
		t.Fatalf("BackupContent: backups=%+v errs=%v", backups, errs)
	}
	if err := os.WriteFile(live, []byte("port: 9090 # mutated"), 0o644); err != nil {
		t.Fatalf("simulate mutation: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "sed -i 's/8080/9090/' " + live, Dir: filepath.Dir(live), ContentBackups: backups}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "restore content: "+live) {
		t.Errorf("stdout missing the content-restore preview, got:\n%s", out.String())
	}

	got, err := os.ReadFile(live)
	if err != nil || string(got) != "port: 8080" {
		t.Errorf("restored content = %q (err %v), want the original %q", got, err, "port: 8080")
	}
}

// TestRunUndoDisplaysAndRestoresTrashedItem covers the trash entry type's
// display line through the undo subcommand itself, not just undo.Apply in
// isolation (already covered by loop_test.go's full runLoop scenario).
func TestRunUndoDisplaysAndRestoresTrashedItem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	items, errs := undo.TrashPreserve([]string{src})
	if len(errs) != 0 || len(items) != 1 {
		t.Fatalf("TrashPreserve: items=%+v errs=%v", items, errs)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove src: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "rm " + src, Dir: filepath.Dir(src), Trashed: items}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "restore from trash: "+src) {
		t.Errorf("stdout missing the trash-restore preview, got:\n%s", out.String())
	}
	got, err := os.ReadFile(src)
	if err != nil || string(got) != "keep me" {
		t.Errorf("restored content = %q (err %v), want %q", got, err, "keep me")
	}
}

// TestRunUndoDisplaysAndRestoresMetadataBackup covers the metadata entry
// type's display line through the undo subcommand itself.
func TestRunUndoDisplaysAndRestoresMetadataBackup(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o640); err != nil {
		t.Fatalf("write f: %v", err)
	}
	backups, errs := undo.BackupMetadata([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("BackupMetadata returned errors: %v", errs)
	}
	if err := os.Chmod(f, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "chmod -R 777 " + dir, Dir: dir, MetadataBackups: backups}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "restore permissions: "+f) {
		t.Errorf("stdout missing the permissions-restore preview, got:\n%s", out.String())
	}
	info, err := os.Stat(f)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("restored mode = %v (err %v), want 0640", info.Mode(), err)
	}
}

// TestRunUndoDisplaysAndAppliesGitReset covers the git-reset entry type's
// display line through the undo subcommand itself, against a real
// throwaway repository.
func TestRunUndoDisplaysAndAppliesGitReset(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "first")
	sha, err := undo.CaptureGitHead(dir)
	if err != nil {
		t.Fatalf("CaptureGitHead: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "second")

	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "git reset --hard", Dir: dir, GitReset: sha}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "reset git HEAD back to: "+sha) {
		t.Errorf("stdout missing the git-reset preview, got:\n%s", out.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(got) != "v1" {
		t.Errorf("f.txt after undo = %q (err %v), want %q restored", got, err, "v1")
	}
}

// TestRunUndoShowsUnhandledNote confirms a command whose effects couldn't
// be fully reconstructed still surfaces that fact to the user rather than
// silently applying a partial undo.
func TestRunUndoShowsUnhandledNote(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{Timestamp: time.Now(), Command: "some command", Dir: t.TempDir(), Unhandled: []string{"orphan.txt"}}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "orphan.txt") {
		t.Errorf("stdout missing the unhandled-change note, got:\n%s", out.String())
	}
}

// TestRunUndoPeekErrorIsReported confirms a corrupt journal is reported as
// an error rather than silently treated as empty.
func TestRunUndoPeekErrorIsReported(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "undo.log")
	if err := os.WriteFile(journalPath, []byte("not valid json\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(neverConfirm(t), &out, &errOut, journalPath)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Error("expected an error message on stderr for a corrupt journal")
	}
}

// TestRunUndoPopFailureIsReported confirms a failure removing the entry
// from the journal (not applying it) is also surfaced as an error, not
// silently ignored or treated as success.
func TestRunUndoPopFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	journalPath := filepath.Join(t.TempDir(), "undo.log")

	entry := undo.Entry{Timestamp: time.Now(), Command: "touch newfile.txt", Dir: t.TempDir(), Created: []string{"newfile.txt"}}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}
	// Read-only on the file itself: unlike removing/renaming a directory
	// entry, rewriting an *existing* file's content only needs write
	// permission on the file's own mode bits, not the containing
	// directory — so the permission has to be revoked here, not on the dir.
	if err := os.Chmod(journalPath, 0o444); err != nil {
		t.Fatalf("chmod journalPath: %v", err)
	}
	defer os.Chmod(journalPath, 0o644) // let t.TempDir() clean it up

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout:\n%s", code, out.String())
	}
	if !strings.Contains(errOut.String(), "permission denied") {
		t.Errorf("errOut = %q, want a permission-denied error from PopLastJournal specifically (not some other failure)", errOut.String())
	}
}

// TestRunUndoApplyFailureIsReported confirms a failed Apply is surfaced as
// an error and a nonzero exit, not silently swallowed.
func TestRunUndoApplyFailureIsReported(t *testing.T) {
	journalPath := filepath.Join(t.TempDir(), "undo.log")
	entry := undo.Entry{
		Timestamp: time.Now(),
		Command:   "mv a.log dest/a.log",
		Dir:       filepath.Join(t.TempDir(), "nonexistent-dir"), // Apply's os.Rename must fail here
		Moves:     []undo.Move{{OldPath: "a.log", NewPath: filepath.Join("dest", "a.log")}},
	}
	if err := undo.AppendJournal(journalPath, entry); err != nil {
		t.Fatalf("AppendJournal: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return true }, &out, &errOut, journalPath)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout:\n%s", code, out.String())
	}
	if errOut.Len() == 0 {
		t.Error("expected an error message on stderr when Apply fails")
	}
}
