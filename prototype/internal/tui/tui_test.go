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

	"synapseos/internal/loopevent"
)

// noopRunner is a TaskRunner that does nothing, for tests that only
// exercise input handling and never start a task.
func noopRunner(context.Context, string, func(string) bool, io.Writer, io.Writer) int {
	return 0
}

// newCaptured builds a Model whose printed lines are recorded instead of sent
// to a terminal. print runs when Update creates the command, so the record is
// in exactly the order lines would reach the scrollback.
func newCaptured() (Model, *[]string) {
	printed := &[]string{}
	m := NewModel(noopRunner)
	m.print = func(s string) tea.Cmd {
		*printed = append(*printed, s)
		return nil
	}
	return m, printed
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

	m, printed := newCaptured()
	m.run = run
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

	// The echo and the runner's output both travel through the events channel.
	m = step(t, m, <-m.events)
	m = step(t, m, <-m.events)
	all := strings.Join(*printed, "\n")
	if !strings.Contains(all, "> list files") {
		t.Errorf("submitted task missing from the scrollback:\n%s", all)
	}
	if !strings.Contains(all, "ran: list files") {
		t.Errorf("runner output missing from the scrollback:\n%s", all)
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
	if strings.Contains(view.Content, "SynapseOS") {
		t.Errorf("the header is printed once into the scrollback, not redrawn with the prompt, got:\n%s", view.Content)
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

// --- viewport / scrollback (M5 step 5) ------------------------------

func sizeMsg(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

// TestViewIsInline pins the reason this UI is not full-screen: the alternate
// screen has no scrollback, so only what fits on screen could be selected or
// scrolled. Finished lines go to the terminal's own scrollback instead.
func TestViewIsInline(t *testing.T) {
	v := NewModel(noopRunner).View()
	if v.AltScreen {
		t.Error("the view must not use the alternate screen: it has no scrollback, so the conversation could not be scrolled or copied")
	}
	if v.MouseMode != tea.MouseModeNone {
		t.Error("the view must not request mouse reporting: it takes native text selection and the wheel with it")
	}
}

// TestViewRendersBeforeFirstWindowSize guards the startup window: the model
// must produce a sane live region before any WindowSizeMsg arrives.
func TestViewRendersBeforeFirstWindowSize(t *testing.T) {
	m := NewModel(noopRunner)
	if content := m.View().Content; !strings.Contains(content, ">") {
		t.Errorf("pre-size view missing the prompt, got:\n%s", content)
	}
}

// TestViewShowsOnlyTheLiveRegion: finished output belongs to the scrollback,
// so the redrawn region must not grow with the conversation.
func TestViewShowsOnlyTheLiveRegion(t *testing.T) {
	m, printed := newCaptured()
	for i := 0; i < 100; i++ {
		m = step(t, m, outputMsg(fmt.Sprintf("line %d\n", i)))
	}
	view := m.View().Content
	if strings.Contains(view, "line 5") || strings.Count(view, "\n") > 4 {
		t.Errorf("the live region carries finished output, got:\n%s", view)
	}
	if got := strings.Count(strings.Join(*printed, "\n"), "line "); got != 100 {
		t.Errorf("printed %d lines, want 100", got)
	}
}

// TestInitPrintsTheHeaderOnce: the header is scrollback like everything else.
func TestInitPrintsTheHeaderOnce(t *testing.T) {
	m, printed := newCaptured()
	m.Init()
	if len(*printed) != 1 || !strings.Contains((*printed)[0], "SynapseOS") {
		t.Fatalf("Init printed %q, want the header once", *printed)
	}
}

// --- streamed-fragment rendering (regression, found in live use) -----

// TestStreamedFragmentsFormOneLine is the bug a real terminal exposed and
// every earlier test missed: streaming delivers mid-line fragments
// ("UNS", then "UPPORTED"), and each chunk was treated as a complete line. The
// screen showed
//
//	UNS
//	UPPORTED
//
// instead of "UNSUPPORTED". The unfinished tail must stay live, unprinted,
// until its newline arrives.
func TestStreamedFragmentsFormOneLine(t *testing.T) {
	m, printed := newCaptured()
	for _, frag := range []string{"UNS", "UPPORTED"} {
		m = step(t, m, outputMsg(frag))
	}
	if len(*printed) != 0 {
		t.Fatalf("printed %q before the line was finished", *printed)
	}
	if m.partial != "UNSUPPORTED" {
		t.Errorf("live line = %q, want %q", m.partial, "UNSUPPORTED")
	}
	if !strings.Contains(m.View().Content, "UNSUPPORTED") {
		t.Error("the unfinished line should be visible in the live region")
	}
	m = step(t, m, outputMsg("\n"))
	if got := *printed; len(got) != 1 || got[0] != "UNSUPPORTED" {
		t.Errorf("printed %q, want [UNSUPPORTED]", got)
	}
	if m.partial != "" {
		t.Errorf("live line = %q after the newline, want empty", m.partial)
	}
}
func TestAppendOutputLineBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		chunks  []string
		printed string // every printed line, joined by "|"
		partial string
	}{
		{"newline closes a line", []string{"abc\n", "def"}, "abc", "def"},
		{"fragments then newline", []string{"ab", "cd\n"}, "abcd", ""},
		{"embedded newline splits", []string{"a\nb"}, "a", "b"},
		{"multiple lines at once", []string{"one\ntwo\nthree\n"}, "one|two|three", ""},
		{"empty chunk is a no-op", []string{"abc", ""}, "", "abc"},
		{"continuation across a closed line", []string{"x\n", "y", "z"}, "x", "yz"},
		{"a bare newline is a blank line", []string{"a\n", "\n"}, "a| ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, printed := newCaptured()
			for _, c := range tc.chunks {
				m = step(t, m, outputMsg(c))
			}
			// A multi-line chunk is one Println; count its lines, not its calls.
			got := strings.Join(strings.Split(strings.Join(*printed, "\n"), "\n"), "|")
			if got != tc.printed {
				t.Errorf("printed %q, want %q", got, tc.printed)
			}
			if m.partial != tc.partial {
				t.Errorf("live line = %q, want %q", m.partial, tc.partial)
			}
		})
	}
}

// TestStartTaskDoesNotAddASecondEventListener guards the concurrency bug
// that scrambled output ordering in live use.
//
// Exactly one waitForEvent Cmd may be outstanding at a time. Init starts
// one, and every branch that *consumes* an event re-issues exactly one,
// keeping the count at one. startTask consumes nothing — it is triggered
// by a keypress — so if it also issued a listener there would be two
// goroutines receiving from the same channel, and which one wins is
// undefined. That is precisely what produced a transcript where "model
// reported..." printed before the "step 1:" line that logically precedes
// it, and it compounds: every task started would add another receiver.
func TestStartTaskDoesNotAddASecondEventListener(t *testing.T) {
	blocked := make(chan struct{})
	run := func(ctx context.Context, _ string, _ func(string) bool, _, _ io.Writer) int {
		close(blocked)
		<-ctx.Done()
		return 0
	}

	m := NewModel(run)
	m.input.SetValue("do a thing")
	_, cmd := m.Update(enterKey())

	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("task never started")
	}

	// A Cmd is fine here now that the spinner ticks while a task runs
	// (that Cmd carries a spinner.TickMsg, nothing to do with the events
	// channel) — what must never happen is a *second* listener on
	// events, which is specifically outputMsg/confirmRequestMsg/
	// taskDoneMsg arriving as the executed Cmd's result.
	if cmd != nil {
		switch cmd().(type) {
		case outputMsg, confirmRequestMsg, taskDoneMsg:
			t.Error("startTask added a second listener on the events channel: two concurrent receivers on one channel loses message ordering")
		}
	}
}

