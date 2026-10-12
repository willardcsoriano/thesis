package undo

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests are the regressions found by the differential test of composition
// (pilot/make_compose.py through cmd/recoverycheck, 2026-10-13): compound lines in
// which part of the line did not run, ran on a file an earlier part had moved, or
// made again a path an earlier part removed. Undo assumed every recorded effect had
// happened, and in each case that made undo itself lose data.

func mustID(t *testing.T, p string) *FileID {
	t.Helper()
	id, ok := IDOf(p)
	if !ok {
		t.Fatalf("no identity for %s", p)
	}
	return &id
}

func TestMoveThatNeverHappenedIsNotMovedBack(t *testing.T) {
	// `test -f nosuch && mv a.txt b.txt`: the move never ran. Moving b.txt onto a.txt
	// would replace a.txt with b's content.
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "a\n", 0o644)
	write(t, filepath.Join(dir, "b.txt"), "b\n", 0o644)
	e := Entry{Dir: dir, Moves: []Move{{OldPath: "a.txt", NewPath: "b.txt", ID: mustID(t, filepath.Join(dir, "a.txt")), NewExisted: true}}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(dir, "a.txt")); got != "a\n" {
		t.Fatalf("a.txt = %q, want it untouched", got)
	}
	if got := read(t, filepath.Join(dir, "b.txt")); got != "b\n" {
		t.Fatalf("b.txt = %q, want it untouched", got)
	}
}

