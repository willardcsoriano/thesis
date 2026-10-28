package main

// Model-free checks of Layer 7's verifiers, which run in every go test. A
// verifier that passes an untouched fixture would score a model that did
// nothing as a success; one that fails the right answer would score a correct
// model as a miss. Each task is therefore checked both ways: untouched, with
// nothing told, it must fail; after its reference solution runs in the fixture,
// with that command's output as what was told, it must pass.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLayer7Verifiers(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	sets := map[string][]utteranceTask{
		"development": append(utteranceCorpus(), extraUtteranceCorpus()...),
		"heldout":     heldOutUtteranceCorpus(),
		"heldout2":    heldOut2UtteranceCorpus(),
	}
	for name, tasks := range sets {
		seen := map[string]bool{}
		for _, task := range tasks {
			if seen[task.id] {
				t.Errorf("%s: task id %s is used twice", name, task.id)
			}
			seen[task.id] = true
			t.Run(name+"/"+task.id, func(t *testing.T) {
				untouched := t.TempDir()
				task.setup(t, untouched)
				if ok, _ := task.verify(untouched, ""); ok {
					t.Error("passes an untouched fixture with nothing told: a model that did nothing would score")
				}

				dir := t.TempDir()
				task.setup(t, dir)
				if task.solution == "" {
					t.Fatal("no reference solution, so the verifier cannot be checked")
				}
				cmd := exec.Command("bash", "-c", task.solution)
				cmd.Dir = dir
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("the reference solution %q failed: %v", task.solution, err)
				}
				if ok, why := task.verify(dir, string(out)); !ok {
					t.Errorf("rejects the reference solution %q: %s (told %q)", task.solution, why, out)
				}
				// A second reading must be as checkable as the first: it
				// refuses an untouched fixture, accepts its own solution, and
				// the first reading's solution does not need it.
				if task.plainAlso == nil {
					return
				}
				if task.plainAlsoSolution == "" {
					t.Fatal("plainAlso has no solution, so it cannot be checked")
				}
				if ok, _ := task.plainAlso(untouched, ""); ok {
					t.Error("plainAlso passes an untouched fixture with nothing told")
				}
				alt := t.TempDir()
				task.setup(t, alt)
				altCmd := exec.Command("bash", "-c", task.plainAlsoSolution)
				altCmd.Dir = alt
				altOut, err := altCmd.Output()
				if err != nil {
					t.Fatalf("the second reading's solution %q failed: %v", task.plainAlsoSolution, err)
				}
				if ok, why := task.plainAlso(alt, string(altOut)); !ok {
					t.Errorf("plainAlso rejects its own solution %q: %s (told %q)", task.plainAlsoSolution, why, altOut)
				}
				if ok, _ := task.score(1, alt, string(altOut)); ok {
					t.Error("the second reading is accepted beyond the plain tier")
				}
			})
		}
	}
}

// TestLayer7Gathered pins the U1/U8 check against the ways a model can almost
// gather files: any folder name passes, but splitting them, leaving them only
// where they were, or linking instead of moving does not.
func TestLayer7Gathered(t *testing.T) {
	cases := []struct {
		name string
		do   string
		want bool
	}{
		{"named as asked", "mkdir PDFs && mv a.pdf b.pdf PDFs/", true},
		{"other spelling", "mkdir pdfs && mv a.pdf b.pdf pdfs/", true},
		{"nested folder", "mkdir -p docs/all && mv a.pdf b.pdf docs/all/", true},
		{"copied, not moved", "mkdir pdfs && cp a.pdf b.pdf pdfs/", true},
		{"left in place", "mkdir pdfs", false},
		{"split across folders", "mkdir x y && mv a.pdf x/ && mv b.pdf y/", false},
		{"only one moved", "mkdir pdfs && mv a.pdf pdfs/", false},
		{"symlinks only", "mkdir pdfs && ln -s ../a.pdf ../b.pdf pdfs/", false},
		{"folders named like the files", "mkdir -p d/a.pdf d/b.pdf", false},
		{"nothing done", "true", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := t.TempDir()
			write(t, d, "a.pdf", "x")
			write(t, d, "b.pdf", "x")
			cmd := exec.Command("bash", "-c", c.do)
			cmd.Dir = d
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v: %s", c.do, err, out)
			}
			if got := gathered(d, "a.pdf", "b.pdf"); got != c.want {
				t.Errorf("gathered after %q = %v, want %v", c.do, got, c.want)
			}
		})
	}
}

// TestLayer7HeldOutFrozen holds the held-out set to the hash it was frozen
// with, before any model ran on it. An edit after results exist would make it
// development data; a new set goes under a new name instead.
func TestLayer7HeldOutFrozen(t *testing.T) {
	checkFrozen(t, "layer7_heldout_test.go", "../../pilot/layer7_heldout.sha256")
}

// TestLayer7HeldOut2Frozen does the same for the second held-out set.
func TestLayer7HeldOut2Frozen(t *testing.T) {
	checkFrozen(t, "layer7_heldout2_test.go", "../../pilot/layer7_heldout2.sha256")
}

// checkFrozen fails unless the sha256 of src is the one recorded in hashFile.
func checkFrozen(t *testing.T, src, hashFile string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(hashFile)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Fatalf("%s changed since it was frozen (sha256 %s, frozen %s)", src, got, strings.TrimSpace(string(want)))
	}
}

// A wording that names the folder is met only by that folder; the plain one,
// which names none, by any (D52). U1 and U8 accepted any folder at every tier
// until the ship review of 2026-10-27, which inflated the named tiers.
func TestLayer7NamedFolderTiers(t *testing.T) {
	byID := map[string]utteranceTask{}
	for _, task := range append(utteranceCorpus(), extraUtteranceCorpus()...) {
		byID[task.id] = task
	}
	for _, c := range []struct {
		id, do string
	}{
		{"U1", "mkdir pdfs && mv a.pdf b.pdf pdfs/"},
		{"U8", "mkdir old && mv one.log two.log old/"},
	} {
		for tier, want := range []bool{true, false, false} {
			d := t.TempDir()
			byID[c.id].setup(t, d)
			cmd := exec.Command("bash", "-c", c.do)
			cmd.Dir = d
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v %s", c.do, err, out)
			}
			if ok, _ := byID[c.id].score(tier, d, ""); ok != want {
				t.Errorf("%s tier %d after %q: scored %v, want %v", c.id, tier, c.do, ok, want)
			}
		}
	}
}
