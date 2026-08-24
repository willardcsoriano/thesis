// Command synapse is CLI mode, SynapseOS's one-shot interface (D19, build
// milestone 2).
//
// Given a single natural-language request, it runs a bounded multi-step loop
// (D21): propose the next command via a local Ollama model, classify it as
// reversible or irreversible, run it immediately (reversible) or block on an
// explicit y/n confirmation first (irreversible), then feed the result back
// so the model can propose the next step or signal the task is done. Every
// step is independently classified and gated — nothing is trusted just
// because an earlier step in the same invocation was approved — and a hard
// step cap stops a confused model from looping indefinitely. This is the
// same reversibility-gated execution model TUI mode (M3) will later reuse
// rather than rebuild. Run with no arguments, it instead walks a built-in
// sample task suite in propose-only mode: a quality smoke test across the
// task categories the study covers, which deliberately never touches the
// real filesystem and never loops.
//
// Usage:
//
//	synapse                              # propose-only sample task suite
//	synapse "find pdfs from this week"   # propose, classify, confirm, execute
//	synapse undo                         # reverse the most recent auto-run reversible command
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
	"io"
	"os"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/executor"
	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

const defaultModel = "qwen2.5-coder:3b"

// systemPrompt constrains the model to emit exactly one runnable command.
// This is intentionally strict and un-tuned: the point of this milestone is to
// measure the stock model's out-of-the-box quality, which sets the baseline
// that later LoRA fine-tuning (scope.md, Python pipeline) has to beat. Used
// only by the propose-only sample suite (proposeOnly) — the ad-hoc path uses
// loopSystemPrompt instead.
const systemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
Convert the user's request into a single bash command that accomplishes it.
Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the request needs multiple steps, combine them into one line with pipes or &&.
- Prefer standard, widely available utilities.
- If the request cannot be done with a shell command, output exactly: UNSUPPORTED`

// loopSystemPrompt drives the ad-hoc path's bounded multi-step loop (D21):
// given the task and, on later steps, what has already run and what it
// produced, propose exactly the next command, or DONE once nothing further
// is needed. This is what lets the loop handle tasks that need more than one
// command (e.g. "mkdir a destination, then move files into it") and correct
// a failed attempt using its own error output — without the model ever
// deciding to skip the classifier/confirmation gate for a later step; that
// gate is enforced by runAdHoc, not by anything the model is trusted to do.
const loopSystemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
You are given a task and, if any commands have already been run toward it, each one's exit code and output.
Output the single next bash command needed to make progress on the task.
Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the commands already run have already fully accomplished the task, output exactly: DONE
- Combine steps into one line with pipes or && where you reasonably can, but if a step depends on seeing the result of a previous command first, propose only that next step.
- Prefer standard, widely available utilities.
- If the task cannot be done with a shell command at all, output exactly: UNSUPPORTED`

// maxLoopSteps hard-caps the ad-hoc path's bounded loop (D21). Reaching the
// cap is reported as an explicit failure, never silently treated as if the
// model had signaled DONE — an unbounded or silently-truncated loop is
// exactly the failure mode the cap exists to prevent.
const maxLoopSteps = 5

const doneSentinel = "DONE"

// stepOutputChars caps how much of a single step's stdout/stderr gets fed
// back into the next step's prompt. A command like a recursive find can
// produce output far larger than useful context; this is a blunt truncation,
// not the compression M6's session context will eventually do for the TUI.
const stepOutputChars = 500

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
		if len(args) == 1 && args[0] == "undo" {
			runUndo(os.Stdout, os.Stderr)
			return
		}
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

// loopStep records one executed command and its result, so later steps'
// prompts can show the model what has already happened.
type loopStep struct {
	command string
	result  executor.Result
}

