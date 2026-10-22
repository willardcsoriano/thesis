package main

// Layer 7's scoring (layer7_test.go, behind the live tag). It lives in an
// untagged file so its own tests run in every `go test`, not only in live runs:
// these helpers decide every number Layer 7 reports, so they are the part that
// most needs checking without a model.
//
// A read-only task leaves no trace in the fixture and is scored on what the
// person was told. That used to be the transcript with the loop's own
// bookkeeping scraped out by line prefix (retrospective.md, 2026-10-23), which made
// three verifiers vacuous: "did it report 2 warnings" was satisfied by
// "step 2:", and "did it report 4" by the 4 in a latency. The loop now hands
// Layer 7 typed events (internal/loopevent), and told() keeps only what reaches
// the person: what the commands printed, the answer, and notices. Step numbers,
// token counts and latencies are fields of other events and never reach it.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"synapseos/internal/loopevent"
)

// layer7Recorder collects the events of one Layer 7 run.
type layer7Recorder struct {
	events []loopevent.Event
}

func (r *layer7Recorder) emit(e loopevent.Event) { r.events = append(r.events, e) }

// told is what the person was told: each command's standard output, the
// answer, and notices, in order. Standard error is left out, as it was when
// the CLI transcript was scored: the CLI writes it to a separate stream.
func told(evs []loopevent.Event) string {
	var b strings.Builder
	add := func(s string) {
		if s == "" {
			return
		}
		b.WriteString(s)
		if !strings.HasSuffix(s, "\n") {
			b.WriteByte('\n')
		}
	}
	for _, e := range evs {
		switch e.Kind {
		case loopevent.Result:
			if !e.NotRun {
				add(e.Stdout)
			}
		case loopevent.Answer, loopevent.Notice:
			add(e.Text)
		}
	}
	return b.String()
}

// narrate renders a run's events for a failure log: everything, labelled, so a
// miss can be diagnosed. It is never scored.
func narrate(evs []loopevent.Event) string {
	var b strings.Builder
	for _, e := range evs {
		switch e.Kind {
		case loopevent.Command:
			fmt.Fprintf(&b, "step %d: %s\n", e.Step, e.Command)
		case loopevent.Result:
			if e.NotRun {
				fmt.Fprintf(&b, "  not run: %s\n", strings.TrimSpace(e.Stderr))
				continue
			}
			b.WriteString(e.Stdout)
			if e.Stderr != "" {
				fmt.Fprintf(&b, "  stderr: %s\n", strings.TrimSpace(e.Stderr))
			}
			fmt.Fprintf(&b, "  exit %d\n", e.ExitCode)
		case loopevent.Answer:
			fmt.Fprintf(&b, "answer: %s\n", e.Text)
		case loopevent.Notice:
			fmt.Fprintf(&b, "notice: %s\n", e.Text)
		case loopevent.Problem:
			fmt.Fprintf(&b, "problem: %s (%s)\n", e.Text, e.Detail)
		case loopevent.Approval:
			fmt.Fprintf(&b, "approval: %s\n", e.Command)
		}
	}
	return b.String()
}

// reports checks that a number appears in what the person was told as a value
// rather than as a digit inside some larger number: "60" must not be
// satisfied by "160" or by a timestamp.
func reports(told, want string) bool {
	for _, f := range strings.FieldsFunc(told, func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.'
	}) {
		if f == want || strings.TrimSuffix(f, ".") == want {
			return true
		}
	}
	return false
}

// sizePattern matches a human-readable size ("296K", "1.2M") or a raw byte
// count large enough to be a fixture's filler rather than an incidental small
// number.
var sizePattern = regexp.MustCompile(`\b\d+(\.\d+)?\s?[KMGT]i?B?\b|\b[1-9]\d{5,}\b`)

// reportsSize accepts any of the forms du or df can produce, because the unit
// depends on which flag the model chose and the task did not specify one.
func reportsSize(told string) bool {
	return sizePattern.MatchString(told)
}

