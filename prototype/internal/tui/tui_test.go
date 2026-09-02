package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// noopRunner is a TaskRunner that does nothing, for tests that only
// exercise input handling and never start a task.
func noopRunner(context.Context, string, func(string) bool, io.Writer, io.Writer) int {
	return 0
}

// --- deterministic state-machine helpers -----------------------------
//
// The tests below that matter most — the confirmation bridge — drive
// Update directly with constructed messages rather than feeding bytes to
// a whole running program. That is deliberate. A full program consumes
// its scripted input as fast as it can parse it, which races the task
// goroutine that publishes the confirmation request, making any such test
// pass or fail on timing rather than on behavior. Driving Update directly
// removes the race entirely and asserts on exactly the transition under
// test. Full-program tests are kept below only for the things that
// genuinely need real terminal input parsing.

func typeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func ctrlC() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
}

func enterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}

// step feeds one message through Update and returns the resulting Model,
// discarding the Cmd (tests that care about a Cmd assert on state
// instead, which is what actually determines rendered behavior).
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	return got
}

// --- confirmation bridge (the safety-critical path) ------------------

// TestConfirmationDeliversYesToBlockedTask is the TUI's counterpart to
// the REPL's shared-reader test. The mechanism differs (channels here, a
// single bufio.Reader there) but the property is identical and is the
// reason this milestone needs its own test at all: a confirmation answer
// must reach the blocked gate intact, never be swallowed or misrouted.
func TestConfirmationDeliversYesToBlockedTask(t *testing.T) {
	m := NewModel(noopRunner)

	m = step(t, m, confirmRequestMsg("run it anyway?"))
	if m.pendingConfirm != "run it anyway?" {
		t.Fatalf("pendingConfirm = %q, want the prompt to be showing", m.pendingConfirm)
	}

	m = step(t, m, typeKey('y'))
	if m.pendingConfirm != "" {
		t.Errorf("pendingConfirm = %q, want cleared after answering", m.pendingConfirm)
	}

	select {
	case got := <-m.answers:
		if !got {
			t.Error("delivered verdict = false, want true after pressing 'y'")
		}
	default:
		t.Fatal("no verdict was delivered to the blocked task")
	}
}

// TestConfirmationFailsClosed checks every non-yes answer resolves to
// false, matching CLI mode where only an explicit "y"/"yes" proceeds. A
// gate that failed open on an ambiguous keystroke would be a genuine
// safety defect, so each accepted "no" key is asserted individually
// rather than assumed to share a branch.
func TestConfirmationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"lowercase n", typeKey('n')},
		{"uppercase N", tea.KeyPressMsg{Code: 'N', Text: "N"}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"bare enter", enterKey()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(noopRunner)
			m = step(t, m, confirmRequestMsg("run it anyway?"))
			m = step(t, m, tc.msg)

			select {
			case got := <-m.answers:
				if got {
					t.Errorf("verdict = true, want false — the gate must fail closed on %s", tc.name)
				}
			default:
				t.Fatal("no verdict delivered")
			}
		})
	}
}

// TestConfirmationAcceptsUppercaseY guards the one key that must resolve
// to yes besides 'y'.
func TestConfirmationAcceptsUppercaseY(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, confirmRequestMsg("run it anyway?"))
	m = step(t, m, tea.KeyPressMsg{Code: 'Y', Text: "Y"})

	select {
	case got := <-m.answers:
		if !got {
			t.Error("verdict = false, want true after pressing 'Y'")
		}
	default:
		t.Fatal("no verdict delivered")
	}
}

// TestUnrelatedKeyDuringConfirmationIsIgnored verifies a stray keystroke
// while a confirmation is outstanding neither answers the gate nor leaks
// into the task input field.
func TestUnrelatedKeyDuringConfirmationIsIgnored(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, confirmRequestMsg("run it anyway?"))
	m = step(t, m, typeKey('z'))

	if m.pendingConfirm == "" {
		t.Error("an unrelated key cleared the confirmation prompt")
	}
	select {
	case v := <-m.answers:
		t.Fatalf("an unrelated key delivered a verdict (%v) — it must be ignored", v)
	default:
	}
	if m.input.Value() != "" {
		t.Errorf("input value = %q, want empty — keys during a confirmation must not reach the input field", m.input.Value())
	}
}

