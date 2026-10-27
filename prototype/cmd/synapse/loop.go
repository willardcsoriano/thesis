package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"synapseos/internal/classifier"
	"synapseos/internal/effects"
	"synapseos/internal/executor"
	"synapseos/internal/gate"
	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
	"synapseos/internal/session"
	"synapseos/internal/telemetry"
	"synapseos/internal/undo"
)

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

// commandOutputLimit bounds how much of each stream of a model-chosen command
// synapse keeps in memory: the first and last half, with a count of the bytes
// between. A command printing gigabytes used to be held whole, several times
// over (docs/cli-breaking.md).
const commandOutputLimit = 1 << 20

// bareCd reports whether cmd is nothing but a directory change: one simple
// command, `cd` or its stack cousins, with no pipeline, list, or redirect. A cd
// that leads into other commands (`cd logs && ls`) is fine: the change lasts for
// exactly the commands that follow it.
func bareCd(cmd string) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil || len(f.Stmts) != 1 {
		return false
	}
	st := f.Stmts[0]
	call, ok := st.Cmd.(*syntax.CallExpr)
	if !ok || len(st.Redirs) != 0 || st.Background || st.Negated || len(call.Args) == 0 {
		return false
	}
	switch call.Args[0].Lit() {
	case "cd", "pushd", "popd":
		return true
	}
	return false
}

// interactiveTUIPrograms names programs that take over the whole screen and
// block waiting for keyboard input — they need a real controlling terminal,
// which executor.Run does not give them (its stdout/stderr are buffers, not a
// pty). Running one here fails immediately (`top: failed tty get`) or, worse,
// hangs until the step timeout. Found live 2026-09-28: asked for CPU usage,
// the model reached for `top`, watched it fail, then reached for `htop`,
// which either failed the same way or hung — either way the task burned
// every step in maxLoopSteps guessing at interactive alternatives instead of
// the one-shot commands these programs are all built on top of and that work
// fine headless. The map's value is what to suggest instead, named for the
// specific program so the model reaches for something that actually answers
// the same question rather than merely another interactive tool.
//
// Deliberately narrow: only programs that are always full-screen with no
// headless mode in ordinary use. `less` and `man` are left off on purpose —
// both already detect a non-terminal stdout and behave non-interactively in
// that case, so blocking them would refuse commands that work.
var interactiveTUIPrograms = map[string]string{
	"top":    "top -bn1 (one batch snapshot instead of the live display)",
	"htop":   "ps aux --sort=-%cpu | head (or top -bn1) — htop has no headless mode",
	"vi":     "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"vim":    "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"nvim":   "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"nano":   "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"emacs":  "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"pico":   "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"joe":    "cat, or a non-interactive editor invocation such as sed -i, for reading or changing a file without opening an editor",
	"watch":  "the command being watched, run once, since watch's own display needs a terminal",
	"tmux":   "the command intended to run inside it, run directly — a new session cannot be attached to here",
	"screen": "the command intended to run inside it, run directly — a new session cannot be attached to here",
	"mc":     "ls, find, cp, or mv directly — mc is a full-screen file manager",
	"ranger": "ls or find directly — ranger is a full-screen file manager",
}

// interactiveProgram reports whether cmd invokes a program from
// interactiveTUIPrograms anywhere in its structure — as the whole command, one
// side of a pipeline, or one statement in a list — and returns that program's
// name and suggested alternative. syntax.Walk visits every node, so this
// catches `ps aux | less` -style composition without hand-rolling a second
// traversal alongside cdThenPwd's.
func interactiveProgram(cmd string) (name, alternative string, found bool) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return "", "", false
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		word := call.Args[0].Lit()
		if alt, ok := interactiveTUIPrograms[word]; ok {
			name, alternative, found = word, alt, true
			return false
		}
		return true
	})
	return name, alternative, found
}

// cdThenPwd reports whether cmd is a chain that starts by changing directory and
// ends by printing it (`cd / && pwd`). It looks like a way of asking where the
// session is and is the opposite: pwd only echoes wherever the cd just went, so
// "are we in the root dir?" comes back "yes" whatever the truth. The model
// reached for exactly this once it could no longer answer with a bare cd.
func cdThenPwd(cmd string) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return false
	}
	var leaves []string
	var walk func(st *syntax.Stmt) bool
	walk = func(st *syntax.Stmt) bool {
		if st == nil || len(st.Redirs) != 0 || st.Background || st.Negated {
			return false
		}
		switch c := st.Cmd.(type) {
		case *syntax.CallExpr:
			if len(c.Args) == 0 {
				return false
			}
			name := c.Args[0].Lit()
			if name == "" {
				return false
			}
			leaves = append(leaves, name)
			return true
		case *syntax.BinaryCmd:
			if c.Op != syntax.AndStmt && c.Op != syntax.OrStmt {
				return false
			}
			return walk(c.X) && walk(c.Y)
		}
		return false
	}
	for _, st := range f.Stmts {
		if !walk(st) {
			return false
		}
	}
	return len(leaves) >= 2 && leaves[0] == "cd" && leaves[len(leaves)-1] == "pwd"
}

// loopStep records one executed command and its result, so later steps'
// prompts can show the model what has already happened.
type loopStep struct {
	command string
	result  executor.Result
	// readOnly and moves are what the analysis predicted for the command, for
	// the brakes (brakes.go): whether it changed no file, and what it moved.
	readOnly bool
	moves    []move
	// changed is what the step created, changed or moved something into, for
	// the refs aid (aids.go); empty when the aid is off or the step failed.
	changed []string
	// removed is what the step deleted or moved away, for the refs aid's
	// singular "it" (aids.go, oneThingNote); empty when the step failed.
	removed []string
}

