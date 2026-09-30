// Package tui implements TUI mode (M5, D11): the bubbletea interface
// wrapping the same shared core (internal/ollama, internal/classifier,
// internal/executor) every mode reuses.
//
// The central design constraint, and the reason this package is as small
// as it is: TUI mode does not reimplement the propose → classify →
// confirm → execute loop. It drives the *identical* loop CLI mode and the
// persistent REPL already use, injected as a TaskRunner. Every
// reversibility verdict, every confirmation gate, every undo-journal
// write therefore runs the same code in every mode, and cannot drift
// between them — which is what `docs/interface-modes.md` means by "the
// moment mode-specific code starts reimplementing classification or
// execution, that's a sign the logic belongs back in the shared core."
// What lives here is strictly input collection, rendering, and session
// lifecycle.
//
// The UI is inline, not full-screen. Finished output lines are printed into
// the terminal's normal scrollback (tea.Println) and only the live region —
// the line being written, the status line, and the prompt — is redrawn. That
// is what lets the terminal's own scrollbar, wheel, and selection work across
// the whole conversation. An alternate-screen viewport has no scrollback, so
// only what fits on screen could be highlighted or copied.
//
// Bridging a synchronous loop into an event-driven runtime is the other
// technical problem this file solves. bubbletea's Update must never
// block, but the loop is blocking and needs to ask a question mid-flight.
// The bridge is two channels: the loop runs on its own goroutine, writes
// output as messages into `events`, and when it hits an irreversible step
// it publishes a confirmation request and blocks reading `answers` until
// Update — having rendered the prompt and taken a keypress — sends the
// verdict back.
//
// Printed text must reach the scrollback in the order it was produced. Every
// line therefore travels through the one ordered `events` channel (the task's
// output, and also the echoed task, the y/n verdict, and notes), and each
// print is sequenced before the next event is read.
package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"synapseos/internal/loopevent"
)

// Styles are defined once at package level. lipgloss v2 Styles are plain
// values with no renderer attached — color downsampling for the terminal
// is handled by Bubble Tea itself — so these are safe as package globals.
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	hintStyle   = lipgloss.NewStyle().Faint(true)
	// Irreversible commands are the one thing in this UI that must never
	// be mistaken for ordinary output, so the confirmation prompt is the
	// only element that gets a border and a warning color.
	confirmStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11")).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(0, 1)
	// The two lines a reader scans for: what was asked, and the command that
	// answered it. Everything else stays in the terminal's own colors.
	echoStyle    = lipgloss.NewStyle().Bold(true)
	commandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	workingStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	// warnGlyph marks the confirmation prompt as the one thing here that
	// must never be mistaken for ordinary output or skimmed past.
	warnGlyph = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render("⚠")
)

// TaskRunner runs one task to completion: proposing, classifying,
// gating, and executing each step, calling confirm whenever an
// irreversible step needs explicit approval, and writing human-readable
// progress to out/errOut. It returns the same exit code cmd/synapse's
// runLoop returns.
//
// This is deliberately an injected function rather than an import: it is
// cmd/synapse's runLoop with the client/model/journal bound in by the
// caller. Injecting it keeps the safety-critical orchestration in exactly
// one place, lets this package stay free of any Ollama or filesystem
// dependency, and lets tests drive the full UI against a scripted runner
// with no model and no real commands.
type TaskRunner func(ctx context.Context, task string, confirm func(string) bool, out, errOut io.Writer) int

// defaultWidth is used for wrapping the live line until the terminal reports its size.
const defaultWidth = 80

type (
	// outputMsg is a chunk of text to append to the conversation: written by
	// the running task, or by the UI itself (echoed task, verdict, notes).
	outputMsg string
	// confirmRequestMsg is the running task asking for a y/n decision.
	// The task goroutine is blocked until an answer is sent back.
	confirmRequestMsg string
	// taskDoneMsg reports that the task goroutine has returned.
	taskDoneMsg struct{ code int }
	// eventMsg is something the loop reports as data rather than text: a command,
	// its result, the answer, a notice. See internal/loopevent.
	eventMsg loopevent.Event
	// toggleDetailsMsg flips the details view. It travels through the events
	// channel so it takes effect in order with the events around it.
	toggleDetailsMsg struct{}
	// warmDoneMsg reports that the startup warm-up has finished, successfully or not.
	warmDoneMsg struct{}
)

