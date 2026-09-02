package typedops

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"synapseos/internal/classifier"
)

func writeFile(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestLookup(t *testing.T) {
	if Lookup("move_files") == nil {
		t.Error(`Lookup("move_files") = nil, want the registered op`)
	}
	if Lookup("does_not_exist") != nil {
		t.Error(`Lookup("does_not_exist") = non-nil, want nil`)
	}
}

func TestToolsMirrorsRegistry(t *testing.T) {
	tools := Tools()
	if len(tools) != len(Registry) {
		t.Fatalf("Tools() returned %d entries, want %d", len(tools), len(Registry))
	}
	for i, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("Tools()[%d].Type = %q, want function", i, tool.Type)
		}
		if tool.Function.Name != Registry[i].Name {
			t.Errorf("Tools()[%d].Function.Name = %q, want %q", i, tool.Function.Name, Registry[i].Name)
		}
	}
}

func TestFindFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeFile(t, filepath.Join(dir, "recent.pdf"), "x", now)
	writeFile(t, filepath.Join(dir, "old.pdf"), "x", now.AddDate(0, 0, -10))
	writeFile(t, filepath.Join(dir, "recent.txt"), "x", now)

	res, err := findFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.pdf", "modified_within_days": float64(7)})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 1 || filepath.Base(res.AffectedPaths[0]) != "recent.pdf" {
		t.Errorf("AffectedPaths = %v, want only recent.pdf", res.AffectedPaths)
	}

	if v, _ := findFilesOp.Classify(nil); v != classifier.Reversible {
		t.Errorf("find_files Classify = %v, want Reversible", v)
	}
}

func TestFindFilesWithNoAgeLimitReturnsEveryMatch(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeFile(t, filepath.Join(dir, "recent.pdf"), "x", now)
	writeFile(t, filepath.Join(dir, "old.pdf"), "x", now.AddDate(0, 0, -100))

	res, err := findFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.pdf"}) // modified_within_days omitted
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 2 {
		t.Errorf("AffectedPaths = %v, want both files (no age limit given)", res.AffectedPaths)
	}
}

func TestFindFilesMissingArg(t *testing.T) {
	if _, err := findFilesOp.Dispatch(map[string]any{"dir": t.TempDir()}); err == nil {
		t.Error("Dispatch with no pattern: want an error, got nil")
	}
}

func TestFindFilesMissingDir(t *testing.T) {
	if _, err := findFilesOp.Dispatch(map[string]any{"pattern": "*"}); err == nil {
		t.Error("Dispatch with no dir: want an error, got nil")
	}
}

func TestFindFilesNonexistentDir(t *testing.T) {
	if _, err := findFilesOp.Dispatch(map[string]any{"dir": filepath.Join(t.TempDir(), "nope"), "pattern": "*"}); err == nil {
		t.Error("Dispatch against a nonexistent dir: want an error, got nil")
	}
}

func TestFindFilesSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.pdf"), "x", time.Now())
	if err := os.Mkdir(filepath.Join(dir, "sub.pdf"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res, err := findFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.pdf"})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 1 || filepath.Base(res.AffectedPaths[0]) != "a.pdf" {
		t.Errorf("AffectedPaths = %v, want only a.pdf (a same-pattern subdirectory must be skipped)", res.AffectedPaths)
	}
}

func TestFindFilesBadPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.pdf"), "x", time.Now())
	if _, err := findFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "["}); err == nil {
		t.Error("Dispatch with a malformed glob pattern: want an error, got nil")
	}
}

func TestMoveFiles(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "Screenshots") // deliberately doesn't exist yet
	writeFile(t, filepath.Join(src, "shot1.png"), "x", time.Now())
	writeFile(t, filepath.Join(src, "shot2.png"), "x", time.Now())
	writeFile(t, filepath.Join(src, "notes.txt"), "x", time.Now())

	res, err := moveFilesOp.Dispatch(map[string]any{"source_dir": src, "pattern": "*.png", "dest_dir": dst})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	sort.Strings(res.AffectedPaths)
	if len(res.AffectedPaths) != 2 {
		t.Fatalf("AffectedPaths = %v, want 2 moved files", res.AffectedPaths)
	}
	if _, err := os.Stat(filepath.Join(dst, "shot1.png")); err != nil {
		t.Errorf("shot1.png not found in dest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "shot1.png")); !os.IsNotExist(err) {
		t.Error("shot1.png still present in source after move")
	}
	if _, err := os.Stat(filepath.Join(src, "notes.txt")); err != nil {
		t.Error("notes.txt should have been left in place (doesn't match *.png)")
	}

	if v, _ := moveFilesOp.Classify(nil); v != classifier.Reversible {
		t.Errorf("move_files Classify = %v, want Reversible", v)
	}
}

