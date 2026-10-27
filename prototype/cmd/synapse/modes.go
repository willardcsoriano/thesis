package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
	"synapseos/internal/session"
	"synapseos/internal/telemetry"
	"synapseos/internal/tui"
	"synapseos/internal/undo"
)

// runTUI launches TUI mode; stateless selects scratch mode's header and its
// nil session context (D43, D44), otherwise identical: same TaskRunner
// injection, same warm-up, same everything else. TUI mode launches without a
// connectivity precheck on purpose (M5 step 3) — a full-screen app that opens
// and reports the problem inside the session beats one that exits to a bare
// shell over a transient backend blip; an unreachable Ollama surfaces as an
// ordinary error line in the transcript on the first proposal attempt, and
// the session stays usable once the backend comes back.
func runTUI(ctx context.Context, client *ollama.Client, model string, stateless bool) {
	journalPath, err := undo.DefaultJournalPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this session won't be undoable: %v\n", err)
		journalPath = ""
	}
	// The injected runner is runLoop itself, with only the client, model, and
	// journal bound in. TUI mode therefore executes the exact same
	// propose/classify/confirm/execute path as CLI and REPL mode — no
	// reimplementation, so safety gating cannot drift between interface
	// modes. A nil sc is the same "no memory" signal scratch mode already
	// uses everywhere else (D43): handleSessionCommand's "context"/"clear"
	// and runLoop's own withSessionContext both already treat it that way.
	var sc *session.Context
	if !stateless {
		sc = session.New()
	}
	tracker := newTaskTracker()
	tel, closeTel := studyLogger(os.Stderr)
	defer closeTel()
	runner := tuiRunner(client, model, journalPath, sc, tracker, tel)
	// Load the model while the user is still reading the header, so the first
	// answer does not pay a cold start (measured at 30-40s on the reference
	// machine). A failure is not fatal: the first real task will report it.
	warm := func(ctx context.Context) error { return client.Preload(ctx, model, generationOptions()) }
	newModel := tui.NewModel
	if stateless {
		newModel = tui.NewScratchModel
	}
	if err := tui.RunModelWithWarmup(newModel(runner), warm); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runAdHoc runs CLI mode's bounded multi-step loop (D21) for a single