// loopOption tunes runLoop without widening its signature for the many
// existing callers that need none of it — a variadic option keeps every
// current call site (CLI, REPL, and eight test call sites) unchanged.
type loopOption func(*loopConfig)

type loopConfig struct {
	// tokenSink, when non-nil, receives model output token-by-token as it
	// is generated. Nil means non-streaming, which stays the default and
	// the documented CLI/REPL behavior.
	tokenSink io.Writer

	// session, when non-nil, supplies cross-task conversation memory and
	// receives this task's turn once it finishes. Nil means each task
	// starts cold — which stays correct for one-shot CLI mode, where
	// statelessness is the point (D19), not a limitation.
	session *session.Context

	// telemetry, when non-nil, records the study's event log for this task
	// (M7). Nil is the norm: ordinary CLI, REPL, and TUI use writes no
	// telemetry at all. A nil *telemetry.Logger is itself a no-op, so the
	// emission sites below are unconditional — a branch at every call site
	// is how an event eventually goes unlogged.
	telemetry *telemetry.Logger

	// taskID identifies which study task prompt these events belong to. It
	// is meaningless without telemetry and is set by the same option.
	taskID string

	// answerDisabled suppresses the natural-language reply (D31). The
	// polarity is deliberate: answering is the default, and a caller opts
	// out. An opt-in flag would have to be remembered at every call site,
	// and forgetting it at one is precisely how the session logger shipped
	// recording nothing. Only tests that script a fixed number of model
	// responses set this.
	answerDisabled bool

	// events, when non-nil, receives what the loop would otherwise narrate as
	// text: the command chosen, what it did, the answer, notices and problems.
	// The loop then writes none of that to out or errOut. What runs, what is
	// gated, what is journaled, and what telemetry records are identical; only
	// the presentation moves to the interface. Each step is asked for in a
	// non-streaming call, because a command printed piece by piece into a
	// scrollback reads as noise. The reply is streamed, as AnswerPart events
	// ahead of the Answer, because it is the one thing the person waits to
	// read. Used by TUI mode, `run --json` and Layer 7.
	events func(loopevent.Event)

	// told, when non-nil, receives the reply the person was shown (not one
	// withheld), so the session can remember what "it" was (aids.go,
	// referenceNote).
	told *string

	// requireUndo marks a real session, where commands run without asking
	// only because undo can take them back. When the journal cannot be
	// written, every command that changes files asks instead (finding
	// r2-atk8-08). Tests that run without a journal on purpose leave it off.
	requireUndo bool
	noUndo      bool // set by runLoop when requireUndo cannot be met

	// onEnd, when non-nil, receives the task's outcome as the loop returns:
	// the same value the study log's task_end records. Used by `run --json`
	// to close its stream with how the task ended.
	onEnd func(outcome string)
}

// withRequireUndo marks a real session; see loopConfig.requireUndo.
func withRequireUndo() loopOption {
	return func(c *loopConfig) { c.requireUndo = true }
}

// withOutcome reports the task's final outcome to fn. See loopConfig.onEnd.
func withOutcome(fn func(outcome string)) loopOption {
	return func(c *loopConfig) { c.onEnd = fn }
}

// withTokenStreaming makes the loop stream each step's generation into w as it
// arrives. Only tests use it now: the TUI takes events and streams the reply
// alone (AnswerPart), and CLI mode renders once generation finishes
// (docs/interface-modes.md).
func withTokenStreaming(w io.Writer) loopOption {
	return func(c *loopConfig) { c.tokenSink = w }
}

// withEvents hands the loop's narration to fn as typed events instead of text.
// See loopConfig.events.
func withEvents(fn func(loopevent.Event)) loopOption {
	return func(c *loopConfig) { c.events = fn }
}

// eventsFrom returns withEvents for a writer that wants events, or nothing for
// one that does not, so every other caller keeps the text output unchanged.
func eventsFrom(w io.Writer) []loopOption {
	if em, ok := w.(loopevent.Emitter); ok {
		return []loopOption{withEvents(em.Emit)}
	}
	return nil
}

// notice shows text to the person now: as an event, or as the given text line.
func (c loopConfig) notice(out io.Writer, text string) {
	if c.events != nil {
		c.events(loopevent.Event{Kind: loopevent.Notice, Text: text})
		return
	}
	fmt.Fprintln(out, text)
}

// note records bookkeeping that is only worth showing on request. Without an
// event consumer it is written as the given legacy text.
func (c loopConfig) note(out io.Writer, text, legacy string) {
	if c.events != nil {
		c.events(loopevent.Event{Kind: loopevent.Note, Text: text})
		return
	}
	fmt.Fprint(out, legacy)
}

// problem reports something that went wrong: plain words and the technical
// cause as an event, or the legacy line otherwise.
func (c loopConfig) problem(errOut io.Writer, plain, detail, legacy string) {
	// A raw error can run to kilobytes (a path that grew without bound in
	// finding r2-atk5-06); the plain sentence carries the meaning.
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	if len(legacy) > 400 {
		legacy = legacy[:400] + "…\n"
	}
	if c.events != nil {
		c.events(loopevent.Event{Kind: loopevent.Problem, Text: plain, Detail: detail})
		return
	}
	fmt.Fprint(errOut, legacy)
}

