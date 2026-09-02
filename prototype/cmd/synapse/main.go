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
//	synapse repl                         # persistent session (M3a): issue several tasks in one process
//	synapse tui                          # full-screen TUI mode (M3b): streaming, scrollback, in-session confirmation
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
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/executor"
	"synapseos/internal/ollama"
	"synapseos/internal/tui"
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

// stepExecutionTimeout bounds how long a single executed step may run before
// it is forcibly killed. Without this, a command that hangs — one that
// blocks on stdin the harness never provides, an infinite loop, a stuck
// network call — freezes the entire process indefinitely with no recovery
// (verified gap, Session 23: runLoop previously passed the outer, unbounded
// context straight through to executor.Run). Matches the model-generation
// budget (proposeStep) rather than an arbitrary guess: long enough for
// legitimate multi-file operations (a large find, a package install), short
// enough that a hang becomes a bounded, reported failure instead of an
// indefinite freeze.
const stepExecutionTimeout = 120 * time.Second

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
	args := os.Args[1:]

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
	// was a real, verified defect dating to M2/F2, found by review, not a
	// deliberate design choice — nothing in decisions.md justified it).
	if len(args) == 1 {
		switch args[0] {
		case "undo":
			journalPath, err := undo.DefaultJournalPath()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			reader := bufio.NewReader(os.Stdin)
			os.Exit(runUndo(func(p string) bool { return confirm(reader, os.Stdout, p) }, os.Stdout, os.Stderr, journalPath))
		case "tui":
			// M3b step 3: the real execution loop is wired in, but TUI mode
			// still launches without a connectivity precheck on purpose. A
			// full-screen app that opens and reports the problem inside the
			// session beats one that exits to a bare shell over a transient
			// backend blip — an unreachable Ollama surfaces as an ordinary
			// error line in the transcript on the first proposal attempt, and
			// the session stays usable once the backend comes back.
			journalPath, err := undo.DefaultJournalPath()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this session won't be undoable: %v\n", err)
				journalPath = ""
			}
			// The injected runner is runLoop itself, with only the client,
			// model, and journal bound in. TUI mode therefore executes the
			// exact same propose/classify/confirm/execute path as CLI and
			// REPL mode — no reimplementation, so safety gating cannot drift
			// between interface modes.
			runner := func(taskCtx context.Context, task string, confirmFn func(string) bool, out, errOut io.Writer) int {
				// withTokenStreaming is the one behavioral difference from
				// CLI/REPL mode, and it is presentation-only: tokens render
				// as they arrive instead of after generation finishes. The
				// command still gets parsed from the fully assembled
				// response and classified exactly as before.
				return runLoop(taskCtx, client, model, task, confirmFn, out, errOut, journalPath, withTokenStreaming(out))
			}
			if err := tui.Run(runner); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Everything below this point actually calls the model, so a failed
	// connectivity check here is a genuine, actionable precondition failure.
	if err := client.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\nIs Ollama running? Start it with:\n  ollama serve\nand pull the model with:\n  ollama pull %s\n", err, model)
		os.Exit(1)
	}

	fmt.Printf("model: %s   endpoint: %s\n\n", model, client.BaseURL)

	if len(args) > 0 {
		if len(args) == 1 && args[0] == "repl" {
			journalPath, err := undo.DefaultJournalPath()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this session won't be undoable: %v\n", err)
				journalPath = ""
			}
			os.Exit(runREPL(ctx, client, model, journalPath, os.Stdin, os.Stdout, os.Stderr))
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
	reader := bufio.NewReader(os.Stdin)
	confirmFn := func(prompt string) bool { return confirm(reader, os.Stdout, prompt) }
	os.Exit(runLoop(ctx, client, model, task, confirmFn, os.Stdout, os.Stderr, journalPath))
}

