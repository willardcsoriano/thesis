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
// same reversibility-gated execution model TUI mode (M5) will later reuse
// rather than rebuild. Run with no arguments, it instead walks a built-in
// sample task suite in propose-only mode: a quality smoke test across the
// task categories the study covers, which deliberately never touches the
// real filesystem and never loops.
//
// Usage:
//
//	synapse                              # propose-only sample task suite
//	synapse "find pdfs from this week"   # propose, classify, confirm, execute
//	synapse repl                         # persistent session (M4): issue several tasks in one process
//	synapse tui                          # full-screen TUI mode (M5): streaming, scrollback, in-session confirmation
//	synapse undo                         # reverse the most recent auto-run reversible command
//
// Environment:
//
//	SYNAPSE_MODEL    model tag       (default: qwen2.5-coder:3b)
//	SYNAPSE_OLLAMA   ollama base URL (default: http://localhost:11434)
//	SYNAPSE_NUM_CTX  context window   (default: 8192; Ollama's own default of 2048 silently truncates)
//	SYNAPSE_SESSION_LOG  path to the study's JSON Lines event log (M7); unset disables telemetry entirely
//	SYNAPSE_PARTICIPANT  anonymous participant code written into every event
//	SYNAPSE_TASK_ID      study task identifier the current invocation belongs to
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mvdan.cc/sh/v3/syntax"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"synapseos/internal/classifier"
	"synapseos/internal/effects"
	"synapseos/internal/executor"
	"synapseos/internal/ollama"
	"synapseos/internal/session"
	"synapseos/internal/telemetry"
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
Output the single next bash command needed to make progress on the task.

The prompt may contain two clearly separate sections. Do not confuse them:
- "Earlier in this session" is background from PREVIOUS, ALREADY-FINISHED tasks. Use it only to understand what words like "it", "that", or "the file" refer to. It is NEVER progress on the current task.
- "Steps already run" is progress on the CURRENT task, and only those steps count toward finishing it.

Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the CURRENT task has no steps run yet, always output a command — never DONE, no matter what earlier tasks did.
- If the steps already run for the CURRENT task have fully accomplished it, output exactly: DONE
- Combine steps into one line with pipes or && where you reasonably can, but if a step depends on seeing the result of a previous command first, propose only that next step.
- Prefer standard, widely available utilities.
- To find out where you are, run pwd. Never run cd to check: a directory change does not carry over from one command to the next.
- Commands run in the working directory given below. Words like "here", "this folder", "this directory", or "the current folder" refer to THAT directory: write a relative path or ".", never an absolute path to somewhere else.
- NEVER output a placeholder path. /path/to/folder, /path/to/file, /your/directory and similar are not real paths and the command will fail. If the task does not name a path, it means the working directory — use a relative path.
- Do not substitute a well-known system directory for one the task did not mention. "the log files here" means log files in the working directory, not /var/log.
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

// defaultNumCtx is the context window requested from Ollama, in tokens.
//
// This must be set explicitly and cannot be left to the server's default.
// Measured against a live server (Session 29): Ollama defaults num_ctx to
// 2048 regardless of what the model supports, and it enforces that limit by
// *silently truncating* the prompt — a ~6000-token prompt came back with
// prompt_eval_count 2050 and a perfectly normal-looking response, having
// never seen most of its own input. There is no error and no warning to
// detect. qwen2.5-coder:3b itself reports context_length 32768, so 2048 was
// never a model limit, only an unstated server default.
//
// 8192 is chosen as a deliberate middle: 4x the silent default, enough for
// the multi-step loop plus M6's cross-task history, and far short of the
// model's 32768 ceiling because the KV cache grows with this value and the
// deployment target is CPU-only with modest RAM (stack.md: 4 GB minimum).
// Override with SYNAPSE_NUM_CTX when a larger window is worth the memory.
const defaultNumCtx = 8192

