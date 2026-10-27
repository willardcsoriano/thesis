package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"synapseos/internal/executor"
	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
)

// A move straight back is not run: the file stays where the first step put it,
// and the run ends under its own outcome with the task answered. A move with
// anything else in it, or after another step, is not a reversal and runs.
func TestBrakeReversal(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
		want  string // outcome
		at    string // the file that must exist afterwards
	}{
		{"rename and back", []string{"mv report.txt summary.txt", "mv summary.txt report.txt"}, outcomeReversedStep, "summary.txt"},
		{"into a folder and out", []string{"mkdir old && mv report.txt old/", "mv old/report.txt ."}, outcomeReversedStep, "old/report.txt"},
		{"move then an unrelated move", []string{"mv report.txt summary.txt", "mv other.txt else.txt", "DONE"}, outcomeComplete, "else.txt"},
		{"back, plus something else", []string{"mv report.txt summary.txt", "mv summary.txt report.txt && touch z", "DONE"}, outcomeComplete, "z"},
		{"back after another step", []string{"mv report.txt summary.txt", "ls", "mv summary.txt report.txt", "DONE"}, outcomeComplete, "report.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withAnalysis(t, "strict")
			dir := t.TempDir()
			t.Chdir(dir)
			for _, f := range []string{"report.txt", "other.txt"} {
				if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			code, outcome, evs := runInDir(t, c.steps)
			if c.want != "" && outcome != c.want {
				t.Errorf("outcome %q, want %q (exit %d)", outcome, c.want, code)
			}
			if c.want == outcomeReversedStep {
				if code != exitOK {
					t.Errorf("exit %d, want 0", code)
				}
				var notRun bool
				for _, e := range evs {
					notRun = notRun || (e.Kind == loopevent.Result && e.NotRun && strings.Contains(e.Stderr, "move back"))
				}
				if !notRun {
					t.Errorf("no not-run result says why the step was refused: %v", kinds(evs))
				}
			}
			if _, err := os.Stat(c.at); err != nil {
				t.Errorf("%s missing afterwards: %v", c.at, err)
			}
		})
	}
}

// The brake reads the analysis; with the analysis off there is nothing to read,
// and the step runs as it did before the brake.
func TestBrakeReversalNeedsTheAnalysis(t *testing.T) {
	t.Setenv("SYNAPSE_ANALYSIS", "off")
	t.Chdir(t.TempDir())
	os.WriteFile("report.txt", []byte("x"), 0o644)
	_, outcome, _ := runInDir(t, []string{"mv report.txt summary.txt", "mv summary.txt report.txt", "DONE"})
	if outcome != outcomeComplete {
		t.Errorf("outcome %q, want complete", outcome)
	}
	if _, err := os.Stat("report.txt"); err != nil {
		t.Errorf("the move back did not run: %v", err)
	}
}

// runInDir is runScripted in the current directory, for fixtures made first.
func runInDir(t *testing.T, steps []string) (int, string, []loopevent.Event) {
	t.Helper()
	server := scriptedOllamaServer(t, steps)
	defer server.Close()
	evs, opt := collectEvents()
	var outcome string
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "do it", neverConfirm(t), &out, &errOut, "",
		opt, withOutcome(func(o string) { outcome = o }))
	return code, outcome, *evs
}

