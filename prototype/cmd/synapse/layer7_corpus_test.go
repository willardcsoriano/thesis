package main

// Layer 7's task definitions and fixture helpers (development set), shared by
// the live run (layer7_test.go) and the model-free checks of the verifiers
// (layer7_corpus_check_test.go). Untagged so those checks run in every go test.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tier int

const (
	tierPlain tier = iota
	tierInterface
	tierTechnical
)

func (t tier) String() string {
	return [...]string{"plain", "interface", "technical"}[t]
}

// utteranceTask is one intent, phrased three ways, with a fixture to act on
// and a check on the resulting state.
type utteranceTask struct {
	id       string
	category string
	// setup populates a fresh working directory. The utterances refer to it
	// as "here" or "this folder" — no absolute paths, because a plain-tier
	// speaker would not use one.
	setup func(t *testing.T, dir string)
	// phrasings is indexed by tier.
	phrasings [3]string
	// solution is one shell command that satisfies the intent. It is never
	// shown to a model: layer7_corpus_check_test.go runs it to check that the
	// verifier accepts a right answer.
	solution string
	// verify reports whether the outcome satisfies the intent, and why not. It
	// receives the fixture directory and what the person was told (told(), in
	// layer7_score_test.go), because a read-only task leaves no filesystem
	// trace and can only be scored on that.
	verify func(dir, transcript string) (bool, string)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tail returns the last n bytes of a transcript on one line, which is where
// the loop's exit path announces itself ("repeated_failure", the unsupported
// message, the step-limit answer). A failure reason alone does not say which.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "..." + s[len(s)-n:]
	}
	return strings.ReplaceAll(s, "\n", " | ")
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func utteranceCorpus() []utteranceTask {
	return []utteranceTask{
		{
			id: "U1", category: "file search",
			setup: func(t *testing.T, d string) {
				write(t, d, "a.pdf", "x")
				write(t, d, "b.pdf", "x")
				write(t, d, "notes.txt", "x")
			},
			phrasings: [3]string{
				"put all my pdfs together in one place",
				"move every PDF file in this folder into a new folder called PDFs",
				"create a directory PDFs and mv all *.pdf into it",
			},
			solution: `mkdir PDFs && mv *.pdf PDFs/`,
			verify: func(d, out string) (bool, string) {
				if !exists(d, "PDFs/a.pdf") || !exists(d, "PDFs/b.pdf") {
					return false, "pdfs not both in PDFs/"
				}
				if !exists(d, "notes.txt") {
					return false, "unrelated file was moved"
				}
				return true, ""
			},
		},
		{
			id: "U2", category: "file search",
			setup: func(t *testing.T, d string) {
				write(t, d, "small.txt", "x")
				write(t, d, "big.bin", strings.Repeat("y", 200000))
			},
			phrasings: [3]string{
				"what's taking up the most room here",
				"show me the largest file in this folder",
				"list files here sorted by size, largest first",
			},
			solution: `ls -S | head -1`,
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(out, "big.bin") {
					return false, "never named big.bin as the largest"
				}
				return true, ""
			},
		},
		{
			id: "U3", category: "text processing",
			setup: func(t *testing.T, d string) { write(t, d, "notes.txt", "draft one\ndraft two\n") },
			phrasings: [3]string{
				"in notes.txt change the word draft to final everywhere",
				"replace all occurrences of draft with final in notes.txt",
				"run a sed in-place substitution of draft to final on notes.txt",
			},
			solution: `sed -i s/draft/final/g notes.txt`,
			verify: func(d, out string) (bool, string) {
				b, err := os.ReadFile(filepath.Join(d, "notes.txt"))
				if err != nil {
					return false, "notes.txt gone"
				}
				if strings.Contains(string(b), "draft") {
					return false, "draft still present"
				}
				if !strings.Contains(string(b), "final") {
					return false, "final not written"
				}
				return true, ""
			},
		},
		{
			id: "U4", category: "text processing",
			setup: func(t *testing.T, d string) { write(t, d, "log.txt", "a\nb\nc\nd\n") },
			phrasings: [3]string{
				"how long is log.txt",
				"count the number of lines in log.txt",
				"wc -l on log.txt",
			},
			solution: `wc -l log.txt`,
			verify: func(d, out string) (bool, string) {
				if !reports(out, "4") {
					return false, "did not report 4 lines"
				}
				return true, ""
			},
		},
		{
			id: "U5", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "keep.txt", "x")
				write(t, d, "junk.tmp", "x")
				write(t, d, "other.tmp", "x")
			},
			phrasings: [3]string{
				"get rid of the temporary files here",
				"delete all files ending in .tmp in this folder",
				"rm -f *.tmp",
			},
			solution: `rm -f *.tmp`,
			verify: func(d, out string) (bool, string) {
				if exists(d, "junk.tmp") || exists(d, "other.tmp") {
					return false, "tmp files remain"
				}
				if !exists(d, "keep.txt") {
					return false, "deleted something it should not have"
				}
				return true, ""
			},
		},
		{
			id: "U6", category: "file organisation",
			setup: func(t *testing.T, d string) { write(t, d, "report.txt", "x") },
			phrasings: [3]string{
				"give report.txt a different name, call it summary.txt",
				"rename report.txt to summary.txt",
				"mv report.txt summary.txt",
			},
			solution: `mv report.txt summary.txt`,
			verify: func(d, out string) (bool, string) {
				if !exists(d, "summary.txt") {
					return false, "summary.txt missing"
				}
				if exists(d, "report.txt") {
					return false, "original still present — copied rather than renamed"
				}
				return true, ""
			},
		},
	}
}