// generationOptions builds the per-request options map. Every call into the
// model goes through here so num_ctx can never be accidentally omitted from
// one path and set on another — the failure mode that would reintroduce
// silent truncation on whichever path was missed.
func generationOptions() map[string]any {
	return map[string]any{
		"temperature": 0,
		"num_ctx":     envIntOr("SYNAPSE_NUM_CTX", defaultNumCtx),
	}
}

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

// envIntOr reads a positive integer from the environment, falling back to
// the default on absence, unparseable input, or a non-positive value. A bad
// value degrades to the working default rather than failing the run: a typo
// in an env var should not stop a user from getting work done.
func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "warning: ignoring invalid %s=%q, using %d\n", key, v, fallback)
		return fallback
	}
	return n
}

const doneSentinel = "DONE"

// stepOutputChars caps how much of a single step's stdout/stderr gets fed
// back into the next step's prompt. A command like a recursive find can
// produce output far larger than useful context; this is a blunt truncation,
// not the compression M6's session context will eventually do for the TUI.
const stepOutputChars = 500

// answerOutputChars is the per-step output budget when summarising for the
// user. It is larger than stepOutputChars because the two serve opposite
// ends: the step prompt only needs enough context to decide the next command,
// whereas the answer has to contain the values the user actually asked for,
// and truncating a listing before the line they wanted produces a confident
// wrong answer rather than a short one.
const answerOutputChars = 2000

// answerTimeout bounds the summarising call. It is short because the work is
// already done by the time it runs: a slow answer must degrade to no answer,
// never to a slow task.
const answerTimeout = 30 * time.Second

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
	// was a real, verified defect dating to M1/F2, found by review, not a
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
			tel, closeTel := studyLogger(os.Stderr)
			code := runUndo(func(p string) bool { return confirm(reader, os.Stdout, p) }, os.Stdout, os.Stderr, journalPath, tel, os.Getenv("SYNAPSE_TASK_ID"))
			closeTel()
			os.Exit(code)
		case "tui":
			// M5 step 3: the real execution loop is wired in, but TUI mode
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
			sc := session.New()
			tracker := newTaskTracker()
			tel, closeTel := studyLogger(os.Stderr)
			defer closeTel()
			runner := func(taskCtx context.Context, task string, confirmFn func(string) bool, out, errOut io.Writer) int {
				// Session commands are answered locally, never generated —
				// see handleSessionCommand.
				if handleSessionCommand(task, sc, tracker, out) {
					return 0
				}
				// withTokenStreaming is the one behavioral difference from
				// CLI/REPL mode, and it is presentation-only: tokens render
				// as they arrive instead of after generation finishes. The
				// command still gets parsed from the fully assembled
				// response and classified exactly as before.
				return runLoop(taskCtx, client, model, task, confirmFn, out, errOut, journalPath,
					withTokenStreaming(out), withSessionContext(sc), withTelemetry(tel, tracker.current()))
			}
			// Load the model while the user is still reading the header, so the first
			// answer does not pay a cold start (measured at 30-40s on the reference
			// machine). A failure is not fatal: the first real task will report it.
			warm := func(ctx context.Context) error { return client.Preload(ctx, model, generationOptions()) }
			if err := tui.RunWithWarmup(runner, warm); err != nil {
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
	fmt.Fprintf(os.Stdout, "each step may run for up to %s before it's automatically stopped.\n\n", stepExecutionTimeout)
	// Also answered on the one-shot path, not only in the session loops:
	// `synapse hello` is as likely a first contact as typing it at the prompt.
	if answerConversational(task, os.Stdout) {
		os.Exit(0)
	}
	tel, closeTel := studyLogger(os.Stderr)
	code := runLoop(ctx, client, model, task, confirmFn, os.Stdout, os.Stderr, journalPath,
		withTelemetry(tel, newTaskTracker().current()))
	closeTel()
	os.Exit(code)
}