// Each case is one that was mis-scored when the transcript was scraped, or the
// structural guarantee that replaced it.
func TestLayer7Scoring(t *testing.T) {
	// A two-step run: grep printed 2, then the loop finished. Step numbers,
	// tokens and latency are all 2 or 4 here on purpose.
	run := []loopevent.Event{
		{Kind: loopevent.Command, Step: 2, Command: "grep -c WARN app.log", Tokens: 4, Latency: 4 * time.Second},
		{Kind: loopevent.Result, Step: 2, Command: "grep -c WARN app.log", Stdout: "2\n", ExitCode: 0},
		{Kind: loopevent.Note, Text: "Task complete in 2 step(s)."},
	}
	// The same run, but the command printed nothing.
	silent := []loopevent.Event{
		{Kind: loopevent.Command, Step: 2, Command: "grep -c WARN app.log", Tokens: 4, Latency: 4 * time.Second},
		{Kind: loopevent.Result, Step: 2, Command: "grep -c WARN app.log", ExitCode: 1},
		{Kind: loopevent.Problem, Text: "the command failed 2 times", Detail: "exit 1"},
		{Kind: loopevent.Note, Text: "Task complete in 2 step(s)."},
	}

	t.Run("only output, answers and notices are told", func(t *testing.T) {
		if got := told(run); got != "2\n" {
			t.Errorf("told = %q, want only the command's output", got)
		}
		got := told(append(append([]loopevent.Event{}, run...),
			loopevent.Event{Kind: loopevent.Answer, Text: "There are 2 warnings."},
			loopevent.Event{Kind: loopevent.Notice, Text: "Nothing was changed."}))
		if got != "2\nThere are 2 warnings.\nNothing was changed.\n" {
			t.Errorf("told = %q", got)
		}
	})

	t.Run("a step number, token count or latency does not satisfy a count", func(t *testing.T) {
		for _, want := range []string{"2", "4"} {
			if reports(told(silent), want) {
				t.Errorf("reports(%q) accepted bookkeeping from a run that printed nothing: %q", want, told(silent))
			}
		}
		if !reports(told(run), "2") {
			t.Error("reports() rejected a genuine count")
		}
	})

	t.Run("a refused command's message is not output", func(t *testing.T) {
		refused := []loopevent.Event{{Kind: loopevent.Result, NotRun: true, Stderr: "blocked: 3 files would be lost", ExitCode: 1}}
		if got := told(refused); got != "" {
			t.Errorf("told = %q, want nothing from a command that did not run", got)
		}
	})

	t.Run("standard error is not output", func(t *testing.T) {
		if got := told([]loopevent.Event{{Kind: loopevent.Result, Stderr: "du: 296K", ExitCode: 1}}); got != "" {
			t.Errorf("told = %q", got)
		}
	})

	t.Run("a digit inside a larger number does not count", func(t *testing.T) {
		if reports("160\n", "60") {
			t.Error("reports() matched 60 inside 160")
		}
		if !reports("60\n", "60") || !reports("The total is 60.\n", "60") {
			t.Error("reports() rejected an exact match")
		}
	})

	t.Run("size is recognised in any unit du or df might use", func(t *testing.T) {
		for _, ok := range []string{"296K\t.\n", "1.2M\t.\n", "304096\t.\n", "296 KB\n", "/dev/sda1  100G  40G  55G  43% /\n", "1.8T\n"} {
			if !reportsSize(ok) {
				t.Errorf("reportsSize() missed %q", ok)
			}
		}
		for _, bad := range []string{"", "nothing here\n", "4\t.\n"} {
			if reportsSize(bad) {
				t.Errorf("reportsSize() accepted %q", bad)
			}
		}
	})

	t.Run("narrate shows everything for diagnosis", func(t *testing.T) {
		got := narrate(silent)
		for _, want := range []string{"step 2: grep -c WARN app.log", "exit 1", "problem: the command failed 2 times"} {
			if !strings.Contains(got, want) {
				t.Errorf("narrate() lacks %q:\n%s", want, got)
			}
		}
	})
}
