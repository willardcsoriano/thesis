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
	"strings"
	"testing"
	"time"

	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
)

// layer7Repeats is how many times each (task, tier) pair runs. A single run
// of a nondeterministic model is an anecdote; the pass *rate* is the finding,
// and the variance across repeats says whether a failure is systematic or a
// coin flip. Override with SYNAPSE_LAYER7_REPEATS when iterating.
var layer7Repeats = envIntOr("SYNAPSE_LAYER7_REPEATS", 3)

// layer7RunTimeout bounds one (task, tier) run. A person waits about this long
// for one request; a run cut here counts as a miss, and its RUN line says
// capped=true so a timing taken from it is known to be a lower bound.
const layer7RunTimeout = 3 * time.Minute

func TestLayer7UtteranceTiers(t *testing.T) {
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("needs a running Ollama: %v", err)
	}
	model := envOr("SYNAPSE_MODEL", defaultModel)
	t.Logf("analysis: %s", shippedAnalysis(t))

	sets := layer7Sets(t, os.Getenv("SYNAPSE_LAYER7_SET"))
	if only := os.Getenv("SYNAPSE_LAYER7_ONLY"); only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		for i := range sets {
			var keep []utteranceTask
			for _, task := range sets[i].tasks {
				if want[task.id] {
					keep = append(keep, task)
				}
			}
			sets[i].tasks = keep
		}
	}
	for _, set := range sets {
		if len(set.tasks) > 0 {
			runLayer7Set(t, client, model, set)
		}
	}
}

// layer7Set is a named list of tasks. The development set is the one the
// system was built and tuned against with the 3B model, so it is development
// data for that model; the held-out set (layer7_heldout_test.go) was written
// and frozen before any candidate ran on it (D46). Every result names its set.
type layer7Set struct {
	name  string
	tasks []utteranceTask
}

// layer7Sets picks the sets SYNAPSE_LAYER7_SET names: development (the default),
// heldout, or both. The held-out set runs only when asked for by name: a run
// that names nothing is a tuning run, and one that also ran the held-out set
// would spend it (ship review, 2026-10-27). An unknown name fails rather than
// running nothing.
func layer7Sets(t *testing.T, which string) []layer7Set {
	dev := layer7Set{"development", append(utteranceCorpus(), extraUtteranceCorpus()...)}
	held := layer7Set{"heldout", heldOutUtteranceCorpus()}
	switch which {
	case "both":
		return []layer7Set{dev, held}
	case "", "development":
		return []layer7Set{dev}
	case "heldout":
		return []layer7Set{held}
	}
	t.Fatalf("SYNAPSE_LAYER7_SET=%q: want development (the default), heldout, or both", which)
	return nil
}

func runLayer7Set(t *testing.T, client *ollama.Client, model string, set layer7Set) {
	corpus := set.tasks
	reps := layer7Repeats
	passes := map[string][3]int{}
	cleans := map[string][3]int{}
	var repeats [3]int

	for _, task := range corpus {
		var row, clean [3]int
		for ti := range task.phrasings {
			for r := 0; r < reps; r++ {
				dir := t.TempDir()
				task.setup(t, dir)
				t.Chdir(dir)

				started := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), layer7RunTimeout)
				var out, errOut strings.Builder
				rec := &layer7Recorder{}
				var outcome string
				// Auto-approve: this layer measures whether intent was
				// understood, not whether the gate works — the gate has its
				// own tests and the fixture is disposable. The loop reports
				// through typed events, so only what reached the person is
				// scored (layer7_score_test.go).
				code := runLoop(ctx, client, model, task.phrasings[ti],
					func(string) bool { return true },
					io.Writer(&out), io.Writer(&errOut), "", withEvents(rec.emit),
					withOutcome(func(o string) { outcome = o }))
				capped := ctx.Err() == context.DeadlineExceeded
				cancel()
				elapsed := time.Since(started)
				// In event mode the loop writes no narration as text. Text
				// here would be narration that bypassed the events and was
				// therefore never scored.
				if out.Len() > 0 {
					t.Errorf("%s %s: the loop wrote text in event mode, outside what is scored: %s",
						task.id, tier(ti), tail(out.String(), 300))
				}

				ok, why := task.score(ti, dir, told(rec.events))
				// One machine-readable line per run, for timing a model
				// (tools/model-box reads it). capped means the run was cut
				// at layer7RunTimeout, so its time is a lower bound.
				t.Logf("RUN set=%s task=%s tier=%s rep=%d secs=%.1f exit=%d ok=%t capped=%t outcome=%s",
					set.name, task.id, tier(ti), r+1, elapsed.Seconds(), code, ok, capped, outcome)
				// Every command the model proposed, passed or not: how a
				// model translates a phrasing is the comparison's subject,
				// and a log of misses alone hides what the good runs did.
				for _, e := range rec.events {
					if e.Kind == loopevent.Command {
						t.Logf("CMD set=%s task=%s tier=%s rep=%d step=%d %q",
							set.name, task.id, tier(ti), r+1, e.Step, e.Command)
					}
				}
				if ok {
					row[ti]++
				} else {
					t.Logf("%-4s %-10s run %d  MISS  %s", task.id, tier(ti), r+1, why)
					t.Logf("    run: %s", tail(narrate(rec.events), 600))
					// runLoop reports propose/execute errors to errOut, not to
					// the events. Without this a backend failure looks
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
				// Clean means the model said it was done. A stop on a repeated
				// command also exits 0, but the loop ended it, not the model, so
				// it is counted on its own line.
				if outcome == outcomeRepeatedSuccess {
					repeats[ti]++
				}
				if outcome == outcomeComplete || outcome == outcomeNothingToDo {
					clean[ti]++
				} else if ok {
					t.Logf("%-4s %-10s run %d  DIRTY intent satisfied but exit %d (%s)",
						task.id, tier(ti), r+1, code, outcome)
					t.Logf("    run: %s", tail(narrate(rec.events), 600))
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
	t.Logf("=== Layer 7, %s set, model %s — %d tasks x 3 tiers x %d repeats ===", set.name, model, len(corpus), reps)
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
	t.Log("stopped on a repeated command (exit 0, but the model never said it was done):")
	t.Logf("  plain %d  interface %d  technical %d", repeats[0], repeats[1], repeats[2])

	// Deliberately not asserted. A low plain-tier score is a finding about the
	// system and the model, not a broken test — see this file's header.
}