// runREPL is M4's persistent CLI loop: instead of one process per task
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
	fmt.Fprintf(out, "each step may run for up to %s before it's automatically stopped.\n", stepExecutionTimeout)
	fmt.Fprintln(out, "follow-ups can refer back (\"move it to Downloads\"); type context to see what's remembered, clear to forget it.")
	if tel := os.Getenv("SYNAPSE_SESSION_LOG"); tel != "" {
		fmt.Fprintln(out, "study telemetry is recording; type task <id> to mark which task the following events belong to.")
	}

	reader := bufio.NewReader(in)
	confirmFn := func(prompt string) bool { return confirm(reader, out, prompt) }
	sc := session.New()
	tracker := newTaskTracker()
	tel, closeTel := studyLogger(errOut)
	defer closeTel()

	for {
		fmt.Fprint(out, "> ")
		line, readErr := reader.ReadString('\n')

		if task := strings.TrimSpace(line); task != "" {
			if strings.EqualFold(task, "exit") || strings.EqualFold(task, "quit") {
				return 0
			}
			// Memory commands are handled here rather than sent to the
			// model: they are about the session itself, and a user asking
			// what is remembered needs a truthful answer, not a generated
			// one.
			if handled := handleSessionCommand(task, sc, tracker, out); handled {
				if readErr != nil {
					return 0
				}
				continue
			}
			runTaskInterruptibly(ctx, client, model, task, confirmFn, out, errOut, journalPath,
				withSessionContext(sc), withTelemetry(tel, tracker.current()))
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
// Known limitation, accepted rather than hidden: if the task is blocked on
// a [y/N] confirmation when Ctrl+C arrives, the context is cancelled but
// the pending os.Stdin read is not interrupted, so nothing visibly happens
// until the next Enter — which the gate then treats as "no" and fails
// closed. The outcome is correct and safe, just not instant. Fixing that
// properly needs the stdin read moved off the main goroutine, which would
// reintroduce exactly the two-readers-over-one-stream hazard M4 was built
// to eliminate; not worth trading a real correctness guarantee for a
// cosmetic improvement.
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

func runTaskInterruptibly(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string, opts ...loopOption) {
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
			fmt.Fprintln(syncOut, "\ncancelling this task — the session stays open.")
			cancel()
		case <-watchDone:
		}
	}()

	runLoop(taskCtx, client, model, task, confirmFn, syncOut, syncErr, journalPath, opts...)
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
}