// runREPL is M3a's persistent CLI loop: instead of one process per task
// (runAdHoc), it reads one line of task input at a time from in and runs
// each through the same runLoop, in the same long-running process, until
// EOF or an explicit "exit"/"quit" line. Every task still goes through the
// identical classify/confirm/execute path as the one-shot path — nothing
// about the safety gating changes, only the process lifecycle around it.
//
// A single bufio.Reader wrapping in serves both the task-line reads and
// every confirmation prompt a task's irreversible step triggers. This is
// the one thing that must be gotten right for a persistent session:
// bufio.Reader reads ahead into its own internal buffer, so a second,
// independent reader wrapping the same in would silently steal input the
// first reader hadn't asked for yet (e.g. a task typed right after
// answering a confirmation prompt) — the exact "state leaked between
// iterations" failure mode this milestone exists to retire. Confirmation
// happens through confirm(reader, ...), not the outer per-task confirmFn
// parameter runLoop normally takes, precisely so it shares that one reader.
//
// A task that fails (propose error, UNSUPPORTED, step limit, cancelled
// confirmation) does not end the session — runLoop's own return code is
// intentionally ignored here; the whole point of a persistent loop is that
// one bad task doesn't force a restart to try another.
func runREPL(ctx context.Context, client *ollama.Client, model, journalPath string, in io.Reader, out, errOut io.Writer) int {
	fmt.Fprintln(out, "persistent session — type a task and press enter; type exit or quit (or Ctrl+D) to leave.")
	fmt.Fprintln(out, "while a task is running, Ctrl+C cancels just that task and returns you here.")

	reader := bufio.NewReader(in)
	confirmFn := func(prompt string) bool { return confirm(reader, out, prompt) }

	for {
		fmt.Fprint(out, "> ")
		line, readErr := reader.ReadString('\n')

		if task := strings.TrimSpace(line); task != "" {
			if strings.EqualFold(task, "exit") || strings.EqualFold(task, "quit") {
				return 0
			}
			runTaskInterruptibly(ctx, client, model, task, confirmFn, out, errOut, journalPath)
			fmt.Fprintln(out)
		}

		if readErr != nil {
			return 0
		}
	}
}

// notifyInterrupts and stopInterrupts wrap signal.Notify/signal.Stop so
// tests can drive the interrupt path without sending real signals to the
// test process — the same dependency-indirection pattern already used for
// os.Link in internal/undo and WaitDelay in internal/executor.
var (
	notifyInterrupts = func(c chan<- os.Signal) { signal.Notify(c, os.Interrupt) }
	stopInterrupts   = func(c chan<- os.Signal) { signal.Stop(c) }
)

// runTaskInterruptibly runs exactly one task with Ctrl+C wired to cancel
// that task alone, leaving the session alive — added Session 28 after a
// review found that M3a, by making the process long-lived, had turned a
// harmless behavior into a real one: in one-shot CLI mode Ctrl+C killed a
// process that was about to exit anyway, but in a persistent session it
// destroyed the whole session, which is precisely the thing M3a exists to
// keep alive. Combined with stepExecutionTimeout (120s), a single hung
// command could otherwise hold the session with no escape short of killing
// everything.
//
// Interrupt handling is installed per task and torn down as soon as the
// task finishes, deliberately: while sitting idle at the "> " prompt the
// default SIGINT behavior applies, so Ctrl+C there still terminates the
// process the way a user expects. Catching signals for the whole session
// instead would swallow that, leaving Ctrl+C looking broken at the prompt.
//
// Known limitation, accepted rather than hidden: if the task is blocked on
// a [y/N] confirmation when Ctrl+C arrives, the context is cancelled but
// the pending os.Stdin read is not interrupted, so nothing visibly happens
// until the next Enter — which the gate then treats as "no" and fails
// closed. The outcome is correct and safe, just not instant. Fixing that
// properly needs the stdin read moved off the main goroutine, which would
// reintroduce exactly the two-readers-over-one-stream hazard M3a was built
// to eliminate; not worth trading a real correctness guarantee for a
// cosmetic improvement.
func runTaskInterruptibly(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string) {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	notifyInterrupts(sigCh)
	defer stopInterrupts(sigCh)

	watchDone := make(chan struct{})
	defer close(watchDone)

	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(out, "\ncancelling this task — the session stays open.")
			cancel()
		case <-watchDone:
		}
	}()

	runLoop(taskCtx, client, model, task, confirmFn, out, errOut, journalPath)
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
// loopOption tunes runLoop without widening its signature for the many
// existing callers that need none of it — a variadic option keeps every
// current call site (CLI, REPL, and eight test call sites) unchanged.
type loopOption func(*loopConfig)