func TestUnsupportedClaims(t *testing.T) {
	ls := []loopStep{{command: "ls", result: executor.Result{Stdout: "a.txt\nbig.bin\n"}}}
	wc := []loopStep{{command: "wc -l log.txt", result: executor.Result{Stdout: "4 log.txt\n"}}}
	moved := []loopStep{{command: "mkdir PDFs && mv *.pdf PDFs/", moves: []move{{"/w/a.pdf", "/w/PDFs/a.pdf"}, {"/w/b.pdf", "/w/PDFs/b.pdf"}}}}
	cases := []struct {
		name    string
		reply   string
		task    string
		history []loopStep
		want    []string
	}{
		{"a number the output printed", "log.txt has 4 lines.", "how long is log.txt", wc, nil},
		{"an invented number", "log.txt has 12 lines.", "how long is log.txt", wc, []string{"12"}},
		{"a count of the lines printed", "There are 2 files: a.txt and big.bin.", "list files", ls, nil},
		{"an invented file", "The largest file is huge.iso.", "largest file", ls, []string{"huge.iso"}},
		{"a number from the task", "Here are the 10 largest files.", "find the 10 largest files", ls, nil},
		{"files and a count only the analysis knew", "Moved 2 files, a.pdf and b.pdf, into PDFs.", "gather pdfs", moved, nil},
		{"a size read off the output", "big.bin is 200K.", "size", []loopStep{{command: "du -h big.bin", result: executor.Result{Stdout: "200K\tbig.bin\n"}}}, nil},
		{"a decimal nothing showed", "It uses 1.5 GB.", "space", ls, []string{"1.5"}},
		{"no claims at all", "Done, the folder is tidy.", "tidy", ls, nil},
		{"nothing run", "There are 3 files.", "how many files", nil, []string{"3"}},
		{"an exit code and a step number", "Step 1 finished with exit code 0.", "make a folder", []loopStep{{command: "mkdir stuff"}}, nil},
		{"a count still checked after steps", "There are 3 files in stuff.", "how many", []loopStep{{command: "mkdir stuff"}, {command: "ls stuff", result: executor.Result{Stdout: "one.txt\ntwo.txt\n"}}}, []string{"3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := unsupportedClaims(c.reply, c.task, c.history)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("unsupportedClaims(%q) = %q, want %q", c.reply, got, c.want)
			}
		})
	}
}

// A reply stating what nothing showed is not told as fact; the person is told
// it was left out and why, and the pieces already streamed are discarded,
// since no Answer follows them.
func TestRunLoopWithholdsAReplyNothingShowed(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := newAnswerServer(t, streamReply("There are ", "7 files."))
	defer srv.Close()
	evs, opt := collectEvents()
	var out, errOut bytes.Buffer
	if code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "", opt); code != 0 {
		t.Fatalf("exit %d; stderr %s", code, errOut.String())
	}
	if countKind(*evs, loopevent.Answer) != 0 {
		t.Error("the unsupported reply was told as the Answer")
	}
	var withheld, kept bool
	for _, e := range *evs {
		withheld = withheld || (e.Kind == loopevent.Notice && strings.Contains(e.Text, "left out my summary"))
		kept = kept || (e.Kind == loopevent.Note && strings.Contains(e.Text, "There are 7 files."))
	}
	if !withheld || !kept {
		t.Errorf("withheld notice %v, reply kept as a note %v: %v", withheld, kept, kinds(*evs))
	}
	// What the person was told must not carry the claim it withheld: a
	// notice that repeated "7" told it after all, and scored it.
	if got := told(*evs); reports(got, "7") {
		t.Errorf("the withheld number reached what was told: %q", got)
	}
}

// The text interface tells the same: no reply line, the notice instead.
func TestRunLoopWithholdsAReplyInTextMode(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := newAnswerServer(t, streamReply("There are 7 files."))
	defer srv.Close()
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "")
	if strings.Contains(out.String(), "There are 7 files.") || !strings.Contains(out.String(), "left out my summary") {
		t.Errorf("text output:\n%s", out.String())
	}
}

// After a search that matched nothing, the person is told which search it was:
// "there are no warnings" may rest on looking for the wrong word.
func TestRunLoopSaysWhenTheLastSearchFoundNothing(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
		want  bool
	}{
		{"grep that matched nothing", []string{"grep -c WARNING app.log", "DONE"}, true},
		{"a search that found something", []string{"grep -c WARN app.log", "DONE"}, false},
		{"a change that printed nothing", []string{"touch made", "DONE"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withAnalysis(t, "strict")
			t.Chdir(t.TempDir())
			os.WriteFile("app.log", []byte("WARN bad\n"), 0o644)
			_, _, evs := runInDir(t, c.steps)
			var said bool
			for _, e := range evs {
				said = said || (e.Kind == loopevent.Notice && strings.Contains(e.Text, "found nothing"))
			}
			if said != c.want {
				t.Errorf("found-nothing notice = %v, want %v: %v", said, c.want, kinds(evs))
			}
		})
	}
}