// user-supplied task against the real terminal, then exits the process with
// runLoop's verdict. It is the thin process-control wrapper around runLoop —
// see that function for the actual loop behavior.
func runAdHoc(ctx context.Context, client *ollama.Client, model, task string, asJSON bool) {
	journalPath, err := undo.DefaultJournalPath()
	if err != nil {
		// Non-fatal: undo recording is a safety net, not the task itself.
		// A task should still run even if e.g. $HOME isn't writable.
		fmt.Fprintf(os.Stderr, "warning: undo journal unavailable, this run won't be undoable: %v\n", err)
		journalPath = ""
	}
	// The first Ctrl+C, SIGTERM (what supervisors and `timeout` send) or SIGHUP
	// (a closed terminal or SSH session) cancels the task, so the loop stops
	// the running command, journals what it changed, records how the task
	// ended, and exits 128 plus the signal's number. Before, SIGTERM and
	// SIGHUP killed synapse outright and the step that ran went unjournaled
	// (finding atk4-03). Handling then stands down, so a second signal ends
	// the process at once, including while a confirmation prompt waits.
	ctx, cancelOnSignal := context.WithCancel(ctx)
	defer cancelOnSignal()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, stopSignals...)
	var caught atomic.Int32
	go func() {
		select {
		case sig := <-sigs:
			caught.Store(int32(sig.(syscall.Signal)))
			signal.Stop(sigs)
			cancelOnSignal()
		case <-ctx.Done():
			signal.Stop(sigs)
		}
	}()

	// Losing stdout cancels the task the same way (outputGuard).
	ctx, stdoutGone := context.WithCancel(ctx)
	defer stdoutGone()
	stdout := &outputGuard{w: os.Stdout, broken: stdoutGone}

	// Prompts go to stderr with the other diagnostics: stdout carries only
	// the task's results, so it can be piped or parsed.
	// A Ctrl+C at a confirmation prompt ends the wait at once: confirmLine
	// returns as soon as ctx is cancelled, declining, and the loop records
	// the task as interrupted.
	src := newLineSource(os.Stdin)
	confirmFn := func(prompt string) bool { return confirmLine(ctx, src, os.Stderr, prompt) }
	fmt.Fprintf(os.Stderr, "Each step may run for up to %s before it's automatically stopped.\n\n", stepExecutionTimeout)

	var emitter *jsonEmitter
	if asJSON {
		emitter = newJSONEmitter(stdout)
	}
	finish := func(outcome string, code int) {
		if sig := caught.Load(); sig != 0 && outcome == outcomeInterrupted {
			code = 128 + int(sig) // 130 Ctrl+C, 143 SIGTERM, 129 SIGHUP
		}
		if err := stdout.failure(); err != nil {
			fmt.Fprintf(os.Stderr, "error: could not write the results to standard output: %v\n", err)
			code = exitFailed
		}
		if emitter != nil {
			emitter.End(outcome, code)
			if err := emitter.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "error: writing --json output: %v\n", err)
				if code == exitOK {
					code = exitFailed
				}
			}
		}
		os.Exit(code)
	}

	// Also answered on the one-shot path, not only in the session loops:
	// `synapse run hello` is as likely a first contact as typing it at the prompt.
	var reply strings.Builder
	if answerConversational(task, &reply) {
		if emitter != nil {
			emitter.Emit(loopevent.Event{Kind: loopevent.Answer, Text: strings.TrimRight(reply.String(), "\n")})
		} else {
			fmt.Fprint(stdout, displaySafe(reply.String()))
		}
		finish(outcomeComplete, exitOK)
	}

	tel, closeTel := studyLogger(os.Stderr)
	outcome := outcomeIncomplete
	opts := []loopOption{withTelemetry(tel, newTaskTracker().current()), withOutcome(func(o string) { outcome = o }), withRequireUndo()}
	if emitter != nil {
		opts = append(opts, withEvents(emitter.Emit))
	}
	code := runLoop(ctx, client, model, task, confirmFn, stdout, os.Stderr, journalPath, opts...)
	closeTel()
	finish(outcome, code)
}

// runREPL is M4's persistent CLI loop: instead of one process per task
// (runAdHoc), it reads one line of task input at a time from in and runs
// each through the same runLoop, in the same long-running process, until
// EOF or an explicit "exit"/"quit" line. Every task still goes through the
// identical classify/confirm/execute path as the one-shot path — nothing
// about the safety gating changes, only the process lifecycle around it.
//
// One lineSource owns in and serves both the task-line reads and every
// confirmation prompt a task triggers, in order. Two readers over the same
// stream would let one buffer input the other was about to consume (a task
// typed right after answering a prompt), the state leak this milestone exists
// to retire. Because the source reads on its own goroutine, a confirmation
// can stop waiting when its task is cancelled, and the line typed afterwards
// still arrives, in order, at the session's own prompt.
//
// A task that fails (propose error, UNSUPPORTED, step limit, cancelled
// confirmation) does not end the session — runLoop's own return code is
// intentionally ignored here; the whole point of a persistent loop is that
// one bad task doesn't force a restart to try another.
func runREPL(ctx context.Context, client *ollama.Client, model, journalPath string, in io.Reader, out, errOut io.Writer) int {
	return runInteractiveSession(ctx, client, model, journalPath, in, out, errOut, false)
}

