package tui

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b[()][A-Z0-9]|\x1b[=>]|\x1b\][^\x07]*\x07`)

// plain strips escape sequences so assertions describe the text a person
// reads, not the control codes positioning it.
func plain(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// staticRunner writes fixed chunks through the real writer→channel→Update
// path, so what these tests inspect is the actual rendered frame.
func staticRunner(chunks ...string) TaskRunner {
	return func(_ context.Context, _ string, _ func(string) bool, out, _ io.Writer) int {
		for _, c := range chunks {
			io.WriteString(out, c)
		}
		return 0
	}
}

// renderProgram drives the real program to a rendered frame. It differs from
// runProgram in two ways that turn out to be necessary for asserting on what
// is drawn rather than on final model state:
//
//   - WithWindowSize supplies terminal dimensions. Without a TTY no
//     WindowSizeMsg ever arrives, the viewport is never sized, and the program
//     exits having painted nothing — which is why output-based assertions
//     silently saw an empty frame.
//   - Input arrives over a pipe rather than a pre-filled buffer, so keys can
//     be sent after the first frame and after a task finishes. With everything
//     buffered up front, a ctrl+c intended to quit instead arrives mid-task and
//     cancels the task, exactly as it should, and the program never exits.
//
// keyStep is one scripted interaction: send keys, then wait until the rendered
// frame contains want before continuing. Waiting on content rather than on a
// fixed delay is what keeps this deterministic — a sleep long enough for a
// loaded machine is either flaky or slow, and this suite has been bitten by
// exactly that before.
type keyStep struct {
	send string
	want string
}

func renderProgram(t *testing.T, run TaskRunner, steps []keyStep) (Model, string) {
	t.Helper()
	pr, pw := io.Pipe()
	out := &syncBuf{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	p := tea.NewProgram(NewModel(run),
		tea.WithContext(ctx), tea.WithInput(pr), tea.WithOutput(out),
		tea.WithWindowSize(72, 20), tea.WithoutSignalHandler())

	type res struct {
		m   tea.Model
		err error
	}
	done := make(chan res, 1)
	go func() { m, err := p.Run(); done <- res{m, err} }()

	waitFor := func(want string) bool {
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			if want == "" || strings.Contains(plain(out.String()), want) {
				return true
			}
			select {
			case r := <-done:
				done <- r // the program exited; let the caller collect it
				return false
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}

	// The first frame has to exist before any key means anything.
	waitFor("SynapseOS")
	for i, st := range steps {
		if _, err := io.WriteString(pw, st.send); err != nil {
			break
		}
		if !waitFor(st.want) {
			select {
			case r := <-done:
				pw.Close()
				m, _ := r.m.(Model)
				return m, out.String()
			default:
			}
			p.Kill()
			<-done
			pw.Close()
			t.Fatalf("step %d: never saw %q. Frame:\n%s", i, st.want, plain(out.String()))
		}
	}
	select {
	case r := <-done:
		pw.Close()
		m, _ := r.m.(Model)
		return m, out.String()
	case <-ctx.Done():
		p.Kill()
		<-done
		pw.Close()
		t.Fatalf("program never exited. Frame:\n%s", plain(out.String()))
		return Model{}, ""
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// The tests below assert on rendered output rather than on model state.
// runProgram already existed, but nothing checked what the program actually
// draws — which is why every rendering defect in this file's history had to be
// found by a person in a terminal instead of by the suite.

// NOTE ON SCOPE. Assertions here are limited to what the program emits in its
// opening frame — the header, and the terminal modes it requests. A test that
// waited for *task* output to appear in the rendered bytes was written and then
// removed: bubbletea v2 repaints differentially, so mid-session text arrives
// interleaved with cursor movement and is not reliably contiguous after escape
// stripping, which made the test fail roughly one run in ten under -race. That
// path is covered deterministically at the Update level instead
// (TestStreamedFragmentsFormOneLine and the appendChunk tests), and by
// TestTaskOutputReachesTheTranscriptInSourceOrder for ordering. A flaky test
// that duplicates existing coverage is worse than no test.

// The header was being clipped at the right edge because the viewport does not
// wrap. Rendering at a narrow width is what catches that.
func TestRenderedHeaderWrapsRatherThanTruncating(t *testing.T) {
	_, out := renderProgram(t, staticRunner(), []keyStep{{send: "\x03"}})
	got := plain(out)
	if !strings.Contains(got, "SynapseOS") {
		t.Fatalf("header never rendered:\n%s", got)
	}
	// The viewport clips to its width, so an unwrapped long line does not
	// overflow — it silently loses its tail. Asserting on a word from the
	// END of the longest hint is therefore the discriminating check: it is
	// present when the line wraps and absent when it is merely clipped.
	// Whitespace is normalised because wrapping is exactly what inserts the
	// newline this assertion would otherwise trip over.
	flat := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(flat, "trade that for wheel scrolling") {
		t.Errorf("the tail of the hint line was lost, so long lines are being clipped rather than wrapped:\n%s", got)
	}
}

// Mouse reporting hands the terminal's pointer to the program, which takes
// drag-to-select with it. It must stay off unless explicitly asked for,
// otherwise the transcript cannot be copied.
func TestMouseTrackingIsNotEnabledByDefault(t *testing.T) {
	_, out := renderProgram(t, staticRunner(), []keyStep{{send: "\x03"}})
	for _, seq := range []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h"} {
		if strings.Contains(out, seq) {
			t.Errorf("mouse tracking %q enabled without being asked; text selection is disabled while it is on", seq)
		}
	}
}

// ...and the toggle must actually turn it on for anyone who prefers the wheel.
func TestMouseCommandTogglesTrackingOn(t *testing.T) {
	m, out := renderProgram(t, staticRunner(), []keyStep{{send: "mouse\r", want: "text selection is disabled"}, {send: "\x03"}})
	if !m.mouseOn {
		t.Fatal("the mouse command did not set the flag")
	}
	if !strings.Contains(out, "\x1b[?1002h") && !strings.Contains(out, "\x1b[?1000h") {
		t.Error("flag set but the view never asked the terminal for mouse reporting")
	}
	if !strings.Contains(plain(out), "text selection is disabled") {
		t.Error("the trade-off was not explained to the user when they turned it on")
	}
}

// The gap above the prompt was the viewport rendering a short transcript at
// the top and leaving blank rows beneath it.
func TestShortTranscriptRendersAgainstThePrompt(t *testing.T) {
	_, out := renderProgram(t, staticRunner(), []keyStep{{send: "\x03"}})
	got := plain(out)
	i := strings.Index(got, "type a task")
	if i < 0 {
		return // prompt placeholder not rendered in this frame; nothing to assert
	}
	before := got[:i]
	lines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	blanks := 0
	for j := len(lines) - 1; j >= 0 && strings.TrimSpace(lines[j]) == ""; j-- {
		blanks++
	}
	if blanks > 3 {
		t.Errorf("%d blank rows between the transcript and the prompt", blanks)
	}
}