type loopConfig struct {
	// tokenSink, when non-nil, receives model output token-by-token as it
	// is generated. Nil means non-streaming, which stays the default and
	// the documented CLI/REPL behavior.
	tokenSink io.Writer
}

// withTokenStreaming makes the loop stream generation into w as it
// arrives. Used only by TUI mode; see docs/interface-modes.md for why
// CLI mode deliberately renders once generation finishes instead.
func withTokenStreaming(w io.Writer) loopOption {
	return func(c *loopConfig) { c.tokenSink = w }
}

func runLoop(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string, opts ...loopOption) int {
	var cfg loopConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	fmt.Fprintf(out, "each step may run for up to %s before it's automatically stopped.\n\n", stepExecutionTimeout)

	var history []loopStep

	for i := 1; i <= maxLoopSteps; i++ {
		resp, cmd, err := proposeStep(ctx, client, model, task, history, cfg.tokenSink)
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

		// Resolved once per step and used both for the filesystem-aware
		// classification below (cp-onto-existing-destination needs to know
		// the destination's actual path) and for undo snapshotting — best
		// effort either way; an unresolvable wd just degrades both to their
		// no-filesystem-check behavior rather than failing the step.
		wd, _ := os.Getwd()

		verdict, reason := classifier.ClassifyForDir(cmd, wd)
		if verdict == classifier.Irreversible {
			fmt.Fprintf(out, "blocked: %s is irreversible — %s\n", cmd, reason)
			if !confirmFn("run it anyway?") {
				fmt.Fprintln(out, "cancelled.")
				return 0
			}
		}

		// Reversible gets the directory-diff safety net (undoBefore); a
		// confirmed Irreversible one gets whichever combination of
		// pre-execution backups its shape calls for, via
		// backupBeforeIrreversible — the "guiltless" half of
		// accurate-and-guiltless (Sessions 24-26). Neither happens without
		// journalPath, and every backup is best-effort: a failure degrades to
		// "no safety net for this piece," never blocks execution the user
		// already confirmed. More than one backup can legitimately apply to
		// a single step when the command itself is a chain (e.g. "chmod -R
		// 755 dir && rm other.txt" triggers both metadata backup and trash).
		var undoBefore map[string]bool
		var contentBackups []undo.ContentBackup
		var trashed []undo.TrashedItem
		var gitReset string
		var metadataBackups []undo.MetadataBackup
		if journalPath != "" && wd != "" {
			switch verdict {
			case classifier.Reversible:
				undoBefore, _ = undo.Snapshot(wd)
			case classifier.Irreversible:
				contentBackups, trashed, gitReset, metadataBackups = backupBeforeIrreversible(ctx, cmd, wd, errOut)
			}
		}

		execCtx, execCancel := context.WithTimeout(ctx, stepExecutionTimeout)
		result := executor.Run(execCtx, cmd)
		execCancel()
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
		if result.TimedOut {
			fmt.Fprintf(out, "command exceeded %s and was terminated.\n", stepExecutionTimeout)
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
		} else if len(contentBackups) > 0 || len(trashed) > 0 || gitReset != "" || len(metadataBackups) > 0 {
			// Journaled regardless of exit code: every backup here was
			// already taken before the command ran, so it's available to
			// restore even if the command exited nonzero after partially
			// applying its effect — exactly the surprising-outcome case this
			// safety net exists for.
			entry := undo.Entry{
				Timestamp:       time.Now(),
				Command:         cmd,
				Dir:             wd,
				ContentBackups:  contentBackups,
				Trashed:         trashed,
				GitReset:        gitReset,
				MetadataBackups: metadataBackups,
			}
			if err := undo.AppendJournal(journalPath, entry); err != nil {
				fmt.Fprintf(errOut, "warning: could not record undo entry: %v\n", err)
			}
		}

		history = append(history, loopStep{command: cmd, result: result})
	}

	fmt.Fprintf(errOut, "error: step limit reached (%d steps) without the task being reported complete — stopping.\n", maxLoopSteps)
	return 1
}

