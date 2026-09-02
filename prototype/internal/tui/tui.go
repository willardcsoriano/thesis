// Package tui implements TUI mode (M3b, D11): the bubbletea interface
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
// Bridging a synchronous loop into an event-driven runtime is the whole
// technical problem this file solves. bubbletea's Update must never
// block, but the loop is blocking and needs to ask a question mid-flight.
// The bridge is two channels: the loop runs on its own goroutine, writes
// output as messages into `events`, and when it hits an irreversible step
// it publishes a confirmation request and blocks reading `answers` until
// Update — having rendered the prompt and taken a keypress — sends the
// verdict back.
package tui

import (
	"context"
	"io"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
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

// transcriptLimit caps how many rendered lines are retained. A viewport
// with real scrollback is step 5 of the M3b build sequence; until then
// this bounds memory and keeps the view from growing without limit.
const transcriptLimit = 200

type (
	// outputMsg is a chunk written by the running task to stdout/stderr.
	outputMsg string
	// confirmRequestMsg is the running task asking for a y/n decision.
	// The task goroutine is blocked until an answer is sent back.
	confirmRequestMsg string
	// taskDoneMsg reports that the task goroutine has returned.
	taskDoneMsg struct{ code int }
)

// Model is TUI mode's bubbletea state.
type Model struct {
	input textinput.Model
	view  viewport.Model
	run   TaskRunner

	// ready guards against rendering the viewport before the first
	// WindowSizeMsg tells us the real terminal dimensions.
	ready bool

	transcript []string

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

	return Model{
		input:   ti,
		view:    viewport.New(),
		run:     run,
		events:  make(chan tea.Msg, 256),
		answers: make(chan bool, 1),
		transcript: []string{
			headerStyle.Render("SynapseOS — TUI mode"),
			hintStyle.Render("Type a task and press enter. Ctrl+C cancels a running task; at an idle prompt it quits."),
			hintStyle.Render("PgUp/PgDn scroll the transcript, including while a confirmation is pending."),
			"",
		},
	}
}

// Init returns the cursor-blink command and begins listening for task
// events. Calling Focus() again is idempotent and intentional: the focus
// flag was already set for real in NewModel, and this call exists only to
// obtain the blink Cmd.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), waitForEvent(m.events))
}

// waitForEvent blocks on the task-event channel inside a Cmd, which
// bubbletea runs on its own goroutine — this is what lets a synchronous
// task publish into an event loop that must never block. Every branch of
// Update that consumes an event re-issues this, so the listener persists
// for the life of the session.
func waitForEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		// Reserve rows for the input line, the prompt/status line, and
		// the surrounding blank lines so the viewport never overflows
		// the terminal and pushes the input box off-screen.
		const chromeHeight = 4
		m.view.SetWidth(msg.Width)
		m.view.SetHeight(max(1, msg.Height-chromeHeight))
		m.input.SetWidth(max(1, msg.Width-2))
		m.ready = true
		m.refreshViewport()
		return m, nil

	case outputMsg:
		m.transcript = appendTranscript(m.transcript, string(msg))
		m.refreshViewport()
		return m, waitForEvent(m.events)

	case confirmRequestMsg:
		m.pendingConfirm = string(msg)
		return m, waitForEvent(m.events)

	case taskDoneMsg:
		m.running = false
		m.pendingConfirm = ""
		m.cancelTask = nil
		m.transcript = appendTranscript(m.transcript, "")
		m.refreshViewport()
		return m, waitForEvent(m.events)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// refreshViewport re-renders the transcript into the viewport and pins
// the view to the newest output. Auto-scrolling only when the user is
// already at the bottom is deliberate: if they have scrolled up to read
// earlier output, new output must not yank the view away from them.
func (m *Model) refreshViewport() {
	atBottom := m.view.AtBottom()
	m.view.SetContent(strings.Join(m.transcript, "\n"))
	if atBottom {
		m.view.GotoBottom()
	}
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
			m.transcript = appendTranscript(m.transcript, "\ncancelling this task — the session stays open.\n")
			return m, nil
		}
		return m, tea.Quit
	}

	if m.pendingConfirm != "" {
		switch key {
		case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			return m, cmd
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

	// Scrolling stays available at all times, including mid-task and
	// while a confirmation is pending — being able to scroll back to read
	// what a command actually proposed is precisely what a user needs
	// before answering y/N on an irreversible step.
	switch key {
	case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd
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
		m.transcript = appendTranscript(m.transcript, "> "+task+"\n")
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
	m.transcript = appendTranscript(m.transcript, verdict+"\n")
	m.answers <- ok // buffered by one; never blocks the UI thread
	return m, nil
}

// startTask launches the injected runner on its own goroutine, wiring its
// output and its confirmation callback back through the event channels.
func (m Model) startTask(task string) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.running = true
	m.cancelTask = cancel

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

	return m, waitForEvent(m.events)
}

func (m Model) View() tea.View {
	var b strings.Builder

	// Before the first WindowSizeMsg the real terminal size is unknown,
	// so the transcript is rendered plainly rather than through a
	// viewport sized from a guess.
	if m.ready {
		b.WriteString(m.view.View())
	} else {
		b.WriteString(strings.Join(m.transcript, "\n"))
	}
	b.WriteString("\n")

	switch {
	case m.pendingConfirm != "":
		b.WriteString("\n" + confirmStyle.Render(m.pendingConfirm+"  [y/N]") + "\n")
	case m.running:
		b.WriteString("\n" + workingStyle.Render("working... (ctrl+c cancels this task)") + "\n")
	default:
		b.WriteString("\n" + m.input.View() + "\n")
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// msgWriter adapts the io.Writer the task loop writes progress to into
// bubbletea messages. Write copies its argument (via string conversion)
// because io.Writer explicitly permits callers to reuse the buffer.
type msgWriter struct{ events chan<- tea.Msg }

func (w msgWriter) Write(p []byte) (int, error) {
	w.events <- outputMsg(string(p))
	return len(p), nil
}

// appendTranscript adds a chunk as one or more lines, trimming the
// trailing newline the writers emit so blank lines aren't doubled, and
// bounding total retained lines.
func appendTranscript(lines []string, chunk string) []string {
	chunk = strings.TrimSuffix(chunk, "\n")
	lines = append(lines, strings.Split(chunk, "\n")...)
	if len(lines) > transcriptLimit {
		lines = lines[len(lines)-transcriptLimit:]
	}
	return lines
}

// Run starts TUI mode against the real terminal, driving the supplied
// runner. Returns whatever error bubbletea's own Run reports.
func Run(run TaskRunner) error {
	_, err := tea.NewProgram(NewModel(run)).Run()
	return err
}