// extraUtteranceCorpus brings the suite to twelve intents, matching the core
// task count of the study's own suite (Appendix C) and spreading across its
// four categories. System, process, and package tasks are represented by
// read-only variants: stopping a process or removing a package is not
// something an automated suite should do repeatedly on a real machine, and
// the intent-parsing question this layer asks is unaffected by whether the
// verb mutates system state.
func extraUtteranceCorpus() []utteranceTask {
	return []utteranceTask{
		{
			id: "U7", category: "file search",
			setup: func(t *testing.T, d string) {
				write(t, d, "alpha.txt", "needle here\n")
				write(t, d, "beta.txt", "nothing\n")
			},
			phrasings: [3]string{
				"which file in here mentions the word needle",
				"search this folder for files containing the text needle",
				"grep -rl needle .",
			},
			solution: `grep -rl needle .`,
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(out, "alpha.txt") {
					return false, "did not identify alpha.txt"
				}
				return true, ""
			},
		},
		{
			id: "U8", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "one.log", "x")
				write(t, d, "two.log", "x")
				write(t, d, "keep.txt", "x")
			},
			phrasings: [3]string{
				"tidy the log files away into their own folder",
				"move all .log files in this folder into a folder named logs",
				"mkdir -p logs && mv *.log logs/",
			},
			solution: `mkdir -p logs && mv *.log logs/`,
			verify: func(d, out string) (bool, string) {
				if !exists(d, "logs/one.log") || !exists(d, "logs/two.log") {
					return false, "logs not both relocated"
				}
				if !exists(d, "keep.txt") {
					return false, "moved an unrelated file"
				}
				return true, ""
			},
		},
		{
			id: "U9", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "data.csv", "name,score\nann,10\nbob,20\ncat,30\n")
			},
			phrasings: [3]string{
				"add up all the scores in data.csv for me",
				"sum the values in the score column of data.csv",
				"awk -F, 'NR>1{s+=$2}END{print s}' data.csv",
			},
			solution: `awk -F, 'NR>1{s+=$2}END{print s}' data.csv`,
			verify: func(d, out string) (bool, string) {
				if !reports(out, "60") {
					return false, "did not report the total 60"
				}
				return true, ""
			},
		},
		{
			id: "U10", category: "text processing",
			setup: func(t *testing.T, d string) {
				write(t, d, "app.log", "INFO ok\nWARN bad\nINFO ok\nWARN bad\n")
			},
			phrasings: [3]string{
				"how many warnings are in app.log",
				"count the lines containing WARN in app.log",
				"grep -c WARN app.log",
			},
			solution: `grep -c WARN app.log`,
			verify: func(d, out string) (bool, string) {
				if !reports(out, "2") {
					return false, "did not report 2"
				}
				return true, ""
			},
		},
		{
			id: "U11", category: "system monitoring",
			setup: func(t *testing.T, d string) {
				write(t, d, "filler.bin", strings.Repeat("z", 300000))
			},
			phrasings: [3]string{
				"how much space is this folder using",
				"report the total disk usage of this folder",
				"du -sh .",
			},
			solution: `du -sh .`,
			verify: func(d, out string) (bool, string) {
				if !reportsSize(out) {
					return false, "no size reported"
				}
				return true, ""
			},
		},
		{
			id: "U12", category: "file organisation",
			setup: func(t *testing.T, d string) {
				write(t, d, "src/deep/file.txt", "x")
				write(t, d, "src/top.txt", "x")
			},
			phrasings: [3]string{
				"make a copy of everything in src, call the copy backup",
				"recursively copy the src folder to a new folder called backup",
				"cp -r src backup",
			},
			solution: `cp -r src backup`,
			verify: func(d, out string) (bool, string) {
				if !exists(d, "backup/deep/file.txt") || !exists(d, "backup/top.txt") {
					return false, "copy incomplete or not recursive"
				}
				if !exists(d, "src/top.txt") {
					return false, "moved instead of copied"
				}
				return true, ""
			},
		},
	}
}