func TestMoveFilesMissingArgs(t *testing.T) {
	full := map[string]any{"source_dir": "a", "pattern": "*", "dest_dir": "b"}
	for _, missing := range []string{"source_dir", "pattern", "dest_dir"} {
		args := map[string]any{}
		for k, v := range full {
			if k != missing {
				args[k] = v
			}
		}
		if _, err := moveFilesOp.Dispatch(args); err == nil {
			t.Errorf("Dispatch with %q missing: want an error, got nil", missing)
		}
	}
}

func TestMoveFilesNonexistentSourceDir(t *testing.T) {
	if _, err := moveFilesOp.Dispatch(map[string]any{"source_dir": filepath.Join(t.TempDir(), "nope"), "pattern": "*", "dest_dir": t.TempDir()}); err == nil {
		t.Error("Dispatch against a nonexistent source_dir: want an error, got nil")
	}
}

func TestMoveFilesSkipsSubdirectories(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.Mkdir(filepath.Join(src, "sub.png"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := moveFilesOp.Dispatch(map[string]any{"source_dir": src, "pattern": "*.png", "dest_dir": dst})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 0 {
		t.Errorf("AffectedPaths = %v, want none (a same-pattern subdirectory must be skipped)", res.AffectedPaths)
	}
}

func TestMoveFilesBadPattern(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.png"), "x", time.Now())
	if _, err := moveFilesOp.Dispatch(map[string]any{"source_dir": src, "pattern": "[", "dest_dir": t.TempDir()}); err == nil {
		t.Error("Dispatch with a malformed glob pattern: want an error, got nil")
	}
}

func TestMoveFilesMkdirAllFailure(t *testing.T) {
	src := t.TempDir()
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	dst := filepath.Join(blocker, "sub") // can't mkdir under a regular file

	if _, err := moveFilesOp.Dispatch(map[string]any{"source_dir": src, "pattern": "*", "dest_dir": dst}); err == nil {
		t.Error("Dispatch with an uncreatable dest_dir: want an error, got nil")
	}
}

func TestMoveFilesRenameFailureIsSurfaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "readonly")
	writeFile(t, filepath.Join(src, "a.png"), "x", time.Now())
	if err := os.Mkdir(dst, 0o555); err != nil { // exists but not writable
		t.Fatalf("mkdir readonly dst: %v", err)
	}
	defer os.Chmod(dst, 0o755) // let t.TempDir() clean it up

	if _, err := moveFilesOp.Dispatch(map[string]any{"source_dir": src, "pattern": "*.png", "dest_dir": dst}); err == nil {
		t.Error("Dispatch into a read-only dest_dir: want an error, got nil")
	}
}

func TestDeleteFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeFile(t, filepath.Join(dir, "old.tmp"), "x", now.AddDate(0, 0, -2))
	writeFile(t, filepath.Join(dir, "new.tmp"), "x", now)
	writeFile(t, filepath.Join(dir, "old.log"), "x", now.AddDate(0, 0, -2))

	res, err := deleteFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.tmp", "older_than_days": float64(1)})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 1 || filepath.Base(res.AffectedPaths[0]) != "old.tmp" {
		t.Errorf("AffectedPaths = %v, want only old.tmp deleted", res.AffectedPaths)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.tmp")); err != nil {
		t.Error("new.tmp should not have been deleted (not old enough)")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.log")); err != nil {
		t.Error("old.log should not have been deleted (doesn't match *.tmp)")
	}

	if v, reason := deleteFilesOp.Classify(nil); v != classifier.Irreversible || reason == "" {
		t.Errorf("delete_files Classify = (%v, %q), want (Irreversible, non-empty reason)", v, reason)
	}
}

func TestDeleteFilesMissingArgs(t *testing.T) {
	full := map[string]any{"dir": "a", "pattern": "*"}
	for _, missing := range []string{"dir", "pattern"} {
		args := map[string]any{}
		for k, v := range full {
			if k != missing {
				args[k] = v
			}
		}
		if _, err := deleteFilesOp.Dispatch(args); err == nil {
			t.Errorf("Dispatch with %q missing: want an error, got nil", missing)
		}
	}
}

func TestDeleteFilesNonexistentDir(t *testing.T) {
	if _, err := deleteFilesOp.Dispatch(map[string]any{"dir": filepath.Join(t.TempDir(), "nope"), "pattern": "*"}); err == nil {
		t.Error("Dispatch against a nonexistent dir: want an error, got nil")
	}
}

func TestDeleteFilesSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub.tmp"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := deleteFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.tmp"})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 0 {
		t.Errorf("AffectedPaths = %v, want none (a same-pattern subdirectory must be skipped)", res.AffectedPaths)
	}
}

func TestDeleteFilesBadPattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.tmp"), "x", time.Now())
	if _, err := deleteFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "["}); err == nil {
		t.Error("Dispatch with a malformed glob pattern: want an error, got nil")
	}
}

func TestDeleteFilesBadOlderThanDays(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.tmp"), "x", time.Now())
	if _, err := deleteFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.tmp", "older_than_days": "not a number"}); err == nil {
		t.Error("Dispatch with a non-numeric older_than_days: want an error, got nil")
	}
}

func TestDeleteFilesRemoveFailureIsSurfaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.tmp"), "x", time.Now())
	if err := os.Chmod(dir, 0o555); err != nil { // read+execute only: entries can't be removed
		t.Fatalf("chmod dir: %v", err)
	}
	defer os.Chmod(dir, 0o755) // let t.TempDir() clean it up

	if _, err := deleteFilesOp.Dispatch(map[string]any{"dir": dir, "pattern": "*.tmp"}); err == nil {
		t.Error("Dispatch against a read-only dir: want an error, got nil")
	}
}

func TestRenameFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jpeg"), "x", time.Now())
	writeFile(t, filepath.Join(dir, "b.jpeg"), "x", time.Now())
	writeFile(t, filepath.Join(dir, "c.png"), "x", time.Now())

	res, err := renameFilesOp.Dispatch(map[string]any{"dir": dir, "old_ext": ".jpeg", "new_ext": ".jpg"})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 2 {
		t.Fatalf("AffectedPaths = %v, want 2 renamed files", res.AffectedPaths)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jpg")); err != nil {
		t.Error("a.jpg not found after rename")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.jpeg")); !os.IsNotExist(err) {
		t.Error("a.jpeg still present after rename")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.png")); err != nil {
		t.Error("c.png should have been left in place (doesn't match extension)")
	}
}

func TestRenameFilesMissingArgs(t *testing.T) {
	full := map[string]any{"dir": "a", "old_ext": ".jpeg", "new_ext": ".jpg"}
	for _, missing := range []string{"dir", "old_ext", "new_ext"} {
		args := map[string]any{}
		for k, v := range full {
			if k != missing {
				args[k] = v
			}
		}
		if _, err := renameFilesOp.Dispatch(args); err == nil {
			t.Errorf("Dispatch with %q missing: want an error, got nil", missing)
		}
	}
}

func TestRenameFilesNonexistentDir(t *testing.T) {
	if _, err := renameFilesOp.Dispatch(map[string]any{"dir": filepath.Join(t.TempDir(), "nope"), "old_ext": ".jpeg", "new_ext": ".jpg"}); err == nil {
		t.Error("Dispatch against a nonexistent dir: want an error, got nil")
	}
}

func TestRenameFilesSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub.jpeg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := renameFilesOp.Dispatch(map[string]any{"dir": dir, "old_ext": ".jpeg", "new_ext": ".jpg"})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 0 {
		t.Errorf("AffectedPaths = %v, want none (a same-extension subdirectory must be skipped)", res.AffectedPaths)
	}
}

func TestRenameFilesRenameFailureIsSurfaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jpeg"), "x", time.Now())
	if err := os.Chmod(dir, 0o555); err != nil { // read+execute only: entries can't be renamed
		t.Fatalf("chmod dir: %v", err)
	}
	defer os.Chmod(dir, 0o755) // let t.TempDir() clean it up

	if _, err := renameFilesOp.Dispatch(map[string]any{"dir": dir, "old_ext": ".jpeg", "new_ext": ".jpg"}); err == nil {
		t.Error("Dispatch against a read-only dir: want an error, got nil")
	}
}

func TestRenameFilesClassify(t *testing.T) {
	if v, reason := renameFilesOp.Classify(nil); v != classifier.Reversible || reason != "" {
		t.Errorf("rename_files Classify = (%v, %q), want (Reversible, \"\")", v, reason)
	}
}