// runScratch is scratch mode (D43): the same persistent, plain-text
// back-and-forth loop as REPL, minus memory. Every task starts fresh —
// "whatever the last command is is its scope" — which is what makes it
// lighter than REPL: no rolling window, no budget to approach, nothing to
// compact in the background (D42), nothing a stray "clear" needs to undo.
// The trade is exactly what REPL exists to avoid: a follow-up like "move it
// to Downloads" cannot resolve, because there is no earlier turn to resolve
// it against. For a quick, disposable, one-thing-after-another session where
// that trade is fine, this is the lighter mode to reach for.
func runScratch(ctx context.Context, client *ollama.Client, model, journalPath string, in io.Reader, out, errOut io.Writer) int {
	return runInteractiveSession(ctx, client, model, journalPath, in, out, errOut, true)
}

// runInteractiveSession is REPL's and scratch's shared loop (D43): read a
// line, run it through the identical propose/classify/confirm/execute path
// every mode uses, print the result, repeat until exit/quit/EOF. stateless
// selects the one real difference — whether session memory is threaded
// through runLoop at all — everything else (the shared bufio.Reader wiring
// confirmation prompts to task-line reads, Ctrl+C-per-task, telemetry) is
// identical between the two, which is deliberately kept in one place so
// they cannot drift apart from each other by accident.
func runInteractiveSession(ctx context.Context, client *ollama.Client, model, journalPath string, in io.Reader, out, errOut io.Writer, stateless bool) int {
	// The banner, the "> " prompt and confirmation prompts go to stderr, as in
	// one-shot mode: stdout carries only results, so a session's output can be
	// piped or saved without the chrome (finding atk4-11).
	if stateless {
		fmt.Fprintln(errOut, "Scratch mode: type a task and press enter; type exit or quit (or Ctrl+D) to leave.")
		fmt.Fprintln(errOut, "Nothing carries over between tasks. Each one starts fresh, with no memory of the one before it.")
	} else {
		fmt.Fprintln(errOut, "Persistent session: type a task and press enter; type exit or quit (or Ctrl+D) to leave.")
		fmt.Fprintln(errOut, "Follow-ups can refer back (\"move it to Downloads\"); type context to see what's remembered, clear to forget it.")
	}
	fmt.Fprintln(errOut, "While a task is running, Ctrl+C cancels just that task and returns you here.")
	fmt.Fprintf(errOut, "Each step may run for up to %s before it's automatically stopped.\n", stepExecutionTimeout)
	if tel := os.Getenv("SYNAPSE_SESSION_LOG"); tel != "" {
		fmt.Fprintln(errOut, "Study telemetry is recording; type task <id> to mark which task the following events belong to.")
	}

	src := newLineSource(in)
	// With tasks piped in rather than typed, the line after a confirmation is
	// the next task, not an answer: reading it as one silently dropped that
	// task (findings atk1-04, atk4-12). Such a session declines and says so;
	// approving in a script is what `synapse run` with a piped y is for.
	interactive := true
	if f, ok := in.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
			interactive = false
		}
	}
	confirmFn := func(ctx context.Context, prompt string) bool {
		if !interactive {
			fmt.Fprintf(errOut, "%s I didn't run it: the requests are coming from a file or another program, so there is no one here to answer yes or no. (To approve from a script, use synapse run and pipe in y.)\n", prompt)
			return false
		}
		return confirmLine(ctx, src, errOut, prompt)
	}
	// A nil *session.Context is scratch mode's whole implementation: every
	// consumer downstream (handleMemoryCommand's "context"/"clear", and
	// runLoop's own withSessionContext) already treats nil as "no memory" —
	// that convention already existed for every other caller that omits
	// session context (CLI mode, D19), so scratch mode needs no new branch
	// anywhere except right here.
	var sc *session.Context
	if !stateless {
		sc = session.New()
	}
	tracker := newTaskTracker()
	tel, closeTel := studyLogger(errOut)
	defer closeTel()

	for {
		fmt.Fprint(errOut, "> ")
		// Idle at the prompt no interrupt handler is installed, so Ctrl+C
		// here ends the process as a user expects; nothing cancels this read.
		line, readErr := src.next(context.Background())
		if errors.Is(readErr, errLineTooLong) {
			fmt.Fprintf(errOut, "That line was longer than %d KiB, so it was ignored. Please type a shorter request.\n", maxLine>>10)
			continue
		}

		if task := strings.TrimSpace(line); task != "" {
			if strings.EqualFold(task, "exit") || strings.EqualFold(task, "quit") {
				return 0
			}
			// Memory commands are handled here rather than sent to the
			// model: they are about the session itself, and a user asking
			// what is remembered needs a truthful answer, not a generated
			// one.
			// "undo" typed at the prompt means synapse's undo, not a request
			// for the model, which once answered it by proposing to delete the
			// file the previous task had moved.
			if isUndoRequest(task) {
				runUndo(func(p string) bool { return confirmFn(ctx, p) }, out, errOut, journalPath, tel, tracker.current())
				if readErr != nil {
					return 0
				}
				continue
			}
			if handled := handleSessionCommand(task, sc, tracker, out); handled {
				if readErr != nil {
					return 0
				}
				continue
			}
			opts := []loopOption{withTelemetry(tel, tracker.current()), withRequireUndo()}
			if sc != nil {
				opts = append(opts, withSessionContext(sc))
			}
			runTaskInterruptibly(ctx, client, model, task, confirmFn, out, errOut, journalPath, opts...)
			fmt.Fprintln(out)
		}

		if readErr != nil {
			return 0
		}
	}
}

