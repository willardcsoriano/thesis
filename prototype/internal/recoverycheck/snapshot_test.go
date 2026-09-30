package recoverycheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func build(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(d, "sub"), 0o755))
	must(os.WriteFile(filepath.Join(d, "a.txt"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(d, "sub", "b.txt"), []byte("b"), 0o600))
	must(os.WriteFile(filepath.Join(d, ".hidden"), []byte("h"), 0o644))
	must(os.Symlink("a.txt", filepath.Join(d, "lnk")))
	return d
}

func snap(t *testing.T, d string) Tree {
	t.Helper()
	tr, err := Snapshot(d)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestEqualTrees(t *testing.T) {
	d := build(t)
	if diff := Diff(snap(t, d), snap(t, d)); len(diff) != 0 {
		t.Fatalf("equal trees differ: %v", diff)
	}
	if _, ok := snap(t, d)[".hidden"]; !ok {
		t.Fatal("hidden files must be recorded")
	}
}

func expect(t *testing.T, want Tree, d, sub string) {
	t.Helper()
	diff := Diff(want, snap(t, d))
	if len(diff) != 1 || !strings.Contains(diff[0], sub) {
		t.Fatalf("diff = %v, want one entry containing %q", diff, sub)
	}
}

func TestChangedContent(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.WriteFile(filepath.Join(d, "a.txt"), []byte("changed"), 0o644)
	expect(t, want, d, "content a.txt")
}

func TestChangedMode(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.Chmod(filepath.Join(d, "sub", "b.txt"), 0o644)
	expect(t, want, d, "mode sub/b.txt")
}

func TestExtraFile(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.WriteFile(filepath.Join(d, "new.txt"), nil, 0o644)
	expect(t, want, d, "extra new.txt")
}

func TestMissingFile(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.Remove(filepath.Join(d, "a.txt"))
	// lnk still exists but now dangles; only the file itself is reported missing.
	expect(t, want, d, "missing a.txt")
}

func TestSymlinkChange(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.Remove(filepath.Join(d, "lnk"))
	os.Symlink("sub/b.txt", filepath.Join(d, "lnk"))
	expect(t, want, d, "link lnk")
}

func TestTypeChange(t *testing.T) {
	d := build(t)
	want := snap(t, d)
	os.Remove(filepath.Join(d, "a.txt"))
	os.Mkdir(filepath.Join(d, "a.txt"), 0o755)
	expect(t, want, d, "type a.txt")
}

func TestGitInternalsIgnoredExceptHeadAndRefs(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".git", "refs", "heads"), 0o755)
	os.MkdirAll(filepath.Join(d, ".git", "logs"), 0o755)
	os.WriteFile(filepath.Join(d, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte("abc\n"), 0o644)
	os.WriteFile(filepath.Join(d, ".git", "index"), []byte("1"), 0o644)
	want := snap(t, d)
	os.WriteFile(filepath.Join(d, ".git", "index"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(d, ".git", "logs", "HEAD"), []byte("x"), 0o644)
	if diff := Diff(want, snap(t, d)); len(diff) != 0 {
		t.Fatalf("git internals other than HEAD/refs must be ignored: %v", diff)
	}
	os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte("def\n"), 0o644)
	expect(t, want, d, "content .git/refs/heads/main")
}

func TestBytes(t *testing.T) {
	d := build(t)
	if got := Bytes(snap(t, d)); got != 3 {
		t.Fatalf("Bytes = %d, want 3", got)
	}
}