// Model is TUI mode's bubbletea state.
type Model struct {
	input   textinput.Model
	spinner spinner.Model
	run     TaskRunner

	// warm loads the model in the background at startup; warming is true from
	// Init until it reports back. Nil means there is nothing to warm, and the
	// status line never appears.
	warm    func(context.Context) error
	warming bool

	// header is printed once, into the scrollback, when the program starts.
	header string

	// print turns text into a command that writes it into the terminal's
	// scrollback above the live region. It is a field so tests can capture
	// what would be printed; the real one is tea.Println.
	print func(string) tea.Cmd

	// partial is the line currently being written: the most recent chunk did
	// not end in a newline, so the next one continues it. Streaming makes this
	// the common case. It is drawn in the live region and only printed once
	// the line is complete.
	partial string

	width    int
	quitting bool

	// verbose is the details view: every command and its raw output are printed
	// as they happen. Off, a task prints its answer and a one-line note of what
	// ran, and the rest waits for Ctrl+O.
	verbose bool
	// cur is the task in progress; last is the most recent task that ran a
	// command, kept so Ctrl+O can show what was behind an answer already printed.
	cur, last taskRecord
	// status is the command running right now, for the live line.
	status string
	// blockOpen is true once something has been printed for the current task,
	// so the next block is set apart by a blank line.
	blockOpen bool
	// cancelled is set the moment Ctrl+C interrupts a running task, and read
	// once at taskDoneMsg to decide what to tell the user. It cannot be
	// decided at the keypress itself: the step already in flight may finish
	// and even mutate the filesystem before the context cancellation is
	// noticed, so saying "cancelling" there and nothing more would leave a
	// completed step looking like it never happened.
	cancelled bool

	// neat, when on, keeps only the latest exchange on screen: emit draws into
	// neatLines instead of committing to the terminal's scrollback, and a new
	// task discards the previous turn's lines instead of appending to them.
	// Off by default — D38 moved finished output into scrollback specifically
	// so the whole conversation could be scrolled and copied natively, and
	// neat mode deliberately gives that up for a clean, unchanging window
	// instead. Purely a rendering choice: session memory (what the model
	// remembers) is unaffected either way, and Ctrl+O still works, showing
	// the current turn's detail in place rather than printing it below.
	neat bool
	// neatLines holds the current turn's rendered blocks while neat is on, in
	// the order emit received them.
	neatLines []string

	// events carries messages from the running task's goroutine into the
	// bubbletea event loop. Buffered: the task writes output faster than
	// Update consumes it, and a full buffer should apply backpressure to
	// the task rather than risk dropping output.
	events chan tea.Msg
	// answers carries a confirmation verdict back to the blocked task.
	// Buffered by one so Update never blocks the UI thread delivering it.
	answers chan bool

	// pendingConfirm is the prompt text while a confirmation is awaiting
	// a keypress; empty when no confirmation is outstanding.
	pendingConfirm string
	running        bool
	cancelTask     context.CancelFunc
}

// stepRecord is everything known about one command the loop ran.
type stepRecord struct {
	step    int
	command string
	tokens  int
	latency time.Duration

	ran      bool
	stdout   string
	stderr   string
	exit     int
	timedOut bool
	notRun   bool

	// announced: the "$ command" line is already in the scrollback. printed: its
	// result is too.
	announced, printed bool
}

// taskRecord is what the loop reported for one task.
type taskRecord struct {
	steps    []stepRecord
	notes    []string
	answered bool
}

// NewModel builds the initial state. The input is focused here rather
// than in Init because Init can only return a Cmd, never mutate the Model
// the runtime holds — focusing there would apply to a discarded copy and
// silently leave the input ignoring every keystroke (a real bug caught by
// test earlier in this milestone, and what the upstream docs' example
// would have led to).
func NewModel(run TaskRunner) Model {
	return newModel(run, false)
}