// tuiRunner is what the TUI runs for each line typed: a session command or
// undo answered locally, anything else a task through runLoop.
func tuiRunner(client *ollama.Client, model, journalPath string, sc *session.Context, tracker *taskTracker, tel *telemetry.Logger) func(context.Context, string, func(string) bool, io.Writer, io.Writer) int {
	return func(taskCtx context.Context, task string, confirmFn func(string) bool, out, errOut io.Writer) int {
		// Session commands are answered locally, never generated — see
		// handleSessionCommand.
		if handleSessionCommand(task, sc, tracker, out) {
			return 0
		}
		// "undo" means synapse's undo here too, as in the REPL: sent to the
		// model, it was once answered by proposing to delete the file the
		// previous task had moved (ship review, 2026-10-27: the TUI did not
		// route it).
		if isUndoRequest(task) {
			return runUndo(confirmFn, out, errOut, journalPath, tel, tracker.current())
		}
		opts := append(eventsFrom(out), withTelemetry(tel, tracker.current()), withRequireUndo())
		if sc != nil {
			opts = append(opts, withSessionContext(sc))
		}
		// The writer the TUI hands in also accepts typed events, so the loop
		// reports commands, results, and answers as events and the TUI
		// decides how to show them. That is presentation only: the command
		// is still parsed from the whole response and classified exactly as
		// before.
		return runLoop(taskCtx, client, model, task, confirmFn, out, errOut, journalPath, opts...)
	}
}

// stopSignals are the signals that stop a task: the step running is ended and
// recorded, and synapse exits 128 plus the signal's number. The help lists
// each one's code (TestHelpDocumentsEveryVariableAndExitCode reads this list).
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// notifyInterrupts and stopInterrupts wrap signal.Notify/signal.Stop so
// tests can drive the interrupt path without sending real signals to the
// test process — the same dependency-indirection pattern already used for
// os.Link in internal/undo and WaitDelay in internal/executor.
var (
	notifyInterrupts = func(c chan<- os.Signal) { signal.Notify(c, os.Interrupt) }
	stopInterrupts   = func(c chan<- os.Signal) { signal.Stop(c) }
)

