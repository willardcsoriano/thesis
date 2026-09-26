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

	"charm.land/bubbles/v2/spinner"
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

// transcriptLimit caps how many rendered lines are retained. A viewport
// with real scrollback is step 5 of the M5 build sequence; until then
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
	// warmDoneMsg reports that the startup warm-up has finished, successfully or not.
	warmDoneMsg struct{}
)

// Model is TUI mode's bubbletea state.
type Model struct {
	input   textinput.Model
	view    viewport.Model
	spinner spinner.Model
	run     TaskRunner

	// warm loads the model in the background at startup; warming is true from
	// Init until it reports back. Nil means there is nothing to warm, and the
	// status line never appears.
	warm    func(context.Context) error
	warming bool

	// mouseOn controls whether the view asks the terminal for mouse
	// reporting. It defaults to off: turning it on hands the terminal's
	// pointer to this program, which takes drag-to-select with it, and
	// losing copy-paste in a tool whose whole output is text is a worse
	// trade than losing the wheel. PgUp/PgDn scroll either way.
	mouseOn bool

	// ready guards against rendering the viewport before the first
	// WindowSizeMsg tells us the real terminal dimensions.
	ready bool

	transcript []string
	// lineOpen reports whether the last transcript line is mid-write —
	// i.e. the most recent chunk did not end in a newline, so the next
	// one continues it. Streaming makes this the common case, not an
	// edge case.
	lineOpen bool

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
		view:    viewport.New(),
		spinner: sp,
		run:     run,
		events:  make(chan tea.Msg, 256),
		answers: make(chan bool, 1),
		transcript: []string{
			headerStyle.Render("SynapseOS — TUI mode"),
			hintStyle.Render("Type a task and press enter. Ctrl+C cancels a running task; at an idle prompt it quits."),
			hintStyle.Render("PgUp/PgDn scroll the transcript, including while a confirmation is pending. Text is selectable; type mouse to trade that for wheel scrolling."),
			hintStyle.Render("Follow-ups can refer back (\"move it to Downloads\"). Type context to see what's remembered, clear to forget it."),
			"",
		},
	}
}

// Init returns the cursor-blink command and begins listening for task
// events. Calling Focus() again is idempotent and intentional: the focus
// flag was already set for real in NewModel, and this call exists only to
// obtain the blink Cmd.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.input.Focus(), waitForEvent(m.events)}
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		// Reserve exactly the rows View() emits below the viewport: one
		// newline closing the viewport, one blank separator, the input or
		// status line, and its trailing newline. Derived from View rather
		// than guessed — the previous hardcoded value was a guess that
		// could not be checked without looking at a running terminal.
		const chromeHeight = 4
		m.view.SetWidth(msg.Width)
		m.view.SetHeight(max(1, msg.Height-chromeHeight))
		m.input.SetWidth(max(1, msg.Width-2))
		m.ready = true
		m.refreshViewport()
		return m, nil

	case tea.MouseWheelMsg:
		// The alt-screen buffer replaces the terminal's own scrollback, so
		// without forwarding the wheel the transcript is unreachable by any
		// means except PgUp/PgDn. Handled here rather than by the catch-all
		// below because that path feeds the text input, which ignores it.
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd

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
		m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, string(msg))
		m.refreshViewport()
		return m, waitForEvent(m.events)

	case confirmRequestMsg:
		m.pendingConfirm = string(msg)
		return m, waitForEvent(m.events)

	case taskDoneMsg:
		m.running = false
		m.pendingConfirm = ""
		m.cancelTask = nil
		// Close any half-written line before the blank separator, so a
		// task ending mid-fragment doesn't merge into the next one.
		if m.lineOpen {
			m.transcript, m.lineOpen = appendChunk(m.transcript, true, "\n")
		}
		m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, "\n")
		m.refreshViewport()
		return m, waitForEvent(m.events)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// wrapLine breaks one transcript line to width columns on word boundaries,
