// Command synapse turns a request in plain language into shell commands. It
// runs a bounded multi-step loop (D21): propose the next command with a local
// Ollama model, gate it on its recoverability, run it or ask first, and feed
// the result back until the task is done or the step cap is reached. Every
// step is gated on its own; nothing is trusted because an earlier step was
// approved.
//
// The command line is the system's contract (D48): docs/cli.md states it,
// `synapse help` prints it, and cli_test.go and cli_contract_test.go enforce
// it end to end. The TUI, REPL and GUI session are presentation over the same
// loop.
//
// Files:
//
//	main.go      dispatch: parse, validate configuration, run one mode
//	cli.go       the command-line parser, help text and version
//	config.go    the SYNAPSE_* variables, validated and documented from one list
//	outcome.go   task outcomes and the exit code each one maps to
//	jsonl.go     the run --json event stream
//	loop.go      the task loop, its options, and the gate around each step
//	prompts.go   prompts and model calls
//	modes.go     the interactive modes (TUI, REPL, scratch) and one-shot run
//	undo_cmd.go  synapse undo
//	analysis.go  the effect analysis's policy switches
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"synapseos/internal/ollama"
	"synapseos/internal/undo"
)

const defaultModel = "qwen2.5-coder:3b"

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
	catchBrokenPipe()

	// The command line is parsed in full before anything else happens, and a
	// refused one exits here: it never reaches the model (see parseArgs).
	inv, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "synapse: %v\n\n", err)
		printUsage(os.Stderr)
		os.Exit(exitUsage)
	}

	// Configuration is checked before anything reads it, so a mistyped value
	// stops the run instead of silently falling back to a default. undo is
	// exempt: it reads none of it, and it is the safety net, so a bad setting
	// meant for the model must never stand between a person and their undo.
	if inv.mode != modeHelp && inv.mode != modeVersion {
		check := checkEnv
		if inv.mode == modeUndo {
			check = checkTelemetryEnv
		}
		if err := check(os.Getenv); err != nil {
			fmt.Fprintf(os.Stderr, "synapse: invalid configuration:\n%v\n\nSee synapse help for accepted values.\n", err)
			os.Exit(exitUsage)
		}
	}

	// Subcommands that never call the model are dispatched *before* any
	// connectivity check. Gating them on Ollama would make them unavailable
	// exactly when the model backend is down — and for `undo` that is a real
	// safety problem, not just an inconvenience: the scenario undo exists for
	// is "a destructive command already ran and I want it back", which is
	// entirely filesystem state (internal/undo's journal, trash, and content
	// backups) with no model involvement whatsoever. If Ollama crashed, was
	// stopped, or the machine rebooted between the destructive command and the
	// undo attempt, requiring a live model here would remove the safety net at
	// the exact moment it is most likely to be needed. Fixed Session 28 (this
	// was a real, verified defect dating to M1/F2, found by review, not a
	// deliberate design choice — nothing in decisions.md justified it).
	switch inv.mode {
	case modeHelp:
		printUsage(os.Stdout)
		return
	case modeVersion:
		fmt.Println(versionString())
		return
	case modeTUIScratch:
		runTUI(ctx, client, model, true)
		return
	case modeTUI:
		runTUI(ctx, client, model, false)
		return
	case modeUndo:
		journalPath, err := undo.DefaultJournalPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		src := newLineSource(os.Stdin)
		tel, closeTel := studyLogger(os.Stderr)
		code := runUndo(func(p string) bool { return confirmLine(ctx, src, os.Stderr, p) }, os.Stdout, os.Stderr, journalPath, tel, os.Getenv("SYNAPSE_TASK_ID"))
		closeTel()
		os.Exit(code)
	}

	// Everything below this point actually calls the model, so a failed
	// connectivity check here is a genuine, actionable precondition failure.
	if err := client.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\nIs Ollama running? Start it with:\n  ollama serve\nand pull the model with:\n  ollama pull %s\n", err, model)
		if inv.json {
			newJSONEmitter(os.Stdout).End(outcomeModelUnavailable, exitModelUnavailable)
		}
		os.Exit(exitModelUnavailable)
	}

	// Diagnostics go to stderr so stdout carries only the task's own output.
	fmt.Fprintf(os.Stderr, "model: %s   endpoint: %s\n\n", model, client.DisplayURL())

	switch inv.mode {
	case modeREPL, modeScratch:
		journalPath, err := undo.DefaultJournalPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this session won't be undoable: %v\n", err)
			journalPath = ""
		}
		if inv.mode == modeREPL {
			os.Exit(runREPL(ctx, client, model, journalPath, os.Stdin, os.Stdout, os.Stderr))
		}
		os.Exit(runScratch(ctx, client, model, journalPath, os.Stdin, os.Stdout, os.Stderr))
	case modeRun:
		runAdHoc(ctx, client, model, inv.task, inv.json)
	case modeDemo:
		for _, tc := range sampleSuite {
			proposeOnly(ctx, client, model, tc.category, tc.task)
		}
	default:
		// Every mode parseArgs can return is handled above; reaching here is
		// a programming error, and it must not fall through to a task.
		fmt.Fprintf(os.Stderr, "synapse: internal error: unhandled mode %d\n", inv.mode)
		os.Exit(1)
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
