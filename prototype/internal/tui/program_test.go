package tui

import (
	"bytes"
	"context"
	"fmt"
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
//     WindowSizeMsg ever arrives, and the program can exit having painted
//     nothing — which is why output-based assertions once silently saw an
//     empty frame.
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
// (TestStreamedFragmentsFormOneLine and the appendOutput tests), and by
// TestTaskOutputReachesTheScrollbackInSourceOrder for ordering. A flaky test
// that duplicates existing coverage is worse than no test.

// The header is printed into the scrollback, so the terminal wraps it. A tail
// word is the discriminating check: it is present only if the line was not
// clipped. Whitespace is normalised because wrapping inserts newlines.
func TestRenderedHeaderIsPrintedInFull(t *testing.T) {
	_, out := renderProgram(t, staticRunner(), []keyStep{{send: "\x03"}})
	flat := strings.Join(strings.Fields(plain(out)), " ")
	if !strings.Contains(flat, "SynapseOS") {
		t.Fatalf("header never rendered:\n%s", flat)
	}
	if !strings.Contains(flat, "the whole conversation stays in its scrollback") {
		t.Errorf("the tail of the hint line was lost:\n%s", flat)
	}
}

// Neither the alternate screen nor mouse reporting may ever be requested:
// the first has no scrollback, the second takes native selection and the
// wheel with it. Both are what made the conversation impossible to copy.
func TestProgramNeverRequestsAltScreenOrMouseReporting(t *testing.T) {
	_, out := renderProgram(t, staticRunner("done\n"), []keyStep{{send: "go\r", want: "done"}, {send: "\x03"}})
	for _, seq := range []string{"\x1b[?1049h", "\x1b[?47h", "\x1b[?1000h", "\x1b[?1002h", "\x1b[?1003h"} {
		if strings.Contains(out, seq) {
			t.Errorf("the program emitted %q; it must stay in the normal screen with the mouse left to the terminal", seq)
		}
	}
}

// Every finished line of a long output is printed exactly once and in order.
// This is what puts the whole conversation in the terminal's scrollback; a
// viewport would only ever have shown what fits on screen.
func TestLongOutputIsPrintedOnceAndInOrder(t *testing.T) {
	var chunks []string
	for i := 0; i < 60; i++ {
		chunks = append(chunks, fmt.Sprintf("row-%02d\n", i))
	}
	_, out := renderProgram(t, staticRunner(chunks...), []keyStep{{send: "list\r", want: "row-59"}, {send: "\x03"}})
	got := plain(out)
	last := -1
	for i := 0; i < 60; i++ {
		tag := fmt.Sprintf("row-%02d", i)
		if n := strings.Count(got, tag); n != 1 {
			t.Fatalf("%s appears %d times, want exactly once (printed lines must not be redrawn)", tag, n)
		}
		at := strings.Index(got, tag)
		if at < last {
			t.Fatalf("%s printed out of order", tag)
		}
		last = at
	}
}