// tellAnswer tells the person the model's reply, unless it states a number or
// file name nothing showed, in which case it says so instead (brakes.go). After
// a last step that found nothing, it says which search that was.
func (c loopConfig) tellAnswer(out io.Writer, task string, history []loopStep, answer string) {
	if claims := unsupportedClaims(answer, task, history); len(claims) > 0 {
		c.notice(out, withheldReply(claims))
		// Kept as bookkeeping, so a log shows what was held back and why.
		c.note(out, "The summary that was left out: "+answer, "")
	} else {
		c.printAnswer(out, answer)
		c.telemetry.TaskAnswered(c.taskID, answer)
		if c.told != nil {
			*c.told = answer
		}
	}
	if cmd, ok := foundNothing(history); ok {
		c.notice(out, foundNothingNote(cmd))
	}
}

// printAnswer writes the model's reply.
func (c loopConfig) printAnswer(out io.Writer, answer string) {
	if c.events != nil {
		c.events(loopevent.Event{Kind: loopevent.Answer, Text: answer})
		return
	}
	fmt.Fprintln(out, displaySafe(answer))
}

// withSessionContext gives the loop memory of earlier tasks in the same
// session, and records this task into that memory when it finishes (M6,
// D10). Used by REPL and TUI mode; one-shot CLI mode deliberately omits
// it, since a fresh process per invocation is what makes it scriptable.
func withSessionContext(sc *session.Context) loopOption {
	return func(c *loopConfig) { c.session = sc }
}

// withoutAnswer suppresses the natural-language reply. See loopConfig for why
// this is opt-out rather than opt-in.
func withoutAnswer() loopOption {
	return func(c *loopConfig) { c.answerDisabled = true }
}

