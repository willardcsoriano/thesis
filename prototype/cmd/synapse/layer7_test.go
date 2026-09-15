//go:build live

// Layer 7 of testing-plan.md: proficiency-tiered utterance robustness.
//
// Every other layer feeds the model a phrasing a developer wrote. This one
// asks a different question, and it is the question the thesis actually rests
// on: does the system work for someone who cannot describe what they want in
// computer vocabulary?
//
// The study's task suite (Appendix C) gives each task a single prompt. Real
// participants will not use it. The same intent arrives as "what's eating my
// disk space", "show me the biggest files in this folder", or "list the ten
// largest files by size" depending on who is asking — and the first of those
// is the target user, per vision.md's hypothesis that the advantage is largest
// for people fluent in neither existing interface.
//
// Each task is therefore phrased three ways:
//
//   - tierPlain      — everyday words, no computer vocabulary at all. Names a
//     goal, not a mechanism. This is the target user.
//   - tierInterface  — GUI vocabulary: folder, file, application. Knows their
//     way around a desktop, not a terminal.
//   - tierTechnical  — shell vocabulary, tool names, flags. Could have typed
//     the command themselves.
//
// Scoring is on the fixture's real end state, never on the command text: many
// commands satisfy a request and the study scores outcomes, not phrasing.
// Each (task, tier) pair runs against its own fresh fixture so one tier's
// effects cannot influence the next.
//
// The result is a matrix, and its shape is the finding. Passing at every tier
// means the interface is doing its job. Passing only at tierTechnical means
// the system still rewards knowing the words, which would undercut the
// thesis's central claim — that is a legitimate outcome to report, not a bug
// to prompt-engineer away, and it is exactly what the user study is designed
// to measure at larger scale.
//
// Run with:
//
//	go test -tags live ./cmd/synapse/... -run TestLayer7UtteranceTiers -v
package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"synapseos/internal/ollama"
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
	// verify reports whether the outcome satisfies the intent, and why not. It
	// receives the fixture directory and the full transcript, because a
	// read-only task leaves no filesystem trace and can only be scored on what
	// the user was actually told.
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

