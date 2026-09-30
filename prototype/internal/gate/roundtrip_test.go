package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"synapseos/internal/effects"
	"synapseos/internal/recoverycheck"
	"synapseos/internal/undo"
)

// TestRealCommandsRoundTrip runs each command for real in a fixture, undoes it from the gate's
// entry alone, and compares the tree with the one taken before. These are the shapes the
// recovery check on the generated corpora found the undo could not restore.
func TestRealCommandsRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	cases := []struct {
		name, cmd string
		needs     string // a tool the case cannot run without
	}{
		{"hard link over a file", "ln -f a.txt b.txt", ""},
		{"symlink over a file", "ln -sf a.txt b.txt", ""},
		{"sed backup suffix", "sed -i.bak 's/alpha/x/' a.txt", ""},
		{"sed long backup suffix", "sed --in-place=.orig 's/alpha/x/' a.txt", ""},
		{"mv backup", "mv -b a.txt b.txt", ""},
		{"mv numbered backup", "mv --backup=numbered a.txt b.txt", ""},
		{"cp backup", "cp -b a.txt b.txt", ""},
		{"cp parents into a directory the line makes", "mkdir out && cp -P --parents d/sub/f out/", ""},
		{"perl rename", "rename 's/txt/md/' *.txt", "rename"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.needs != "" {
				if _, err := exec.LookPath(c.needs); err != nil {
					t.Skipf("%s not installed", c.needs)
				}
			}
			dir := fixture(t)
			if err := os.MkdirAll(filepath.Join(dir, "d", "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, "d", "sub", "f"), "deep")

			an := analyzer(dir, effects.DefaultRunner(10*time.Second))
			d := Decide(context.Background(), an, c.cmd, Capture)
			if d.Verdict.Class == effects.Unrecoverable {
				t.Fatalf("analysis refused it: %v", d.Verdict.Reasons)
			}
			want, err := recoverycheck.Snapshot(dir)
			if err != nil {
				t.Fatal(err)
			}
			entry, errs := d.Capture(dir, c.cmd)
			if len(errs) != 0 {
				t.Fatalf("capture: %v", errs)
			}

			run := exec.Command("bash", "-c", c.cmd)
			run.Dir = dir
			if out, err := run.CombinedOutput(); err != nil {
				t.Fatalf("running %q: %v\n%s", c.cmd, err, out)
			}
			if errs := undo.Apply(entry); len(errs) != 0 {
				t.Fatalf("undo: %v", errs)
			}
			got, err := recoverycheck.Snapshot(dir)
			if err != nil {
				t.Fatal(err)
			}
			if diff := recoverycheck.Diff(want, got); len(diff) != 0 {
				t.Fatalf("tree differs after undo: %v", diff)
			}
		})
	}
}