// returning the pieces. The viewport clips rather than wraps, so anything
// longer than the terminal is simply invisible without this — which is how
// the header ended up truncated mid-sentence in live use.
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
	// Preserve a deliberately blank line rather than collapsing it away;
	// blank lines are the transcript's separator between tasks.
	if line == "" {
		return []string{""}
	}
	return out
}

// refreshViewport re-renders the transcript into the viewport and pins
// the view to the newest output. Auto-scrolling only when the user is
// already at the bottom is deliberate: if they have scrolled up to read
// earlier output, new output must not yank the view away from them.
func (m *Model) refreshViewport() {
	atBottom := m.view.AtBottom()

	width := m.view.Width()
	rendered := make([]string, 0, len(m.transcript))
	for _, line := range m.transcript {
		rendered = append(rendered, wrapLine(line, width)...)
	}

	// Pad from the top so short transcripts sit against the input line
	// instead of leaving a block of dead space between the last output and
	// the prompt. The viewport is a fixed-height window: without this the
	// content renders at the top and the remaining rows render blank, which
	// is what the gap above the prompt actually was.
	if h := m.view.Height(); h > len(rendered) {
		rendered = append(make([]string, h-len(rendered)), rendered...)
	}

	m.view.SetContent(strings.Join(rendered, "\n"))
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
			m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, "\ncancelling this task — the session stays open.\n")
			return m, nil
		}
		return m, tea.Quit
	}

	if m.pendingConfirm != "" {
		switch key {
		case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end", "up", "down":
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
	//
	// Up and Down scroll too. The input is a single line, so they have no other
	// job, and in the alternate screen most terminals turn the mouse wheel into
	// exactly these two keys when the program has not asked for mouse reporting
	// — which is how the wheel scrolls here without giving up text selection.
	switch key {
	case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end", "up", "down":
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
		// Handled here rather than in the task runner because it is view
		// state, not something the execution loop knows or should know.
		if strings.EqualFold(task, "mouse") {
			m.mouseOn = !m.mouseOn
			m.input.SetValue("")
			note := "mouse scrolling off — drag to select and copy as usual."
			if m.mouseOn {
				note = "mouse scrolling on — the terminal's own text selection is disabled while it is; type mouse again to turn it off."
			}
			m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, note+"\n")
			m.refreshViewport()
			return m, nil
		}
		m.input.SetValue("")
		m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, "> "+task+"\n")
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
	m.transcript, m.lineOpen = appendChunk(m.transcript, m.lineOpen, verdict+"\n")
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
	return m, m.spinner.Tick
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

	v := tea.NewView(b.String())
	v.AltScreen = true
	// Mouse reporting is view state in bubbletea v2, not a program option,
	// and it is off unless asked for: enabling it makes the terminal send
	// clicks and drags here instead of performing a native selection, so
	// the transcript stops being copyable. Toggled with the `mouse`
	// command for anyone who prefers wheel scrolling to selection.
	if m.mouseOn {
		v.MouseMode = tea.MouseModeCellMotion
	}
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

// appendChunk adds a chunk of output to the transcript, honouring the
// fact that a chunk is not necessarily a whole line.
//
// This distinction is the entire point of the function. Streaming
// delivers whatever fragment the model produced — "UNS", then
// "UPPORTED" — with no newline between them, while the loop's own status
// writes ("step 1: ...\n") are newline-terminated. Treating every chunk
// as a complete line renders the former as two separate lines, which is
// exactly what a real terminal showed before this was fixed. The `open`
// flag carries whether the last line is still being written to, so a
// fragment continues it instead of starting a new one; the returned flag
// is the caller's new state.
func appendChunk(lines []string, open bool, chunk string) ([]string, bool) {
	if chunk == "" {
		return lines, open
	}

	endsLine := strings.HasSuffix(chunk, "\n")
	parts := strings.Split(strings.TrimSuffix(chunk, "\n"), "\n")

	for i, part := range parts {
		if i == 0 && open && len(lines) > 0 {
			lines[len(lines)-1] += part
			continue
		}
		lines = append(lines, part)
	}

	if len(lines) > transcriptLimit {
		lines = lines[len(lines)-transcriptLimit:]
	}
	return lines, !endsLine
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
