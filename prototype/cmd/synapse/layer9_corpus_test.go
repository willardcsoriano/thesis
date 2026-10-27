package main

// Layer 9's conversations (development set) and the model-free checks of their
// verifiers, which run in every go test. Layer 7 asks whether one request is
// understood; this asks whether a request is understood in the light of the
// ones before it: "move it", "the other one", "put it back". Session memory has
// its own unit tests, but they check that earlier turns reach the prompt, not
// what a model makes of them, and the one time that mattered (a follow-up
// answered DONE because the model read the previous task as this one,
// manual-tests/m6-session-context.md) only a live run found it.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// convTurn is one thing the person says, its reference solution as a shell
// command (never shown to a model; TestLayer9Verifiers runs it), and a check
// on the state after this turn and what this turn told the person.
type convTurn struct {
	say      string
	solution string
	verify   func(dir, told string) (bool, string)
}

// conversation is a fixture and the turns spoken against it, in order, in one
// session. A turn is scored on its own, so a later turn can pass after an
// earlier one failed; whether every turn passed is reported as well.
type conversation struct {
	id       string
	category string
	setup    func(t *testing.T, dir string)
	turns    []convTurn
}

// age sets name's modification time to d before now.
func age(t *testing.T, dir, name string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	if err := os.Chtimes(filepath.Join(dir, name), when, when); err != nil {
		t.Fatal(err)
	}
}

func contents(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return string(b)
}