// --- end-to-end output path (the live-found regression) --------------

func TestTaskOutputReachesTheScrollbackInSourceOrder(t *testing.T) {
	run := func(_ context.Context, _ string, _ func(string) bool, out, errOut io.Writer) int {
		fmt.Fprint(out, "step 1: asking the model for a command\n")
		// Streamed tokens arrive as bare fragments with no newline
		// between them — the shape that broke rendering.
		for _, fragment := range []string{"UNS", "UPP", "ORTED"} {
			fmt.Fprint(out, fragment)
		}
		fmt.Fprint(out, "\n")
		fmt.Fprint(errOut, "model reported it cannot do this task\n")
		return 1
	}

	m, printed := newCaptured()
	m.run = run
	m.input.SetValue("do the impossible")

	next, cmd := m.handleKey(enterKey())
	m = next.(Model)
	// As above: a spinner Cmd is expected and fine; an events-channel
	// listener is not.
	if cmd != nil {
		switch cmd().(type) {
		case outputMsg, confirmRequestMsg, taskDoneMsg:
			t.Fatal("starting a task must not issue a second event listener: " +
				"two concurrent receivers on the events channel make delivery order undefined")
		}
	}

	for done := false; !done; {
		select {
		case msg := <-m.events:
			if _, isDone := msg.(taskDoneMsg); isDone {
				done = true
			}
			m = step(t, m, msg)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for task events")
		}
	}

	lines := strings.Split(strings.Join(*printed, "\n"), "\n")
	indexOf := func(want string) int {
		for i, line := range lines {
			if strings.Contains(line, want) {
				return i
			}
		}
		t.Fatalf("nothing printed contains %q, got:\n%s", want, strings.Join(lines, "\n"))
		return -1
	}

	for _, split := range []string{"UNS", "UPP", "ORTED"} {
		for _, line := range lines {
			if strings.TrimSpace(line) == split {
				t.Errorf("streamed fragment %q printed as its own line; "+
					"fragments must continue the open line, got:\n%s", split, strings.Join(lines, "\n"))
			}
		}
	}

	echo, step1, unsupported, reported := indexOf("> do the impossible"), indexOf("step 1:"), indexOf("UNSUPPORTED"), indexOf("model reported")
	if !(echo < step1 && step1 < unsupported && unsupported < reported) {
		t.Errorf("output out of source order: echo at %d, step 1 at %d, UNSUPPORTED at %d, model-reported at %d\n%s",
			echo, step1, unsupported, reported, strings.Join(lines, "\n"))
	}
}

