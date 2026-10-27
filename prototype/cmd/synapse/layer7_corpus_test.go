package main

// Layer 7's task definitions and fixture helpers (development set), shared by
// the live run (layer7_test.go) and the model-free checks of the verifiers
// (layer7_corpus_check_test.go). Untagged so those checks run in every go test.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// plainAlso accepts a second reading of the plain phrasing only, for a
	// request a person could fairly mean two ways ("how long is log.txt":
	// lines or bytes; "put all my pdfs together": one folder or one file).
	// The owner decided on 2026-10-26, before any harness change was measured
	// against it, that such a request is answered by either reading (D52).
	// The interface and technical phrasings name one reading and keep it.
	// plainAlsoSolution performs the second reading, and the corpus check
	// runs it, so the alternative is known to be reachable and recognised.
	plainAlso         func(dir, out string) (bool, string)
	plainAlsoSolution string
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

// inFolder reports whether every one of names is a regular file directly in
// dir/folder: the check for a wording that names the folder.
func inFolder(dir, folder string, names ...string) bool {
	for _, n := range names {
		if fi, err := os.Lstat(filepath.Join(dir, folder, n)); err != nil || !fi.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// gathered reports whether one folder below dir, of any name and at any
// depth, holds every one of names. It is the check for "put these together"
// when the wording leaves the folder's name to the speaker: a plain-tier
// "in one place" is met by pdfs/ as much as by PDFs/, and a check that wanted
// one spelling would score a right answer as a miss. dir itself does not
// count, since the fixture already holds the files there.
func gathered(dir string, names ...string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || !e.IsDir() || p == dir {
			return nil
		}
		for _, n := range names {
			if fi, err := os.Lstat(filepath.Join(p, n)); err != nil || !fi.Mode().IsRegular() {
				return nil
			}
		}
		found = true
		return filepath.SkipAll
	})
	return found
}

func utteranceCorpus() []utteranceTask {
	return []utteranceTask{
		{
			id: "U1", category: "file search",
			setup: func(t *testing.T, d string) {
				// Real one-page PDFs, so the merge reading of the plain
				// phrasing can actually succeed (plainAlso).
				write(t, d, "a.pdf", string(minimalPDF("a")))
				write(t, d, "b.pdf", string(minimalPDF("b")))
				write(t, d, "notes.txt", "x")
			},
			phrasings: [3]string{
				"put all my pdfs together in one place",
				"move every PDF file in this folder into a new folder called PDFs",
				"create a directory PDFs and mv all *.pdf into it",
			},
			solution: `mkdir PDFs && mv *.pdf PDFs/`,
			verify: func(d, out string) (bool, string) {
				// The interface and technical wordings name the folder, and
				// keep that reading (D52); the plain one names none, and
				// plainAlso accepts any folder for it.
				if !inFolder(d, "PDFs", "a.pdf", "b.pdf") {
					return false, "pdfs not both in PDFs/"
				}
				if !exists(d, "notes.txt") {
					return false, "unrelated file was moved"
				}
				return true, ""
			},
			plainAlsoSolution: `pdfunite a.pdf b.pdf all.pdf`,
			plainAlso: func(d, out string) (bool, string) {
				// "In one place" names no folder, so any one folder holds
				// them together; and together in one file is a new PDF
				// holding both pages.
				if !exists(d, "notes.txt") {
					return false, "unrelated file was moved"
				}
				if !gathered(d, "a.pdf", "b.pdf") && mergedPDF(d, 2) == "" {
					return false, "pdfs not in one folder, and no PDF holding both"
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
			plainAlsoSolution: `stat -c '%s bytes' log.txt`,
			plainAlso: func(d, out string) (bool, string) {
				// How long in size: 8 bytes, said as bytes.
				if !reports(out, "8") || !strings.Contains(strings.ToLower(out), "byte") {
					return false, "did not report 8 bytes"
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
				// The named folder at the tiers that name it, as in U1.
				if !inFolder(d, "logs", "one.log", "two.log") {
					return false, "logs not both in logs/"
				}
				if !exists(d, "keep.txt") {
					return false, "moved an unrelated file"
				}
				return true, ""
			},
			// "Their own folder" names none.
			plainAlsoSolution: `mkdir old && mv *.log old/`,
			plainAlso: func(d, out string) (bool, string) {
				if !gathered(d, "one.log", "two.log") {
					return false, "logs not both in one folder"
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

// score is how a run of the task at tier is judged: by verify, or, for the
// plain tier of a task with a second fair reading, by either (see plainAlso).
func (u utteranceTask) score(tier int, dir, out string) (bool, string) {
	ok, why := u.verify(dir, out)
	if !ok && tier == 0 && u.plainAlso != nil {
		if ok2, _ := u.plainAlso(dir, out); ok2 {
			return true, ""
		}
	}
	return ok, why
}

// minimalPDF is a valid one-page PDF showing label, with correct
// cross-reference offsets, so real PDF tools accept it.
func minimalPDF(label string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>",
	}
	stream := fmt.Sprintf("BT /F1 12 Tf 20 100 Td (%s) Tj ET", label)
	objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return []byte(b.String())
}

// mergedPDF returns the first PDF anywhere under dir, other than the two
// originals, that pdfinfo reads as having at least pages pages, or "". It asks
// pdfinfo rather than scanning bytes, because a merging tool may compress
// its page objects (Ghostscript does).
func mergedPDF(dir string, pages int) string {
	found := ""
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(strings.ToLower(p), ".pdf") {
			return nil
		}
		if n := filepath.Base(p); (n == "a.pdf" || n == "b.pdf") && filepath.Dir(p) == dir {
			return nil
		}
		out, err := exec.Command("pdfinfo", p).Output()
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(line, "Pages:"); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= pages {
					found = p
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	return found
}