func conversationCorpus() []conversation {
	return []conversation{
		{
			id: "C1", category: "reference to a result",
			setup: func(t *testing.T, d string) {
				write(t, d, "small.txt", "x")
				write(t, d, "big.bin", strings.Repeat("y", 200000))
			},
			turns: []convTurn{
				{"what's the biggest file here", `ls -S | head -1`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "big.bin") {
						return false, "never named big.bin"
					}
					return true, ""
				}},
				{"move it into a folder called old", `mkdir old && mv big.bin old/`, func(d, told string) (bool, string) {
					if !exists(d, "old/big.bin") {
						return false, "big.bin is not in old/"
					}
					if !exists(d, "small.txt") {
						return false, "small.txt was moved too"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C2", category: "correction",
			setup: func(t *testing.T, d string) {
				write(t, d, "notes.txt", "buy milk\n")
				write(t, d, "draft.txt", "dear sam\n")
			},
			turns: []convTurn{
				{"show me what's in notes.txt", `cat notes.txt`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "buy milk") {
						return false, "did not show notes.txt"
					}
					return true, ""
				}},
				{"no, I meant the other text file", `cat draft.txt`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "dear sam") {
						return false, "did not show draft.txt"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C3", category: "undo",
			setup: func(t *testing.T, d string) { write(t, d, "report.txt", "x") },
			turns: []convTurn{
				{"move report.txt into a new folder called archive", `mkdir archive && mv report.txt archive/`, func(d, told string) (bool, string) {
					if !exists(d, "archive/report.txt") {
						return false, "report.txt is not in archive/"
					}
					return true, ""
				}},
				// The word the REPL itself answers, without the model.
				{"undo", `mv archive/report.txt .`, func(d, told string) (bool, string) {
					if !exists(d, "report.txt") || exists(d, "archive/report.txt") {
						return false, "report.txt is not back where it was"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C4", category: "undo",
			setup: func(t *testing.T, d string) { write(t, d, "report.txt", "x") },
			turns: []convTurn{
				{"move report.txt into a new folder called archive", `mkdir archive && mv report.txt archive/`, func(d, told string) (bool, string) {
					if !exists(d, "archive/report.txt") {
						return false, "report.txt is not in archive/"
					}
					return true, ""
				}},
				// The same intent in words, which goes to the model.
				{"actually, put it back where it was", `mv archive/report.txt .`, func(d, told string) (bool, string) {
					if !exists(d, "report.txt") || exists(d, "archive/report.txt") {
						return false, "report.txt is not back where it was"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C5", category: "change of topic",
			setup: func(t *testing.T, d string) {
				write(t, d, "log.txt", "a\nb\nc\n")
				write(t, d, "big.bin", strings.Repeat("y", 200000))
			},
			turns: []convTurn{
				{"how many lines are in log.txt", `wc -l log.txt`, func(d, told string) (bool, string) {
					if !reports(told, "3") {
						return false, "did not report 3"
					}
					return true, ""
				}},
				{"different question: which file here is the largest", `ls -S | head -1`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "big.bin") {
						return false, "never named big.bin"
					}
					if contents(d, "log.txt") != "a\nb\nc\n" {
						return false, "log.txt was changed"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C6", category: "chat around work",
			setup: func(t *testing.T, d string) {
				write(t, d, "notes.txt", "keep\n")
				write(t, d, "a.txt", "x")
			},
			turns: []convTurn{
				{"hi! how many files are in this folder?", `ls | wc -l`, func(d, told string) (bool, string) {
					if !reports(told, "2") {
						return false, "did not report 2"
					}
					return true, ""
				}},
				{"thanks! now make a copy of notes.txt called notes.bak", `cp notes.txt notes.bak`, func(d, told string) (bool, string) {
					if contents(d, "notes.bak") != "keep\n" {
						return false, "notes.bak is not a copy of notes.txt"
					}
					if !exists(d, "notes.txt") {
						return false, "notes.txt is gone"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C7", category: "reference to a result",
			setup: func(t *testing.T, d string) {
				write(t, d, "alpha.txt", "needle here\n")
				write(t, d, "beta.txt", "nothing\n")
			},
			turns: []convTurn{
				{"which file mentions the word needle", `grep -l needle *.txt`, func(d, told string) (bool, string) {
					if !strings.Contains(told, "alpha.txt") {
						return false, "did not name alpha.txt"
					}
					return true, ""
				}},
				{"rename that file to found.txt", `mv alpha.txt found.txt`, func(d, told string) (bool, string) {
					if !strings.Contains(contents(d, "found.txt"), "needle") {
						return false, "found.txt is not the file with needle"
					}
					if exists(d, "alpha.txt") || !exists(d, "beta.txt") {
						return false, "the wrong file was renamed, or it was copied"
					}
					return true, ""
				}},
			},
		},
		{
			id: "C8", category: "building on earlier turns",
			setup: func(t *testing.T, d string) {
				write(t, d, "one.txt", "x")
				write(t, d, "two.txt", "x")
				write(t, d, "photo.jpg", "x")
			},
			turns: []convTurn{
				{"make a folder called stuff", `mkdir stuff`, func(d, told string) (bool, string) {
					if fi, err := os.Stat(filepath.Join(d, "stuff")); err != nil || !fi.IsDir() {
						return false, "no folder stuff"
					}
					return true, ""
				}},
				{"put the two text files in it", `mv one.txt two.txt stuff/`, func(d, told string) (bool, string) {
					if !exists(d, "stuff/one.txt") || !exists(d, "stuff/two.txt") {
						return false, "the text files are not both in stuff/"
					}
					if !exists(d, "photo.jpg") {
						return false, "photo.jpg was moved too"
					}
					return true, ""
				}},
				{"how many files are in there now", `ls stuff | wc -l`, func(d, told string) (bool, string) {
					if !reports(told, "2") {
						return false, "did not report 2"
					}
					return true, ""
				}},
			},
		},
	}
}

// TestLayer9Verifiers checks each turn's verifier both ways, in the order the
// conversation happens: after the turns before it were solved, the turn must
// fail with nothing done and nothing told, or a model that ignored it would
// score; after its own reference solution, with that command's output as what
// was told, it must pass.
func TestLayer9Verifiers(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	sets := map[string][]conversation{
		"development": conversationCorpus(),
		"heldout":     heldOutConversationCorpus(),
	}
	for name, convs := range sets {
		seen := map[string]bool{}
		for _, c := range convs {
			if seen[c.id] {
				t.Errorf("%s: conversation id %s is used twice", name, c.id)
			}
			seen[c.id] = true
			t.Run(name+"/"+c.id, func(t *testing.T) {
				if len(c.turns) < 2 {
					t.Fatal("a conversation needs at least two turns")
				}
				dir := t.TempDir()
				c.setup(t, dir)
				for i, turn := range c.turns {
					if ok, _ := turn.verify(dir, ""); ok {
						t.Errorf("turn %d (%q) passes before anything was done for it", i+1, turn.say)
					}
					if turn.solution == "" {
						t.Fatalf("turn %d has no reference solution", i+1)
					}
					cmd := exec.Command("bash", "-c", turn.solution)
					cmd.Dir = dir
					out, err := cmd.Output()
					if err != nil {
						t.Fatalf("turn %d: the reference solution %q failed: %v", i+1, turn.solution, err)
					}
					if ok, why := turn.verify(dir, string(out)); !ok {
						t.Errorf("turn %d rejects its reference solution %q: %s (told %q)", i+1, turn.solution, why, out)
					}
				}
			})
		}
	}
}

// TestLayer9HeldOutFrozen holds the held-out conversations to the hash they
// were frozen with, as TestLayer7HeldOutFrozen does for Layer 7's.
func TestLayer9HeldOutFrozen(t *testing.T) {
	src, err := os.ReadFile("layer9_heldout_test.go")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../pilot/layer9_heldout.sha256")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(src)
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Fatalf("layer9_heldout_test.go changed since it was frozen (sha256 %s, frozen %s)", got, strings.TrimSpace(string(want)))
	}
}