// The viewport clips rather than wraps, so without this the header was
// truncated mid-sentence in live use and long command output vanished off
// the right edge.
func TestWrapLineBreaksOnWordBoundaries(t *testing.T) {
	got := wrapLine("the quick brown fox jumps", 10)
	want := []string{"the quick", "brown fox", "jumps"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	for i, line := range got {
		if len([]rune(line)) > 10 {
			t.Errorf("line %d exceeds width: %q", i, line)
		}
	}
}

// A single token longer than the terminal has no word boundary to break on
// and must be split rather than allowed to overflow — long paths and URLs in
// command output are the common case.
func TestWrapLineHardSplitsOverlongTokens(t *testing.T) {
	got := wrapLine("/very/long/path/that/never/breaks", 10)
	if len(got) < 2 {
		t.Fatalf("overlong token was not split: %q", got)
	}
	for _, line := range got {
		if len([]rune(line)) > 10 {
			t.Errorf("piece exceeds width: %q", line)
		}
	}
}

func TestWrapLinePreservesBlankSeparators(t *testing.T) {
	if got := wrapLine("", 40); len(got) != 1 || got[0] != "" {
		t.Errorf("blank line became %q; blank lines separate tasks in the transcript", got)
	}
}

func TestWrapLineLeavesShortLinesAlone(t *testing.T) {
	if got := wrapLine("short", 40); len(got) != 1 || got[0] != "short" {
		t.Errorf("got %q, want [short]", got)
	}
}

// --- scrolling with arrow keys (the wheel, in the alternate screen) ------

// --- model warm-up ------------------------------------------------------

func TestWarmupShowsAStatusLineUntilItFinishes(t *testing.T) {
	m := NewModel(noopRunner)
	m.warm = func(context.Context) error { return nil }
	m.warming = true

	if v := m.View().Content; !strings.Contains(v, "loading the model") {
		t.Errorf("view should say the model is loading, got:\n%s", v)
	} else if !strings.Contains(v, m.input.View()) {
		// Compared against the input's own rendering: the cursor is drawn over
		// the placeholder's first letter, so the literal text is split by styling.
		t.Errorf("the prompt must stay visible while warming, got:\n%s", v)
	}

	m = step(t, m, warmDoneMsg{})
	if m.warming {
		t.Fatal("warmDoneMsg did not clear the warming state")
	}
	if v := m.View().Content; strings.Contains(v, "loading the model") {
		t.Errorf("the loading line should be gone once warm-up finishes, got:\n%s", v)
	}
}

func TestInitRunsTheWarmupOnce(t *testing.T) {
	calls := make(chan struct{}, 4)
	m := NewModel(noopRunner)
	m.warm = func(context.Context) error { calls <- struct{}{}; return nil }
	m.warming = true

	// Run every Cmd Init returns that resolves to warmDoneMsg, the way bubbletea would.
	done := false
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				// waitForEvent blocks on the events channel; skip anything that would.
				ch := make(chan tea.Msg, 1)
				go func(s tea.Cmd) { ch <- s() }(sub)
				select {
				case r := <-ch:
					if _, ok := r.(warmDoneMsg); ok {
						done = true
					}
				case <-time.After(200 * time.Millisecond):
				}
			}
		case warmDoneMsg:
			done = true
		}
	}
	walk(m.Init())

	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("Init never ran the warm-up")
	}
	if !done {
		t.Error("the warm-up Cmd did not report warmDoneMsg")
	}
}