// backupBeforeIrreversible runs every pre-execution backup that applies to
// a confirmed Irreversible cmd, matching each shape the classifier
// recognizes to the mechanism that actually protects it: a content copy
// for something that mutates a file's existing bytes in place, a hardlink
// into trash for something that only removes a directory entry, a
// captured commit SHA for a git reset, and a metadata record for a
// recursive permission change. Every piece is independent and
// best-effort — errOut gets a warning on failure, but nothing here ever
// blocks the execution the user already confirmed.
func backupBeforeIrreversible(ctx context.Context, cmd, wd string, errOut io.Writer) (contentBackups []undo.ContentBackup, trashed []undo.TrashedItem, gitReset string, metadataBackups []undo.MetadataBackup) {
	targets := classifier.ContentMutationTargets(cmd, wd)
	if dst, ok := classifier.CpOverwriteTarget(cmd, wd); ok {
		targets = append(targets, dst)
	}
	if dst, ok := classifier.RawWriteOverwriteTarget(cmd, wd); ok {
		targets = append(targets, dst)
	}
	if len(targets) > 0 {
		var errs []error
		contentBackups, errs = undo.BackupContent(targets)
		for _, e := range errs {
			fmt.Fprintf(errOut, "warning: could not back up a file before running: %v\n", e)
		}
	}

	trashCandidates := classifier.TrashTargets(cmd, wd)
	if dryRunCmd, ok := classifier.GitCleanDryRunCommand(cmd); ok {
		dryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result := executor.Run(dryCtx, dryRunCmd)
		cancel()
		if result.Err != nil {
			fmt.Fprintf(errOut, "warning: could not preview git clean's effect before running: %v\n", result.Err)
		} else {
			for _, p := range parseGitCleanDryRunOutput(result.Stdout) {
				if !filepath.IsAbs(p) {
					p = filepath.Join(wd, p)
				}
				trashCandidates = append(trashCandidates, p)
			}
		}
	}
	if len(trashCandidates) > 0 {
		var errs []error
		trashed, errs = undo.TrashPreserve(trashCandidates)
		for _, e := range errs {
			fmt.Fprintf(errOut, "warning: could not preserve a file before deleting it: %v\n", e)
		}
	}

	if classifier.IsGitResetHard(cmd) {
		sha, err := undo.CaptureGitHead(wd)
		if err != nil {
			fmt.Fprintf(errOut, "warning: could not capture git HEAD before resetting: %v\n", err)
		} else {
			gitReset = sha
		}
	}

	if permTargets := classifier.RecursivePermissionTargets(cmd, wd); len(permTargets) > 0 {
		var errs []error
		metadataBackups, errs = undo.BackupMetadata(permTargets)
		for _, e := range errs {
			fmt.Fprintf(errOut, "warning: could not back up permissions before changing them: %v\n", e)
		}
	}

	return contentBackups, trashed, gitReset, metadataBackups
}

// parseGitCleanDryRunOutput extracts the paths git clean -n reports it
// would remove — "Would remove <path>" one per line, directories reported
// with a trailing "/" — the exact shape verified empirically against a
// real git repository (Session 26).
func parseGitCleanDryRunOutput(stdout string) []string {
	const prefix = "Would remove "
	var paths []string
	for _, line := range strings.Split(stdout, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			paths = append(paths, strings.TrimSuffix(rest, "/"))
		}
	}
	return paths
}