// TestKeystrokeBeforeConfirmationPromptIsDiscarded documents deliberate
// behavior, not an accident, and is the one place TUI mode intentionally
// differs from the REPL. In the REPL a typed-ahead "y" sits in the
// terminal's line buffer and is consumed when the gate asks for it. Here,
// keys are consumed as they arrive, so a "y" pressed while a task is
// running but *before* its confirmation prompt appears is dropped.
//
// That is the safer of the two behaviors and is kept on purpose: a
// pre-typed keystroke must never auto-approve a destructive command the
// user has not actually seen described yet.
func TestKeystrokeBeforeConfirmationPromptIsDiscarded(t *testing.T) {
	m := NewModel(noopRunner)
	m.running = true // a task is in flight, no confirmation requested yet

	m = step(t, m, typeKey('y'))

	select {
	case v := <-m.answers:
		t.Fatalf("a 'y' typed before the prompt appeared delivered a verdict (%v) — it must be discarded", v)
	default:
	}
	if m.input.Value() != "" {
		t.Errorf("input value = %q, want empty — keys during a running task must not reach the input field", m.input.Value())
	}
}

// --- task lifecycle ---------------------------------------------------

// TestCtrlCDuringTaskCancelsWithoutQuitting verifies TUI mode mirrors the
// REPL's interrupt semantics: mid-task ctrl+c cancels that task's context
// and leaves the session alive rather than tearing down the program.
func TestCtrlCDuringTaskCancelsWithoutQuitting(t *testing.T) {
	cancelled := make(chan struct{})
	run := func(ctx context.Context, _ string, _ func(string) bool, _, _ io.Writer) int {
		<-ctx.Done()
		close(cancelled)
		return 1
	}

	m := NewModel(run)
	m.input.SetValue("sleep")
	m = step(t, m, enterKey())
	if !m.running {
		t.Fatal("expected the task to be running after enter")
	}

	m = step(t, m, ctrlC())

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+c during a task did not cancel the task's context")
	}
	if !m.running {
		t.Error("ctrl+c cleared running state directly; it should stay set until taskDoneMsg arrives")
	}
}

// TestTaskDoneRestoresIdlePrompt verifies the session returns to an
// input-accepting state once a task reports completion.
func TestTaskDoneRestoresIdlePrompt(t *testing.T) {
	m := NewModel(noopRunner)
	m.running = true
	m.pendingConfirm = "stale prompt"

	m = step(t, m, taskDoneMsg{code: 0})

	if m.running {
		t.Error("running should be false after taskDoneMsg")
	}
	if m.pendingConfirm != "" {
		t.Error("a pending confirmation should be cleared when the task ends")
	}
}

// TestEnterRunsTaskThroughInjectedRunner verifies the wiring that makes
// M3b step 3 meaningful: the typed task reaches the injected runner
// verbatim, and whatever that runner writes lands in the transcript.
func TestEnterRunsTaskThroughInjectedRunner(t *testing.T) {
	var (
		mu      sync.Mutex
		gotTask string
		done    = make(chan struct{})
	)
	run := func(_ context.Context, task string, _ func(string) bool, out, _ io.Writer) int {
		mu.Lock()
		gotTask = task
		mu.Unlock()
		fmt.Fprintf(out, "ran: %s\n", task)
		close(done)
		return 0
	}

	m := NewModel(run)
	m.input.SetValue("list files")
	m = step(t, m, enterKey())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner was never invoked")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotTask != "list files" {
		t.Errorf("runner got task %q, want %q", gotTask, "list files")
	}
	if m.input.Value() != "" {
		t.Errorf("input = %q, want cleared after submitting", m.input.Value())
	}
	if !strings.Contains(strings.Join(m.transcript, "\n"), "> list files") {
		t.Errorf("submitted task missing from transcript:\n%s", strings.Join(m.transcript, "\n"))
	}

	// Drain the runner's output through Update and confirm it renders.
	m = step(t, m, <-m.events)
	if !strings.Contains(strings.Join(m.transcript, "\n"), "ran: list files") {
		t.Errorf("runner output missing from transcript:\n%s", strings.Join(m.transcript, "\n"))
	}
}

// TestEmptyEnterDoesNotStartATask guards a small but real footgun:
// pressing enter on an empty or whitespace-only prompt must not run
// anything.
func TestEmptyEnterDoesNotStartATask(t *testing.T) {
	for _, value := range []string{"", "   "} {
		started := make(chan struct{}, 1)
		run := func(context.Context, string, func(string) bool, io.Writer, io.Writer) int {
			started <- struct{}{}
			return 0
		}
		m := NewModel(run)
		m.input.SetValue(value)
		m = step(t, m, enterKey())

		select {
		case <-started:
			t.Errorf("enter with input %q started a task", value)
		default:
		}
		if m.running {
			t.Errorf("enter with input %q set running state", value)
		}
	}
}

// --- rendering / plumbing --------------------------------------------