// runAdHoc runs CLI mode's bounded multi-step loop (D21) for a single
// user-supplied task against the real terminal, then exits the process with
// runLoop's verdict. It is the thin process-control wrapper around runLoop —
// see that function for the actual loop behavior.
func runAdHoc(ctx context.Context, client *ollama.Client, model, task string) {
	journalPath, err := undo.DefaultJournalPath()
	if err != nil {
		// Non-fatal: undo recording is a safety net, not the task itself.
		// A task should still run even if e.g. $HOME isn't writable.
		fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this run won't be undoable: %v\n", err)
		journalPath = ""
	}
	os.Exit(runLoop(ctx, client, model, task, confirm, os.Stdout, os.Stderr, journalPath))
}

// runLoop is runAdHoc's testable core: propose the next command, classify
// its reversibility, auto-run it if safe or block on confirmFn if not,
// execute it, then feed the result back so the model can propose the next
// step or signal DONE. Every step — not just the first — goes through the
// same classifier and confirmation gate; nothing is trusted just because an
// earlier step in the same run was approved. Stops on DONE, on a
// blocked-then-declined confirmation, or on hitting maxLoopSteps, whichever
// comes first, and returns the process exit code that outcome warrants.
//
// I/O and the confirmation prompt are taken as parameters — never os.Stdin,
// os.Stdout, os.Stderr, or os.Exit directly — so tests can drive this
// against a mocked Ollama server and canned confirmation answers, then
// assert on the returned exit code and captured output, without spawning a
// subprocess or touching the real terminal.
//
// journalPath, if non-empty, records an undo entry (internal/undo) for
// every auto-run reversible step that has an observable filesystem effect,
// by snapshotting the working directory before and after the step. Pass ""
// to disable recording entirely (tests do this — they mostly operate on
// absolute paths into a scratch directory, not the process's actual working
// directory, which is what gets snapshotted). Irreversible steps are never
// recorded: the user already explicitly approved the risk knowing there is
// no undo, and this package's snapshot-diff approach structurally cannot
// reconstruct deleted content anyway.
func runLoop(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string) int {
	var history []loopStep

	for i := 1; i <= maxLoopSteps; i++ {
		resp, cmd, err := proposeStep(ctx, client, model, task, history)
		if err != nil {
			fmt.Fprintf(errOut, "error: %v\n", err)
			return 1
		}

		fmt.Fprintf(out, "step %d: %s\n  stats: %d tokens in %s\n",
			i, cmd, resp.EvalCount, resp.Latency().Round(time.Millisecond))

		if cmd == "" || cmd == "UNSUPPORTED" {
			fmt.Fprintln(out, "model reported this request cannot be done with a shell command.")
			return 1
		}
		if strings.EqualFold(cmd, doneSentinel) {
			if len(history) == 0 {
				fmt.Fprintln(out, "model reported nothing needs to be done.")
				return 0
			}
			fmt.Fprintf(out, "task complete in %d step(s).\n", len(history))
			return 0
		}

		verdict, reason := classifier.Classify(cmd)
		if verdict == classifier.Irreversible {
			fmt.Fprintf(out, "blocked: %s is irreversible — %s\n", cmd, reason)
			if !confirmFn("run it anyway?") {
				fmt.Fprintln(out, "cancelled.")
				return 0
			}
		}

		wd, undoBefore := "", map[string]bool(nil)
		if journalPath != "" && verdict == classifier.Reversible {
			if w, err := os.Getwd(); err == nil {
				wd = w
				undoBefore, _ = undo.Snapshot(wd) // best-effort: a snapshot failure just disables recording for this step
			}
		}

		result := executor.Run(ctx, cmd)
		if result.Err != nil {
			fmt.Fprintf(errOut, "error: command did not run: %v\n", result.Err)
			return 1
		}
		if result.Stdout != "" {
			fmt.Fprint(out, result.Stdout)
		}
		if result.Stderr != "" {
			fmt.Fprint(errOut, result.Stderr)
		}
		fmt.Fprintf(out, "exit code: %d\n\n", result.ExitCode)

		if wd != "" && undoBefore != nil && result.ExitCode == 0 {
			if after, err := undo.Snapshot(wd); err == nil {
				if entry := undo.BuildEntry(wd, cmd, undoBefore, after); !entry.IsNoop() {
					if err := undo.AppendJournal(journalPath, entry); err != nil {
						fmt.Fprintf(errOut, "warning: could not record undo entry: %v\n", err)
					}
				}
			}
		}

		history = append(history, loopStep{command: cmd, result: result})
	}

	fmt.Fprintf(errOut, "error: step limit reached (%d steps) without the task being reported complete — stopping.\n", maxLoopSteps)
	return 1
}

