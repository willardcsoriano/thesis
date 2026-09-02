package undo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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
	if (Entry{ContentBackups: []ContentBackup{{Path: "a"}}}).IsNoop() {
		t.Error("Entry with a ContentBackup should not be a no-op")
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

// TestBackupContentCopiesFileAndReturnsEntry is the core "guiltless"
// content-backup path: a file about to be overwritten by a confirmed
// content-mutating command gets a full copy taken first.
func TestBackupContentCopiesFileAndReturnsEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	src := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(src, []byte("port: 8080"), 0o640); err != nil {
		t.Fatalf("write src: %v", err)
	}

	backups, errs := BackupContent([]string{src})
	if len(errs) != 0 {
		t.Fatalf("BackupContent returned errors: %v", errs)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %+v, want exactly 1", backups)
	}
	b := backups[0]
	if b.Path != src {
		t.Errorf("Path = %q, want %q", b.Path, src)
	}
	if b.Mode != 0o640 {
		t.Errorf("Mode = %v, want 0640", b.Mode)
	}
	got, err := os.ReadFile(b.BackupPath)
	if err != nil || string(got) != "port: 8080" {
		t.Errorf("backup content = %q (err %v), want %q", got, err, "port: 8080")
	}
}

func TestBackupContentSkipsNonexistentPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	backups, errs := BackupContent([]string{filepath.Join(t.TempDir(), "does-not-exist.txt")})
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none (a missing target is not an error, just nothing to protect)", errs)
	}
	if len(backups) != 0 {
		t.Errorf("backups = %+v, want none", backups)
	}
}

func TestBackupContentSkipsDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	backups, errs := BackupContent([]string{t.TempDir()})
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
	if len(backups) != 0 {
		t.Errorf("backups = %+v, want none (a directory isn't a content-mutation target)", backups)
	}
}

func TestBackupContentMultipleFilesGetDistinctBackupPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dirA, dirB := t.TempDir(), t.TempDir()
	srcA := filepath.Join(dirA, "same-name.txt")
	srcB := filepath.Join(dirB, "same-name.txt")
	if err := os.WriteFile(srcA, []byte("A"), 0o644); err != nil {
		t.Fatalf("write srcA: %v", err)
	}
	if err := os.WriteFile(srcB, []byte("B"), 0o644); err != nil {
		t.Fatalf("write srcB: %v", err)
	}

	backups, errs := BackupContent([]string{srcA, srcB})
	if len(errs) != 0 {
		t.Fatalf("BackupContent returned errors: %v", errs)
	}
	if len(backups) != 2 {
		t.Fatalf("backups = %+v, want exactly 2", backups)
	}
	if backups[0].BackupPath == backups[1].BackupPath {
		t.Errorf("both backups landed at the same path %q — two same-basename files must not collide", backups[0].BackupPath)
	}
	contentA, _ := os.ReadFile(backups[0].BackupPath)
	contentB, _ := os.ReadFile(backups[1].BackupPath)
	if string(contentA) != "A" || string(contentB) != "B" {
		t.Errorf("backup contents = %q, %q, want %q, %q (no cross-contamination)", contentA, contentB, "A", "B")
	}
}

func TestDefaultContentBackupDirFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := DefaultContentBackupDir(); err == nil {
		t.Error("DefaultContentBackupDir with no $HOME: want an error, got nil")
	}
}

func TestDefaultContentBackupDirFailsWhenHomeIsAFile(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write notADir: %v", err)
	}
	t.Setenv("HOME", notADir)

	if _, err := DefaultContentBackupDir(); err == nil {
		t.Error("DefaultContentBackupDir with $HOME pointing at a regular file: want an error, got nil")
	}
}

func TestBackupContentFailsWhenBackupDirUnavailable(t *testing.T) {
	t.Setenv("HOME", "")

	backups, errs := BackupContent([]string{"/whatever"})
	if len(errs) == 0 {
		t.Error("expected an error when the backup dir itself can't be resolved, got none")
	}
	if len(backups) != 0 {
		t.Errorf("backups = %+v, want none", backups)
	}
}

func TestCopyFileReadFailureIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	// os.Open succeeds on a directory; reading from it during io.Copy does
	// not — this exercises the copy failure path distinct from open/create.
	if err := copyFile(dir, filepath.Join(dir, "out"), 0o644); err == nil {
		t.Error("copyFile with a directory as src: want an error, got nil")
	}
}

func TestBackupContentSurfacesCopyFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	backupHome := t.TempDir()
	t.Setenv("HOME", backupHome)

	// Make the backup directory itself read-only after it's created, so the
	// write inside BackupContent fails.
	backupDir := filepath.Join(backupHome, ".synapse", "content-backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backupDir: %v", err)
	}
	if err := os.Chmod(backupDir, 0o555); err != nil {
		t.Fatalf("chmod backupDir: %v", err)
	}
	defer os.Chmod(backupDir, 0o755) // let t.TempDir() clean it up

	src := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	backups, errs := BackupContent([]string{src})
	if len(errs) == 0 {
		t.Error("expected an error when the backup directory isn't writable, got none")
	}
	if len(backups) != 0 {
		t.Errorf("backups = %+v, want none (the copy failed)", backups)
	}
}

// TestApplyRestoresContentBackup is the restore half of the "guiltless"
// path: a file whose content was overwritten in place (no directory-diff
// signature at all — it never disappeared) gets its original bytes and
// mode back.
func TestApplyRestoresContentBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	live := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(live, []byte("port: 8080"), 0o640); err != nil {
		t.Fatalf("write live: %v", err)
	}

	backups, errs := BackupContent([]string{live})
	if len(errs) != 0 || len(backups) != 1 {
		t.Fatalf("BackupContent: backups=%+v errs=%v", backups, errs)
	}

	// Simulate the confirmed command actually mutating the file in place.
	if err := os.WriteFile(live, []byte("port: 9090 # mutated"), 0o600); err != nil {
		t.Fatalf("simulate mutation: %v", err)
	}

	entry := Entry{Command: "sed -i 's/8080/9090/' " + live, Dir: filepath.Dir(live), ContentBackups: backups}
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}

	got, err := os.ReadFile(live)
	if err != nil || string(got) != "port: 8080" {
		t.Errorf("restored content = %q (err %v), want %q", got, err, "port: 8080")
	}
	info, err := os.Stat(live)
	if err != nil || info.Mode() != 0o640 {
		t.Errorf("restored mode = %v (err %v), want 0640", info.Mode(), err)
	}
}

func TestApplyContentBackupRestoreFailureIsSurfaced(t *testing.T) {
	entry := Entry{
		ContentBackups: []ContentBackup{{Path: "/nonexistent/config.yaml", BackupPath: "/nonexistent/backup", Mode: 0o644}},
	}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error restoring from a nonexistent backup path, got none")
	}
}

// TestTrashPreserveHardlinksRegularFile confirms preservation is a true
// hardlink (same inode), not a copy — the whole point of choosing this
// mechanism over BackupContent for deletions.
func TestTrashPreserveHardlinksRegularFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}

	items, errs := TrashPreserve([]string{src})
	if len(errs) != 0 {
		t.Fatalf("TrashPreserve returned errors: %v", errs)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly 1", items)
	}
	trashInfo, err := os.Stat(items[0].TrashPath)
	if err != nil {
		t.Fatalf("stat trash path: %v", err)
	}
	if !os.SameFile(srcInfo, trashInfo) {
		t.Error("trash path is not the same inode as the original — this should be a hardlink, not a copy")
	}

	// The real point: removing the original must not affect the trash
	// copy's content, since rm only removes a directory entry.
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove src: %v", err)
	}
	got, err := os.ReadFile(items[0].TrashPath)
	if err != nil || string(got) != "keep me" {
		t.Errorf("trash content after removing the original = %q (err %v), want %q still intact", got, err, "keep me")
	}
}

func TestTrashPreserveHandlesDirectoryTreeWithoutCopyingData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir := filepath.Join(t.TempDir(), "doomed-dir")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("top"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	items, errs := TrashPreserve([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("TrashPreserve returned errors: %v", errs)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v, want exactly 1", items)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove original dir: %v", err)
	}

	top, err := os.ReadFile(filepath.Join(items[0].TrashPath, "top.txt"))
	if err != nil || string(top) != "top" {
		t.Errorf("trashed top.txt = %q (err %v), want %q", top, err, "top")
	}
	inner, err := os.ReadFile(filepath.Join(items[0].TrashPath, "sub", "inner.txt"))
	if err != nil || string(inner) != "inner" {
		t.Errorf("trashed sub/inner.txt = %q (err %v), want %q", inner, err, "inner")
	}
}

func TestTrashPreserveSkipsNonexistentPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	items, errs := TrashPreserve([]string{filepath.Join(t.TempDir(), "does-not-exist.txt")})
	if len(errs) != 0 {
		t.Errorf("errs = %v, want none (a missing target is not an error, just nothing to protect)", errs)
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want none", items)
	}
}

func TestTrashPreserveFailsWhenTrashDirUnavailable(t *testing.T) {
	t.Setenv("HOME", "")

	items, errs := TrashPreserve([]string{"/whatever"})
	if len(errs) == 0 {
		t.Error("expected an error when the trash dir itself can't be resolved, got none")
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want none", items)
	}
}

func TestTrashPreserveMultipleFilesGetDistinctTrashPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dirA, dirB := t.TempDir(), t.TempDir()
	srcA := filepath.Join(dirA, "same-name.txt")
	srcB := filepath.Join(dirB, "same-name.txt")
	if err := os.WriteFile(srcA, []byte("A"), 0o644); err != nil {
		t.Fatalf("write srcA: %v", err)
	}
	if err := os.WriteFile(srcB, []byte("B"), 0o644); err != nil {
		t.Fatalf("write srcB: %v", err)
	}

	items, errs := TrashPreserve([]string{srcA, srcB})
	if len(errs) != 0 {
		t.Fatalf("TrashPreserve returned errors: %v", errs)
	}
	if len(items) != 2 || items[0].TrashPath == items[1].TrashPath {
		t.Fatalf("items = %+v, want 2 with distinct trash paths", items)
	}
}

// TestApplyRestoresTrashedItem is the restore half: the original is gone
// (rm ran for real), and Apply must move it back from trash.
func TestApplyRestoresTrashedItem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	items, errs := TrashPreserve([]string{src})
	if len(errs) != 0 || len(items) != 1 {
		t.Fatalf("TrashPreserve: items=%+v errs=%v", items, errs)
	}
	if err := os.Remove(src); err != nil { // simulate the confirmed rm actually running
		t.Fatalf("remove src: %v", err)
	}

	entry := Entry{Command: "rm " + src, Dir: filepath.Dir(src), Trashed: items}
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}

	got, err := os.ReadFile(src)
	if err != nil || string(got) != "keep me" {
		t.Errorf("restored content = %q (err %v), want %q", got, err, "keep me")
	}
}

func TestApplyRestoresTrashedItemRecreatingRemovedParentDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	src := filepath.Join(parent, "doomed.txt")
	if err := os.WriteFile(src, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	items, errs := TrashPreserve([]string{src})
	if len(errs) != 0 || len(items) != 1 {
		t.Fatalf("TrashPreserve: items=%+v errs=%v", items, errs)
	}
	if err := os.RemoveAll(parent); err != nil { // simulate rm -r removing the whole parent
		t.Fatalf("remove parent: %v", err)
	}

	entry := Entry{Command: "rm -r " + parent, Dir: filepath.Dir(parent), Trashed: items}
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}
	got, err := os.ReadFile(src)
	if err != nil || string(got) != "keep me" {
		t.Errorf("restored content = %q (err %v), want %q", got, err, "keep me")
	}
}

func TestDefaultTrashDirFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := DefaultTrashDir(); err == nil {
		t.Error("DefaultTrashDir with no $HOME: want an error, got nil")
	}
}

func TestDefaultTrashDirFailsWhenHomeIsAFile(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write notADir: %v", err)
	}
	t.Setenv("HOME", notADir)

	if _, err := DefaultTrashDir(); err == nil {
		t.Error("DefaultTrashDir with $HOME pointing at a regular file: want an error, got nil")
	}
}

func TestTrashPreserveFailsWhenTrashDirUnwritable(t *testing.T) {
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
	defer os.Chmod(trashDir, 0o755) // let t.TempDir() clean it up

	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	items, errs := TrashPreserve([]string{src})
	if len(errs) == 0 {
		t.Error("expected an error when the trash directory isn't writable, got none")
	}
	if len(items) != 0 {
		t.Errorf("items = %+v, want none", items)
	}
}

// TestPreserveViaHardlinkFallsBackToCopyOnCrossDevice simulates EXDEV
// (linkFunc stubbed, since a real cross-device link isn't reproducible in
// a single-filesystem test sandbox) and confirms the fallback still
// produces a correct, independent copy of the file's content.
func TestPreserveViaHardlinkFallsBackToCopyOnCrossDevice(t *testing.T) {
	orig := linkFunc
	linkFunc = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	defer func() { linkFunc = orig }()

	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("cross-device"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "trashed.txt")
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}

	if err := preserveViaHardlink(src, dst, info); err != nil {
		t.Fatalf("preserveViaHardlink returned an error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "cross-device" {
		t.Errorf("copied content = %q (err %v), want %q", got, err, "cross-device")
	}
}