// withTelemetry records this task's execution to the study's session log
// under taskID (M7). Used only by study builds; every other caller omits it
// and logs nothing.
func withTelemetry(l *telemetry.Logger, taskID string) loopOption {
	return func(c *loopConfig) { c.telemetry = l; c.taskID = taskID }
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
func runLoop(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string, opts ...loopOption) int {
	var cfg loopConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	var history []loopStep
	// journalFailed: a step ran but its undo entry could not be written. The
	// task then does not end as "complete": its result cannot be taken back,
	// and exit 0 told a script otherwise (volkswagen audit, finding 10).
	journalFailed := false

	if cfg.requireUndo {
		if journalPath == "" {
			cfg.noUndo = true
		} else if err := undo.JournalWritable(journalPath); err != nil {
			cfg.noUndo = true
			journalPath = ""
			cfg.problem(errOut, "Undo is not available right now: synapse can't write its undo history. I'll ask before anything that changes files.", err.Error(), "warning: undo is unavailable ("+err.Error()+"); every command that changes files will ask first\n")
		}
	}

	// A working folder that was deleted or moved away makes every command fail
	// in confusing ways, and the model then reports "0 files" as a fact or
	// gives up (finding atk4-07). Say what is wrong before asking it anything.
	if _, err := os.Getwd(); err != nil {
		cfg.telemetry.TaskStart(cfg.taskID, task)
		cfg.telemetry.TaskEnd(cfg.taskID, outcomeNoWorkingDir, 0)
		cfg.problem(errOut, "The folder you are working in no longer exists (it may have been deleted or moved). Change to another folder and try again.", err.Error(), "error: the current folder no longer exists: "+err.Error()+"\n")
		if cfg.onEnd != nil {
			cfg.onEnd(outcomeNoWorkingDir)
		}
		return exitCodeFor(outcomeNoWorkingDir)
	}

	// Mirrors the session defer below: the outcome is recorded on whichever
	// path the loop actually leaves by, because a task that failed halfway
	// is a data point the study needs, not an absence. Each return below
	// sets taskOutcome immediately before returning.
	taskOutcome := outcomeIncomplete
	cfg.telemetry.TaskStart(cfg.taskID, task)
	defer func() {
		cfg.telemetry.TaskEnd(cfg.taskID, taskOutcome, len(history))
		if cfg.onEnd != nil {
			cfg.onEnd(taskOutcome)
		}
	}()

	// Recorded on every exit path — DONE, UNSUPPORTED, a declined
	// confirmation, the step cap, a propose error, or a cancelled context.
	// A deferred call rather than one at the end of the happy path,
	// because a task that failed halfway still did whatever it did, and a
	// follow-up ("undo that", "try again with sudo") needs to see it. A
	// partial turn is memory; a missing one is a lie about what happened.
	if cfg.session != nil {
		var told string
		cfg.told = &told
		defer func() {
			steps := make([]session.Step, 0, len(history))
			for _, h := range history {
				steps = append(steps, session.Step{
					Command: h.command,
					Result:  stepResultNote(h.result),
					Changed: h.changed,
				})
			}
			cfg.session.AppendAnswered(task, steps, told)
			// Compacted is drained first: a background Compact from an
			// earlier turn can land in the gap between this Append and this
			// report, and a compaction that happened must not be reported as
			// a silent drop just because it was noticed a turn late.
			if n := cfg.session.TakeCompacted(); n > 0 {
				text := fmt.Sprintf("Condensed %d older turn(s) into a short summary to stay within the context budget.", n)
				cfg.note(out, text, fmt.Sprintf("note: condensed %d older turn(s) into a summary to stay within the context budget.\n", n))
			}
			if n := cfg.session.TakeDropped(); n > 0 {
				text := fmt.Sprintf("Dropped %d older turn(s) from memory to stay within the context budget.", n)
				cfg.note(out, text, fmt.Sprintf("note: dropped %d older turn(s) from memory to stay within the context budget.\n", n))
			}
			maybeCompact(client, model, cfg.session)
		}()
	}

	reasked := false
	for i := 1; i <= maxLoopSteps; i++ {
		// When streaming, the model's output lands in the transcript as it
		// arrives, so the step label is printed first and the tokens append
		// to it. Printing the command again afterwards is what produced
		// "UNSUPPORTEDstep 1: UNSUPPORTED" in live use.
		if cfg.tokenSink != nil {
			fmt.Fprintf(out, "step %d: ", i)
		}
		resp, cmd, err := proposeStep(ctx, client, model, task, history, cfg.tokenSink, cfg.session)
		if err != nil {
			if cfg.tokenSink != nil {
				fmt.Fprintln(out)
			}
			taskOutcome = outcomeProposeError
			if ctx.Err() != nil {
				// Ctrl+C or a cancelled session, not a model failure (atk4-04).
				taskOutcome = outcomeInterrupted
				cfg.problem(errOut, "Stopped because you interrupted.", err.Error(), "stopped: interrupted\n")
				return exitCodeFor(taskOutcome)
			}
			if errors.Is(err, errCutOff) {
				// The backend answered; the answer was unusable (r2-atk8-10).
				cfg.problem(errOut, "The model's reply was too long and got cut off, so I didn't use it. Try asking again in simpler words.", err.Error(), fmt.Sprintf("error: %v\n", err))
				return exitCodeFor(taskOutcome)
			}
			cfg.problem(errOut, "I couldn't get an answer from the language model.", err.Error(), fmt.Sprintf("error: %v\n", err))
			return exitCodeFor(taskOutcome)
		}
		// Given up straight after a program was refused as not installed (the
		// tools aid): asked once more, with what the installed programs like
		// it do (aids.go, reaskAfterMissing). Once per task, so it cannot loop.
		if (cmd == "" || cmd == "UNSUPPORTED") && !reasked {
			if again := reaskAfterMissing(task, history); again != "" {
				reasked = true
				cfg.note(out, "Asked the model once more, with what the installed programs like the missing one do.", "")
				if cfg.tokenSink != nil {
					fmt.Fprint(out, "\n  asked again: ")
				}
				if r2, c2, err2 := proposeStep(ctx, client, model, again, history, cfg.tokenSink, cfg.session); err2 == nil {
					resp, cmd = r2, c2
				}
			}
		}
		// Guidance in the prompt reduces repetition but does not eliminate it at
		// this model size. Stopping is better than spending the remaining steps
		// re-running a known failure: it fails faster, it does not pad the
		// study's step counts with identical attempts, and it gives the user a
		// truthful reason instead of five identical error blocks.
		for _, prior := range failedCommands(history) {
			if prior == cmd {
				if cfg.events != nil {
					cfg.notice(out, "I already tried that and it did not work, so I'm stopping instead of repeating it.")
					cfg.note(out, "Not run again, because it already failed here: "+cmd, "")
				} else {
					fmt.Fprintf(out, "This command already failed here, so running it again will not help: %s\n", displaySafe(cmd))
				}
				taskOutcome = outcomeRepeatedFailure
				if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
					cfg.tellAnswer(out, task, history, answer)
				}
				return exitCodeFor(taskOutcome)
			}
		}
		// The same command again, straight after it succeeded, means the model
		// has nothing new to do but did not say DONE: small models do this until
		// the step limit, and the person waits through it to be told the task
		// failed (1.5B, 2026-10-23: 0 of 12 clean exits, 10 tasks done). Only
		// the step just before counts, since nothing ran in between to change
		// what it would show; a later repeat, such as ls before and after a mv,
		// is a check. Not running it also keeps a side effect from happening
		// twice. The outcome is its own, so a run that stopped here is never
		// counted as one the model finished.
		if n := len(history); n > 0 && history[n-1].command == cmd && history[n-1].result.ExitCode == 0 && !history[n-1].result.TimedOut {
			cfg.note(out, "Not run again, because it just ran and its result is above: "+cmd,
				fmt.Sprintf("This command just ran, so running it again would show nothing new: %s\n", displaySafe(cmd)))
			taskOutcome = outcomeRepeatedSuccess
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				cfg.tellAnswer(out, task, history, answer)
			} else if aerr != nil {
				cfg.problem(errOut, "I couldn't put the result into words this time.", aerr.Error(), fmt.Sprintf("note: could not summarise the result: %v\n", aerr))
			}
			return exitCodeFor(taskOutcome)
		}
		cfg.telemetry.CommandIssued(cfg.taskID, i, cmd)

		if cfg.events != nil {
			// Announced below, once it is known to be a command.
		} else if cfg.tokenSink != nil {
			fmt.Fprintln(out)
			// The streamed text is the model's raw output; what actually
			// runs has been through cleanCommand. Show the canonical form
			// only when they differ — fenced or backticked output — so the
			// user always sees the command that is about to execute without
			// a redundant echo on the common path.
			if strings.TrimSpace(resp.Response) != cmd {
				fmt.Fprintf(out, "  command: %s\n", displaySafe(cmd))
			}
			fmt.Fprintf(out, "  stats: %d tokens in %s\n", resp.EvalCount, resp.Latency().Round(time.Millisecond))
		} else {
			fmt.Fprintf(out, "step %d: %s\n  stats: %d tokens in %s\n",
				i, displaySafe(cmd), resp.EvalCount, resp.Latency().Round(time.Millisecond))
		}

		if (cmd == "" || cmd == "UNSUPPORTED") && len(failedCommands(history)) > 0 {
			// Not "this has no command-line equivalent": a command was tried and
			// failed, and the model gave up. Saying the request is impossible
			// would be false, and exit 4 would tell a script the same lie
			// (finding atk1-01).
			msg := "The command I tried didn't work, and I couldn't find another way to do it. Its error message is above; it may say what went wrong, such as a file name that doesn't exist."
			// The failed step's own error, not the last step's: after
			// `pdftk … ` failed with "command not found", a following `ls`
			// succeeded silently, and the notice claimed there was no error
			// message (seen in the Layer 7 logs of 2026-10-25).
			if last := lastFailed(history); last == nil || strings.TrimSpace(last.result.Stderr) == "" {
				// No message to point at: a search that matched nothing ends
				// this way too (finding r2-atk5-07).
				msg = "The command I tried found nothing or didn't work (it gave no error message), and I couldn't find another way to do it."
			}
			cfg.notice(out, msg)
			taskOutcome = outcomeGaveUp
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				cfg.tellAnswer(out, task, history, answer)
			}
			return exitCodeFor(taskOutcome)
		}
		if cmd == "" || cmd == "UNSUPPORTED" {
			// Phrased for the person, not the system. This is the response a
			// participant meets whenever they ask for something outside the
			// shell — including the boundary tasks the study administers
			// deliberately — so it has to state the limit, say what the system
			// is for, and give a way forward. The previous wording ("model
			// reported this request cannot be done with a shell command")
			// reported an internal verdict and left the user with nothing.
			// Deliberately does not assert *why* it could not be done. The
			// model emits UNSUPPORTED for at least three different reasons —
			// genuinely outside shell capability, impossible in this
			// environment, or simply not understood — and it does not
			// distinguish them. The previous message picked one ("visual
			// tasks like editing images") and asserted it every time, which
			// made a failed `du` read as a request to edit an image
			// (open-problems.md row 4, hit in live testing 2026-09-15).
			if cfg.events != nil {
				cfg.notice(out, strings.Join([]string{
					"I couldn't work out a command for that.",
					"I work by running shell commands on this machine, so I can reach files and folders, disk usage, processes, packages, text in files, and network settings — but not things with no command-line equivalent, like clicking buttons in a graphical application or editing an image.",
					"I also don't answer general questions about the world — I only report what I can find on this machine, so that anything I tell you can be traced to a command that actually ran.",
					"If it is something the command line can do, try naming the file or folder — for example \"how much space is this folder using\" or \"find the ten largest files here\".",
				}, "\n"))
				taskOutcome = outcomeUnsupported
				return exitCodeFor(taskOutcome)
			}
			fmt.Fprintln(out, "I couldn't work out a command for that.")
			fmt.Fprintln(out, "I work by running shell commands on this machine, so I can reach files and folders, disk usage, processes, packages, text in files, and network settings — but not things with no command-line equivalent, like clicking buttons in a graphical application or editing an image.")
			fmt.Fprintln(out, "I also don't answer general questions about the world — I only report what I can find on this machine, so that anything I tell you can be traced to a command that actually ran.")
			fmt.Fprintln(out, "If it is something the command line can do, try naming the file or folder — for example \"how much space is this folder using\" or \"find the ten largest files here\".")
			taskOutcome = outcomeUnsupported
			return exitCodeFor(taskOutcome)
		}
		if strings.EqualFold(cmd, doneSentinel) {
			if len(history) == 0 {
				cfg.notice(out, "Nothing needs to be done.")
				taskOutcome = outcomeNothingToDo
				return exitCodeFor(taskOutcome)
			}
			// The answer comes before the mechanical completion line: it is
			// what the user asked for, and the step count is bookkeeping.
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				cfg.tellAnswer(out, task, history, answer)
			} else if aerr != nil {
				// Reported, not fatal. The task succeeded; only the summary
				// did not, and the raw output above already stands on its own.
				cfg.problem(errOut, "I couldn't put the result into words this time.", aerr.Error(), fmt.Sprintf("note: could not summarise the result: %v\n", aerr))
			}
			if cfg.events == nil {
				fmt.Fprintf(out, "Task complete in %d step(s).\n", len(history))
			}
			taskOutcome = outcomeComplete
			if journalFailed {
				cfg.notice(out, "The task ran, but at least one step could not be recorded for undo, so it cannot be undone.")
				taskOutcome = outcomeNotUndoable
			}
			return exitCodeFor(taskOutcome)
		}

		if cfg.events != nil {
			cfg.events(loopevent.Event{Kind: loopevent.Command, Step: i, Command: cmd, Tokens: resp.EvalCount, Latency: resp.Latency()})
		}

		// Resolved once per step and used both for the filesystem-aware
		// classification below (cp-onto-existing-destination needs to know
		// the destination's actual path) and for undo snapshotting — best
		// effort either way; an unresolvable wd just degrades both to their
		// no-filesystem-check behavior rather than failing the step.
		wd, _ := os.Getwd()

		// Each step runs in its own shell, so a lone `cd` changes nothing that
		// outlasts it. Running it anyway and reporting success is how the session
		// once answered "are we in the root dir?" with "We are now in the root
		// directory" while sitting exactly where it started. Say so instead, as a
		// failed step, so the next proposal sees it and reaches for `pwd` or a
		// path.
		if bareCd(cmd) || cdThenPwd(cmd) {
			note := "cd only changes the directory of the shell that runs it, so it cannot move this session, which stays in " +
				wd + ", and pwd right after a cd just repeats where the cd went. To look somewhere else, name the path in the command (for example ls /some/folder); to see where this session is, run pwd on its own."
			if cfg.events != nil {
				cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 1, NotRun: true})
			} else {
				fmt.Fprintf(out, "not run: %s\n%s\n", displaySafe(cmd), displaySafe(note))
			}
			cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 1, RawExitCode: 1})
			history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 1, RawExitCode: 1}})
			continue
		}

		// A full-screen program has nothing to run against here — see
		// interactiveTUIPrograms. Refused the same way a bare cd is: reported
		// as a failed step with a working alternative, so the next proposal
		// has something to act on instead of repeating the same dead end.
		// A program that is not installed fails with "command not found",
		// and a small model then tends to give up; saying so before running,
		// and asking for another way, keeps it going (aids.go, aidTools).
		if aidOn(aidTools) {
			if missing := missingPrograms(cmd); len(missing) > 0 {
				note := missingProgramsNote(missing, cmd)
				if cfg.events != nil {
					cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 127, NotRun: true})
				} else {
					fmt.Fprintf(out, "not run: %s\n%s\n", displaySafe(cmd), note)
				}
				cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 127, RawExitCode: 127})
				history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 127, RawExitCode: 127}})
				continue
			}
		}
		// A command that would run but cannot give what it is meant to, such
		// as awk's $2 on a CSV, is not run, and the model is told what works
		// (aids.go, aidLint). Reported as a failed step, like a bare cd.
		if aidOn(aidLint) {
			if note := lintCommand(cmd, wd); note != "" {
				if cfg.events != nil {
					cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 1, NotRun: true})
				} else {
					fmt.Fprintf(out, "not run: %s\n%s\n", displaySafe(cmd), note)
				}
				cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 1, RawExitCode: 1})
				history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 1, RawExitCode: 1}})
				continue
			}
		}
		if prog, alt, ok := interactiveProgram(cmd); ok {
			note := prog + " needs an interactive terminal, which is not available here, so it cannot run. Use " + alt + "."
			if cfg.events != nil {
				cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 1, NotRun: true})
			} else {
				fmt.Fprintf(out, "not run: %s\n%s\n", displaySafe(cmd), displaySafe(note))
			}
			cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 1, RawExitCode: 1})
			history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 1, RawExitCode: 1}})
			continue
		}

		verdict, reason := classifier.ClassifyForDir(cmd, wd)
		gd := analysisDecision(ctx, cmd, wd)
		// A step that only moves back what the step before it moved undoes the
		// work: 1.5B renamed report.txt to summary.txt and back until the step
		// limit. The analysis sees the reversal before it runs (brakes.go), so
		// it is not run and the task is answered from what had run.
		if n := len(history); n > 0 && history[n-1].result.ExitCode == 0 && reversesLast(history[n-1].moves, gd) {
			note := "Not run: it would move back what the step before it just moved."
			if cfg.events != nil {
				cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 1, NotRun: true})
			} else {
				fmt.Fprintf(out, "%s\n", note)
			}
			taskOutcome = outcomeReversedStep
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				cfg.tellAnswer(out, task, history, answer)
			} else if aerr != nil {
				cfg.problem(errOut, "I couldn't put the result into words this time.", aerr.Error(), fmt.Sprintf("note: could not summarise the result: %v\n", aerr))
			}
			return exitCodeFor(taskOutcome)
		}
		// A request about "it", one thing, that would move or delete two things
		// over the task: 3B moved small.txt with big.bin, told "it" was
		// big.bin. Not run, and the model told why (aids.go, aidRefs).
		if aidOn(aidRefs) {
			if note := oneThingNote(task, history, gd, wd); note != "" {
				if cfg.events != nil {
					cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd, Stderr: note, ExitCode: 1, NotRun: true})
				} else {
					fmt.Fprintf(out, "not run: %s\n%s\n", displaySafe(cmd), note)
				}
				cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 1, RawExitCode: 1})
				history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 1, RawExitCode: 1}})
				continue
			}
		}
		if verdict == classifier.Irreversible && analysisClearsTextScan(reason, gd) {
			// The list read a quoted '>' or "tee" as an overwrite; the
			// analysis resolved the command fully and found nothing to lose.
			cfg.note(out, "Ran without asking: the pattern list flagged \""+reason+"\", but the analysis found nothing that could be lost.", "")
			verdict, reason = classifier.Reversible, ""
		}
		extras := detectRiskExtras(cmd)
		extras.ownState = touchesOwnState(cmd, gd)
		// Without undo, nothing that changes files may run unasked; with the
		// analysis off there is no telling what does, so everything asks.
		changesWithoutUndo := cfg.noUndo && (gd == nil || gd.Analysis == nil || !gd.Analysis.ReadOnly())
		if verdict == classifier.Irreversible || (gd != nil && gd.Confirm) || extras.privileged || extras.network || extras.ownState || changesWithoutUndo || len(extras.emptiesBeforeReading) > 0 {
			if verdict != classifier.Irreversible {
				switch {
				case gd != nil && gd.Confirm:
					reason = firstReason(gd)
				case extras.ownState:
					reason = "changes synapse's own undo history and saved copies"
				case changesWithoutUndo && !extras.privileged && !extras.network:
					reason = "undo is not available"
				case len(extras.emptiesBeforeReading) > 0 && !extras.privileged && !extras.network:
					reason = "empties a file before reading it"
				case extras.privileged:
					reason = "uses administrator rights"
				default:
					reason = "reaches the network"
				}
			}
			// Consent has to cover recoverability, not just danger. D30
			// argues that showing the command is what makes approval
			// consent; row 19 showed that is not sufficient. A user shown
			// `ls -d */ | xargs rm -rf` approved it reasonably, and lost a
			// directory the system could not capture because the targets
			// only exist at runtime. Saying so before the prompt is the
			// difference between an informed yes and an uninformed one.
			unprotected := journalPath != "" && wd != "" && classifier.DeletionTargetsUnresolvable(cmd, wd)
			if gd != nil && journalPath != "" {
				// With the analysis on, the warning follows what it could actually
				// capture, not just the list's guess about run-time targets.
				unprotected = !gd.Confident || gd.Verdict.Class == effects.Unrecoverable
			}
			ask := func() bool {
				c := buildConsent(verdict == classifier.Irreversible, reason, gd, extras, unprotected, journalPath != "" && wd != "", wd)
				approvalText := c.text()
				if cfg.events != nil {
					cfg.events(loopevent.Event{Kind: loopevent.Approval, Step: i, Command: cmd, Text: approvalText})
				} else {
					// With the prompt, on stderr: stdout carries only results.
					fmt.Fprintf(errOut, "I need your OK before running this command:\n  %s\n%s\n", displaySafe(cmd), approvalText)
				}
				return confirmFn("Should I go ahead?")
			}
			approved := ask()
			// The answer was given for the folder as it was when the question
			// was asked. A file that appeared or changed while the person was
			// deciding can change what the command destroys and what can be
			// saved (finding r2-atk8-04: a file created at the prompt was
			// shredded with no copy). Check again, and ask again if it differs.
			for recheck := 0; approved && gd != nil && recheck < 2; recheck++ {
				now := analysisDecision(ctx, cmd, wd)
				if sameDecision(gd, now) {
					break
				}
				gd = now
				unprotected = journalPath == "" || !gd.Confident || gd.Verdict.Class == effects.Unrecoverable
				reason = firstReason(gd)
				cfg.notice(out, "Something in the folder changed while you were deciding, so I checked the command again. Here is what it would do now:")
				approved = ask()
			}
			cfg.telemetry.ConfirmationTriggered(cfg.taskID, i, cmd, reason, approved)
			if !approved {
				cfg.notice(out, "Cancelled.")
				taskOutcome = outcomeDeclined
				if ctx.Err() != nil {
					// Ctrl+C at the prompt, not a considered no.
					taskOutcome = outcomeInterrupted
				}
				return exitCodeFor(taskOutcome)
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
		var undoBeforeIDs map[string]undo.FileID
		var gateEntry *undo.Entry
		var contentBackups []undo.ContentBackup
		var trashed []undo.TrashedItem
		var gitReset string
		var metadataBackups []undo.MetadataBackup
		if journalPath != "" && wd != "" && gd != nil && gd.Confident {
			// The analysis found nothing it could not resolve, so its effect set is
			// complete and its plan replaces the legacy backups for this step.
			e, errs := gd.Capture(wd, cmd)
			if len(errs) > 0 {
				// The person was told undo could put this back, or it ran without
				// asking because it could; either way, without the copy that is
				// not true, so it does not run (finding r2-atk5-06: a failed copy
				// was reported and the approved deletion ran anyway).
				detail := errs[0].Error()
				if len(detail) > 300 {
					detail = detail[:300] + "…"
				}
				cfg.problem(errOut, "I couldn't save a copy of what this command changes, so I did not run it. Nothing was changed.", detail, "error: could not save a copy first, so the command was not run: "+detail+"\n")
				taskOutcome = outcomeCaptureFailed
				return exitCodeFor(taskOutcome)
			}
			gateEntry = &e
		} else if journalPath != "" && wd != "" {
			switch verdict {
			case classifier.Reversible:
				undoBefore, _ = undo.Snapshot(wd)
				undoBeforeIDs, _ = undo.SnapshotIDs(wd)
			case classifier.Irreversible:
				contentBackups, trashed, gitReset, metadataBackups = backupBeforeIrreversible(ctx, cmd, wd, errOut)
				// Snapshot regardless. When capture found nothing to copy —
				// row 19's case — the before/after directory diff is the
				// only remaining record of what was destroyed, and a command
				// that deletes a directory with no journal entry at all is
				// how that incident became invisible after the fact. A
				// listing is not a backup and cannot restore contents; it is
				// an audit trail, and it is strictly better than silence.
				// Only when *nothing* was captured. Setting undoBefore
				// selects the directory-diff journal branch below, which
				// would otherwise discard a captured git SHA or metadata
				// record — caught by the existing loop tests.
				if len(trashed) == 0 && len(contentBackups) == 0 &&
					gitReset == "" && len(metadataBackups) == 0 {
					undoBefore, _ = undo.Snapshot(wd)
				}
			}
		}

		execCtx, execCancel := context.WithTimeout(ctx, stepExecutionTimeout)
		execStart := time.Now()
		result := executor.RunCapped(execCtx, cmd, commandOutputLimit)
		execLatency := time.Since(execStart)
		execCancel()
		if result.Err != nil {
			cfg.problem(errOut, "I couldn't run that command.", result.Err.Error(), fmt.Sprintf("error: command did not run: %v\n", result.Err))
			taskOutcome = outcomeExecutionError
			if ctx.Err() != nil {
				taskOutcome = outcomeInterrupted
			}
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				cfg.tellAnswer(out, task, history, answer)
			}
			return exitCodeFor(taskOutcome)
		}
		cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{
			ExitCode:    result.ExitCode,
			RawExitCode: result.RawExitCode,
			SIGPIPE:     result.SIGPIPE,
			Latency:     execLatency,
		})
		if cfg.events != nil {
			cfg.events(loopevent.Event{Kind: loopevent.Result, Step: i, Command: cmd,
				Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, TimedOut: result.TimedOut})
		} else {
			if result.Stdout != "" {
				fmt.Fprint(out, displaySafe(result.Stdout))
			}
			if result.Stderr != "" {
				fmt.Fprint(errOut, displaySafe(result.Stderr))
			}
			if result.TimedOut {
				fmt.Fprintf(out, "command exceeded %s and was terminated.\n", stepExecutionTimeout)
			}
			fmt.Fprintf(out, "exit code: %d\n\n", result.ExitCode)
		}

		if gateEntry != nil {
			// Journaled whatever the exit code: the captures were taken before the
			// command ran, so they can restore even a command that failed partway.
			// What it was expected to create or move but did not is dropped.
			observed := gateEntry.KeepObserved()
			gateEntry = &observed
			emptied := emptiedFiles(gateEntry.ContentBackups)
			for _, p := range emptied {
				// A command that empties the file it reads (`sort f > f`) ends
				// with exit 0; say so, since the model's summary may claim
				// success (finding r3-atk9-04).
				cfg.notice(out, fmt.Sprintf("Note: %s is now empty. If that is not what you wanted, synapse undo brings back what was in it.", displaySafe(p)))
			}
			if gateEntry.Restorable() {
				if err := undo.AppendJournal(journalPath, *gateEntry); err != nil {
					cfg.problem(errOut, "I couldn't record this step for undo, so it cannot be undone.", err.Error(), fmt.Sprintf("warning: could not record undo entry: %v\n", err))
					journalFailed = true
				}
			}
		} else if wd != "" && undoBefore != nil && result.ExitCode == 0 {
			if after, err := undo.Snapshot(wd); err == nil {
				afterIDs, _ := undo.SnapshotIDs(wd)
				if entry := undo.BuildEntryIDs(wd, cmd, undoBefore, after, undoBeforeIDs, afterIDs); !entry.IsNoop() {
					if err := undo.AppendJournal(journalPath, entry); err != nil {
						cfg.problem(errOut, "I couldn't record this step for undo, so it cannot be undone.", err.Error(), fmt.Sprintf("warning: could not record undo entry: %v\n", err))
						journalFailed = true
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
				cfg.problem(errOut, "I couldn't record this step for undo, so it cannot be undone.", err.Error(), fmt.Sprintf("warning: could not record undo entry: %v\n", err))
				journalFailed = true
			}
		}

		step := loopStep{command: cmd, result: result}
		step.readOnly = changesNothing(gd)
		step.moves = movesOf(gd)
		if result.ExitCode == 0 && aidOn(aidRefs) {
			step.changed = changedPaths(gd, wd)
			step.removed = removedPaths(gd, wd)
		}
		history = append(history, step)
	}

	taskOutcome = outcomeStepLimit
	// Answering matters more here than on the happy path. A task that ran out
	// of steps leaves the user with a wall of failed commands and no statement
	// of what went wrong; the raw output is evidence, not an explanation.
	if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
		cfg.tellAnswer(out, task, history, answer)
	}
	cfg.problem(errOut, "I ran out of steps before finishing this task.",
		fmt.Sprintf("step limit reached (%d steps) without the task being reported complete", maxLoopSteps),
		fmt.Sprintf("error: step limit reached (%d steps) without the task being reported complete — stopping.\n", maxLoopSteps))
	return exitCodeFor(taskOutcome)
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