// proposeStep asks the model for the next command given task and everything
// that has run so far, with the same deterministic decoding as propose.
func proposeStep(ctx context.Context, client *ollama.Client, model, task string, history []loopStep) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := client.Generate(reqCtx, model, loopSystemPrompt, buildStepPrompt(task, history), map[string]any{"temperature": 0})
	if err != nil {
		return nil, "", err
	}
	return resp, cleanCommand(resp.Response), nil
}

// buildStepPrompt renders the task plus a truncated history of already-run
// commands and their results, in the shape loopSystemPrompt expects.
func buildStepPrompt(task string, history []loopStep) string {
	if len(history) == 0 {
		return "Task: " + task
	}

	var b strings.Builder
	b.WriteString("Task: ")
	b.WriteString(task)
	b.WriteString("\n\nSteps already run:\n")
	for i, s := range history {
		fmt.Fprintf(&b, "%d. $ %s\n   exit code: %d\n", i+1, s.command, s.result.ExitCode)
		if out := strings.TrimSpace(s.result.Stdout); out != "" {
			fmt.Fprintf(&b, "   stdout: %s\n", truncate(out, stepOutputChars))
		}
		if errOut := strings.TrimSpace(s.result.Stderr); errOut != "" {
			fmt.Fprintf(&b, "   stderr: %s\n", truncate(errOut, stepOutputChars))
		}
	}
	b.WriteString("\nWhat is the next command? Output DONE if the task is already fully accomplished.")
	return b.String()
}

// truncate shortens s to at most n bytes, marking that it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + " ...(truncated)"
}

// runUndo reverses the most recently journaled reversible command. It shows
// what the undo will do and asks for confirmation before touching anything
// — undoing is itself a consequential filesystem action, so it goes through
// the same explicit-confirmation pattern as an irreversible command rather
// than running silently. The journal entry is only removed (PopLastJournal)
// after confirmation; declining leaves it in place so a later `synapse undo`
// can still act on it.
func runUndo(out, errOut io.Writer) {
	path, err := undo.DefaultJournalPath()
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		os.Exit(1)
	}

	entry, ok, err := undo.PeekLastJournal(path)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		os.Exit(1)
	}
	if !ok {
		fmt.Fprintln(out, "nothing to undo.")
		return
	}

	fmt.Fprintf(out, "undoing: %s\n  (ran in %s at %s)\n", entry.Command, entry.Dir, entry.Timestamp.Format(time.RFC3339))
	for _, m := range entry.Moves {
		fmt.Fprintf(out, "  move back: %s -> %s\n", m.NewPath, m.OldPath)
	}
	for _, c := range entry.Created {
		fmt.Fprintf(out, "  remove: %s\n", c)
	}
	if len(entry.Unhandled) > 0 {
		fmt.Fprintf(out, "  note: %d change(s) from this command could not be safely reconstructed and are not part of this undo: %v\n", len(entry.Unhandled), entry.Unhandled)
	}

	if !confirm("apply this undo?") {
		fmt.Fprintln(out, "cancelled.")
		return
	}

	if _, _, err := undo.PopLastJournal(path); err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		os.Exit(1)
	}
	if errs := undo.Apply(entry); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(errOut, "error: %v\n", e)
		}
		os.Exit(1)
	}
	fmt.Fprintln(out, "undo complete.")
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