func TestNoWarmupMeansNoStatusLine(t *testing.T) {
	m := NewModel(noopRunner)
	if v := m.View().Content; strings.Contains(v, "loading the model") {
		t.Errorf("no warm-up configured, yet the view mentions loading, got:\n%s", v)
	}
}

// --- printing into the scrollback ---------------------------------------

// The echo, the verdict, and the task's own lines must reach the scrollback in
// the order they happened, so all of them are queued through the events channel.
func TestVerdictIsQueuedBeforeTheTaskCanContinue(t *testing.T) {
	m, _ := newCaptured()
	m = step(t, m, confirmRequestMsg("run it anyway?"))
	m = step(t, m, typeKey('y'))

	select {
	case msg := <-m.events:
		if msg != outputMsg("y\n") {
			t.Errorf("queued %#v, want the verdict line", msg)
		}
	default:
		t.Error("the verdict was not queued for printing")
	}
	if ok := <-m.answers; !ok {
		t.Error("answer should be yes")
	}
}

func TestTaskDoneClosesAnOpenLineAndLeavesABlankSeparator(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	m = step(t, m, outputMsg("half a line"))
	m = step(t, m, taskDoneMsg{code: 0})
	got := strings.Split(strings.Join(*printed, "\n"), "\n")
	if len(got) != 2 || got[0] != "half a line" || strings.TrimSpace(got[1]) != "" {
		t.Errorf("printed %q, want the open line then a blank separator", got)
	}
	if m.partial != "" {
		t.Errorf("live line = %q after the task ended", m.partial)
	}
}

func TestQuittingClearsTheLiveRegion(t *testing.T) {
	m := NewModel(noopRunner)
	m = step(t, m, ctrlC())
	if got := m.View().Content; got != "" {
		t.Errorf("view after quit = %q, want empty so no stale prompt is left behind", got)
	}
}