// syncWriter serialises writes to one stream. The interrupt watcher below
// reports cancellation on the same writer the running task is producing
// output on, so without this the two goroutines interleave mid-line and, on
// a bytes.Buffer, race outright. Both streams share one mutex so that a
// cancellation notice cannot land inside a half-written stderr line either.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// runTaskInterruptibly runs exactly one task with Ctrl+C wired to cancel
// that task alone, leaving the session alive — added Session 28 after a
// review found that M4, by making the process long-lived, had turned a
// harmless behavior into a real one: in one-shot CLI mode Ctrl+C killed a
// process that was about to exit anyway, but in a persistent session it
// destroyed the whole session, which is precisely the thing M4 exists to
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
// A Ctrl+C while the task waits at a confirmation prompt cancels it at once:
// confirmFn receives the task's context, and confirmLine stops waiting when
// it ends (see lineSource).
func runTaskInterruptibly(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(context.Context, string) bool, out, errOut io.Writer, journalPath string, opts ...loopOption) {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	syncOut := syncWriter{mu: &mu, w: out}
	syncErr := syncWriter{mu: &mu, w: errOut}

	sigCh := make(chan os.Signal, 1)
	notifyInterrupts(sigCh)
	defer stopInterrupts(sigCh)

	watchDone := make(chan struct{})
	defer close(watchDone)

	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(syncOut, "\nCancelling this task — the session stays open.")
			cancel()
		case <-watchDone:
		}
	}()

	confirmTask := func(prompt string) bool { return confirmFn(taskCtx, prompt) }
	runLoop(taskCtx, client, model, task, confirmTask, syncOut, syncErr, journalPath, opts...)
}

// taskTracker holds which study task the current events belong to.
//
// One-shot CLI runs one task per process, so SYNAPSE_TASK_ID alone is enough
// there. REPL and TUI mode do not work that way: a participant completes the
// whole task set inside a single session, so a task identifier fixed at
// process start would attribute every event of the session to one task and
// make per-task completion time — the study's primary measure — impossible to
// recover. The facilitator therefore advances it with `task <id>` as each
// printed prompt is handed over.
type taskTracker struct{ id string }

func newTaskTracker() *taskTracker {
	id := os.Getenv("SYNAPSE_TASK_ID")
	if id == "" {
		id = "unset"
	}
	return &taskTracker{id: id}
}

func (t *taskTracker) current() string {
	if t == nil {
		return ""
	}
	return t.id
}

// handleSessionCommand answers the inputs that are resolved locally rather
// than sent to the model: session memory (context, clear) and the study task
// pointer (task <id>). It reports whether it consumed the input.
func handleSessionCommand(task string, sc *session.Context, tr *taskTracker, out io.Writer) bool {
	trimmed := strings.TrimSpace(task)
	// The prefix is matched case-insensitively, but the identifier is taken
	// from the original text: task ids are case-sensitive labels from the
	// printed prompt sheet (T5, C4), and lowercasing them would silently
	// record events under an id that matches nothing in the task suite.
	if len(trimmed) > 5 && strings.EqualFold(trimmed[:5], "task ") {
		if id := strings.TrimSpace(trimmed[5:]); id != "" && tr != nil {
			tr.id = id
			fmt.Fprintf(out, "Now recording events under task %s.\n", id)
			return true
		}
	}
	if strings.EqualFold(trimmed, "task") && tr != nil {
		fmt.Fprintf(out, "Current task: %s\n", tr.current())
		return true
	}
	if answerConversational(task, out) {
		return true
	}
	return handleMemoryCommand(task, sc, out)
}