// NewScratchModel is NewModel for scratch mode (D44, mirroring cmd/synapse's
// D43): the header says plainly that nothing carries between tasks instead
// of advertising follow-up resolution this mode does not have. Everything
// else — rendering, Ctrl+O, neat mode, the confirmation gate — is identical;
// only what the header tells the person to expect differs. Whether memory
// actually exists is decided entirely by the TaskRunner passed in (main.go
// passes a nil session in scratch mode, per D43) — this constructor changes
// no behavior of its own, only honesty about it.
func NewScratchModel(run TaskRunner) Model {
	return newModel(run, true)
}

func newModel(run TaskRunner, stateless bool) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "type a task..."
	ti.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(spinnerStyle))

	hints := []string{
		headerStyle.Render("SynapseOS — TUI mode"),
		hintStyle.Render("Type a task and press enter. Ctrl+C cancels a running task; at an idle prompt it quits."),
		hintStyle.Render("Ctrl+O shows or hides the commands behind each answer. Scroll and select text with the terminal as usual."),
	}
	if stateless {
		hints = append(hints, hintStyle.Render("Scratch mode: nothing carries over between tasks — each one starts fresh, with no memory of the one before it."))
	} else {
		hints = append(hints, hintStyle.Render("Follow-ups can refer back (\"move it to Downloads\"). Type context to see what's remembered, clear to forget it."))
	}
	hints = append(hints, hintStyle.Render("Type neat for a mode that shows only the latest exchange instead of the full scrollback."))

	return Model{
		input:   ti,
		spinner: sp,
		run:     run,
		width:   defaultWidth,
		events:  make(chan tea.Msg, 256),
		answers: make(chan bool, 1),
		print:   func(s string) tea.Cmd { return tea.Println(s) },
		header:  strings.Join(hints, "\n"),
	}
}

// Init prints the header, returns the cursor-blink command, and begins
// listening for task events. Calling Focus() again is idempotent and
// intentional: the focus flag was already set for real in NewModel, and this
// call exists only to obtain the blink Cmd.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.emit(m.header), m.input.Focus(), waitForEvent(m.events)}
	if m.warming {
		cmds = append(cmds, m.spinner.Tick, warmUp(m.warm))
	}
	return tea.Batch(cmds...)
}

// warmUp runs the startup warm-up off the UI thread and reports when it is done.
// Its error is deliberately dropped: a model that will not load shows up as a
// clear error on the first real task, which is a better place to say so than a
// startup banner the user may never read.
func warmUp(f func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		_ = f(context.Background())
		return warmDoneMsg{}
	}
}

// waitForEvent blocks on the task-event channel inside a Cmd, which
// bubbletea runs on its own goroutine — this is what lets a synchronous
// task publish into an event loop that must never block. Every branch of
// Update that consumes an event re-issues this, so the listener persists
// for the life of the session.
func waitForEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// emit prints text into the scrollback, or — in neat mode — adds it to the
// current turn's live content instead, returning no command since nothing
// needs to reach the terminal until the next redraw. An empty text becomes a
// placeholder space, which Println needs to still emit a blank line.
func (m *Model) emit(text string) tea.Cmd {
	if text == "" {
		text = " "
	}
	if m.neat {
		m.neatLines = append(m.neatLines, text)
		return nil
	}
	return m.print(text)
}