func TestRecreatedPathIsRemovedBeforeTheOriginalIsRestored(t *testing.T) {
	// `rm a.txt || exit 1 ; cp b.txt a.txt`: removing "what the line created" after
	// restoring a.txt from trash deleted the restored original.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	write(t, a, "original\n", 0o644)
	trashed, errs := TrashPreserve([]string{a})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	write(t, a, "made by the line\n", 0o644)
	e := Entry{Dir: dir, Trashed: trashed, Created: []string{"a.txt"}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "original\n" {
		t.Fatalf("a.txt = %q, want the original", got)
	}
}

func TestOriginalLeftInPlaceIsNotRemovedAsACreation(t *testing.T) {
	// `false && rm a.txt && touch a.txt`: nothing ran, and a.txt is still the original,
	// which a creation over it must not be mistaken for.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	write(t, a, "original\n", 0o644)
	trashed, errs := TrashPreserve([]string{a})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	e := Entry{Dir: dir, Trashed: trashed, Created: []string{"a.txt"}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "original\n" {
		t.Fatalf("a.txt = %q, want the original", got)
	}
}

func TestTrashedDirectoryMergesIntoOneMadeAgain(t *testing.T) {
	// `if rm -r logs; then mkdir logs; fi`, and `rm logs/x.log || rm -r logs` where
	// the second part never ran: the directory exists again, and restoring it by
	// rename failed, leaving its files lost.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	write(t, filepath.Join(logs, "x.log"), "x\n", 0o644)
	write(t, filepath.Join(logs, "y.log"), "y\n", 0o644)
	if err := os.Chmod(logs, 0o750); err != nil {
		t.Fatal(err)
	}
	trashed, errs := TrashPreserve([]string{logs})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.Remove(filepath.Join(logs, "x.log")); err != nil { // the part that ran
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Trashed: trashed}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(logs, "x.log")); got != "x\n" {
		t.Fatalf("logs/x.log = %q, want it restored", got)
	}
	if fi, err := os.Stat(logs); err != nil || fi.Mode().Perm() != 0o750 {
		t.Fatalf("logs mode = %v, %v; want 0750", fi, err)
	}
}

func TestChainedMoveIsUndoneAlongTheChain(t *testing.T) {
	// `mv a.txt b.txt ; mv b.txt tmp/`: the second move carries a's file, so its
	// identity is a's, not the original b's.
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a\n", 0o644)
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := mustID(t, a)
	if err := os.Rename(a, filepath.Join(dir, "tmp", "b.txt")); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Moves: []Move{
		{OldPath: "a.txt", NewPath: "b.txt", ID: id},
		{OldPath: "b.txt", NewPath: "tmp/b.txt", ID: id},
	}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "a\n" {
		t.Fatalf("a.txt = %q, want it moved back", got)
	}
	if _, err := os.Lstat(b); !os.IsNotExist(err) {
		t.Fatalf("b.txt should not exist after undo: %v", err)
	}
}

func TestContentRestoreDoesNotWriteThroughARestoredFile(t *testing.T) {
	// `{ mv a.txt b.txt; mv b.txt tmp/; }` where the second move failed: b.txt is a's
	// inode, a.txt comes back from trash as that same inode, and restoring b's content
	// in place wrote b's bytes into a.txt as well.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a\n", 0o644)
	write(t, b, "b\n", 0o644)
	trashed, errs := TrashPreserve([]string{a})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	backups, errs := BackupContent([]string{b})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.Rename(a, b); err != nil { // the first move ran
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Trashed: trashed, ContentBackups: backups}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "a\n" {
		t.Fatalf("a.txt = %q, want a's own content", got)
	}
	if got := read(t, b); got != "b\n" {
		t.Fatalf("b.txt = %q, want b's own content", got)
	}
}

func TestMoveBackMergesIntoADirectoryMadeAgain(t *testing.T) {
	// `mv logs tmp/ ; mkdir logs`: moving tmp/logs back failed because logs existed.
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	write(t, filepath.Join(logs, "x.log"), "x\n", 0o644)
	id := mustID(t, logs)
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(logs, filepath.Join(dir, "tmp", "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logs, 0o777); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Moves: []Move{{OldPath: "logs", NewPath: "tmp/logs", ID: id}}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(logs, "x.log")); got != "x\n" {
		t.Fatalf("logs/x.log = %q, want it back", got)
	}
	if _, err := os.Lstat(filepath.Join(dir, "tmp", "logs")); !os.IsNotExist(err) {
		t.Fatalf("tmp/logs should be gone after undo: %v", err)
	}
}

func TestMoveBackReplacesAFileMadeAtTheVacatedName(t *testing.T) {
	// `mv logs tmp/ ; echo y > logs`: a file now stands where the directory goes back.
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	write(t, filepath.Join(logs, "x.log"), "x\n", 0o644)
	id := mustID(t, logs)
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(logs, filepath.Join(dir, "tmp", "logs")); err != nil {
		t.Fatal(err)
	}
	write(t, logs, "y\n", 0o644)
	e := Entry{Dir: dir, Moves: []Move{{OldPath: "logs", NewPath: "tmp/logs", ID: id}}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(logs, "x.log")); got != "x\n" {
		t.Fatalf("logs/x.log = %q, want the directory back", got)
	}
}

func TestOriginalInsideARestoredDirectoryIsNotRemovedAsACreation(t *testing.T) {
	// `rm -f logs/*.log ; rm -r logs && echo x > logs/x.log`: the creation never
	// happened, and logs/x.log comes back from the trashed directory as the original.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	write(t, filepath.Join(logs, "x.log"), "x\n", 0o644)
	trashed, errs := TrashPreserve([]string{logs})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.RemoveAll(logs); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Trashed: trashed, Created: []string{"logs/x.log"}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(logs, "x.log")); got != "x\n" {
		t.Fatalf("logs/x.log = %q, want the original", got)
	}
}

func TestPipelineMovesGoBackWhicheverOrderTheyRan(t *testing.T) {
	// `mv b.txt tmp/ | mv a.txt b.txt`: the stages race. When a.txt moved first, a's
	// file went on to tmp/b.txt, where it was taken for a leftover and removed.
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a\n", 0o644)
	write(t, b, "b\n", 0o644)
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	idA, idB := mustID(t, a), mustID(t, b)
	backups, errs := BackupContent([]string{b})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := os.Rename(a, b); err != nil { // the second stage ran first
		t.Fatal(err)
	}
	if err := os.Rename(b, filepath.Join(dir, "tmp", "b.txt")); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, ContentBackups: backups, Moves: []Move{
		{OldPath: "b.txt", NewPath: "tmp/b.txt", ID: idB},
		{OldPath: "a.txt", NewPath: "b.txt", ID: idA, NewExisted: true},
	}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "a\n" {
		t.Fatalf("a.txt = %q, want a's file back", got)
	}
	if got := read(t, b); got != "b\n" {
		t.Fatalf("b.txt = %q, want b's content", got)
	}
}

func TestOriginalInsideAMovedDirectoryIsNotRemovedAsACreation(t *testing.T) {
	// `mv logs tmp/ || exit 1 ; echo x > logs/x.log`: the write failed, and the
	// original logs/x.log comes back with the moved directory.
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs")
	write(t, filepath.Join(logs, "x.log"), "x\n", 0o644)
	id, xid := mustID(t, logs), mustID(t, filepath.Join(logs, "x.log"))
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(logs, filepath.Join(dir, "tmp", "logs")); err != nil {
		t.Fatal(err)
	}
	e := Entry{Dir: dir, Moves: []Move{{OldPath: "logs", NewPath: "tmp/logs", ID: id}},
		Created: []string{"logs/x.log"}, Preexisting: map[string]FileID{"logs/x.log": *xid}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(logs, "x.log")); got != "x\n" {
		t.Fatalf("logs/x.log = %q, want the original", got)
	}
}
