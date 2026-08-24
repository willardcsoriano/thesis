package undo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildEntrySimpleMove(t *testing.T) {
	before := map[string]bool{"a.txt": true, "b.txt": true}
	after := map[string]bool{"b.txt": true, "sub/a.txt": true}

	e := BuildEntry("/work", "mv a.txt sub/a.txt", before, after)

	if len(e.Moves) != 1 || e.Moves[0] != (Move{OldPath: "a.txt", NewPath: "sub/a.txt"}) {
		t.Fatalf("Moves = %+v, want a single a.txt -> sub/a.txt move", e.Moves)
	}
	if len(e.Created) != 0 || len(e.Unhandled) != 0 {
		t.Errorf("expected no Created/Unhandled, got Created=%v Unhandled=%v", e.Created, e.Unhandled)
	}
}

func TestBuildEntryMkdirAndMoveIntoIt(t *testing.T) {
	// The flagship scenario: `mkdir dest && mv *.log dest` as a single
	// reversible auto-run command.
	before := map[string]bool{"a.log": true, "b.log": true, "c.txt": true}
	after := map[string]bool{"c.txt": true, "dest": true, "dest/a.log": true, "dest/b.log": true}

	e := BuildEntry("/work", "mkdir dest && mv *.log dest", before, after)

	wantMoves := map[Move]bool{
		{OldPath: "a.log", NewPath: "dest/a.log"}: true,
		{OldPath: "b.log", NewPath: "dest/b.log"}: true,
	}
	if len(e.Moves) != 2 {
		t.Fatalf("Moves = %+v, want exactly 2", e.Moves)
	}
	for _, m := range e.Moves {
		if !wantMoves[m] {
			t.Errorf("unexpected move %+v", m)
		}
	}
	if len(e.Created) != 1 || e.Created[0] != "dest" {
		t.Fatalf("Created = %v, want just [\"dest\"] (the directory itself, once its contents pair off as moves)", e.Created)
	}
	if len(e.Unhandled) != 0 {
		t.Errorf("Unhandled = %v, want none", e.Unhandled)
	}
}

func TestBuildEntryPureCreation(t *testing.T) {
	before := map[string]bool{}
	after := map[string]bool{"newfile.txt": true}

	e := BuildEntry("/work", "touch newfile.txt", before, after)

	if len(e.Moves) != 0 {
		t.Errorf("Moves = %+v, want none", e.Moves)
	}
	if len(e.Created) != 1 || e.Created[0] != "newfile.txt" {
		t.Fatalf("Created = %v, want [\"newfile.txt\"]", e.Created)
	}
}

func TestBuildEntryUnmatchedDisappearanceIsUnhandledNotGuessed(t *testing.T) {
	before := map[string]bool{"orphan.txt": true}
	after := map[string]bool{}

	e := BuildEntry("/work", "some command with no visible counterpart", before, after)

	if len(e.Moves) != 0 || len(e.Created) != 0 {
		t.Errorf("expected no Moves/Created, got Moves=%v Created=%v", e.Moves, e.Created)
	}
	if len(e.Unhandled) != 1 || e.Unhandled[0] != "orphan.txt" {
		t.Fatalf("Unhandled = %v, want [\"orphan.txt\"]", e.Unhandled)
	}
}

func TestEntryIsNoop(t *testing.T) {
	if !(Entry{}).IsNoop() {
		t.Error("zero-value Entry should be a no-op")
	}
	if (Entry{Moves: []Move{{OldPath: "a", NewPath: "b"}}}).IsNoop() {
		t.Error("Entry with a Move should not be a no-op")
	}
}

func TestSnapshotSeesOneLevelOfNesting(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "top.txt"))
	mustMkdir(t, filepath.Join(dir, "sub"))
	mustWriteFile(t, filepath.Join(dir, "sub", "inner.txt"))
	mustMkdir(t, filepath.Join(dir, "sub", "deeper"))
	mustWriteFile(t, filepath.Join(dir, "sub", "deeper", "invisible.txt"))

	got, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	for _, want := range []string{"top.txt", "sub", filepath.Join("sub", "inner.txt")} {
		if !got[want] {
			t.Errorf("Snapshot missing expected path %q, got %v", want, got)
		}
	}
	if got[filepath.Join("sub", "deeper", "invisible.txt")] {
		t.Error("Snapshot saw two levels deep; it's documented to only see one")
	}
}

func TestApplyRestoresMkdirAndMoveIntoIt(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.log"))
	mustWriteFile(t, filepath.Join(dir, "b.log"))
	mustWriteFile(t, filepath.Join(dir, "c.txt"))

	before, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot before: %v", err)
	}

	// Simulate what `mkdir dest && mv *.log dest` would have done.
	mustMkdir(t, filepath.Join(dir, "dest"))
	mustRename(t, filepath.Join(dir, "a.log"), filepath.Join(dir, "dest", "a.log"))
	mustRename(t, filepath.Join(dir, "b.log"), filepath.Join(dir, "dest", "b.log"))

	after, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot after: %v", err)
	}

	entry := BuildEntry(dir, "mkdir dest && mv *.log dest", before, after)
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}

	for _, want := range []string{"a.log", "b.log", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %s restored to %s: %v", want, dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "dest")); !os.IsNotExist(err) {
		t.Errorf("expected dest/ removed after undo, stat err = %v", err)
	}
}

func TestApplyRemovesPureCreation(t *testing.T) {
	dir := t.TempDir()
	before, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot before: %v", err)
	}
	mustWriteFile(t, filepath.Join(dir, "newfile.txt"))
	after, err := Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot after: %v", err)
	}

	entry := BuildEntry(dir, "touch newfile.txt", before, after)
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "newfile.txt")); !os.IsNotExist(err) {
		t.Errorf("expected newfile.txt removed after undo, stat err = %v", err)
	}
}

func TestJournalAppendAndPopRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undo.log")

	e1 := Entry{Command: "first", Dir: "/work", Created: []string{"a"}}
	e2 := Entry{Command: "second", Dir: "/work", Created: []string{"b"}}

	if err := AppendJournal(path, e1); err != nil {
		t.Fatalf("AppendJournal e1: %v", err)
	}
	if err := AppendJournal(path, e2); err != nil {
		t.Fatalf("AppendJournal e2: %v", err)
	}

	popped, ok, err := PopLastJournal(path)
	if err != nil || !ok {
		t.Fatalf("PopLastJournal: ok=%v err=%v", ok, err)
	}
	if popped.Command != "second" {
		t.Errorf("popped.Command = %q, want %q (most recent first)", popped.Command, "second")
	}

	popped, ok, err = PopLastJournal(path)
	if err != nil || !ok {
		t.Fatalf("PopLastJournal (second pop): ok=%v err=%v", ok, err)
	}
	if popped.Command != "first" {
		t.Errorf("popped.Command = %q, want %q", popped.Command, "first")
	}

	_, ok, err = PopLastJournal(path)
	if err != nil {
		t.Fatalf("PopLastJournal on empty journal: %v", err)
	}
	if ok {
		t.Error("expected ok=false popping an exhausted journal")
	}
}

func TestPopLastJournalOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.log")
	_, ok, err := PopLastJournal(path)
	if err != nil {
		t.Fatalf("PopLastJournal on missing file: %v", err)
	}
	if ok {
		t.Error("expected ok=false for a journal that doesn't exist yet")
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustRename(t *testing.T, oldpath, newpath string) {
	t.Helper()
	if err := os.Rename(oldpath, newpath); err != nil {
		t.Fatalf("rename %s -> %s: %v", oldpath, newpath, err)
	}
}