func TestViewShowsPromptWhenIdle(t *testing.T) {
	view := NewModel(noopRunner).View()
	if !strings.Contains(view.Content, "SynapseOS") {
		t.Errorf("view missing header, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, ">") {
		t.Errorf("view missing input prompt, got:\n%s", view.Content)
	}
}

func TestViewShowsConfirmationPrompt(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, confirmRequestMsg("rm x is irreversible — run it anyway?"))
	view := m.View()
	if !strings.Contains(view.Content, "rm x is irreversible") {
		t.Errorf("view missing the block reason, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "[y/N]") {
		t.Errorf("view missing the y/N affordance, got:\n%s", view.Content)
	}
}

func TestMsgWriterForwardsOutputAsMessages(t *testing.T) {
	events := make(chan tea.Msg, 1)
	w := msgWriter{events: events}

	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len("hello\n") {
		t.Errorf("Write returned n = %d, want %d", n, len("hello\n"))
	}
	if got := <-events; got != outputMsg("hello\n") {
		t.Errorf("forwarded %#v, want outputMsg(%q)", got, "hello\n")
	}
}

// TestMsgWriterCopiesItsBuffer guards the io.Writer contract: callers are
// explicitly allowed to reuse the slice they pass, so retaining it would
// corrupt already-queued output.
func TestMsgWriterCopiesItsBuffer(t *testing.T) {
	events := make(chan tea.Msg, 1)
	w := msgWriter{events: events}

	buf := []byte("first")
	if _, err := w.Write(buf); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	copy(buf, "SECND") // caller reuses the buffer, as io.Writer permits

	if got := <-events; got != outputMsg("first") {
		t.Errorf("message was corrupted by buffer reuse: got %q, want %q", got, "first")
	}
}

func TestAppendTranscriptBoundsRetainedLines(t *testing.T) {
	var lines []string
	for i := 0; i < transcriptLimit*2; i++ {
		lines = appendTranscript(lines, fmt.Sprintf("line %d", i))
	}
	if len(lines) > transcriptLimit {
		t.Errorf("transcript grew to %d lines, want at most %d", len(lines), transcriptLimit)
	}
	if last := lines[len(lines)-1]; last != fmt.Sprintf("line %d", transcriptLimit*2-1) {
		t.Errorf("newest line was trimmed; last = %q", last)
	}
}

// --- full-program tests (real terminal input parsing) ----------------

// runProgram drives a Model against simulated raw terminal input — actual
// bytes, parsed by bubbletea itself exactly as a real terminal's input
// would be. Reserved for cases that genuinely need that parsing path;
// see the note above on why the confirmation tests do not use it.
//
// quitCleanly reports whether the program ended on its own rather than
// being killed by the watchdog. Tests about quit behavior must assert on
// it explicitly: otherwise such a test passes vacuously, since the
// watchdog terminates the program either way and only timing differs.
func runProgram(t *testing.T, run TaskRunner, input string, timeout time.Duration) (m Model, output string, quitCleanly bool) {
	t.Helper()

	var in bytes.Buffer
	in.WriteString(input)
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := tea.NewProgram(NewModel(run),
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&out),
	)

	finalModel, err := p.Run()
	quitCleanly = ctx.Err() == nil
	if err != nil && quitCleanly {
		t.Fatalf("Run() error: %v", err)
	}
	if finalModel != nil {
		got, ok := finalModel.(Model)
		if !ok {
			t.Fatalf("final model is %T, want Model", finalModel)
		}
		m = got
	}
	return m, out.String(), quitCleanly
}

// TestQuitsOnCtrlCAtIdlePrompt asserts the program self-terminates rather
// than being killed by the watchdog — the only thing that actually
// distinguishes a working quit binding from a broken one.
func TestQuitsOnCtrlCAtIdlePrompt(t *testing.T) {
	_, _, quitCleanly := runProgram(t, noopRunner, "\x03", 2*time.Second)
	if !quitCleanly {
		t.Error("ctrl+c at an idle prompt did not quit — it ran until the watchdog expired")
	}
}

// TestBareQDoesNotQuit is the specific regression case: "q" must be
// ordinary typed text, not a quit signal, because the input field holds
// arbitrary task descriptions.
func TestBareQDoesNotQuit(t *testing.T) {
	m, _, _ := runProgram(t, noopRunner, "q\x03", 2*time.Second)
	if m.input.Value() != "q" {
		t.Errorf("input value = %q, want %q — bare 'q' must be typed text, not a quit", m.input.Value(), "q")
	}
}

func TestTypedTextAccumulatesInInput(t *testing.T) {
	m, _, _ := runProgram(t, noopRunner, "delete doomed.txt\x03", 2*time.Second)
	if want := "delete doomed.txt"; m.input.Value() != want {
		t.Errorf("input value = %q, want %q", m.input.Value(), want)
	}
}

