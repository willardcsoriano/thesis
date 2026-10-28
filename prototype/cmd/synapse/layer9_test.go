//go:build live

// Layer 9: multi-turn conversations, scored turn by turn on real end state.
// The conversations and their model-free checks are in layer9_corpus_test.go
// (development), layer9_heldout_test.go and layer9_heldout2_test.go (held
// out, frozen).
//
// Each conversation runs in one session, as the REPL runs one: the same
// session memory and the same undo journal across its turns, and "undo"
// answered by synapse itself rather than the model. That dispatch mirrors
// runREPL's (modes.go); the two are kept apart only because runREPL writes
// text and this layer scores events.
//
// Like Layer 7, it asserts nothing about the model: a low score is a finding.
//
// Run with:
//
//	go test -tags live ./cmd/synapse/... -run TestLayer9Conversations -v
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/ollama"
	"synapseos/internal/session"
)

var layer9Repeats = envIntOr("SYNAPSE_LAYER9_REPEATS", 3)

func TestLayer9Conversations(t *testing.T) {
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("needs a running Ollama: %v", err)
	}
	model := envOr("SYNAPSE_MODEL", defaultModel)
	t.Logf("analysis: %s", shippedAnalysis(t))
	sets := []struct {
		name  string
		convs []conversation
	}{
		{"development", conversationCorpus()},
		{"heldout", heldOutConversationCorpus()},
		{"heldout2", heldOut2ConversationCorpus()},
	}
	// The held-out set runs only when named, as in Layer 7: an unnamed run is
	// a tuning run and must not spend it.
	which := os.Getenv("SYNAPSE_LAYER9_SET")
	switch which {
	case "":
		which = "development"
	case "development", "heldout", "both", "heldout2":
	default:
		t.Fatalf("SYNAPSE_LAYER9_SET=%q: want development (the default), heldout, both, or heldout2", which)
	}
	// SYNAPSE_LAYER9_ONLY names conversation ids to run, as Layer 7's _ONLY
	// does, so a run can be split across processes (tools/layer-box).
	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("SYNAPSE_LAYER9_ONLY"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			only[id] = true
		}
	}
	for _, set := range sets {
		// "both" is development and heldout; heldout2 runs only by name.
		both := which == "both" && (set.name == "development" || set.name == "heldout")
		if !both && which != set.name {
			continue
		}
		convs := set.convs
		if len(only) > 0 {
			convs = nil
			for _, c := range set.convs {
				if only[c.id] {
					convs = append(convs, c)
				}
			}
		}
		if len(convs) > 0 {
			runLayer9Set(t, client, model, set.name, convs)
		}
	}
}

func runLayer9Set(t *testing.T, client *ollama.Client, model, setName string, convs []conversation) {
	turnsPassed, turnsRun, wholePassed, wholeRun := 0, 0, 0, 0
	for _, c := range convs {
		for r := 0; r < layer9Repeats; r++ {
			dir := t.TempDir()
			c.setup(t, dir)
			t.Chdir(dir)
			journal := filepath.Join(t.TempDir(), "undo.log")
			sc := session.New()
			all := true
			for ti, turn := range c.turns {
				started := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), layer7RunTimeout)
				var said, outcome string
				var code int
				var transcript string
				if isUndoRequest(turn.say) {
					var out, errOut strings.Builder
					code = runUndo(func(string) bool { return true }, &out, &errOut, journal, nil, "")
					said, outcome, transcript = out.String(), "undo", out.String()+errOut.String()
				} else {
					rec := &layer7Recorder{}
					var out, errOut strings.Builder
					// Auto-approve, as in Layer 7: this measures understanding,
					// and the gate has its own tests.
					code = runLoop(ctx, client, model, turn.say, func(string) bool { return true },
						&out, &errOut, journal, withEvents(rec.emit), withSessionContext(sc),
						withOutcome(func(o string) { outcome = o }))
					if out.Len() > 0 {
						t.Errorf("%s turn %d: the loop wrote text in event mode, outside what is scored: %s",
							c.id, ti+1, tail(out.String(), 300))
					}
					said, transcript = told(rec.events), narrate(rec.events)
					if e := strings.TrimSpace(errOut.String()); e != "" {
						transcript += " | stderr: " + e
					}
				}
				capped := ctx.Err() == context.DeadlineExceeded
				cancel()
				ok, why := turn.verify(dir, said)
				t.Logf("TURN set=%s conv=%s turn=%d rep=%d secs=%.1f exit=%d ok=%t capped=%t outcome=%s",
					setName, c.id, ti+1, r+1, time.Since(started).Seconds(), code, ok, capped, outcome)
				turnsRun++
				if ok {
					turnsPassed++
				} else {
					all = false
					t.Logf("%-3s turn %d run %d  MISS  %s  (said %q)", c.id, ti+1, r+1, why, turn.say)
					t.Logf("    run: %s", tail(transcript, 600))
				}
				// Every reply, passed or not: a reply that states something
				// the commands did not show is the failure this layer most
				// needs a person to read for, and it leaves no state to check.
				for _, line := range strings.Split(strings.TrimSpace(said), "\n") {
					if line != "" {
						t.Logf("TOLD conv=%s turn=%d rep=%d %q", c.id, ti+1, r+1, line)
					}
				}
			}
			wholeRun++
			if all {
				wholePassed++
			}
		}
	}
	t.Log("")
	t.Logf("=== Layer 9, %s set, model %s — %d conversations x %d repeats ===", setName, model, len(convs), layer9Repeats)
	if turnsRun == 0 {
		return
	}
	t.Logf("turns satisfied:          %3d/%d  %5.1f%%", turnsPassed, turnsRun, 100*float64(turnsPassed)/float64(turnsRun))
	t.Logf("conversations all right:  %3d/%d  %5.1f%%", wholePassed, wholeRun, 100*float64(wholePassed)/float64(wholeRun))
}
