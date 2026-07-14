// Command synapse is CLI mode, SynapseOS's one-shot interface (D19, build
// milestone 2).
//
// Given a single natural-language request, it proposes a bash command via a
// local Ollama model, classifies the command as reversible or irreversible,
// and either runs it immediately (reversible) or blocks on an explicit y/n
// confirmation first (irreversible) — the same reversibility-gated execution
// model TUI mode (M3) will later reuse rather than rebuild. Run with no
// arguments, it instead walks a built-in sample task suite in propose-only
// mode: a quality smoke test across the task categories the study covers,
// which deliberately never touches the real filesystem.
//
// Usage:
//
//	synapse                              # propose-only sample task suite
//	synapse "find pdfs from this week"   # propose, classify, confirm, execute
//
// Environment:
//
//	SYNAPSE_MODEL    model tag       (default: qwen2.5-coder:3b)
//	SYNAPSE_OLLAMA   ollama base URL (default: http://localhost:11434)
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/executor"
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
		runAdHoc(ctx, client, model, strings.Join(args, " "))
		return
	}

	for _, tc := range sampleSuite {
		proposeOnly(ctx, client, model, tc.category, tc.task)
	}
}

// proposeOnly runs one sample-suite task through the model and prints the
// proposed command without classifying or executing it. Used for the
// built-in suite, which is a quality smoke test, not a live filesystem
// action.
func proposeOnly(ctx context.Context, client *ollama.Client, model, category, task string) {
	resp, cmd, err := propose(ctx, client, model, task)
	if err != nil {
		fmt.Printf("[%s]\n  intent : %s\n  ERROR  : %v\n\n", category, task, err)
		return
	}
	fmt.Printf("[%s]\n  intent : %s\n  command: %s\n  stats  : %d tokens in %s\n\n",
		category, task, cmd, resp.EvalCount, resp.Latency().Round(time.Millisecond))
}

// runAdHoc runs the full CLI-mode pipeline for a single user-supplied task:
// propose a command, classify its reversibility, auto-run it if safe or
// block on confirmation if not, then print its output and exit code.
func runAdHoc(ctx context.Context, client *ollama.Client, model, task string) {
	resp, cmd, err := propose(ctx, client, model, task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("intent : %s\ncommand: %s\nstats  : %d tokens in %s\n\n",
		task, cmd, resp.EvalCount, resp.Latency().Round(time.Millisecond))

	if cmd == "" || cmd == "UNSUPPORTED" {
		fmt.Println("model reported this request cannot be done with a shell command.")
		os.Exit(1)
	}

	verdict, reason := classifier.Classify(cmd)
	if verdict == classifier.Irreversible {
		fmt.Printf("blocked : %s is irreversible — %s\n", cmd, reason)
		if !confirm("run it anyway?") {
			fmt.Println("cancelled.")
			return
		}
	}

	result := executor.Run(ctx, cmd)
	if result.Err != nil {
		fmt.Fprintf(os.Stderr, "error: command did not run: %v\n", result.Err)
		os.Exit(1)
	}
	if result.Stdout != "" {
		fmt.Print(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
	}
	fmt.Printf("exit code: %d\n", result.ExitCode)
	os.Exit(result.ExitCode)
}

// propose sends task to the model with deterministic decoding (temperature
// 0, so the smoke test is reproducible and matches how the offline accuracy
// evaluator will score the model) and returns the cleaned command it proposed.
func propose(ctx context.Context, client *ollama.Client, model, task string) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := client.Generate(reqCtx, model, systemPrompt, task, map[string]any{"temperature": 0})
	if err != nil {
		return nil, "", err
	}
	return resp, cleanCommand(resp.Response), nil
}

// confirm prints prompt and blocks for an explicit "y"/"yes" on stdin. Any
// other input, including a read error or EOF, is treated as "no" — the
// confirmation gate fails closed.
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// cleanCommand strips the formatting small models add despite instructions:
// triple-backtick code fences, a whole-line wrapped in a single pair of
// inline backticks (observed in live testing 2026-07-12: 3 of 8 sample-suite
// responses came back backtick-wrapped instead of as a bare command), and
// surrounding whitespace. It is defensive parsing, not a sanitizer — it does
// not validate that the result is safe or even syntactically valid shell;
// that's the classifier and the shell itself.
func cleanCommand(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i != -1 {
			s = s[i+1:] // drop the opening ```lang line
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if len(s) > 1 && strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "`"), "`")
		s = strings.TrimSpace(s)
	}
	return s
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
