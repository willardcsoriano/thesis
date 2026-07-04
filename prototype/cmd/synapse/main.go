// Command synapse is the SynapseOS walking skeleton (build milestone 2).
//
// It exercises the single riskiest assumption in the whole runtime before any
// of the surrounding machinery exists: can a local 3B model turn plain-English
// intent into a usable bash command? It sends natural language to Ollama and
// prints the command the model proposes. It does NOT execute anything — that is
// the next milestone (the execution engine + confirmation gate).
//
// Usage:
//
//	synapse                       # run the built-in sample task suite
//	synapse "find pdfs from this week"   # run one ad-hoc task
//
// Environment:
//
//	SYNAPSE_MODEL    model tag       (default: qwen2.5-coder:3b)
//	SYNAPSE_OLLAMA   ollama base URL (default: http://localhost:11434)
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"synapseos/internal/ollama"
)

const defaultModel = "qwen2.5-coder:3b"

// systemPrompt constrains the model to emit exactly one runnable command.
// This is intentionally strict and un-tuned: the point of this milestone is to
// measure the stock model's out-of-the-box quality, which sets the baseline
// that later LoRA fine-tuning (scope.md, Python pipeline) has to beat.
const systemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
Convert the user's request into a single bash command that accomplishes it.
Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the request needs multiple steps, combine them into one line with pipes or &&.
- Prefer standard, widely available utilities.
- If the request cannot be done with a shell command, output exactly: UNSUPPORTED`

// sampleSuite is a first pass across the four task categories the study covers
// (scope.md → Custom cross-platform task suite). It is a smoke test for
// eyeballing quality, not the real study suite.
var sampleSuite = []struct{ category, task string }{
	{"file search & organization", "find all PDF files in my home folder modified in the last 7 days"},
	{"file search & organization", "move every screenshot on my desktop into a folder called Screenshots"},
	{"system & process monitoring", "show me the 5 processes using the most memory right now"},
	{"system & process monitoring", "how much free disk space is left on the main drive"},
	{"application & package management", "install the VLC media player"},
	{"application & package management", "list every package I've installed that isn't a system default"},
	{"text & data processing", "count how many lines in access.log contain the word error"},
	{"text & data processing", "replace every tab with a comma in data.txt and save it as data.csv"},
}

func main() {
	model := envOr("SYNAPSE_MODEL", defaultModel)
	client := ollama.New(os.Getenv("SYNAPSE_OLLAMA"))

	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\nIs Ollama running? Start it with:\n  ollama serve\nand pull the model with:\n  ollama pull %s\n", err, model)
		os.Exit(1)
	}

	fmt.Printf("model: %s   endpoint: %s\n\n", model, client.BaseURL)

	if args := os.Args[1:]; len(args) > 0 {
		runTask(ctx, client, model, "ad-hoc", strings.Join(args, " "))
		return
	}

	for _, tc := range sampleSuite {
		runTask(ctx, client, model, tc.category, tc.task)
	}
}

func runTask(ctx context.Context, client *ollama.Client, model, category, task string) {
	// Deterministic decoding: temperature 0 makes the smoke test reproducible
	// and matches how the accuracy evaluator will score the model offline.
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := client.Generate(reqCtx, model, systemPrompt, task, map[string]any{"temperature": 0})
	if err != nil {
		fmt.Printf("[%s]\n  intent : %s\n  ERROR  : %v\n\n", category, task, err)
		return
	}

	cmd := cleanCommand(resp.Response)
	fmt.Printf("[%s]\n  intent : %s\n  command: %s\n  stats  : %d tokens in %s\n\n",
		category, task, cmd, resp.EvalCount, resp.Latency().Round(time.Millisecond))
}

// cleanCommand strips the formatting small models add despite instructions:
// markdown code fences and surrounding whitespace. It is defensive parsing for
// the skeleton, not the final sanitizer — the execution engine will own real
// command validation.
func cleanCommand(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i != -1 {
			s = s[i+1:] // drop the opening ```lang line
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