func TestMouseCommandExplainsThereIsNothingToToggle(t *testing.T) {
	m := NewModel(noopRunner)
	m.input.SetValue("mouse")
	m = step(t, m, enterKey())
	if m.running {
		t.Fatal("the mouse command must not be sent to the model as a task")
	}
	msg := <-m.events
	if !strings.Contains(string(msg.(outputMsg)), "nothing to toggle") {
		t.Errorf("got %#v", msg)
	}
}

// --- presenting the loop's events (compact view, details on Ctrl+O) --------

func ev(kind loopevent.Kind, mut func(*loopevent.Event)) eventMsg {
	e := loopevent.Event{Kind: kind}
	if mut != nil {
		mut(&e)
	}
	return eventMsg(e)
}

func command(step int, cmd string) eventMsg {
	return ev(loopevent.Command, func(e *loopevent.Event) { e.Step, e.Command, e.Tokens = step, cmd, 12 })
}

func result(stdout string, exit int) eventMsg {
	return ev(loopevent.Result, func(e *loopevent.Event) { e.Stdout, e.ExitCode = stdout, exit })
}

func answer(text string) eventMsg {
	return ev(loopevent.Answer, func(e *loopevent.Event) { e.Text = text })
}

func joined(printed *[]string) string { return strings.Join(*printed, "\n") }