func TestRenameFilesNormalizesExtensionsWithoutDot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jpeg"), "x", time.Now())

	res, err := renameFilesOp.Dispatch(map[string]any{"dir": dir, "old_ext": "jpeg", "new_ext": "jpg"})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 1 || filepath.Base(res.AffectedPaths[0]) != "a.jpg" {
		t.Errorf("AffectedPaths = %v, want a.jpg (extensions without a leading dot should still work)", res.AffectedPaths)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(src, []byte("port: 8080"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "config.yaml.bak")

	if v, reason := copyFileOp.Classify(map[string]any{"dst": dst}); v != classifier.Reversible || reason != "" {
		t.Errorf("Classify on fresh dst = (%v, %q), want (Reversible, \"\")", v, reason)
	}

	res, err := copyFileOp.Dispatch(map[string]any{"src": src, "dst": dst})
	if err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
	if len(res.AffectedPaths) != 1 || res.AffectedPaths[0] != dst {
		t.Errorf("AffectedPaths = %v, want [%s]", res.AffectedPaths, dst)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "port: 8080" {
		t.Errorf("copied file content = %q (err %v), want %q", got, err, "port: 8080")
	}
}

func TestCopyFileClassifyFlagsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "config.yaml.bak")
	if err := os.WriteFile(dst, []byte("old backup"), 0o644); err != nil {
		t.Fatalf("seed existing dst: %v", err)
	}

	v, reason := copyFileOp.Classify(map[string]any{"dst": dst})
	if v != classifier.Irreversible {
		t.Errorf("Classify on existing dst = %v, want Irreversible", v)
	}
	if reason == "" {
		t.Error("Classify on existing dst: reason is empty, want an explanation suitable for a confirmation prompt")
	}
}

func TestCopyFileClassifyWithMissingDstFallsBackToReversible(t *testing.T) {
	// Malformed args are a Dispatch-time concern (it returns the real
	// error); Classify degrades to Reversible rather than guessing.
	if v, reason := copyFileOp.Classify(map[string]any{}); v != classifier.Reversible || reason != "" {
		t.Errorf("Classify with no dst = (%v, %q), want (Reversible, \"\")", v, reason)
	}
}

func TestCopyFileCreateFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	dst := filepath.Join(dir, "no-such-subdir", "out") // parent doesn't exist: os.Create must fail

	if _, err := copyFileOp.Dispatch(map[string]any{"src": src, "dst": dst}); err == nil {
		t.Error("Dispatch with an uncreatable dst: want an error, got nil")
	}
}

func TestCopyFileReadFailureIsSurfaced(t *testing.T) {
	dir := t.TempDir()
	// os.Open succeeds on a directory; reading from it during io.Copy does
	// not — this exercises the copy failure path distinct from open/create.
	if _, err := copyFileOp.Dispatch(map[string]any{"src": dir, "dst": filepath.Join(dir, "out")}); err == nil {
		t.Error("Dispatch with a directory as src: want an error, got nil")
	}
}

func TestCopyFileMissingArgs(t *testing.T) {
	if _, err := copyFileOp.Dispatch(map[string]any{"src": "a"}); err == nil {
		t.Error("Dispatch with no dst: want an error, got nil")
	}
	if _, err := copyFileOp.Dispatch(map[string]any{"dst": "b"}); err == nil {
		t.Error("Dispatch with no src: want an error, got nil")
	}
}

func TestCopyFileMissingSourceSurfacesRealError(t *testing.T) {
	dir := t.TempDir()
	_, err := copyFileOp.Dispatch(map[string]any{"src": filepath.Join(dir, "nope"), "dst": filepath.Join(dir, "out")})
	if err == nil {
		t.Error("Dispatch with a nonexistent src: want an error, got nil")
	}
}

func TestIntArgRejectsNonNumericValue(t *testing.T) {
	if _, err := findFilesOp.Dispatch(map[string]any{"dir": t.TempDir(), "pattern": "*", "modified_within_days": "seven"}); err == nil {
		t.Error("Dispatch with a non-numeric modified_within_days: want an error, got nil")
	}
}

func TestIntArgAcceptsRawIntNotJustFloat64(t *testing.T) {
	// encoding/json always decodes numbers as float64, but a caller
	// constructing args directly in Go (as opposed to unmarshaling a
	// model's JSON) may pass a plain int — intArg must accept both.
	if _, err := intArg(map[string]any{"n": 7}, "n", 0); err != nil {
		t.Errorf("intArg with a raw int: %v, want no error", err)
	}
}

func TestStringArgRejectsWrongType(t *testing.T) {
	if _, err := stringArg(map[string]any{"dir": 42}, "dir"); err == nil {
		t.Error("stringArg with a non-string value: want an error, got nil")
	}
}

func TestStringArgRejectsEmptyString(t *testing.T) {
	if _, err := stringArg(map[string]any{"dir": ""}, "dir"); err == nil {
		t.Error("stringArg with an empty string: want an error, got nil")
	}
}