func TestPreserveViaHardlinkSurfacesNonEXDEVError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "doomed.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}
	// A destination whose parent directory doesn't exist: os.Link fails
	// with ENOENT, not EXDEV.
	dst := filepath.Join(t.TempDir(), "no-such-subdir", "trashed.txt")

	if err := preserveViaHardlink(src, dst, info); err == nil {
		t.Error("preserveViaHardlink with an uncreatable dst: want an error, got nil")
	}
}

func TestHardlinkTreeSurfacesWalkError(t *testing.T) {
	err := hardlinkTree(filepath.Join(t.TempDir(), "does-not-exist"), filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Error("hardlinkTree over a nonexistent source dir: want an error, got nil")
	}
}

func TestHardlinkTreeFallsBackToCopyOnCrossDeviceForFileInsideTree(t *testing.T) {
	orig := linkFunc
	linkFunc = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
	defer func() { linkFunc = orig }()

	srcDir := filepath.Join(t.TempDir(), "doomed-dir")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "inner.txt"), []byte("cross-device"), 0o644); err != nil {
		t.Fatalf("write inner.txt: %v", err)
	}
	dstDir := filepath.Join(t.TempDir(), "trashed-dir")

	if err := hardlinkTree(srcDir, dstDir); err != nil {
		t.Fatalf("hardlinkTree returned an error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "inner.txt"))
	if err != nil || string(got) != "cross-device" {
		t.Errorf("copied content = %q (err %v), want %q", got, err, "cross-device")
	}
}

func TestHardlinkTreeSurfacesNonEXDEVLinkError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	srcDir := filepath.Join(t.TempDir(), "doomed-dir")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir srcDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write inner.txt: %v", err)
	}

	dstDir := filepath.Join(t.TempDir(), "trashed-dir")
	if err := os.MkdirAll(dstDir, 0o555); err != nil { // read+execute only: linking a new entry inside it fails
		t.Fatalf("mkdir dstDir: %v", err)
	}
	defer os.Chmod(dstDir, 0o755) // let t.TempDir() clean it up

	if err := hardlinkTree(srcDir, dstDir); err == nil {
		t.Error("hardlinkTree into a read-only dst dir: want an error, got nil")
	}
}

func TestApplyTrashRestoreMkdirAllFailureIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	entry := Entry{
		Trashed: []TrashedItem{{
			OriginalPath: filepath.Join(blocker, "sub", "doomed.txt"), // can't mkdir under a regular file
			TrashPath:    filepath.Join(dir, "trash-path"),
		}},
	}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error when the original path's parent can't be recreated, got none")
	}
}

// TestBackupMetadataCapturesModeAndOwnership confirms metadata backup
// records the file's mode/uid/gid without touching its content at all —
// the whole point of this being the cheapest of the three backup shapes.
func TestBackupMetadataCapturesModeAndOwnership(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("keep me"), 0o640); err != nil {
		t.Fatalf("write f: %v", err)
	}
	wantInfo, err := os.Stat(f)
	if err != nil {
		t.Fatalf("stat f: %v", err)
	}
	wantStat := wantInfo.Sys().(*syscall.Stat_t)

	backups, errs := BackupMetadata([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("BackupMetadata returned errors: %v", errs)
	}

	var found *MetadataBackup
	for i := range backups {
		if backups[i].Path == f {
			found = &backups[i]
		}
	}
	if found == nil {
		t.Fatalf("backups = %+v, want an entry for %s", backups, f)
	}
	if found.Mode != 0o640 {
		t.Errorf("Mode = %v, want 0640", found.Mode)
	}
	if found.UID != int(wantStat.Uid) || found.GID != int(wantStat.Gid) {
		t.Errorf("UID/GID = %d/%d, want %d/%d", found.UID, found.GID, wantStat.Uid, wantStat.Gid)
	}

	// The directory itself (dirs are always included, since chmod -R
	// changes the target directory's own mode too, not just its contents).
	var foundDir bool
	for _, b := range backups {
		if b.Path == dir {
			foundDir = true
		}
	}
	if !foundDir {
		t.Errorf("backups = %+v, want an entry for the directory itself (%s)", backups, dir)
	}
}