// answerConversational handles the inputs that are addressed to the system
// rather than to the machine: greetings, and questions about what it can do.
//
// D31 already decided these are answered locally, without invoking the model,
// for the same reason `context` and `clear` are — but the handler was never
// written, so they fell through to the loop. The model, asked to translate
// "hello" into a shell command, correctly concluded it could not and emitted
// UNSUPPORTED, and the user was told their greeting was a visual task like
// editing images. That is the first thing anyone types, so it was also the
// system's first impression (found in live testing 2026-09-15).
//
// Matching is exact on the normalised input rather than by prefix or
// substring: "hi" is a greeting, but "hi, delete the logs" is a task with a
// greeting attached, and swallowing the latter would be far worse than
// failing to recognise it.
func answerConversational(task string, out io.Writer) bool {
	norm := normaliseChatter(task)
	switch norm {
	case "hello", "hi", "hey", "yo", "hello there", "good morning", "good afternoon", "good evening":
		fmt.Fprintln(out, "Hello. Tell me what you want done to this machine, in ordinary words — for example \"how much space is this folder using\" or \"put the log files in their own folder\".")
		fmt.Fprintln(out, "Type help to see what I can reach, or exit to leave.")
		return true
	case "what ai model are you", "what model are you", "which model are you", "what llm are you",
		"are you human", "are you a human", "are you an ai", "are you a robot", "are you chatgpt",
		"are you real", "are you a person":
		// Identity questions are about the system, so the system answers
		// them — and answers them locally, because the one thing worse than
		// refusing is letting a 3B coder model improvise its own identity.
		fmt.Fprintf(out, "I'm SynapseOS — a program on this machine, not a person. I use a local language model (%s) running on your own hardware through Ollama; nothing you type leaves this computer.\n", envOr("SYNAPSE_MODEL", defaultModel))
		fmt.Fprintln(out, "What I actually do is turn what you say into shell commands and run them here. Type help for what I can reach.")
		return true
	case "help", "what can you do", "what can you do?", "who are you", "what are you", "what is this":
		fmt.Fprintln(out, "I turn what you say into shell commands and run them on this machine, showing you each command before it runs.")
		fmt.Fprintln(out, "I can reach anything the command line can: files and folders, disk usage, processes, packages, text in files, and network settings.")
		fmt.Fprintln(out, "I cannot click buttons in graphical applications, edit images, or browse web pages.")
		fmt.Fprintln(out, "Anything that cannot be undone stops and asks you first, and undo reverses the last thing I ran.")
		fmt.Fprintln(out, "Session commands: context (what I remember), clear (forget it), exit.")
		return true
	case "thanks", "thank you", "ty", "thx", "cheers", "thanks a lot", "thank you very much", "thanks so much", "much appreciated":
		fmt.Fprintln(out, "You're welcome.")
		return true
	case "how are you", "how are you doing", "how are you today", "how are you doing today", "how is it going",
		"how's it going", "hows it going", "how do you do", "what's up", "whats up", "sup", "how have you been":
		// Asked out of politeness, and a 3B coder model asked to turn it into a
		// shell command spent 26 seconds concluding it could not. The honest answer
		// is short and points back at the one thing this can do.
		fmt.Fprintln(out, "I'm running fine — I'm a program on this machine, so there's not much to report.")
		fmt.Fprintln(out, "Tell me what you want done, in ordinary words, or type help to see what I can reach.")
		return true
	case "really", "seriously", "are you sure", "are you serious", "what", "huh", "why", "why not", "what do you mean",
		"how so", "oh really", "is that so", "is that right", "come again", "pardon", "sorry", "sorry?", "wait", "hold on":
		// A reaction to what was just said, not a request. Nothing here can be
		// checked on the machine, so say what this can and cannot stand behind
		// rather than guessing at which earlier line the user is reacting to.
		fmt.Fprintln(out, "Yes — I can only act by running commands on this machine, and I only report what they return.")
		fmt.Fprintln(out, "Say what you want checked or done, in ordinary words, or type help.")
		return true
	case "ok", "okay", "k", "kk", "cool", "nice", "great", "good", "alright", "all right", "got it", "i see", "sure",
		"yes", "yeah", "yep", "yup", "no", "nope", "nah", "nothing", "never mind", "nevermind", "lol", "haha", "hehe",
		"wow", "hmm", "hm", "hmmm", "oh", "ah", "ooh", "interesting", "fine", "perfect", "awesome", "understood", "right":
		fmt.Fprintln(out, "OK. Tell me what you want done when you're ready — help lists what I can reach.")
		return true
	case "bye", "goodbye", "good bye", "see you", "see ya", "cya", "good night", "goodnight", "later":
		fmt.Fprintln(out, "Goodbye. Type exit to leave, or keep going.")
		return true
	}
	return false
}