// TestViewShowsWorkingIndicatorWhileRunning covers the third render
// state: while a task runs the input box is replaced by a progress line
// that also advertises how to cancel, so the session never looks frozen.
func TestViewShowsWorkingIndicatorWhileRunning(t *testing.T) {
	m := NewModel(noopRunner)
	m.running = true

	view := m.View()
	if !strings.Contains(view.Content, "working") {
		t.Errorf("view missing the working indicator, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "ctrl+c") {
		t.Errorf("view should tell the user how to cancel, got:\n%s", view.Content)
	}
}

// --- viewport / scrollback (M3b step 5) ------------------------------

func sizeMsg(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

// TestViewportSizesToTerminalLeavingRoomForInput verifies the transcript
// pane never claims the whole terminal — if it did, the input box would
// be pushed off-screen and the session would be unusable.
func TestViewportSizesToTerminalLeavingRoomForInput(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, sizeMsg(80, 24))

	if !m.ready {
		t.Fatal("model should be ready after a window size message")
	}
	if got := m.view.Height(); got >= 24 {
		t.Errorf("viewport height = %d, want less than the terminal's 24 to leave room for the input", got)
	}
	if got := m.view.Height(); got < 1 {
		t.Errorf("viewport height = %d, want at least 1", got)
	}
}

// TestViewportHeightStaysPositiveOnTinyTerminal guards the arithmetic:
// subtracting fixed chrome from a very short terminal must not produce a
// zero or negative height.
func TestViewportHeightStaysPositiveOnTinyTerminal(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, sizeMsg(20, 2))

	if got := m.view.Height(); got < 1 {
		t.Errorf("viewport height = %d on a 2-row terminal, want at least 1", got)
	}
}

// TestNewOutputAutoScrollsWhenAtBottom verifies the common case: a user
// watching live output keeps seeing the newest lines without touching
// anything.
func TestNewOutputAutoScrollsWhenAtBottom(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, sizeMsg(80, 10))

	for i := 0; i < 50; i++ {
		m = step(t, m, outputMsg(fmt.Sprintf("line %d\n", i)))
	}

	if !m.view.AtBottom() {
		t.Error("viewport drifted away from the bottom while new output arrived and the user had not scrolled")
	}
}

// TestScrollingUpIsNotYankedBackByNewOutput is the real correctness
// property behind scrollback, not a cosmetic one: if a user scrolls up to
// read what a command actually proposed, incoming output must not snatch
// the view back to the bottom mid-read. That matters most precisely when
// they are deciding how to answer an irreversible confirmation.
func TestScrollingUpIsNotYankedBackByNewOutput(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, sizeMsg(80, 10))
	for i := 0; i < 50; i++ {
		m = step(t, m, outputMsg(fmt.Sprintf("line %d\n", i)))
	}

	// Scroll up, away from the bottom.
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.view.AtBottom() {
		t.Fatal("setup failed: pgup did not move the viewport off the bottom")
	}

	m = step(t, m, outputMsg("newly arrived line\n"))

	if m.view.AtBottom() {
		t.Error("new output yanked the viewport back to the bottom while the user was scrolled up reading")
	}
}

// TestScrollWorksDuringPendingConfirmation verifies scroll keys stay live
// while a confirmation is outstanding — being able to scroll back and
// read the proposed command is exactly what a user needs before deciding
// y or N, so this must not be blocked by the confirmation gate.
func TestScrollWorksDuringPendingConfirmation(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, sizeMsg(80, 10))
	for i := 0; i < 50; i++ {
		m = step(t, m, outputMsg(fmt.Sprintf("line %d\n", i)))
	}
	m = step(t, m, confirmRequestMsg("rm -rf data is irreversible — run it anyway?"))

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})

	if m.view.AtBottom() {
		t.Error("pgup did not scroll while a confirmation was pending")
	}
	// Scrolling must not have been mistaken for an answer.
	if m.pendingConfirm == "" {
		t.Error("scrolling cleared the pending confirmation — it must not count as an answer")
	}
	select {
	case v := <-m.answers:
		t.Fatalf("a scroll key delivered a confirmation verdict (%v)", v)
	default:
	}
}

// TestViewUsesAltScreen pins the full-screen behavior build-order.md
// promises for TUI mode.
func TestViewUsesAltScreen(t *testing.T) {
	if !NewModel(noopRunner).View().AltScreen {
		t.Error("TUI mode should render in the alternate screen buffer")
	}
}

// TestViewRendersBeforeFirstWindowSize guards the startup window: the
// model must produce a sane view before any WindowSizeMsg arrives, rather
// than rendering through a viewport sized from a guess.
func TestViewRendersBeforeFirstWindowSize(t *testing.T) {
	m := NewModel(noopRunner)
	if m.ready {
		t.Fatal("model should not be marked ready before a window size message")
	}
	if content := m.View().Content; !strings.Contains(content, "SynapseOS") {
		t.Errorf("pre-size view missing header, got:\n%s", content)
	}
}
