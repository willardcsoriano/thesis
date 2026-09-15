//go:build live

// Layer 8: does qwen2.5-coder:3b fork correctly between talking and acting?
//
// The current loop is a completion architecture wearing a tool-calling
// costume. loopSystemPrompt asks for one command string, and reserves two
// magic words — DONE and UNSUPPORTED — to mean "stop" and "I can't". Those
// sentinels are a hand-rolled tool-calling protocol encoded in free text, and
// they are why "hello" fails: every output must be a command or a sentinel,
// so a greeting is forced into the nearest sentinel and the user is told
// their greeting was a visual task (open-problems.md row 4).
//
// Coding agents do not classify input as chat-or-task. Tool-calling IS the
// fork: the model emits a tool call when it wants to act and prose when it
// does not, from one prompt, with no router. Adopting that would close four
// open problems at once (rows 1, 2, 4, 16 — see the analysis in session
// notes). It would also be a significant rewrite of runLoop.
//
// So this measures the load-bearing assumption first, because the rewrite is
// worthless if the assumption is false. internal/ollama's own comment flags a
// populated ToolCalls field as "something to verify empirically rather than
// assume" — this is that verification.
//
// The two errors are not symmetric, and the report separates them:
//
//   - PROSE-WHEN-ACTION-NEEDED is the dangerous one. The user asked for work,
//     the model chatted, and nothing happened. Silent non-execution.
//   - CALL-WHEN-PROSE-NEEDED is the cheap one. The user said hello, the model
//     called a tool. Noisy and wrong, but visible and harmless.
//
// A design that fails the second way is workable. One that fails the first
// way is not, and no amount of prompt engineering downstream recovers it.
//
// Run with:
//
//	go test -tags live ./cmd/synapse/... -run TestLayer8ToolCallFork -v
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"synapseos/internal/ollama"
	"synapseos/internal/toolcall"
	"synapseos/internal/typedops"
)

// bashTool is offered alongside the typed operations because the real system
// cannot be typed-ops-only: testing-plan.md's task categories include process
// control, package management, and text processing, none of which the typed
// registry covers. Whether the model picks the typed op over bash when both
// would serve is itself a finding — it is the F6 decision (open-problems.md
// row 1) that has been open since Session 25.
var bashTool = ollama.Tool{
	Type: "function",
	Function: ollama.ToolFunction{
		Name:        "run_bash",
		Description: "Run a bash command on this machine and return its output. Use for anything the typed file operations do not cover: disk usage, processes, packages, text processing, network settings.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The bash command to run.",
				},
			},
			"required": []string{"command"},
		},
	},
}

const probeSystemPrompt = `You are SynapseOS, a conversational interface to a Linux machine running Debian 13.

You have tools that run real commands on the user's machine. Call a tool when the user wants something done to the machine or wants to know something about it. Reply in plain words, calling no tool, when the user is talking to you rather than asking for work — a greeting, a thank you, a question about what you are.

Never claim to have done something you did not do with a tool.`

// probeCase is one utterance and what a correct system would do with it.
type probeCase struct {
	utterance string
	// wantCall is true when the utterance requires acting on the machine.
	wantCall bool
	// tier records the proficiency register, so the report can say whether
	// the fork degrades for plain speakers the way Layer 7's did.
	tier string
}

func probeCorpus() []probeCase {
	return []probeCase{
		// Conversational: a correct system replies in prose, calls nothing.
		{"hello", false, "social"},
		{"hi there", false, "social"},
		{"what can you do", false, "social"},
		{"who are you", false, "social"},
		{"thanks, that worked", false, "social"},
		{"are you a human?", false, "social"},
		{"tell me a joke", false, "social"},
		{"how are you today", false, "social"},

		// Work, plain register — the target user, per vision.md.
		{"how much room is this folder taking up", true, "plain"},
		{"what's in here", true, "plain"},
		{"get rid of the junk files here", true, "plain"},
		{"how long is log.txt", true, "plain"},
		{"put the log files somewhere out of the way", true, "plain"},

		// Work, interface register.
		{"show me the largest file in this folder", true, "interface"},
		{"move all .log files into a folder called logs", true, "interface"},
		{"count the lines in log.txt", true, "interface"},
		{"how much disk space is free", true, "interface"},

		// Work, technical register.
		{"du -sh .", true, "technical"},
		{"wc -l log.txt", true, "technical"},
		{"find . -name '*.log' -delete", true, "technical"},

		// The trap: conversational opener attached to real work. Treating
		// these as chat is the dangerous error — the user asked for
		// something and would get a friendly reply and no action.
		{"hi, can you tell me how much space this folder uses", true, "trap"},
		{"hello, please count the lines in log.txt", true, "trap"},
		{"thanks! now delete the log files", true, "trap"},
	}
}

