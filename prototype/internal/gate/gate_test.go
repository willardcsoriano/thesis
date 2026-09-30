package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"synapseos/internal/effects"
	"synapseos/internal/undo"
)

// fixture is a working directory holding a.txt and b.txt, with HOME redirected so
// trash and content backups land in a temp dir.
func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	// Resolve symlinks so the analyzer's paths and the test's paths agree.
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	write(t, filepath.Join(dir, "a.txt"), "alpha")
	write(t, filepath.Join(dir, "b.txt"), "bravo")
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fakeSys(answers map[string]string) effects.Runner {
	return func(_ context.Context, _ string, argv []string) ([]byte, error) {
		if out, ok := answers[strings.Join(argv, " ")]; ok {
			return []byte(out), nil
		}
		return nil, exec.ErrNotFound
	}
}

func analyzer(dir string, run effects.Runner) *effects.Analyzer {
	a := effects.New(dir)
	a.Run = run
	return a
}

func TestDecideConfirmByPolicy(t *testing.T) {
	dir := fixture(t)
	cases := []struct {
		name        string
		cmd         string
		class       effects.Class
		strict      bool
		capture     bool
		confident   bool
		wantCapture bool
	}{
		{"read only", "cat a.txt", effects.Recoverable, false, false, true, false},
		{"rm needs capture", "rm a.txt", effects.RecoverableWithCapture, true, false, true, true},
		{"cp over existing needs capture", "cp a.txt b.txt", effects.RecoverableWithCapture, true, false, true, true},
		{"unresolved glob substitution", "rm $(ls *.txt)", effects.Unrecoverable, true, true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			an := analyzer(dir, nil) // no Run: anything needing resolution fails closed
			strict := Decide(context.Background(), an, c.cmd, Strict)
			capt := Decide(context.Background(), an, c.cmd, Capture)

			if strict.Verdict.Class != c.class || capt.Verdict.Class != c.class {
				t.Fatalf("class = %v/%v, want %v", strict.Verdict.Class, capt.Verdict.Class, c.class)
			}
			if strict.Confirm != c.strict {
				t.Errorf("strict Confirm = %v, want %v", strict.Confirm, c.strict)
			}
			if capt.Confirm != c.capture {
				t.Errorf("capture Confirm = %v, want %v", capt.Confirm, c.capture)
			}
			if strict.Confident != c.confident {
				t.Errorf("Confident = %v, want %v (issues: %+v)", strict.Confident, c.confident, strict.Analysis.Issues)
			}
			if got := len(strict.Plan.Captures) > 0; got != c.wantCapture {
				t.Errorf("plan has captures = %v, want %v: %+v", got, c.wantCapture, strict.Plan)
			}
		})
	}
}

// An unknown policy must behave like the conservative default rather than
// silently auto-approving.
func TestDecideUnknownPolicyIsStrict(t *testing.T) {
	dir := fixture(t)
	d := Decide(context.Background(), analyzer(dir, nil), "rm a.txt", Policy("bogus"))
	if !d.Confirm {
		t.Error("an unrecognised policy must still ask for a command that needs capture")
	}
}

// Capturing a command that was left untouched must not leave a droppable entry
// that pretends to have undo state, and it must not touch the files.
func TestCaptureReadOnlyIsNoop(t *testing.T) {
	dir := fixture(t)
	d := Decide(context.Background(), analyzer(dir, nil), "cat a.txt", Capture)
	e, errs := d.Capture(dir, "cat a.txt")
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if !e.IsNoop() {
		t.Errorf("entry for a read-only command = %+v, want a no-op", e)
	}
	if e.Command != "cat a.txt" || e.Dir != dir || e.Timestamp.IsZero() {
		t.Errorf("entry header = %+v", e)
	}
}

func TestCaptureRmTrashesAndApplyRestores(t *testing.T) {
	dir := fixture(t)
	a := filepath.Join(dir, "a.txt")
	d := Decide(context.Background(), analyzer(dir, nil), "rm a.txt", Capture)

	e, errs := d.Capture(dir, "rm a.txt")
	if len(errs) != 0 {
		t.Fatalf("Capture errs: %v", errs)
	}
	if len(e.Trashed) != 1 || e.Trashed[0].OriginalPath != a {
		t.Fatalf("Trashed = %+v, want one item for %s", e.Trashed, a)
	}
	if len(e.ContentBackups) != 0 || len(e.Moves) != 0 || len(e.Inverses) != 0 {
		t.Errorf("unexpected extra state: %+v", e)
	}

	// Run the command for real, then undo.
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if errs := undo.Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, a); got != "alpha" {
		t.Errorf("restored a.txt = %q, want %q", got, "alpha")
	}
}