// said strips SynapseOS's own bookkeeping from a transcript, leaving what the
// user was actually told: command stdout, the answer, and any refusal or
// blocked message.
//
// This function should not need to exist. The core hands every interface a
// flat io.Writer (open-problems.md row 2), so a command's output and the
// loop's own narration arrive on the same undifferentiated stream with
// nothing but a line prefix to tell them apart. Scoring the raw transcript is
// what made three verifiers here vacuous: "did it report 2 warnings" was
// satisfied by the string "step 2:", and "did it report 4" by the "4" in a
// latency. Both passed at every tier regardless of what the model did.
//
// Prefix-scraping is the wrong fix and is kept deliberately visible as
// evidence for that row: the seam should carry structure, and until it does,
// this is the honest measurement and it is fragile by construction.
func said(transcript string) string {
	noise := []string{"step ", "  command: ", "  stats: ", "exit code: ", "task complete in ", "note: "}
	var keep []string
	for _, line := range strings.Split(transcript, "\n") {
		drop := false
		for _, p := range noise {
			if strings.HasPrefix(line, p) {
				drop = true
				break
			}
		}
		if !drop {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

// reports checks that a number appears in what the user was told as a value
// rather than as a digit inside some larger number: "60" must not be
// satisfied by "160" or by a timestamp.
func reports(transcript, want string) bool {
	for _, f := range strings.FieldsFunc(said(transcript), func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.'
	}) {
		if f == want || strings.TrimSuffix(f, ".") == want {
			return true
		}
	}
	return false
}

// sizePattern matches a human-readable size ("296K", "1.2M") or a raw byte
// count large enough to be this fixture's 300 KB of filler rather than an
// incidental small number.
var sizePattern = regexp.MustCompile(`\b\d+(\.\d+)?\s?[KMG]i?B?\b|\b[1-9]\d{5,}\b`)

// reportsSize accepts any of the forms du can produce, because the unit
// depends on which flag the model chose and the task did not specify one.
func reportsSize(transcript string) bool {
	return sizePattern.MatchString(said(transcript))
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
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(said(out), "big.bin") {
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
			verify: func(d, out string) (bool, string) {
				if !strings.Contains(said(out), "alpha.txt") {
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

// layer7Repeats is how many times each (task, tier) pair runs. A single run
// of a nondeterministic model is an anecdote; the pass *rate* is the finding,
// and the variance across repeats says whether a failure is systematic or a
// coin flip. Override with SYNAPSE_LAYER7_REPEATS when iterating.
var layer7Repeats = envIntOr("SYNAPSE_LAYER7_REPEATS", 3)

func TestLayer7UtteranceTiers(t *testing.T) {
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("needs a running Ollama: %v", err)
	}
	model := envOr("SYNAPSE_MODEL", defaultModel)

	corpus := append(utteranceCorpus(), extraUtteranceCorpus()...)
	if only := os.Getenv("SYNAPSE_LAYER7_ONLY"); only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		var keep []utteranceTask
		for _, task := range corpus {
			if want[task.id] {
				keep = append(keep, task)
			}
		}
		corpus = keep
	}
	reps := layer7Repeats
	passes := map[string][3]int{}
	cleans := map[string][3]int{}

	for _, task := range corpus {
		var row, clean [3]int
		for ti := range task.phrasings {
			for r := 0; r < reps; r++ {
				dir := t.TempDir()
				task.setup(t, dir)
				t.Chdir(dir)

				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				var out, errOut strings.Builder
				// Auto-approve: this layer measures whether intent was
				// understood, not whether the gate works — the gate has its
				// own tests and the fixture is disposable.
				code := runLoop(ctx, client, model, task.phrasings[ti],
					func(string) bool { return true },
					io.Writer(&out), io.Writer(&errOut), "")
				cancel()

				ok, why := task.verify(dir, out.String())
				if ok {
					row[ti]++
				} else {
					t.Logf("%-4s %-10s run %d  MISS  %s", task.id, tier(ti), r+1, why)
					t.Logf("    said: %s", tail(said(out.String()), 500))
					// runLoop reports propose/execute errors to errOut, not to
					// the transcript. Without this a backend failure looks
					// identical to a model that understood nothing: a whole
					// 108-invocation run on 2026-09-14 came back at ~10% with
					// empty transcripts and no way to tell the two apart.
					if e := strings.TrimSpace(errOut.String()); e != "" {
						t.Logf("    stderr: %s", tail(e, 300))
					}
				}
				// Tracked apart from intent on purpose. runLoop returns
				// non-zero on five paths (propose_error, repeated_failure,
				// unsupported, execution_error, step_limit), and a task can
				// reach the right end state down any of them — the command
				// ran, then the loop failed to recognise it was finished.
				// Folding that into the tier comparison would charge a
				// termination defect to the phrasing, which is the one thing
				// this file exists to measure cleanly.
				if code == 0 {
					clean[ti]++
				} else if ok {
					t.Logf("%-4s %-10s run %d  DIRTY intent satisfied but exit %d",
						task.id, tier(ti), r+1, code)
					t.Logf("    said: %s", tail(said(out.String()), 500))
				}
			}
		}
		passes[task.id] = row
		cleans[task.id] = clean
		t.Logf("%-4s %-18s plain %d/%d  interface %d/%d  technical %d/%d   (clean exit %d/%d/%d)",
			task.id, task.category, row[0], reps, row[1], reps, row[2], reps,
			clean[0], clean[1], clean[2])
	}

	t.Log("")
	t.Logf("=== Layer 7 — %d tasks x 3 tiers x %d repeats ===", len(corpus), reps)
	var totals, cleanTotals [3]int
	for _, task := range corpus {
		r, c := passes[task.id], cleans[task.id]
		for i := range totals {
			totals[i] += r[i]
			cleanTotals[i] += c[i]
		}
	}
	n := len(corpus) * reps
	pct := func(v int) float64 { return 100 * float64(v) / float64(n) }
	t.Log("intent satisfied (the thesis metric — scored on end state):")
	t.Logf("  plain      %3d/%d  %5.1f%%", totals[0], n, pct(totals[0]))
	t.Logf("  interface  %3d/%d  %5.1f%%", totals[1], n, pct(totals[1]))
	t.Logf("  technical  %3d/%d  %5.1f%%", totals[2], n, pct(totals[2]))
	t.Logf("  plain-to-technical gap: %.1f points", pct(totals[2])-pct(totals[0]))
	t.Log("clean exit (engineering metric — did the loop know it was done):")
	t.Logf("  plain      %3d/%d  %5.1f%%", cleanTotals[0], n, pct(cleanTotals[0]))
	t.Logf("  interface  %3d/%d  %5.1f%%", cleanTotals[1], n, pct(cleanTotals[1]))
	t.Logf("  technical  %3d/%d  %5.1f%%", cleanTotals[2], n, pct(cleanTotals[2]))

	// Deliberately not asserted. A low plain-tier score is a finding about the
	// system and the model, not a broken test — see this file's header.
}

// The scoring helpers are the reason to trust or distrust every number this
// file produces, so they are tested directly. Each case below is one that was
// actually mis-scored before they existed.
func TestLayer7ScoringHelpers(t *testing.T) {
	const twoStepTranscript = "step 1: grep -c WARN app.log\n  stats: 143 tokens in 4.2s\n2\nexit code: 0\n\nstep 2: DONE\ntask complete in 1 step(s).\n"

	t.Run("bookkeeping is not scored", func(t *testing.T) {
		got := said(twoStepTranscript)
		for _, banned := range []string{"stats:", "exit code:", "task complete", "step 2"} {
			if strings.Contains(got, banned) {
				t.Errorf("said() kept bookkeeping %q:\n%s", banned, got)
			}
		}
		if !strings.Contains(got, "2") {
			t.Errorf("said() dropped the command output:\n%s", got)
		}
	})

	t.Run("a step number does not satisfy a count", func(t *testing.T) {
		// The original defect: "step 2:" made every U10 run pass.
		noOutput := "step 1: grep -c WARN app.log\n  stats: 9 tokens in 2.0s\nexit code: 1\n\nstep 2: DONE\ntask complete in 1 step(s).\n"
		if reports(noOutput, "2") {
			t.Error("reports() accepted a step number as the answer")
		}
		if !reports(twoStepTranscript, "2") {
			t.Error("reports() rejected a genuine count")
		}
	})

	t.Run("a digit inside a larger number does not count", func(t *testing.T) {
		if reports("160\n", "60") {
			t.Error("reports() matched 60 inside 160")
		}
		if !reports("60\n", "60") {
			t.Error("reports() rejected an exact match")
		}
	})

	t.Run("latency digits do not satisfy a count", func(t *testing.T) {
		if reports("step 1: wc -l log.txt\n  stats: 41 tokens in 4.1s\nexit code: 1\n", "4") {
			t.Error("reports() scored a latency as the line count")
		}
	})

	t.Run("size is recognised in any unit du might use", func(t *testing.T) {
		for _, ok := range []string{"296K\t.\n", "1.2M\t.\n", "304096\t.\n", "296 KB\n"} {
			if !reportsSize(ok) {
				t.Errorf("reportsSize() missed %q", ok)
			}
		}
		for _, bad := range []string{"exit code: 0\n", "nothing here\n", "4\t.\n"} {
			if reportsSize(bad) {
				t.Errorf("reportsSize() accepted %q", bad)
			}
		}
	})
}