// stepResultNote compacts an executed step's outcome into the one line a
// later task might need to resolve a reference. Deliberately lossy: across
// tasks the result only has to carry *what happened*, not the detail the
// in-task loop reasons over, and every character kept here is one denied
// to an older turn that might hold the actual referent.
func stepResultNote(r executor.Result) string {
	var parts []string
	if r.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit %d", r.ExitCode))
	}
	if r.TimedOut {
		parts = append(parts, "timed out")
	}
	if out := strings.TrimSpace(r.Stdout); out != "" {
		parts = append(parts, out)
	} else if errOut := strings.TrimSpace(r.Stderr); errOut != "" {
		parts = append(parts, errOut)
	} else if r.ExitCode == 0 {
		parts = append(parts, "ok")
	}
	return strings.Join(parts, " — ")
}

// failedCommands returns the distinct commands in history that exited
// nonzero, oldest first. Used both to warn the model off repeating them and
// to detect when it does so anyway.
func failedCommands(history []loopStep) []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range history {
		if st.result.ExitCode == 0 || seen[st.command] {
			continue
		}
		seen[st.command] = true
		out = append(out, st.command)
	}
	return out
}

// sameDecision reports whether two analyses of one command promise the same
// thing: the same verdict, the same confidence, and the same captures.
func sameDecision(a, b *gate.Decision) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Confirm != b.Confirm || a.Confident != b.Confident || a.Verdict.Class != b.Verdict.Class {
		return false
	}
	pa, _ := json.Marshal(a.Plan)
	pb, _ := json.Marshal(b.Plan)
	return bytes.Equal(pa, pb)
}

// emptiedFiles lists the backed-up files that had content before the command
// and are empty after it.
func emptiedFiles(backups []undo.ContentBackup) []string {
	var out []string
	for _, cb := range backups {
		before, err1 := os.Stat(cb.BackupPath)
		target := cb.Path
		if cb.LinkTarget != "" {
			target = cb.LinkTarget
		}
		after, err2 := os.Stat(target)
		if err1 == nil && err2 == nil && before.Size() > 0 && after.Size() == 0 {
			out = append(out, cb.Path)
		}
	}
	return out
}

// lastFailed returns the most recent step that failed, or nil.
func lastFailed(history []loopStep) *loopStep {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].result.ExitCode != 0 {
			return &history[i]
		}
	}
	return nil
}
