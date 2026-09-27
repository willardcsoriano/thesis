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
	"io"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// NewModel builds the initial state. The input is focused here rather
// than in Init because Init can only return a Cmd, never mutate the Model
// the runtime holds — focusing there would apply to a discarded copy and
// silently leave the input ignoring every keystroke (a real bug caught by
// test earlier in this milestone, and what the upstream docs' example
// would have led to).
func NewModel(run TaskRunner) Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "type a task..."
	ti.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(spinnerStyle))

	return Model{
		input:   ti,
		spinner: sp,
		run:     run,
		width:   defaultWidth,
		events:  make(chan tea.Msg, 256),
		answers: make(chan bool, 1),
		print:   func(s string) tea.Cmd { return tea.Println(s) },
		header: strings.Join([]string{
			headerStyle.Render("SynapseOS — TUI mode"),
			hintStyle.Render("Type a task and press enter. Ctrl+C cancels a running task; at an idle prompt it quits."),
			hintStyle.Render("Scroll and select text with the terminal as usual — the whole conversation stays in its scrollback."),
			hintStyle.Render("Follow-ups can refer back (\"move it to Downloads\"). Type context to see what's remembered, clear to forget it."),
		}, "\n"),
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

// emit returns a command that prints text into the scrollback. An empty text
// prints a blank line, which needs a placeholder because Println of nothing
// prints nothing.
func (m Model) emit(text string) tea.Cmd {
	if text == "" {
		text = " "
	}
	return m.print(text)
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
	return m.emit(strings.Join(done, "\n"))
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
		out := m.appendOutput(closing)
		return m, sequence(out, waitForEvent(m.events))
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
			return m, m.say("\ncancelling this task — the session stays open.\n")
		}
		m.quitting = true
		return m, tea.Quit
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

	// The echo is queued before the goroutine exists, so it precedes every
	// line the task writes.
	echo := m.say("> " + task + "\n")

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
	return m, tea.Batch(echo, m.spinner.Tick)
}

// View draws only the live region: the unfinished line, the status or
// confirmation, and the prompt. Everything finished is already in the
// terminal's scrollback.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var b strings.Builder

	if m.partial != "" {
		b.WriteString(strings.Join(wrapLine(m.partial, m.width), "\n") + "\n")
	}

	switch {
	case m.pendingConfirm != "":
		b.WriteString("\n" + confirmStyle.Render(warnGlyph+" "+m.pendingConfirm+"  [y/N]") + "\n")
	case m.running:
		b.WriteString("\n" + m.spinner.View() + workingStyle.Render(" working — ctrl+c cancels this task") + "\n")
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

// Run starts TUI mode against the real terminal, driving the supplied
// runner. Returns whatever error bubbletea's own Run reports.
func Run(run TaskRunner) error {
	return RunWithWarmup(run, nil)
}

// RunWithWarmup is Run with a background warm-up: warm runs once at startup
// (typically loading the language model) while the session is already usable,
// and the UI says so until it returns. A nil warm behaves exactly like Run.
func RunWithWarmup(run TaskRunner, warm func(context.Context) error) error {
	m := NewModel(run)
	if warm != nil {
		m.warm, m.warming = warm, true
	}
	_, err := tea.NewProgram(m).Run()
	return err
}