func TestBackupMetadataSurfacesWalkError(t *testing.T) {
	backups, errs := BackupMetadata([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if len(errs) == 0 {
		t.Error("BackupMetadata over a nonexistent dir: want an error, got none")
	}
	if len(backups) != 0 {
		t.Errorf("backups = %+v, want none", backups)
	}
}

func TestApplyRestoresMetadata(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chown to the current uid/gid is a no-op test, and root can chown to anything anyway")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o640); err != nil {
		t.Fatalf("write f: %v", err)
	}
	backups, errs := BackupMetadata([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("BackupMetadata returned errors: %v", errs)
	}

	if err := os.Chmod(f, 0o777); err != nil { // simulate the confirmed chmod -R actually running
		t.Fatalf("chmod: %v", err)
	}

	entry := Entry{Command: "chmod -R 777 " + dir, Dir: dir, MetadataBackups: backups}
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}

	info, err := os.Stat(f)
	if err != nil || info.Mode() != 0o640 {
		t.Errorf("restored mode = %v (err %v), want 0640", info.Mode(), err)
	}
}

func TestApplyMetadataRestoreFailureIsSurfaced(t *testing.T) {
	entry := Entry{
		MetadataBackups: []MetadataBackup{{Path: filepath.Join(t.TempDir(), "does-not-exist.txt"), Mode: 0o644, UID: os.Getuid(), GID: os.Getgid()}},
	}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error restoring metadata on a nonexistent path, got none")
	}
}

// TestCaptureGitHeadReturnsCurrentCommit confirms the capture side against
// a real (throwaway) git repository rather than assuming git's behavior.
func TestCaptureGitHeadReturnsCurrentCommit(t *testing.T) {
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
	runGit("commit", "-q", "-m", "init")

	wantSHA := new(strings.Builder)
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	wantSHA.Write(out)

	got, err := CaptureGitHead(dir)
	if err != nil {
		t.Fatalf("CaptureGitHead returned error: %v", err)
	}
	if got != strings.TrimSpace(wantSHA.String()) {
		t.Errorf("CaptureGitHead = %q, want %q", got, strings.TrimSpace(wantSHA.String()))
	}
}

func TestCaptureGitHeadLaunchFailureIsSurfaced(t *testing.T) {
	if _, err := CaptureGitHead(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("CaptureGitHead with a nonexistent dir: want an error, got nil")
	}
}

func TestCaptureGitHeadOnNonGitDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	if _, err := CaptureGitHead(t.TempDir()); err == nil {
		t.Error("CaptureGitHead on a non-git directory: want an error, got nil")
	}
}

// TestApplyRestoresGitReset confirms the restore half through a real
// throwaway repository: after a simulated confirmed reset, Apply's
// `git reset --hard <sha>` genuinely brings the file content back.
func TestApplyRestoresGitReset(t *testing.T) {
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

	sha, err := CaptureGitHead(dir)
	if err != nil {
		t.Fatalf("CaptureGitHead: %v", err)
	}

	// Simulate the confirmed `git reset --hard` running against a second commit.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-q", "-m", "second")

	entry := Entry{Command: "git reset --hard", Dir: dir, GitReset: sha}
	if errs := Apply(entry); len(errs) != 0 {
		t.Fatalf("Apply returned errors: %v", errs)
	}

	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(got) != "v1" {
		t.Errorf("f.txt after undo = %q (err %v), want the original %q restored", got, err, "v1")
	}
}

func TestApplyGitResetFailureIsSurfaced(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in this environment")
	}
	entry := Entry{Command: "git reset --hard", Dir: t.TempDir(), GitReset: "not-a-real-sha"}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error resetting to a nonexistent commit, got none")
	}
}

func TestApplyGitResetLaunchFailureIsSurfaced(t *testing.T) {
	entry := Entry{Command: "git reset --hard", Dir: filepath.Join(t.TempDir(), "does-not-exist"), GitReset: "deadbeef"}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error when the repository directory doesn't exist, got none")
	}
}

func TestApplyTrashRestoreFailureIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	entry := Entry{
		Trashed: []TrashedItem{{
			OriginalPath: filepath.Join(dir, "restored", "doomed.txt"), // parent is creatable
			TrashPath:    filepath.Join(dir, "trash-path-that-was-never-created"),
		}},
	}
	errs := Apply(entry)
	if len(errs) == 0 {
		t.Error("expected an error restoring from a nonexistent trash path, got none")
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