// normaliseChatter reduces a line to the form the exact-match table above is
// keyed on: lower case, one space between words, and no trailing punctuation or
// repeated question marks ("really??", "hello!!"). It changes only how a line is
// written, never which words it contains, so a task with a greeting in front of
// it still does not match.
func normaliseChatter(task string) string {
	s := strings.ToLower(strings.Join(strings.Fields(task), " "))
	return strings.TrimRight(strings.TrimLeft(s, " ."), " .!?,;:…")
}

// handleMemoryCommand answers "context" and "clear". sc is nil in scratch
// mode (D43), which has nothing to summarize or forget — both commands still
// answer truthfully rather than falling through to the model, same reasoning
// as every other case here.
// handleMemoryCommand intercepts the two session-memory commands and
// reports whether it consumed the input.
//
// These are handled locally rather than passed to the model on purpose. A
// user asking what is remembered needs the actual contents, not a
// plausible-sounding generated answer — and "clear" must be reliable,
// since its whole job is being the escape hatch when memory has gone
// wrong. Both are also the deterministic reset points a manual test needs.
func handleMemoryCommand(task string, sc *session.Context, out io.Writer) bool {
	switch strings.ToLower(strings.TrimSpace(task)) {
	case "context":
		if sc == nil {
			fmt.Fprintln(out, "no memory is kept in this mode — every task starts fresh.")
			return true
		}
		fmt.Fprintln(out, sc.Summary())
		return true
	case "clear":
		if sc == nil {
			fmt.Fprintln(out, "nothing to forget — this mode already keeps no memory between tasks.")
			return true
		}
		n := sc.Len()
		sc.Clear()
		fmt.Fprintf(out, "Forgot %d remembered task(s); the next task starts fresh.\n", n)
		return true
	}
	return false
}

// studyLogger builds the M7 session logger from the environment, returning a
// nil logger and a no-op closer when SYNAPSE_SESSION_LOG is unset — which is
// every ordinary, non-study invocation. Telemetry is opt-in by absence rather
// than by flag so that a study session cannot silently run unlogged because
// someone forgot to pass something: either the path is configured and events
// are written, or it is not and none are claimed.
//
// A log that cannot be opened is fatal rather than a warning. A participant
// session that runs to completion and records nothing is unrecoverable, and
// twenty of those is the study.
func studyLogger(errOut io.Writer) (*telemetry.Logger, func()) {
	path := os.Getenv("SYNAPSE_SESSION_LOG")
	if path == "" {
		return nil, func() {}
	}
	participant := os.Getenv("SYNAPSE_PARTICIPANT")
	if participant == "" {
		fmt.Fprintln(errOut, "error: SYNAPSE_SESSION_LOG is set but SYNAPSE_PARTICIPANT is empty; refusing to write unattributable telemetry")
		os.Exit(1)
	}
	l, f, err := telemetry.Open(path, participant)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		os.Exit(1)
	}
	l.OnError = func(err error) {
		fmt.Fprintf(errOut, "error: the study log %s could not be written (%v); events from here on are not being recorded.\n", path, err)
	}
	return l, func() { f.Close() }
}

// isUndoRequest recognises the ways people ask the session to undo its last
// step. It is deliberately narrow: anything longer is a task for the model.
func isUndoRequest(task string) bool {
	t := strings.Trim(strings.ToLower(strings.Join(strings.Fields(task), " ")), ".!?")
	switch t {
	case "undo", "undo that", "undo it", "undo last", "undo the last command", "undo the last step", "oops undo that", "oops, undo that", "please undo that", "undo please",
		"take it back", "please take it back", "take that back", "revert that", "revert it":
		return true
	}
	return false
}