// A task that ends in an answer shows the answer and a note of what ran. The
// command and its raw output stay out of the way until asked for.
func TestCompactViewShowsTheAnswerNotTheCommandOrItsOutput(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	for _, msg := range []tea.Msg{command(1, "free -h | grep Mem"), result("Mem: 7.7Gi 5.0Gi\n", 0), answer("About 5 GB of memory is in use."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	got := joined(printed)
	if !strings.Contains(got, "About 5 GB of memory is in use.") {
		t.Errorf("the answer was not printed:\n%s", got)
	}
	for _, hidden := range []string{"free -h", "Mem: 7.7Gi", "tokens"} {
		if strings.Contains(got, hidden) {
			t.Errorf("%q leaked into the compact view:\n%s", hidden, got)
		}
	}
	if !strings.Contains(got, "Ran 1 command") || !strings.Contains(got, "Ctrl+O") {
		t.Errorf("no pointer to the details:\n%s", got)
	}
}

// With no answer the raw output is the only result, so it must show.
func TestCompactViewFallsBackToRawOutputWhenThereIsNoAnswer(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\nb\n", 0), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	got := joined(printed)
	if !strings.Contains(got, "$ ls /tmp") || !strings.Contains(got, "  a\n  b") {
		t.Errorf("the output was hidden although nothing else answered:\n%s", got)
	}
	if strings.Contains(got, "Ran 1 command") {
		t.Error("the details pointer is pointless once the details are already shown")
	}
}

func TestProblemAfterASuccessfulStepShowsTheOutputFirst(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	m = step(t, m, command(1, "ls"))
	m = step(t, m, result("x\n", 0))
	m = step(t, m, ev(loopevent.Problem, func(e *loopevent.Event) {
		e.Text = "I couldn't put the result into words this time."
		e.Detail = "deadline exceeded"
	}))
	got := joined(printed)
	if strings.Index(got, "  x") < 0 || strings.Index(got, "  x") > strings.Index(got, "couldn't put") {
		t.Errorf("the raw output should precede the problem:\n%s", got)
	}
	if !strings.Contains(got, "deadline exceeded") {
		t.Errorf("the cause is missing:\n%s", got)
	}
}

// An approval must always name the command, even in the compact view.
func TestApprovalAlwaysShowsTheCommand(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	m = step(t, m, command(1, "rm keep.txt"))
	m = step(t, m, ev(loopevent.Approval, func(e *loopevent.Event) {
		e.Command, e.Text = "rm keep.txt", "This can't be undone — rm deletes files."
	}))
	got := joined(printed)
	if !strings.Contains(got, "$ rm keep.txt") || !strings.Contains(got, "can't be undone") {
		t.Errorf("the approval did not show what is being approved:\n%s", got)
	}
}

func TestLiveLineShowsTheCommandWhileItRuns(t *testing.T) {
	m, _ := newCaptured()
	m.running = true
	m = step(t, m, command(1, "du -sh ."))
	if v := m.View().Content; !strings.Contains(v, "du -sh .") {
		t.Errorf("the live region does not show what is running:\n%s", v)
	}
	m = step(t, m, result("4K\n", 0))
	if v := m.View().Content; strings.Contains(v, "du -sh .") {
		t.Errorf("the finished command is still shown as running:\n%s", v)
	}
}

// Ctrl+O after an answer prints what was behind it, including model cost.
func TestCtrlOShowsTheLastTasksDetailsAndTogglesBack(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	before := len(*printed)

	m = step(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = step(t, m, <-m.events) // the toggle travels through the events channel
	if !m.verbose {
		t.Fatal("ctrl+o did not turn details on")
	}
	got := strings.Join((*printed)[before:], "\n")
	for _, want := range []string{"Details on", "$ ls /tmp", "  a", "12 tokens"} {
		if !strings.Contains(got, want) {
			t.Errorf("details missing %q:\n%s", want, got)
		}
	}

	m = step(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = step(t, m, <-m.events)
	if m.verbose {
		t.Error("a second ctrl+o did not turn details off")
	}
}

// With details on, later tasks print each command and its output as they happen.
func TestVerboseModePrintsStepsAsTheyHappen(t *testing.T) {
	m, printed := newCaptured()
	m.verbose = true
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	got := joined(printed)
	if !strings.Contains(got, "$ ls /tmp") || !strings.Contains(got, "  a") || !strings.Contains(got, "One file.") {
		t.Errorf("verbose output incomplete:\n%s", got)
	}
	if strings.Contains(got, "Ran 1 command") {
		t.Error("the pointer to details is redundant when they are on")
	}
	if strings.Count(got, "$ ls /tmp") != 1 {
		t.Errorf("the command was printed more than once:\n%s", got)
	}
}

func TestBlocksAreSeparatedByABlankLineButNotFromTheEcho(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	m = step(t, m, ev(loopevent.Notice, func(e *loopevent.Event) { e.Text = "first" }))
	m = step(t, m, ev(loopevent.Notice, func(e *loopevent.Event) { e.Text = "second" }))
	got := *printed
	if len(got) != 2 || got[0] != "first" || got[1] != "\nsecond" {
		t.Errorf("printed %q, want the first block bare and the second set apart", got)
	}
}

func TestMsgWriterEmitsEvents(t *testing.T) {
	events := make(chan tea.Msg, 1)
	msgWriter{events: events}.Emit(loopevent.Event{Kind: loopevent.Answer, Text: "hi"})
	if got, ok := (<-events).(eventMsg); !ok || got.Text != "hi" {
		t.Errorf("got %#v", got)
	}
}

// --- cancellation honesty (found live 2026-09-28) ---------------------
//
// A command that had already run — and possibly changed something — must
// never be reported as if Ctrl+C stopped it before it happened. Live, a
// folder-creation task was cancelled while its trailing summary call was
// still in flight; the folder had already been created, but the transcript
// led with "Cancelling this task" before the command was even shown,
// reading as if nothing had run.

// Ctrl+C must not print anything into the scrollback immediately: the
// in-flight step may finish, or already have, and the real picture is only
// known once the goroutine actually stops.
func TestCtrlCPrintsNothingImmediately(t *testing.T) {
	m, printed := newCaptured()
	m.running, m.cancelTask = true, func() {}
	m = step(t, m, ctrlC())
	if !m.cancelled {
		t.Error("cancelled flag was not set")
	}
	if len(*printed) != 0 {
		t.Errorf("ctrl+c printed %q before the task actually stopped", *printed)
	}
	if v := m.View().Content; !strings.Contains(v, "stopping") {
		t.Errorf("the live line should say a step is being finished, got:\n%s", v)
	}
}

func TestCancelledBeforeAnythingRanSaysSo(t *testing.T) {
	m, printed := newCaptured()
	m.running, m.cancelled = true, true
	m = step(t, m, taskDoneMsg{})
	got := joined(printed)
	if !strings.Contains(got, "Cancelled before anything ran") {
		t.Errorf("got %q", got)
	}
}

// The case that matters: a step already completed — and may have changed
// something — before Ctrl+C landed. That must be shown, not hidden behind a
// flat "cancelled" line.
func TestCancelledAfterAStepRanShowsItRatherThanHidingIt(t *testing.T) {
	m, printed := newCaptured()
	m.running = true
	m = step(t, m, command(1, "mkdir hello_world"))
	m = step(t, m, result("", 0))
	m.cancelled = true
	m = step(t, m, taskDoneMsg{})
	got := joined(printed)
	if !strings.Contains(got, "$ mkdir hello_world") {
		t.Errorf("the step that already ran was not shown:\n%s", got)
	}
	if !strings.Contains(got, "1 step had already run") {
		t.Errorf("the cancellation message should say a step already ran:\n%s", got)
	}
	// The command must appear before the cancellation notice, not after —
	// the live-found bug was exactly this ordering.
	if strings.Index(got, "$ mkdir") > strings.Index(got, "Cancelled") {
		t.Errorf("cancellation notice printed before the step it refers to:\n%s", got)
	}
}

func TestCancelledFlagResetsOnTheNextTask(t *testing.T) {
	m, _ := newCaptured()
	m.cancelled = true
	m.run = noopRunner
	m.input.SetValue("do a thing")
	m = step(t, m, enterKey())
	if m.cancelled {
		t.Error("a stale cancelled flag from a previous task leaked into the next one")
	}
}

// --- neat mode (purely a rendering choice; memory is unaffected) ---------

// While neat is on, nothing reaches the scrollback: everything the task
// writes is drawn in the live region instead.
func TestNeatModeKeepsOutputOffTheScrollback(t *testing.T) {
	m, printed := newCaptured()
	m.neat = true
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	if len(*printed) != 0 {
		t.Errorf("neat mode printed to the scrollback: %q", *printed)
	}
	if v := m.View().Content; !strings.Contains(v, "One file.") {
		t.Errorf("the live region should show the answer, got:\n%s", v)
	}
}

// The whole point: starting a new task drops the previous turn from the live
// region rather than appending to it.
func TestNeatModeReplacesThePreviousTurnOnANewTask(t *testing.T) {
	m, _ := newCaptured()
	m.neat = true
	m.run = noopRunner
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	if !strings.Contains(m.View().Content, "One file.") {
		t.Fatal("setup failed: the first turn's answer should still be showing")
	}

	m.input.SetValue("second task")
	m = step(t, m, enterKey())
	m = step(t, m, <-m.events) // drain the echo
	if strings.Contains(m.View().Content, "One file.") {
		t.Errorf("the previous turn is still showing after a new task started:\n%s", m.View().Content)
	}
	if !strings.Contains(m.View().Content, "> second task") {
		t.Errorf("the new task's echo should be showing, got:\n%s", m.View().Content)
	}
}

// Toggling neat mode itself always reaches the scrollback — it is a note
// about the UI, not part of the conversation neat mode trims — and it works
// in both directions.
func TestNeatCommandTogglesAndAlwaysPrintsToScrollback(t *testing.T) {
	m, printed := newCaptured()
	m.input.SetValue("neat")
	m = step(t, m, enterKey())
	if !m.neat {
		t.Fatal("neat command did not turn the mode on")
	}
	if len(*printed) != 1 || !strings.Contains((*printed)[0], "Neat mode on") {
		t.Errorf("printed = %q", *printed)
	}

	m.input.SetValue("neat")
	m = step(t, m, enterKey())
	if m.neat {
		t.Fatal("a second neat command did not turn the mode off")
	}
	if len(*printed) != 2 || !strings.Contains((*printed)[1], "Neat mode off") {
		t.Errorf("printed = %q", *printed)
	}
}

// Turning neat mode off mid-session must not retroactively dump the trimmed
// history into the scrollback — the trade was made per turn, on purpose.
func TestTurningNeatOffDoesNotBackfillScrollback(t *testing.T) {
	m, printed := newCaptured()
	m.neat = true
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	m.input.SetValue("neat")
	m = step(t, m, enterKey())
	all := joined(printed)
	if strings.Contains(all, "One file.") {
		t.Errorf("turning neat off backfilled trimmed history into the scrollback:\n%s", all)
	}
}

// Ctrl+O is unaffected by neat mode: it still expands the current turn's
// detail, just in the live region instead of the scrollback.
func TestCtrlOStillWorksInNeatMode(t *testing.T) {
	m, printed := newCaptured()
	m.neat = true
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	m = step(t, m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m = step(t, m, <-m.events)
	if !m.verbose {
		t.Fatal("ctrl+o did not turn details on in neat mode")
	}
	if len(*printed) != 0 {
		t.Errorf("details leaked into the scrollback while neat mode was on: %q", *printed)
	}
	if v := m.View().Content; !strings.Contains(v, "$ ls /tmp") {
		t.Errorf("details should show in the live region, got:\n%s", v)
	}
}

// --- scratch mode's header (D44) ------------------------------------------

func TestNewScratchModelHeaderSaysNothingCarriesOverAndOmitsTheMemoryHint(t *testing.T) {
	m := NewScratchModel(noopRunner)
	if !strings.Contains(m.header, "Scratch mode") {
		t.Errorf("header should name the mode, got:\n%s", m.header)
	}
	if strings.Contains(m.header, "Follow-ups can refer back") {
		t.Errorf("scratch mode's header claimed follow-up resolution it does not have:\n%s", m.header)
	}
}

// Everything else about scratch mode's header — the neat-mode hint, the
// general instructions — must be unchanged from the ordinary header.
func TestNewScratchModelKeepsTheNeatModeHint(t *testing.T) {
	m := NewScratchModel(noopRunner)
	if !strings.Contains(m.header, "Type neat for a mode") {
		t.Errorf("scratch mode's header dropped the neat-mode hint, got:\n%s", m.header)
	}
}

func TestNewModelHeaderIsUnchangedByScratchModesExistence(t *testing.T) {
	m := NewModel(noopRunner)
	if !strings.Contains(m.header, "Follow-ups can refer back") {
		t.Errorf("the ordinary header's memory hint is missing, got:\n%s", m.header)
	}
	if strings.Contains(m.header, "Scratch mode") {
		t.Errorf("the ordinary header should never mention scratch mode, got:\n%s", m.header)
	}
}

// The two constructors must otherwise build an identical, working Model —
// scratch mode is a difference in what the header says, not in any
// rendering, gating, or event-handling behavior.
func TestScratchModelBehavesLikeAnOrdinaryModelOtherwiseIdentically(t *testing.T) {
	m, printed := newCaptured()
	m.header = NewScratchModel(noopRunner).header // isolate the one intended difference
	m.running = true
	for _, msg := range []tea.Msg{command(1, "ls /tmp"), result("a\n", 0), answer("One file."), taskDoneMsg{}} {
		m = step(t, m, msg)
	}
	if !strings.Contains(joined(printed), "One file.") {
		t.Errorf("scratch-mode model did not render an answer normally, got:\n%s", joined(printed))
	}
}