// renderNeat renders the current turn's accumulated blocks for the live
// region, wrapped the same way the partial line is: this content is now
// inside the redrawn area, not the terminal's own scrollback, so bubbletea
// has to know its true row count. Lines already carry lipgloss styling by the
// time they arrive here, and wrapping counts those escape codes as width —
// the visible line can end up wrapped a little earlier than strictly
// necessary, never garbled, which is an acceptable trade for not building a
// second, ANSI-aware wrapper for a secondary display mode.
func (m Model) renderNeat() string {
	if len(m.neatLines) == 0 {
		return ""
	}
	var out []string
	for _, block := range m.neatLines {
		for _, line := range strings.Split(block, "\n") {
			out = append(out, wrapLine(line, m.width)...)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// sequence runs the non-nil commands one after another. tea.Batch would run
// them concurrently, and printed lines would then race each other.
func sequence(cmds ...tea.Cmd) tea.Cmd {
	live := make([]tea.Cmd, 0, len(cmds))
	for _, c := range cmds {
		if c != nil {
			live = append(live, c)
		}
	}
	switch len(live) {
	case 0:
		return nil
	case 1:
		return live[0]
	}
	return tea.Sequence(live...)
}

// appendOutput adds a chunk of output, honouring the fact that a chunk is not
// necessarily a whole line, and returns the command that prints the lines the
// chunk completed.
//
// Streaming delivers whatever fragment the model produced — "UNS", then
// "UPPORTED" — with no newline between them, while the loop's own status
// writes ("step 1: ...\n") are newline-terminated. Treating every chunk as a
// complete line would render the former as two lines, which is what a real
// terminal showed before this was handled. The unfinished tail stays in
// m.partial, drawn live, and is printed once its newline arrives.
func (m *Model) appendOutput(chunk string) tea.Cmd {
	if chunk == "" {
		return nil
	}
	parts := strings.Split(m.partial+chunk, "\n")
	m.partial = parts[len(parts)-1]
	done := parts[:len(parts)-1]
	if len(done) == 0 {
		return nil
	}
	for i, line := range done {
		done[i] = styleLine(line)
	}
	return m.emit(strings.Join(done, "\n"))
}

// styleLine emphasises the echoed task ("> ") and the command shown for a step
// ("$ "). It is cosmetic: it goes by the line's first characters, styling
// changes no text, and copying from the terminal yields the plain words.
func styleLine(line string) string {
	switch {
	case strings.HasPrefix(line, "> "):
		return echoStyle.Render(line)
	case strings.HasPrefix(line, "$ "):
		return commandStyle.Render(line)
	}
	return line
}

// say queues UI-authored text (the echoed task, the verdict, a note) through
// the same ordered channel the task's own output uses, so it cannot overtake
// or be overtaken by it. Only if the channel is full does it fall back to
// appending directly, and the returned command then prints it.
func (m *Model) say(text string) tea.Cmd {
	select {
	case m.events <- outputMsg(text):
		return nil
	default:
		return m.appendOutput(text)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(max(1, msg.Width-2))
		return m, nil

	case spinner.TickMsg:
		// Ticks keep arriving for as long as the spinner keeps re-issuing
		// its own Tick command below; once a task ends, simply not
		// re-issuing it is what stops the animation, rather than tracking
		// a separate "should the spinner run" flag.
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// The chain runs while a task is running or the model is still loading.
		if !m.running && !m.warming {
			return m, nil
		}
		return m, cmd

	case warmDoneMsg:
		m.warming = false
		return m, nil

	case outputMsg:
		out := m.appendOutput(string(msg))
		return m, sequence(out, waitForEvent(m.events))

	case eventMsg:
		out := m.onEvent(loopevent.Event(msg))
		return m, sequence(out, waitForEvent(m.events))

	case toggleDetailsMsg:
		out := m.toggleDetails()
		return m, sequence(out, waitForEvent(m.events))

	case confirmRequestMsg:
		m.pendingConfirm = string(msg)
		return m, waitForEvent(m.events)

	case taskDoneMsg:
		m.running = false
		m.pendingConfirm = ""
		m.cancelTask = nil
		// Close any half-written line, then leave a blank separator, so a
		// task ending mid-fragment doesn't merge into the next one.
		closing := "\n"
		if m.partial != "" {
			closing = "\n\n"
		}
		var tail []tea.Cmd
		if m.cancelled {
			tail = append(tail, m.cancelNotice())
		} else if !m.verbose {
			if !m.cur.answered {
				tail = append(tail, m.showHidden())
			} else if n := len(m.cur.steps); n > 0 {
				tail = append(tail, m.emit(hintStyle.Render(fmt.Sprintf("Ran %d %s · Ctrl+O shows what it was", n, plural(n, "command", "commands")))))
			}
		}
		if len(m.cur.steps) > 0 {
			m.last = m.cur
		}
		m.cur, m.status, m.blockOpen, m.cancelled = taskRecord{}, "", false, false
		tail = append(tail, m.appendOutput(closing), waitForEvent(m.events))
		return m, sequence(tail...)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// wrapLine breaks one line to width columns on word boundaries, returning the
// pieces. Only the live, unfinished line goes through this: printed lines are
// wrapped by the terminal itself, which is what keeps them selectable as
// ordinary text. The live region is redrawn in place and must know its own
// height, so it cannot leave wrapping to the terminal.
//
// Width is measured in runes, not bytes, so multi-byte characters are not
// split. This is deliberately not full grapheme-aware wrapping: transcript
// content is command output and ASCII prose, and a dependency on a text
// segmentation library is not worth the one edge case it would buy.
func wrapLine(line string, width int) []string {
	if width < 2 || len([]rune(line)) <= width {
		return []string{line}
	}
	var out []string
	var cur []rune
	flush := func() {
		out = append(out, string(cur))
		cur = cur[:0]
	}
	for _, word := range strings.Fields(line) {
		w := []rune(word)
		switch {
		case len(cur) == 0 && len(w) > width:
			// A single token longer than the line: hard-split it rather
			// than overflow, since no word boundary exists to break on.
			for len(w) > width {
				out = append(out, string(w[:width]))
				w = w[width:]
			}
			cur = append(cur, w...)
		case len(cur) == 0:
			cur = append(cur, w...)
		case len(cur)+1+len(w) <= width:
			cur = append(cur, ' ')
			cur = append(cur, w...)
		default:
			flush()
			cur = append(cur, w...)
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		flush()
	}
	if line == "" {
		return []string{""}
	}
	return out
}

// handleKey routes a keypress by session state. Order matters: an
// outstanding confirmation takes precedence over ordinary typing, so a
// stray keystroke can never be silently swallowed into the input field
// while a destructive command waits on an answer.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		// Mirrors the REPL's behavior deliberately: mid-task, Ctrl+C
		// cancels just that task and leaves the session alive — the
		// whole point of a persistent session — while at an idle prompt
		// it quits, which is what a user expects there.
		if m.running && m.cancelTask != nil {
			m.cancelTask()
			m.cancelled = true
			// No scrollback line here on purpose: the in-flight step may
			// still finish, or already have, and a flat "cancelling" printed
			// now would misreport that. taskDoneMsg has the real picture.
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	}

	if key == "ctrl+o" {
		select {
		case m.events <- toggleDetailsMsg{}:
			return m, nil
		default:
			return m, m.toggleDetails()
		}
	}

	if m.pendingConfirm != "" {
		switch key {
		case "y", "Y":
			return m.answerConfirm(true)
		case "n", "N", "esc", "enter":
			// Anything that isn't an explicit yes is a no: the gate
			// fails closed here exactly as it does on the CLI, where
			// only "y"/"yes" proceeds.
			return m.answerConfirm(false)
		}
		// Ignore every other key while a confirmation is outstanding.
		return m, nil
	}

	if m.running {
		// Input is inert while a task runs; there is no queueing.
		return m, nil
	}

	if key == "enter" {
		task := strings.TrimSpace(m.input.Value())
		if task == "" {
			return m, nil
		}
		m.input.SetValue("")
		// Handled here rather than in the task runner because it is view
		// state, not something the execution loop knows or should know.
		// Mouse reporting used to be a toggle; it is never on now, so the
		// terminal's own selection and wheel always work.
		if strings.EqualFold(task, "mouse") {
			return m, m.say("mouse: nothing to toggle — the terminal's own scrolling and selection are always on.\n")
		}
		// Toggled here, not through say/emit: this is a note about the UI
		// itself, not part of the conversation neat mode is trimming, so it
		// always reaches the scrollback and is never lost when neat turns on.
		if strings.EqualFold(task, "neat") {
			m.neat = !m.neat
			m.neatLines = nil
			if m.neat {
				return m, m.print("Neat mode on — only the latest exchange stays on screen. Type neat again to turn it off.")
			}
			return m, m.print("Neat mode off — the terminal's own scrollback shows everything again.")
		}
		return m.startTask(task)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// answerConfirm delivers a verdict to the blocked task goroutine and
// clears the prompt.
func (m Model) answerConfirm(ok bool) (tea.Model, tea.Cmd) {
	m.pendingConfirm = ""
	verdict := "n"
	if ok {
		verdict = "y"
	}
	// Queued before the answer is sent: the task can only continue, and write
	// more output, after it receives the answer.
	cmd := m.say(verdict + "\n")
	m.answers <- ok // buffered by one; never blocks the UI thread
	return m, cmd
}

// startTask launches the injected runner on its own goroutine, wiring its
// output and its confirmation callback back through the event channels.
func (m Model) startTask(task string) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.running = true
	m.cancelTask = cancel
	m.cancelled = false // defensive: taskDoneMsg already clears this, but a new task must never start reading a stale flag
	var forceRepaint tea.Cmd
	if m.neat {
		// The previous turn stays visible until this moment on purpose — a
		// user reading the last answer should not see it vanish before they
		// have even asked the next question.
		m.neatLines = nil
		// tea.ClearScreen forces a full repaint of the live region rather
		// than bubbletea's normal line-diffing redraw. Necessary here and
		// nowhere else in this file: ultraviolet's inline (non-altscreen)
		// renderer only clears trailing rows left over from a previous,
		// taller frame on an actual terminal *resize* — a same-size window
		// whose content just got shorter does not trigger it. Every other
		// live-region update in this program (the partial line, the status
		// line) only ever grows by a line or two at a time and self-corrects
		// on the next keystroke, so the gap was never visible until neat
		// mode made the live region itself carry a whole turn that can
		// shrink by a lot in one step (found live 2026-09-30: a short
		// answer following a long confirmation left the confirmation's text
		// on screen, stitched together with the new turn's).
		forceRepaint = tea.ClearScreen
	}

	// The echo is queued before the goroutine exists, so it precedes every
	// line the task writes.
	echo := m.say("> " + task + "\n\n")
	m.blockOpen = false

	events, answers, run := m.events, m.answers, m.run
	w := msgWriter{events: events}

	confirm := func(prompt string) bool {
		events <- confirmRequestMsg(prompt)
		return <-answers
	}

	go func() {
		defer cancel()
		code := run(ctx, task, confirm, w, w)
		events <- taskDoneMsg{code: code}
	}()

	// Deliberately no waitForEvent here. Exactly one listener may be
	// outstanding at a time: Init starts it, and every branch that
	// *consumes* an event re-issues exactly one, holding the count at
	// one. startTask consumes nothing — it runs off a keypress — so
	// issuing a listener here would leave two goroutines receiving from
	// the same channel, with delivery order to Update undefined. That
	// produced visibly out-of-order output in live use (a "model
	// reported..." line printing before the "step 1:" line above it),
	// and it compounded: every task started added another receiver.
	//
	// The spinner's own tick chain is separate from that and starts here:
	// it is not an events-channel message, so it cannot race with it.
	return m, tea.Batch(echo, m.spinner.Tick, forceRepaint)
}

// View draws only the live region: the unfinished line, the status or
// confirmation, and the prompt. Everything finished is already in the
// terminal's scrollback.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var b strings.Builder

	if m.neat {
		b.WriteString(m.renderNeat())
	}

	if m.partial != "" {
		b.WriteString(strings.Join(wrapLine(m.partial, m.width), "\n") + "\n")
	}

	switch {
	case m.pendingConfirm != "":
		b.WriteString("\n" + confirmStyle.Render(warnGlyph+" "+m.pendingConfirm+"  [y/N]") + "\n")
	case m.running:
		working := " working — ctrl+c cancels this task"
		if m.cancelled {
			working = " stopping — finishing the step already in progress"
		}
		b.WriteString("\n" + m.spinner.View() + workingStyle.Render(working) + "\n")
		if m.status != "" {
			b.WriteString(hintStyle.Render("  $ "+truncateRunes(m.status, max(10, m.width-6))) + "\n")
		}
	default:
		if m.warming {
			// Above the prompt, not instead of it: the user can already type, and
			// a task started now simply waits for the model like any other.
			b.WriteString("\n" + m.spinner.View() + workingStyle.Render(" loading the model — the first answer waits until it is ready") + "\n")
		} else {
			b.WriteString("\n")
		}
		b.WriteString(m.input.View() + "\n")
	}

	return tea.NewView(b.String())
}

// msgWriter adapts the io.Writer the task loop writes progress to into
// bubbletea messages. Write copies its argument (via string conversion)
// because io.Writer explicitly permits callers to reuse the buffer.
type msgWriter struct{ events chan<- tea.Msg }

func (w msgWriter) Write(p []byte) (int, error) {
	w.events <- outputMsg(string(p))
	return len(p), nil
}

// Emit makes msgWriter a loopevent.Emitter: a loop handed this writer reports
// commands, results, and answers as events instead of writing them as text.
func (w msgWriter) Emit(e loopevent.Event) { w.events <- eventMsg(e) }

// Run starts TUI mode against the real terminal, driving the supplied
// runner. Returns whatever error bubbletea's own Run reports.
func Run(run TaskRunner) error {
	return RunWithWarmup(run, nil)
}

// RunWithWarmup is Run with a background warm-up: warm runs once at startup
// (typically loading the language model) while the session is already usable,
// and the UI says so until it returns. A nil warm behaves exactly like Run.
func RunWithWarmup(run TaskRunner, warm func(context.Context) error) error {
	return RunModelWithWarmup(NewModel(run), warm)
}

// RunModelWithWarmup is RunWithWarmup for a caller that built its own Model —
// NewScratchModel, for instance — rather than the default NewModel. Kept
// separate rather than adding a constructor parameter to RunWithWarmup: that
// would change a signature every existing caller and test already uses.
func RunModelWithWarmup(m Model, warm func(context.Context) error) error {
	if warm != nil {
		m.warm, m.warming = warm, true
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

// --- presenting the loop's events ----------------------------------------

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// closePartial prints a half-written line so a block starts on its own line.
func (m *Model) closePartial() tea.Cmd {
	if m.partial == "" {
		return nil
	}
	return m.appendOutput("\n")
}

// block prints text as its own paragraph, set apart from the previous one by a
// blank line once something has already been printed for this task.
func (m *Model) block(text string) tea.Cmd {
	pre := m.closePartial()
	if m.blockOpen {
		text = "\n" + text
	}
	m.blockOpen = true
	return sequence(pre, m.emit(styleLines(text)))
}

// raw prints text directly under whatever was printed last, with no gap.
func (m *Model) raw(text string) tea.Cmd {
	return sequence(m.closePartial(), m.emit(styleLines(text)))
}

func styleLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = styleLine(l)
	}
	return strings.Join(lines, "\n")
}

func indentLines(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

// renderCommand is the "$ command" header of a step, with what the model spent
// on it, which only the details view shows.
func renderCommand(r stepRecord) string {
	out := "$ " + r.command
	if r.tokens > 0 || r.latency > 0 {
		out += "\n" + hintStyle.Render(fmt.Sprintf("  the model took %s (%d tokens) to choose this", r.latency.Round(100*time.Millisecond), r.tokens))
	}
	return out
}

// renderResult is what a command produced, indented beneath its header.
func renderResult(r stepRecord) string {
	var parts []string
	if r.notRun {
		parts = append(parts, "  (not run)")
	}
	if r.stdout != "" {
		parts = append(parts, indentLines(r.stdout))
	}
	if r.stderr != "" {
		parts = append(parts, indentLines(r.stderr))
	}
	if r.stdout == "" && r.stderr == "" && r.exit == 0 && !r.timedOut && !r.notRun {
		parts = append(parts, "  (no output)")
	}
	if r.timedOut {
		parts = append(parts, "  (stopped: it ran too long)")
	}
	if r.exit != 0 && !r.notRun {
		parts = append(parts, fmt.Sprintf("  (exit code %d)", r.exit))
	}
	return strings.Join(parts, "\n")
}

// renderStep is a whole step; the header is left out when it is already on screen.
func renderStep(r stepRecord) string {
	var parts []string
	if !r.announced {
		parts = append(parts, renderCommand(r))
	}
	if r.ran {
		parts = append(parts, renderResult(r))
	}
	return strings.Join(parts, "\n")
}

// cancelNotice reports what Ctrl+C actually interrupted, decided from what is
// known once the task goroutine has actually stopped rather than guessed at
// the keypress. A step that had already run — and possibly changed something
// — is shown before saying so, the same as any other task that ends without
// an answer (hiddenBefore), so cancelling never reads as "nothing happened"
// when something did.
func (m *Model) cancelNotice() tea.Cmd {
	ran := 0
	for _, r := range m.cur.steps {
		if r.ran {
			ran++
		}
	}
	var text string
	switch {
	case m.cur.answered:
		text = "Cancelled after the answer above — the session stays open."
	case ran == 0:
		text = "Cancelled before anything ran — the session stays open."
	default:
		text = fmt.Sprintf("Cancelled — %d %s already run and shown above before this was stopped. The session stays open.",
			ran, plural(ran, "step had", "steps had"))
	}
	return sequence(m.hiddenBefore(), m.block(text))
}

// showHidden prints the steps of this task that the compact view held back, for
// a task that ended without an answer: the raw output is then the only result.
func (m *Model) showHidden() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.cur.steps {
		r := &m.cur.steps[i]
		if r.printed || !r.ran {
			continue
		}
		cmds = append(cmds, m.block(renderStep(*r)))
		r.announced, r.printed = true, true
	}
	return sequence(cmds...)
}

func (m *Model) lastStep() *stepRecord {
	if len(m.cur.steps) == 0 {
		return nil
	}
	return &m.cur.steps[len(m.cur.steps)-1]
}

// onEvent decides how the loop's report is shown.
func (m *Model) onEvent(ev loopevent.Event) tea.Cmd {
	switch ev.Kind {
	case loopevent.Command:
		m.cur.steps = append(m.cur.steps, stepRecord{step: ev.Step, command: ev.Command, tokens: ev.Tokens, latency: ev.Latency})
		m.status = ev.Command
		if m.verbose {
			r := m.lastStep()
			r.announced = true
			return m.block(renderCommand(*r))
		}

	case loopevent.Result:
		r := m.lastStep()
		if r == nil {
			return nil
		}
		r.ran, r.stdout, r.stderr, r.exit, r.timedOut, r.notRun = true, ev.Stdout, ev.Stderr, ev.ExitCode, ev.TimedOut, ev.NotRun
		m.status = ""
		if m.verbose {
			r.printed = true
			return m.raw(renderResult(*r))
		}

	case loopevent.Answer:
		m.cur.answered = true
		return m.block(ev.Text)

	case loopevent.Notice:
		return sequence(m.hiddenBefore(), m.block(ev.Text))

	case loopevent.Problem:
		text := ev.Text
		if ev.Detail != "" {
			text += "\n" + hintStyle.Render("  "+ev.Detail)
		}
		return sequence(m.hiddenBefore(), m.block(text))

	case loopevent.Note:
		m.cur.notes = append(m.cur.notes, ev.Text)
		if m.verbose {
			return m.raw(hintStyle.Render("  " + ev.Text))
		}

	case loopevent.Approval:
		// The command is always shown before asking: an approval that hides
		// what is being approved is not consent.
		if m.verbose {
			return m.raw(ev.Text)
		}
		if r := m.lastStep(); r != nil {
			r.announced = true
		}
		return m.block("$ " + ev.Command + "\n" + ev.Text)
	}
	return nil
}

// hiddenBefore shows held-back output ahead of a notice or problem that ends a
// task with no answer, so the message is not the only thing on screen.
func (m *Model) hiddenBefore() tea.Cmd {
	if m.verbose || m.cur.answered {
		return nil
	}
	return m.showHidden()
}

// toggleDetails flips the details view. Turning it on prints what is known
// about the task in progress, or else the last one, so pressing the key after an
// answer shows what was behind it. Lines already printed cannot be changed
// afterwards, which is why this adds to the scrollback instead of expanding it.
func (m *Model) toggleDetails() tea.Cmd {
	m.verbose = !m.verbose
	if !m.verbose {
		return m.block(hintStyle.Render("Details off — answers only. Ctrl+O shows them again."))
	}
	title := m.block(hintStyle.Render("Details on — every command and its raw output will show. Ctrl+O hides them."))
	rec := &m.cur
	if len(rec.steps) == 0 {
		rec = &m.last
	}
	if len(rec.steps) == 0 {
		return title
	}
	cmds := []tea.Cmd{title}
	for i := range rec.steps {
		r := &rec.steps[i]
		cmds = append(cmds, m.block(renderCommand(*r)))
		if r.ran {
			cmds = append(cmds, m.raw(renderResult(*r)))
		}
		r.announced, r.printed = true, r.ran
	}
	for _, n := range rec.notes {
		cmds = append(cmds, m.raw(hintStyle.Render("  "+n)))
	}
	return sequence(cmds...)
}