func TestCaptureCpOverwriteBacksUpContent(t *testing.T) {
	dir := fixture(t)
	b := filepath.Join(dir, "b.txt")
	d := Decide(context.Background(), analyzer(dir, nil), "cp a.txt b.txt", Capture)

	e, errs := d.Capture(dir, "cp a.txt b.txt")
	if len(errs) != 0 {
		t.Fatalf("Capture errs: %v", errs)
	}
	if len(e.ContentBackups) != 1 || e.ContentBackups[0].Path != b {
		t.Fatalf("ContentBackups = %+v, want one for %s", e.ContentBackups, b)
	}
	if got := read(t, e.ContentBackups[0].BackupPath); got != "bravo" {
		t.Errorf("backup holds %q, want the pre-command content %q", got, "bravo")
	}
	if len(e.Trashed) != 0 {
		t.Errorf("Trashed = %+v, want none: cp overwrites in place", e.Trashed)
	}

	write(t, b, "alpha") // the cp
	if errs := undo.Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, b); got != "bravo" {
		t.Errorf("b.txt after undo = %q, want %q", got, "bravo")
	}
}

func TestCaptureCpToNewFileRecordsCreation(t *testing.T) {
	dir := fixture(t)
	d := Decide(context.Background(), analyzer(dir, nil), "cp a.txt new.txt", Capture)
	e, errs := d.Capture(dir, "cp a.txt new.txt")
	if len(errs) != 0 {
		t.Fatalf("Capture errs: %v", errs)
	}
	if !reflect.DeepEqual(e.Created, []string{"new.txt"}) {
		t.Fatalf("Created = %v, want [new.txt] (relative to dir)", e.Created)
	}

	write(t, filepath.Join(dir, "new.txt"), "alpha")
	if errs := undo.Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt should have been removed by undo, stat err = %v", err)
	}
	if got := read(t, filepath.Join(dir, "a.txt")); got != "alpha" {
		t.Errorf("source a.txt = %q, must be untouched", got)
	}
}

func TestCaptureMvRecordsMoveAndApplyMovesBack(t *testing.T) {
	dir := fixture(t)
	d := Decide(context.Background(), analyzer(dir, nil), "mv a.txt c.txt", Capture)
	if d.Confirm {
		t.Fatalf("a plain rename is recoverable under the capture policy: %+v", d.Verdict)
	}

	e, errs := d.Capture(dir, "mv a.txt c.txt")
	if len(errs) != 0 {
		t.Fatalf("Capture errs: %v", errs)
	}
	if !reflect.DeepEqual(e.Moves, []undo.Move{{OldPath: "a.txt", NewPath: "c.txt"}}) {
		t.Fatalf("Moves = %+v", e.Moves)
	}
	// The destination is where the moved file lands; it must not also be recorded
	// as a fresh creation, or undo would delete the moved data instead of moving it.
	if len(e.Created) != 0 {
		t.Errorf("Created = %v, want none for a move target", e.Created)
	}

	if err := os.Rename(filepath.Join(dir, "a.txt"), filepath.Join(dir, "c.txt")); err != nil {
		t.Fatal(err)
	}
	if errs := undo.Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if got := read(t, filepath.Join(dir, "a.txt")); got != "alpha" {
		t.Errorf("a.txt after undo = %q, want %q", got, "alpha")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.txt")); !os.IsNotExist(err) {
		t.Errorf("c.txt should be gone after undo, stat err = %v", err)
	}
}

func TestCapturePackageInstallStoresInverse(t *testing.T) {
	answers := map[string]string{
		"apt-get -s install hello":                     "Inst hello (2.10-3 Debian:13.0/stable [amd64])\n",
		"dpkg-query -W -f=${db:Status-Abbrev}\n hello": "\n",
	}
	cases := []struct {
		name     string
		cmd      string
		wantSudo bool
	}{
		{"with sudo", "sudo apt-get install -y hello", true},
		{"without sudo", "apt-get install -y hello", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixture(t)
			d := Decide(context.Background(), analyzer(dir, fakeSys(answers)), c.cmd, Capture)
			if d.Confirm {
				t.Fatalf("an installable package is recoverable with capture, got %+v (issues %+v)", d.Verdict, d.Analysis.Issues)
			}
			strict := Decide(context.Background(), analyzer(dir, fakeSys(answers)), c.cmd, Strict)
			if !strict.Confirm {
				t.Error("strict policy must still ask: the command needs a capture")
			}

			e, errs := d.Capture(dir, c.cmd)
			if len(errs) != 0 {
				t.Fatalf("Capture errs: %v", errs)
			}
			want := []undo.Inverse{{Argv: []string{"apt-get", "purge", "-y", "hello"}, Sudo: c.wantSudo}}
			if !reflect.DeepEqual(e.Inverses, want) {
				t.Fatalf("Inverses = %+v, want %+v", e.Inverses, want)
			}
			if e.IsNoop() {
				t.Error("an entry holding only an inverse must be journaled, not dropped as a no-op")
			}
			if got := e.Inverses[0].String(); got != map[bool]string{true: "sudo ", false: ""}[c.wantSudo]+"apt-get purge -y hello" {
				t.Errorf("manual fallback text = %q", got)
			}
		})
	}
}

// Package resolution without a runner cannot know what would be installed, so
// the gate has to ask even under the capture policy.
func TestDecidePackageInstallWithoutRunnerFailsClosed(t *testing.T) {
	dir := fixture(t)
	d := Decide(context.Background(), analyzer(dir, nil), "apt-get install -y hello", Capture)
	if !d.Confirm {
		t.Errorf("verdict %+v: an unresolvable install must require confirmation", d.Verdict)
	}
	if d.Confident {
		t.Error("Confident must be false when the analysis had issues")
	}
}
