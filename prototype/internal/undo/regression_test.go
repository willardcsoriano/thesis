package undo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These four tests are the regressions found by cmd/recoverycheck, which ran real
// commands in a sandbox, applied undo, and diffed the tree. Each reproduces one
// way a command was not restored exactly.

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

// mv notes.txt renamed.txt: the name changed, so basename pairing saw a creation
// and a disappearance, and undo deleted the renamed file. Data loss.
func TestRenameToNewNameIsUndoneByMovingBack(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"), "precious", 0o644)
	before, _ := Snapshot(dir)
	beforeIDs, _ := SnapshotIDs(dir)
	if err := os.Rename(filepath.Join(dir, "notes.txt"), filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	after, _ := Snapshot(dir)
	afterIDs, _ := SnapshotIDs(dir)

	e := BuildEntryIDs(dir, "mv notes.txt renamed.txt", before, after, beforeIDs, afterIDs)
	if len(e.Moves) != 1 || e.Moves[0] != (Move{OldPath: "notes.txt", NewPath: "renamed.txt"}) || len(e.Created) != 0 {
		t.Fatalf("a rename must be a move, got %+v", e)
	}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := read(t, filepath.Join(dir, "notes.txt")); got != "precious" {
		t.Fatalf("content after undo = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); err == nil {
		t.Fatal("renamed.txt should be gone after undo")
	}
}

// The old basename-only entry point must behave as it did, so callers that have
// not adopted identity are not changed.
func TestBuildEntryWithoutIDsKeepsBasenamePairing(t *testing.T) {
	e := BuildEntryIDs("/w", "mv a sub/a", map[string]bool{"a": true}, map[string]bool{"sub/a": true}, nil, nil)
	if len(e.Moves) != 1 {
		t.Fatalf("got %+v", e)
	}
}

// mv a.txt b.txt over an existing b.txt: b's old content is captured, and undo has
// to put a.txt back before restoring b, or the restore writes b's old bytes into the
// moved file and a.txt's content is lost.
func TestMoveOntoExistingRestoresBothFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "AAA", 0o644)
	write(t, b, "BBB", 0o644)

	cbs, errs := BackupContent([]string{b})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Command: "mv a.txt b.txt", Moves: []Move{{OldPath: "a.txt", NewPath: "b.txt"}}, ContentBackups: cbs}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := read(t, a); got != "AAA" {
		t.Fatalf("a.txt = %q, want AAA", got)
	}
	if got := read(t, b); got != "BBB" {
		t.Fatalf("b.txt = %q, want BBB", got)
	}
}

// git reset --hard discards a dirty tracked file. Restoring the file and then
// resetting erased what had just been restored; the reset has to come first.
func TestGitResetHardRestoresDirtyFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	f := filepath.Join(dir, "tracked.txt")
	write(t, f, "v1\n", 0o644)
	run("add", ".")
	run("commit", "-qm", "one")
	write(t, f, "v1\ndirty\n", 0o644)

	head, err := CaptureGitHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	cbs, errs := BackupContent([]string{f})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	run("reset", "--hard")
	if read(t, f) != "v1\n" {
		t.Fatal("test setup: reset should have discarded the change")
	}
	if errs := Apply(Entry{Dir: dir, Command: "git reset --hard", GitReset: head, ContentBackups: cbs}); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := read(t, f); got != "v1\ndirty\n" {
		t.Fatalf("dirty content after undo = %q", got)
	}
}

// A directory hardlinked into the trash came back as 0755 whatever it was.
func TestTrashRestoreKeepsDirectoryMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	d := filepath.Join(dir, "shared")
	write(t, filepath.Join(d, "f.txt"), "x", 0o644)
	if err := os.Chmod(d, 0o775); err != nil {
		t.Fatal(err)
	}
	items, errs := TrashPreserve([]string{d})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.RemoveAll(d); err != nil {
		t.Fatal(err)
	}
	if errs := Apply(Entry{Dir: dir, Command: "rm -r shared", Trashed: items}); len(errs) != 0 {
		t.Fatal(errs)
	}
	fi, err := os.Stat(d)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o775 {
		t.Fatalf("directory mode after undo = %o, want 775", fi.Mode().Perm())
	}
}

// Renaming a directory: the one-level snapshot also lists the files inside it, and
// undoing those nested moves first recreated the old directory so the directory
// itself could no longer be moved back onto it.
func TestRenamedDirectoryIsUndoneWithoutNestedConflict(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "src", "main.go"), "package main", 0o644)
	before, _ := Snapshot(dir)
	beforeIDs, _ := SnapshotIDs(dir)
	if err := os.Rename(filepath.Join(dir, "src"), filepath.Join(dir, "srcmoved")); err != nil {
		t.Fatal(err)
	}
	after, _ := Snapshot(dir)
	afterIDs, _ := SnapshotIDs(dir)
	e := BuildEntryIDs(dir, "mv src srcmoved", before, after, beforeIDs, afterIDs)
	if len(e.Moves) != 1 {
		t.Fatalf("want one directory move, got %+v", e.Moves)
	}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatal(errs)
	}
	if got := read(t, filepath.Join(dir, "src", "main.go")); got != "package main" {
		t.Fatalf("content = %q", got)
	}
}