func TestLayer8ToolCallFork(t *testing.T) {
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("needs a running Ollama: %v", err)
	}
	model := envOr("SYNAPSE_MODEL", defaultModel)
	tools := append(typedops.Tools(), bashTool)
	reps := envIntOr("SYNAPSE_PROBE_REPEATS", 3)

	type counts struct{ correct, total int }
	byTier := map[string]*counts{}
	var proseWhenAction, callWhenProse, malformed int
	var bashPicked, typedPicked int

	for _, pc := range probeCorpus() {
		for r := 0; r < reps; r++ {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			resp, err := client.Chat(ctx, model, []ollama.ChatMessage{
				{Role: "system", Content: probeSystemPrompt},
				{Role: "user", Content: pc.utterance},
			}, tools, generationOptions())
			cancel()
			if err != nil {
				t.Logf("  %-52q ERROR %v", pc.utterance, err)
				continue
			}

			c := byTier[pc.tier]
			if c == nil {
				c = &counts{}
				byTier[pc.tier] = c
			}
			c.total++

			// Ollama never populates ToolCalls for this model, so the call
			// is recovered from the content field — that is what the
			// toolcall package exists for, and this probe measures the
			// system as it would actually run, parser included.
			known := func(n string) bool {
				return n == bashTool.Function.Name || typedops.Lookup(n) != nil
			}
			var gotCall bool
			call, perr := toolcall.Parse(resp.Message.Content, known)
			switch {
			case perr == nil:
				gotCall = true
				if call.Name == bashTool.Function.Name {
					bashPicked++
				} else {
					typedPicked++
				}
			case errors.Is(perr, toolcall.ErrNoCall):
				gotCall = false
			default:
				// An invented tool, or arguments with no recoverable value.
				// Counted apart from both branches: the model meant to act
				// and produced something undispatchable, which is neither a
				// correct call nor an honest refusal.
				malformed++
				t.Logf("  %-52q UNUSABLE: %v", pc.utterance, perr)
				continue
			}

			switch {
			case gotCall == pc.wantCall:
				c.correct++
			case pc.wantCall && !gotCall:
				proseWhenAction++
				reply := strings.TrimSpace(resp.Message.Content)
				if len(reply) > 90 {
					reply = reply[:90] + "…"
				}
				t.Logf("  %-52q PROSE-WHEN-ACTION-NEEDED: %s", pc.utterance, reply)
			default:
				callWhenProse++
				args, _ := json.Marshal(call.Args)
				t.Logf("  %-52q CALL-WHEN-PROSE-NEEDED: %s%s",
					pc.utterance, call.Name, string(args))
			}
		}
	}

	t.Log("")
	t.Logf("=== Layer 8 — tool-call fork, %s, %d repeats ===", model, reps)
	var allC, allT int
	for _, tier := range []string{"social", "plain", "interface", "technical", "trap"} {
		c := byTier[tier]
		if c == nil || c.total == 0 {
			continue
		}
		allC += c.correct
		allT += c.total
		t.Logf("  %-10s %3d/%-3d  %5.1f%%", tier, c.correct, c.total,
			100*float64(c.correct)/float64(c.total))
	}
	if allT > 0 {
		t.Logf("  %-10s %3d/%-3d  %5.1f%%", "OVERALL", allC, allT, 100*float64(allC)/float64(allT))
	}
	t.Log("")
	t.Logf("  prose when action needed (dangerous): %d", proseWhenAction)
	t.Logf("  call when prose needed (harmless):    %d", callWhenProse)
	t.Logf("  malformed / unknown tool:             %d", malformed)
	t.Logf("  tool choice — typed: %d, bash: %d", typedPicked, bashPicked)

	// Asserts nothing. This measures whether an architecture is viable; a
	// poor result is the finding that prevents a wasted rewrite, not a
	// failing build.
}
