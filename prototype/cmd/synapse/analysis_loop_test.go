package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

// These drive the real loop, with a scripted model, with the effect analysis on.
// They check what the wiring is for: the list can be overruled toward asking, never
// toward not asking; a capture is taken before a command runs; and `synapse undo`'s
// journal entry puts the files back.

func withAnalysis(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("SYNAPSE_ANALYSIS", mode)
	t.Setenv("SYNAPSE_RESOLVE", "never") // hermetic: no resolver process in these tests
	t.Setenv("HOME", t.TempDir())
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func lastEntry(t *testing.T, journal string) undo.Entry {
	t.Helper()
	e, ok, err := undo.PeekLastJournal(journal)
	if err != nil || !ok {
		t.Fatalf("no journal entry: ok=%v err=%v", ok, err)
	}
	return e
}

// A rename is recoverable and needs no confirmation, and undo must bring the
// original name back. This is the command whose undo used to delete the file.
func TestAnalysisRenameIsSilentAndUndoRestoresIt(t *testing.T) {
	withAnalysis(t, "capture")
	dir := t.TempDir()
	chdir(t, dir)
	journal := filepath.Join(t.TempDir(), "undo.log")
	if err := os.WriteFile("notes.txt", []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := scriptedOllamaServer(t, []string{"mv notes.txt renamed.txt", "DONE"})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "rename it", neverConfirm(t), &out, &errOut, journal)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errOut.String())
	}
	if _, err := os.Stat("renamed.txt"); err != nil {
		t.Fatal("the rename did not run")
	}
	if errs := undo.Apply(lastEntry(t, journal)); len(errs) != 0 {
		t.Fatal(errs)
	}
	b, err := os.ReadFile("notes.txt")
	if err != nil || string(b) != "precious" {
		t.Fatalf("after undo notes.txt = %q, %v", b, err)
	}
	if _, err := os.Stat("renamed.txt"); err == nil {
		t.Fatal("renamed.txt should be gone after undo")
	}
}

// In capture mode a deletion is captured silently and undone; in strict mode the
// same deletion asks first. Either way undo restores the file.
func TestAnalysisDeletionCapturedAndUndone(t *testing.T) {
	for _, mode := range []string{"capture", "strict"} {
		t.Run(mode, func(t *testing.T) {
			withAnalysis(t, mode)
			dir := t.TempDir()
			chdir(t, dir)
			journal := filepath.Join(t.TempDir(), "undo.log")
			os.WriteFile("old.log", []byte("keep me"), 0o644)
			server := scriptedOllamaServer(t, []string{"rm old.log", "DONE"})
			defer server.Close()

			asked := 0
			confirm := func(string) bool { asked++; return true }
			var out, errOut bytes.Buffer
			runLoop(context.Background(), ollama.New(server.URL), "m", "delete the log", confirm, &out, &errOut, journal)
			if _, err := os.Stat("old.log"); err == nil {
				t.Fatal("the deletion did not run")
			}
			// The list already asks about rm, so it asks in both modes: the analysis
			// never removes a confirmation the list would have made.
			if asked != 1 {
				t.Fatalf("asked %d times, want 1 (the list asks about rm)", asked)
			}
			if errs := undo.Apply(lastEntry(t, journal)); len(errs) != 0 {
				t.Fatal(errs)
			}
			if b, err := os.ReadFile("old.log"); err != nil || string(b) != "keep me" {
				t.Fatalf("after undo old.log = %q, %v", b, err)
			}
		})
	}
}

// The analysis adds a confirmation the list misses: `find -delete` with resolution
// off cannot know its targets, so it must ask, and must not run when declined.
func TestAnalysisAsksWhereTheListDoesNotAndDeclineStopsIt(t *testing.T) {
	withAnalysis(t, "capture")
	dir := t.TempDir()
	chdir(t, dir)
	os.WriteFile("a.tmp", []byte("x"), 0o644)
	server := scriptedOllamaServer(t, []string{"python3 -c \"import os; os.remove('a.tmp')\""})
	defer server.Close()

	asked := 0
	confirm := func(p string) bool { asked++; return false }
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "remove the tmp file", confirm, &out, &errOut, "")
	if asked != 1 {
		t.Fatalf("an interpreter one-liner the list does not know must ask once, asked %d\n%s", asked, out.String())
	}
	if !strings.Contains(out.String(), "needs confirmation") {
		t.Fatalf("prompt did not say why:\n%s", out.String())
	}
	if _, err := os.Stat("a.tmp"); err != nil {
		t.Fatal("declined, but the file is gone")
	}
}

// With the flag off nothing changes: the same interpreter one-liner runs unasked,
// which is the gap the analysis exists to close.
func TestAnalysisOffLeavesBehaviourUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	chdir(t, dir)
	os.WriteFile("a.tmp", []byte("x"), 0o644)
	server := scriptedOllamaServer(t, []string{"python3 -c \"import os; os.remove('a.tmp')\"", "DONE"})
	defer server.Close()
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "remove the tmp file", neverConfirm(t), &out, &errOut, "")
	if _, err := os.Stat("a.tmp"); err == nil {
		t.Fatal("with the analysis off the list should have let this through")
	}
}
