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
			})
		}
	}
}

// TestLayer7HeldOutFrozen holds the held-out set to the hash it was frozen
// with, before any model ran on it. An edit after results exist would make it
// development data; a new set goes under a new name instead.
func TestLayer7HeldOutFrozen(t *testing.T) {
	src, err := os.ReadFile("layer7_heldout_test.go")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../pilot/layer7_heldout.sha256")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(src)
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Fatalf("layer7_heldout_test.go changed since it was frozen (sha256 %s, frozen %s)", got, strings.TrimSpace(string(want)))
	}
}
