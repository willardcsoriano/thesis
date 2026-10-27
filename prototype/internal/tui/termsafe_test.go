package tui

import (
	"strings"
	"testing"

	"synapseos/internal/loopevent"
)

// The TUI shows a command's control sequences instead of obeying them, in its
// output and in the command line itself (docs/cli.md). This had no test until
// the volkswagen audit noticed the claim was unexercised.
func TestTUIShowsControlSequencesInsteadOfObeyingThem(t *testing.T) {
	const hostile = "\x1b]0;owned\x07\x1b[2Jvisible"
	got := renderResult(stepRecord{ran: true, command: "printf x", stdout: hostile, stderr: hostile, exit: 0})
	if strings.Contains(got, "\x1b]0;owned") || strings.Contains(got, "\x1b[2J") || strings.Contains(got, "\x07") {
		t.Fatalf("raw control sequences reached the screen: %q", got)
	}
	if !strings.Contains(got, "^[]0;owned^G") || !strings.Contains(got, "visible") {
		t.Fatalf("the output is not shown as text: %q", got)
	}
}

// Every event's text is shown as text, in both views: a notice, note,
// problem or approval can quote the model's command or a file's name, and the
// status line shows the running command (ship review, 2026-10-27: only
// results, commands and replies were cleaned).
func TestEveryEventTextIsShownNotObeyed(t *testing.T) {
	const hostile = "\x1b]0;owned\x07\x1b[2J"
	for _, verbose := range []bool{false, true} {
		m, printed := newCaptured()
		m.running = true
		m.verbose = verbose
		m = step(t, m, command(1, "printf "+hostile))
		if v := m.View().Content; strings.Contains(v, "\x1b]0;owned") {
			t.Errorf("verbose=%v: the status line obeyed the command's control bytes: %q", verbose, v)
		}
		for _, ev := range []loopevent.Event{
			{Kind: loopevent.Approval, Step: 1, Command: "printf x", Text: "It deletes " + hostile},
			{Kind: loopevent.Note, Text: "Not run again: " + hostile},
			{Kind: loopevent.Notice, Text: "notice " + hostile},
			{Kind: loopevent.Problem, Text: "problem " + hostile, Detail: "detail " + hostile},
		} {
			m = step(t, m, eventMsg(ev))
		}
		out := joined(printed) + m.View().Content
		if strings.Contains(out, "\x1b]0;owned") || strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x07") {
			t.Errorf("verbose=%v: raw control sequences reached the screen: %q", verbose, out)
		}
		if n := strings.Count(out, "^[]0;owned^G"); n < 4 {
			t.Errorf("verbose=%v: only %d of the hostile texts are shown as text: %q", verbose, n, out)
		}
	}
}