// withTokenStreaming makes the loop stream generation into w as it
// arrives. Used only by TUI mode; see docs/interface-modes.md for why
// CLI mode deliberately renders once generation finishes instead.
func withTokenStreaming(w io.Writer) loopOption {
	return func(c *loopConfig) { c.tokenSink = w }
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

func runLoop(ctx context.Context, client *ollama.Client, model, task string, confirmFn func(string) bool, out, errOut io.Writer, journalPath string, opts ...loopOption) int {
	var cfg loopConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	var history []loopStep

	// Mirrors the session defer below: the outcome is recorded on whichever
	// path the loop actually leaves by, because a task that failed halfway
	// is a data point the study needs, not an absence. Each return below
	// sets taskOutcome immediately before returning.
	taskOutcome := "incomplete"
	cfg.telemetry.TaskStart(cfg.taskID, task)
	defer func() { cfg.telemetry.TaskEnd(cfg.taskID, taskOutcome, len(history)) }()

	// Recorded on every exit path — DONE, UNSUPPORTED, a declined
	// confirmation, the step cap, a propose error, or a cancelled context.
	// A deferred call rather than one at the end of the happy path,
	// because a task that failed halfway still did whatever it did, and a
	// follow-up ("undo that", "try again with sudo") needs to see it. A
	// partial turn is memory; a missing one is a lie about what happened.
	if cfg.session != nil {
		defer func() {
			steps := make([]session.Step, 0, len(history))
			for _, h := range history {
				steps = append(steps, session.Step{
					Command: h.command,
					Result:  stepResultNote(h.result),
				})
			}
			cfg.session.Append(task, steps)
			if n := cfg.session.TakeDropped(); n > 0 {
				fmt.Fprintf(out, "note: dropped %d older turn(s) from memory to stay within the context budget.\n", n)
			}
		}()
	}

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
			fmt.Fprintf(errOut, "error: %v\n", err)
			taskOutcome = "propose_error"
			return 1
		}
		// Guidance in the prompt reduces repetition but does not eliminate it at
		// this model size. Stopping is better than spending the remaining steps
		// re-running a known failure: it fails faster, it does not pad the
		// study's step counts with identical attempts, and it gives the user a
		// truthful reason instead of five identical error blocks.
		for _, prior := range failedCommands(history) {
			if prior == cmd {
				fmt.Fprintf(out, "this command already failed here, so running it again will not help: %s\n", cmd)
				taskOutcome = "repeated_failure"
				if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
					fmt.Fprintln(out, answer)
					cfg.telemetry.TaskAnswered(cfg.taskID, answer)
				}
				return 1
			}
		}
		cfg.telemetry.CommandIssued(cfg.taskID, i, cmd)

		if cfg.tokenSink != nil {
			fmt.Fprintln(out)
			// The streamed text is the model's raw output; what actually
			// runs has been through cleanCommand. Show the canonical form
			// only when they differ — fenced or backticked output — so the
			// user always sees the command that is about to execute without
			// a redundant echo on the common path.
			if strings.TrimSpace(resp.Response) != cmd {
				fmt.Fprintf(out, "  command: %s\n", cmd)
			}
			fmt.Fprintf(out, "  stats: %d tokens in %s\n", resp.EvalCount, resp.Latency().Round(time.Millisecond))
		} else {
			fmt.Fprintf(out, "step %d: %s\n  stats: %d tokens in %s\n",
				i, cmd, resp.EvalCount, resp.Latency().Round(time.Millisecond))
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
			fmt.Fprintln(out, "I couldn't work out a command for that.")
			fmt.Fprintln(out, "I work by running shell commands on this machine, so I can reach files and folders, disk usage, processes, packages, text in files, and network settings — but not things with no command-line equivalent, like clicking buttons in a graphical application or editing an image.")
			fmt.Fprintln(out, "I also don't answer general questions about the world — I only report what I can find on this machine, so that anything I tell you can be traced to a command that actually ran.")
			fmt.Fprintln(out, "if it is something the command line can do, try naming the file or folder — for example \"how much space is this folder using\" or \"find the ten largest files here\".")
			taskOutcome = "unsupported"
			return 1
		}
		if strings.EqualFold(cmd, doneSentinel) {
			if len(history) == 0 {
				fmt.Fprintln(out, "model reported nothing needs to be done.")
				taskOutcome = "nothing_to_do"
				return 0
			}
			// The answer comes before the mechanical completion line: it is
			// what the user asked for, and the step count is bookkeeping.
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				fmt.Fprintln(out, answer)
				cfg.telemetry.TaskAnswered(cfg.taskID, answer)
			} else if aerr != nil {
				// Reported, not fatal. The task succeeded; only the summary
				// did not, and the raw output above already stands on its own.
				fmt.Fprintf(errOut, "note: could not summarise the result: %v\n", aerr)
			}
			fmt.Fprintf(out, "task complete in %d step(s).\n", len(history))
			taskOutcome = "complete"
			return 0
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
			fmt.Fprintf(out, "not run: %s\n%s\n", cmd, note)
			cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{ExitCode: 1, RawExitCode: 1})
			history = append(history, loopStep{command: cmd, result: executor.Result{Stderr: note, ExitCode: 1, RawExitCode: 1}})
			continue
		}

		verdict, reason := classifier.ClassifyForDir(cmd, wd)
		gd := analysisDecision(ctx, cmd, wd)
		if verdict == classifier.Irreversible || (gd != nil && gd.Confirm) {
			if verdict == classifier.Irreversible {
				fmt.Fprintf(out, "blocked: %s is irreversible — %s\n", cmd, reason)
			} else {
				reason = firstReason(gd)
				fmt.Fprintf(out, "blocked: %s needs confirmation — %s\n", cmd, reason)
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
			if unprotected {
				fmt.Fprintln(out, "  WARNING: this command decides what to delete while it runs, so I cannot")
				fmt.Fprintln(out, "  copy anything first. undo will NOT be able to bring it back.")
				fmt.Fprintln(out, "  if you want the safety net, name the files or folders directly instead.")
			}
			approved := confirmFn("run it anyway?")
			// Logged for both answers. A declined gate is evidence about
			// whether the warning is understood, which is exactly what RQ2
			// asks; recording only approvals would leave that unmeasurable.
			cfg.telemetry.ConfirmationTriggered(cfg.taskID, i, cmd, reason, approved)
			if !approved {
				fmt.Fprintln(out, "cancelled.")
				taskOutcome = "declined"
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
			for _, err := range errs {
				fmt.Fprintf(errOut, "warning: could not capture before running: %v\n", err)
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
		result := executor.Run(execCtx, cmd)
		execLatency := time.Since(execStart)
		execCancel()
		if result.Err != nil {
			fmt.Fprintf(errOut, "error: command did not run: %v\n", result.Err)
			taskOutcome = "execution_error"
			if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
				fmt.Fprintln(out, answer)
				cfg.telemetry.TaskAnswered(cfg.taskID, answer)
			}
			return 1
		}
		cfg.telemetry.CommandResult(cfg.taskID, i, cmd, telemetry.CommandOutcome{
			ExitCode:    result.ExitCode,
			RawExitCode: result.RawExitCode,
			SIGPIPE:     result.SIGPIPE,
			Latency:     execLatency,
		})
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

		if gateEntry != nil {
			// Journaled whatever the exit code: the captures were taken before the
			// command ran, so they can restore even a command that failed partway.
			if !gateEntry.IsNoop() {
				if err := undo.AppendJournal(journalPath, *gateEntry); err != nil {
					fmt.Fprintf(errOut, "warning: could not record undo entry: %v\n", err)
				}
			}
		} else if wd != "" && undoBefore != nil && result.ExitCode == 0 {
			if after, err := undo.Snapshot(wd); err == nil {
				afterIDs, _ := undo.SnapshotIDs(wd)
				if entry := undo.BuildEntryIDs(wd, cmd, undoBefore, after, undoBeforeIDs, afterIDs); !entry.IsNoop() {
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

	taskOutcome = "step_limit"
	// Answering matters more here than on the happy path. A task that ran out
	// of steps leaves the user with a wall of failed commands and no statement
	// of what went wrong; the raw output is evidence, not an explanation.
	if answer, aerr := maybeAnswer(ctx, client, model, task, history, cfg); aerr == nil && answer != "" {
		fmt.Fprintln(out, answer)
		cfg.telemetry.TaskAnswered(cfg.taskID, answer)
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
func proposeStep(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, tokenSink io.Writer, sc *session.Context) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var prior string
	if sc != nil {
		prior = sc.Render()
	}
	// Best effort: if the working directory cannot be read, the prompt simply
	// omits it rather than failing the step. A missing line degrades to the
	// previous ungrounded behaviour; a hard error would take out a task that
	// is otherwise fine.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	prompt := buildStepPrompt(task, history, prior, cwd)
	opts := generationOptions()

	// Non-streaming is the default and stays the CLI/REPL behavior
	// (docs/interface-modes.md: CLI renders "once generation finishes").
	// Only a caller that supplies a sink — TUI mode — pays for streaming.
	if tokenSink == nil {
		resp, err := client.Generate(reqCtx, model, loopSystemPrompt, prompt, opts)
		if err != nil {
			return nil, "", err
		}
		calibrate(sc, prompt, resp)
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
	calibrate(sc, prompt, resp)
	// The command is still parsed from the fully assembled response, never
	// from the streamed fragments — streaming changes only when text
	// appears on screen, never what gets classified or executed. Combined
	// with GenerateStream refusing to return a truncated stream at all,
	// a partial generation can never reach the classifier.
	return resp, cleanCommand(resp.Response), nil
}

// handleMemoryCommand intercepts the two session-memory commands and
// reports whether it consumed the input.
//
// These are handled locally rather than passed to the model on purpose. A
// user asking what is remembered needs the actual contents, not a
// plausible-sounding generated answer — and "clear" must be reliable,
// since its whole job is being the escape hatch when memory has gone
// wrong. Both are also the deterministic reset points a manual test needs.
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
			fmt.Fprintf(out, "now recording events under task %s.\n", id)
			return true
		}
	}
	if strings.EqualFold(trimmed, "task") && tr != nil {
		fmt.Fprintf(out, "current task: %s\n", tr.current())
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
	norm := strings.Trim(strings.ToLower(strings.TrimSpace(task)), ".!?,")
	switch norm {
	case "hello", "hi", "hey", "yo", "hello there", "good morning", "good afternoon", "good evening":
		fmt.Fprintln(out, "hello. tell me what you want done to this machine, in ordinary words — for example \"how much space is this folder using\" or \"put the log files in their own folder\".")
		fmt.Fprintln(out, "type help to see what I can reach, or exit to leave.")
		return true
	case "what ai model are you", "what model are you", "which model are you", "what llm are you",
		"are you human", "are you a human", "are you an ai", "are you a robot", "are you chatgpt",
		"are you real", "are you a person":
		// Identity questions are about the system, so the system answers
		// them — and answers them locally, because the one thing worse than
		// refusing is letting a 3B coder model improvise its own identity.
		fmt.Fprintf(out, "I'm SynapseOS — a program on this machine, not a person. I use a local language model (%s) running on your own hardware through Ollama; nothing you type leaves this computer.\n", envOr("SYNAPSE_MODEL", defaultModel))
		fmt.Fprintln(out, "what I actually do is turn what you say into shell commands and run them here. type help for what I can reach.")
		return true
	case "help", "what can you do", "what can you do?", "who are you", "what are you", "what is this":
		fmt.Fprintln(out, "I turn what you say into shell commands and run them on this machine, showing you each command before it runs.")
		fmt.Fprintln(out, "I can reach anything the command line can: files and folders, disk usage, processes, packages, text in files, and network settings.")
		fmt.Fprintln(out, "I cannot click buttons in graphical applications, edit images, or browse web pages.")
		fmt.Fprintln(out, "anything that cannot be undone stops and asks you first, and undo reverses the last thing I ran.")
		fmt.Fprintln(out, "session commands: context (what I remember), clear (forget it), exit.")
		return true
	case "thanks", "thank you", "ty":
		fmt.Fprintln(out, "you're welcome.")
		return true
	}
	return false
}

func handleMemoryCommand(task string, sc *session.Context, out io.Writer) bool {
	switch strings.ToLower(strings.TrimSpace(task)) {
	case "context":
		fmt.Fprintln(out, sc.Summary())
		return true
	case "clear":
		n := sc.Len()
		sc.Clear()
		fmt.Fprintf(out, "forgot %d remembered task(s); the next task starts fresh.\n", n)
		return true
	}
	return false
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

// calibrate feeds the real token cost of a prompt back into the session's
// estimator. Ollama reports prompt_eval_count for every generation, so the
// budget can be corrected against what the tokenizer actually charged
// rather than left on a characters-per-token guess — accuracy for free,
// and no tokenizer dependency that would have to track the model.
func calibrate(sc *session.Context, prompt string, resp *ollama.GenerateResponse) {
	if sc == nil || resp == nil {
		return
	}
	sc.Calibrate(len(loopSystemPrompt)+len(prompt), resp.PromptEvalCount)
}

// buildStepPrompt renders the task plus a truncated history of already-run
// commands and their results, in the shape loopSystemPrompt expects.
// answerSystemPrompt drives the reply the user actually reads (D31). It is
// deliberately a separate prompt from loopSystemPrompt: that one is a
// translator constrained to emit a bare command, and asking the same
// persona to also write prose is how a command translator starts returning
// explanations where a command was expected.
const answerSystemPrompt = `You are reporting the outcome of a task to the person who asked for it.

You are given their request, the commands that were run on their behalf, and the output those commands produced. Reply with one complete sentence that answers their request. Two sentences if the result genuinely needs it.

Rules:
- Always a complete sentence. Never reply with only a filename, a number, a path, or a copy of the output.
- Restate enough of the request that the answer stands alone when read by itself.
- Use the actual values from the output.
- If the commands produced no output and succeeded, say what was done.
- If the task did not succeed, say plainly what went wrong.
- Do not describe which commands ran, and do not mention the shell.
- No markdown, no code blocks, no preamble, no sign-off.

Examples:

Request: how many files are in logs
Output: 4
Reply: There are 4 files in the logs folder.

Request: rename notes.txt to renamed.txt
Output: (none)
Reply: notes.txt has been renamed to renamed.txt.

Request: what files are in this folder
Output: renamed.txt
Reply: This folder contains one file, renamed.txt.

Request: list the contents of a folder called archives
Output: ls: cannot access 'archives': No such file or directory
Reply: There is no folder called archives here.`

// answerFromHistory turns what actually happened into a sentence the user can
// read, which is the difference between a system that executes and one that
// answers (D31). The raw command, its output, and its exit status are still
// printed above this — the answer is the surface, the evidence sits beneath it
// (see vision.md, Principles).
//
// Best-effort by construction: a task that ran correctly must not be reported
// as failed because the summarising call did not come back. On any error the
// caller falls through to the mechanical completion line it printed before.
func answerFromHistory(ctx context.Context, client *ollama.Client, model, task string, history []loopStep) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, answerTimeout)
	defer cancel()

	var b strings.Builder
	b.WriteString("Their request: ")
	b.WriteString(task)
	b.WriteString("\n\nWhat was run:\n")
	for i, st := range history {
		fmt.Fprintf(&b, "%d. %s\n   exit code: %d\n", i+1, st.command, st.result.ExitCode)
		if st.result.TimedOut {
			fmt.Fprintf(&b, "   note: this was stopped after %s without finishing.\n", stepExecutionTimeout)
		}
		if out := strings.TrimSpace(st.result.Stdout); out != "" {
			fmt.Fprintf(&b, "   output: %s\n", truncate(out, answerOutputChars))
		}
		if errOut := strings.TrimSpace(st.result.Stderr); errOut != "" {
			fmt.Fprintf(&b, "   errors: %s\n", truncate(errOut, answerOutputChars))
		}
	}

	resp, err := client.Generate(reqCtx, model, answerSystemPrompt, b.String(), generationOptions())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Response), nil
}

// maybeAnswer applies the opt-out and otherwise produces the reply.
func maybeAnswer(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, cfg loopConfig) (string, error) {
	if cfg.answerDisabled {
		return "", nil
	}
	return answerFromHistory(ctx, client, model, task, history)
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

func buildStepPrompt(task string, history []loopStep, priorSession, cwd string) string {
	// One rendering path, deliberately. There used to be an early return here
	// for the simplest case, and the divergence is what caused the 2026-09-14
	// technical-tier regression described at the trailer below: a first step
	// silently changed shape depending on whether a working directory was
	// known. A prompt that renders differently on a path nobody tests is a
	// prompt that will regress again.
	var b strings.Builder
	// The working directory is stated before anything else because it is what
	// "here" and "this folder" resolve to, and without it they resolve to
	// nothing: the model fills the hole with a literal placeholder
	// (`du -sh /path/to/folder`) or with a familiar system path (/var/log for
	// "the log files here"). Measured 2026-09-14 as the largest single cause
	// of Layer 7's plain-tier failures — roughly half of them — because plain
	// speakers say "here" where technical ones write ".".
	if cwd != "" {
		b.WriteString("Working directory: ")
		b.WriteString(cwd)
		b.WriteString("\n\n")
	}
	// Earlier tasks come first so the current task reads as the most
	// recent thing said — which is what lets a pronoun in it bind to the
	// turn immediately above rather than to something further back.
	if priorSession != "" {
		b.WriteString(priorSession)
		b.WriteString("\n")
	}
	b.WriteString("Task: ")
	b.WriteString(task)
	if len(history) > 0 {
		b.WriteString("\n\nSteps already run:\n")
	}
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
	// A small model reads the transcript above as context but not as an
	// instruction. Without this it re-proposes the command that just failed,
	// verbatim, until the step cap — observed live 2026-09-12.
	if failed := failedCommands(history); len(failed) > 0 {
		b.WriteString("\n\nThese commands already failed. Do not repeat any of them; the same command will fail the same way again:\n")
		for _, c := range failed {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("Read the error above and try a genuinely different approach: a different tool, a different path, or checking an assumption first. If the task cannot be done at all, output UNSUPPORTED.\n")
	}
	// DONE is offered only once something has actually run that could have
	// accomplished the task. Offering it on a step-less task invites the model
	// to read a request that already looks like a finished command — the
	// technical tier's "du -sh ." — as work already done, and answer DONE
	// before running anything.
	//
	// This is a measured regression, not a hypothetical: before 2026-09-14 a
	// first step with no history and no prior session took the early return
	// above and never saw this line. Adding the working directory routed every
	// first step through here, and Layer 7's technical tier fell 100% -> 58.3%
	// with every failure reporting "model reported nothing needs to be done".
	// It also resolves a standing contradiction — loopSystemPrompt already
	// says a task with no steps run always gets a command, never DONE, while
	// this trailer said otherwise whenever a prior session was present.
	// The two branches differ only in whether DONE is on offer. The question
	// itself is deliberately identical and deliberately not "what is the
	// FIRST command": that wording, tried on 2026-09-14, invites the model to
	// decompose a compound request and stop after part of it — "mkdir -p logs
	// && mv *.log logs/" ran the mkdir, reported "The logs folder has been
	// created", and exited cleanly with the files unmoved.
	b.WriteString("\nWhat command should run next?")
	if len(history) > 0 {
		b.WriteString(" Output DONE if the task is already fully accomplished.")
	}
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
	return l, func() { f.Close() }
}

// runUndo takes an optional telemetry logger because M7 folded the former
// M8's undo telemetry into this path. A nil logger is a no-op, so ordinary
// non-study use passes nil and records nothing.
func runUndo(confirmFn func(string) bool, out, errOut io.Writer, journalPath string, tel *telemetry.Logger, taskID string) int {
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
		// A declined undo is a finding, not a non-event: it says the
		// participant reached for recovery and then chose not to take it.
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoDeclined, "")
		return 0
	}

	if _, _, err := undo.PopLastJournal(journalPath); err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoFailed, err.Error())
		return 1
	}
	if errs := undo.Apply(entry); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(errOut, "error: %v\n", e)
		}
		// "The participant tried to recover and could not" is precisely the
		// observation the study needs; logging only successes would bias the
		// record toward the system working.
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoFailed, errs[0].Error())
		return 1
	}
	fmt.Fprintln(out, "undo complete.")
	tel.UndoInvoked(taskID, entry.Command, telemetry.UndoApplied, "")
	return 0
}

// propose sends task to the model with deterministic decoding (temperature
// 0, so the smoke test is reproducible and matches how the offline accuracy
// evaluator will score the model) and returns the cleaned command it proposed.
func propose(ctx context.Context, client *ollama.Client, model, task string) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := client.Generate(reqCtx, model, systemPrompt, task, generationOptions())
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
// about to consume, which is exactly the state-leak risk M4 exists to
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