// proposeStep asks the model for the next command given task and everything
// that has run so far, with the same deterministic decoding as propose.
func proposeStep(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, tokenSink io.Writer) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	prompt := buildStepPrompt(task, history)
	opts := map[string]any{"temperature": 0}

	// Non-streaming is the default and stays the CLI/REPL behavior
	// (docs/interface-modes.md: CLI renders "once generation finishes").
	// Only a caller that supplies a sink — TUI mode — pays for streaming.
	if tokenSink == nil {
		resp, err := client.Generate(reqCtx, model, loopSystemPrompt, prompt, opts)
		if err != nil {
			return nil, "", err
		}
		return resp, cleanCommand(resp.Response), nil
	}

	resp, err := client.GenerateStream(reqCtx, model, loopSystemPrompt, prompt, opts, func(tok string) {
		// Best effort: a failed write to the UI must never abort a
		// generation that is otherwise fine.
		fmt.Fprint(tokenSink, tok)
	})
	if err != nil {
		return nil, "", err
	}
	// The command is still parsed from the fully assembled response, never
	// from the streamed fragments — streaming changes only when text
	// appears on screen, never what gets classified or executed. Combined
	// with GenerateStream refusing to return a truncated stream at all,
	// a partial generation can never reach the classifier.
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
		if s.result.TimedOut {
			fmt.Fprintf(&b, "   note: this command exceeded %s and was terminated before it could finish.\n", stepExecutionTimeout)
		}
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
// runUndo is the `synapse undo` subcommand's testable core: peek the most
// recent journal entry, show what applying it would do, and — if
// confirmFn approves — pop it off the journal and apply it. Mirrors
// runLoop's design (I/O and the confirmation prompt taken as parameters,
// an exit code returned rather than os.Exit called directly) so it can be
// tested against a scratch journal file and canned confirmation answers
// instead of the real terminal and the real ~/.synapse/undo.log.
func runUndo(confirmFn func(string) bool, out, errOut io.Writer, journalPath string) int {
	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintln(out, "nothing to undo.")
		return 0
	}

	fmt.Fprintf(out, "undoing: %s\n  (ran in %s at %s)\n", entry.Command, entry.Dir, entry.Timestamp.Format(time.RFC3339))
	for _, m := range entry.Moves {
		fmt.Fprintf(out, "  move back: %s -> %s\n", m.NewPath, m.OldPath)
	}
	for _, c := range entry.Created {
		fmt.Fprintf(out, "  remove: %s\n", c)
	}
	for _, cb := range entry.ContentBackups {
		fmt.Fprintf(out, "  restore content: %s\n", cb.Path)
	}
	for _, item := range entry.Trashed {
		fmt.Fprintf(out, "  restore from trash: %s\n", item.OriginalPath)
	}
	for _, mb := range entry.MetadataBackups {
		fmt.Fprintf(out, "  restore permissions: %s\n", mb.Path)
	}
	if entry.GitReset != "" {
		fmt.Fprintf(out, "  reset git HEAD back to: %s\n", entry.GitReset)
	}
	if len(entry.Unhandled) > 0 {
		fmt.Fprintf(out, "  note: %d change(s) from this command could not be safely reconstructed and are not part of this undo: %v\n", len(entry.Unhandled), entry.Unhandled)
	}

	if !confirmFn("apply this undo?") {
		fmt.Fprintln(out, "cancelled.")
		return 0
	}

	if _, _, err := undo.PopLastJournal(journalPath); err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return 1
	}
	if errs := undo.Apply(entry); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(errOut, "error: %v\n", e)
		}
		return 1
	}
	fmt.Fprintln(out, "undo complete.")
	return 0
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

// confirm prints prompt to out and blocks for an explicit "y"/"yes" read
// from r. Any other input, including a read error or EOF, is treated as
// "no" — the confirmation gate fails closed. r and out are taken as
// parameters — never os.Stdin/os.Stdout directly — so a persistent session
// (runREPL) can share one reader between its task-line reads and every
// confirmation prompt runLoop triggers along the way: two independent
// readers over the same stdin would let one buffer input the other was
// about to consume, which is exactly the state-leak risk M3a exists to
// retire. It also lets tests assert on prompt text without a real terminal.
func confirm(r *bufio.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	line, err := r.ReadString('\n')
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
