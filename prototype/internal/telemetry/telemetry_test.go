package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func decode(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line is not valid JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// A nil Logger must be safe. This is what lets runLoop be instrumented
// unconditionally; if it panicked, every call site would need a branch and
// eventually one would be missed.
func TestNilLoggerIsNoOp(t *testing.T) {
	var l *Logger
	for _, err := range []error{
		l.TaskStart("t1", "prompt"),
		l.CommandIssued("t1", 1, "ls"),
		l.CommandResult("t1", 1, "ls", CommandOutcome{Latency: time.Second}),
		l.ConfirmationTriggered("t1", 1, "rm -rf x", "rm", true),
		l.UndoInvoked("t1", "rm -rf x", UndoApplied, ""),
		l.TaskEnd("t1", "done", 1),
	} {
		if err != nil {
			t.Fatalf("nil logger returned error: %v", err)
		}
	}
}

func TestEveryEventTypeCarriesRequiredFields(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P07")
	l.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }

	if err := l.TaskStart("T3", "gather the PDFs"); err != nil {
		t.Fatal(err)
	}
	l.CommandIssued("T3", 1, "find . -name '*.pdf'")
	l.CommandResult("T3", 1, "find . -name '*.pdf'", CommandOutcome{Latency: 1500 * time.Millisecond})
	l.ConfirmationTriggered("T3", 2, "rm -rf tmp", "rm", false)
	l.UndoInvoked("T3", "rm -rf tmp", UndoFailed, "backup missing")
	l.TaskEnd("T3", "declined", 2)

	evs := decode(t, &b)
	if len(evs) != 6 {
		t.Fatalf("got %d events, want 6", len(evs))
	}
	for i, ev := range evs {
		for _, f := range []string{"timestamp_ms", "event_type", "participant_id", "condition", "task_id"} {
			if _, ok := ev[f]; !ok {
				t.Errorf("event %d (%v) missing required field %q", i, ev["event_type"], f)
			}
		}
		if ev["participant_id"] != "P07" || ev["condition"] != "A" || ev["task_id"] != "T3" {
			t.Errorf("event %d has wrong session fields: %v", i, ev)
		}
	}
	want := []string{"task_start", "command_issued", "command_result", "confirmation_triggered", "undo_invoked", "task_end"}
	for i, w := range want {
		if evs[i]["event_type"] != w {
			t.Errorf("event %d is %v, want %s", i, evs[i]["event_type"], w)
		}
	}
}

// A successful command exits 0, and 0 is the zero value. With a plain int
// field and omitempty, every success would silently lose its exit code and the
// error-rate metric would be computed from a hole. The pointer is the fix; this
// test is what stops someone "simplifying" it back.
func TestExitCodeZeroIsRecordedNotOmitted(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	l.CommandResult("T1", 1, "true", CommandOutcome{Latency: time.Millisecond})

	ev := decode(t, &b)[0]
	code, ok := ev["exit_code"]
	if !ok {
		t.Fatal("exit_code omitted for a successful command — every success would be unscoreable")
	}
	if code.(float64) != 0 {
		t.Fatalf("exit_code = %v, want 0", code)
	}
}

func TestZeroValuedFieldsSurvive(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	l.CommandResult("T1", 1, "true", CommandOutcome{})
	l.ConfirmationTriggered("T1", 1, "rm x", "rm", false)

	evs := decode(t, &b)
	if _, ok := evs[0]["latency_ms"]; !ok {
		t.Error("latency_ms omitted when zero")
	}
	approved, ok := evs[1]["approved"]
	if !ok {
		t.Fatal("approved omitted when false — a declined confirmation would look unrecorded")
	}
	if approved.(bool) {
		t.Error("approved should be false")
	}
}

func TestInapplicableFieldsAreOmitted(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	l.TaskStart("T1", "do a thing")

	ev := decode(t, &b)[0]
	for _, f := range []string{"exit_code", "latency_ms", "approved", "command"} {
		if _, ok := ev[f]; ok {
			t.Errorf("task_start should not carry %q", f)
		}
	}
}

func TestAllThreeUndoOutcomesAreLoggable(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	for _, o := range []UndoOutcome{UndoApplied, UndoDeclined, UndoFailed} {
		l.UndoInvoked("T1", "rm x", o, "")
	}
	evs := decode(t, &b)
	for i, want := range []string{"applied", "declined", "failed"} {
		if evs[i]["outcome"] != want {
			t.Errorf("outcome %d = %v, want %s", i, evs[i]["outcome"], want)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteErrorSurfaces(t *testing.T) {
	l := New(failingWriter{}, "P01")
	if err := l.TaskStart("T1", "x"); err == nil {
		t.Fatal("write failure was swallowed")
	}
}

type syncCounter struct {
	bytes.Buffer
	n int
}

func (s *syncCounter) Sync() error { s.n++; return nil }

func TestEveryEventIsFlushed(t *testing.T) {
	s := &syncCounter{}
	l := New(s, "P01")
	l.TaskStart("T1", "x")
	l.CommandIssued("T1", 1, "ls")
	if s.n != 2 {
		t.Fatalf("synced %d times for 2 events; unflushed events are lost on a crash", s.n)
	}
}

func TestConcurrentWritesProduceIntactLines(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); l.CommandIssued("T1", i, "ls -la") }(i)
	}
	wg.Wait()
	if evs := decode(t, &b); len(evs) != 50 {
		t.Fatalf("got %d intact lines, want 50", len(evs))
	}
}

// The raw code and the SIGPIPE marker are recorded only when they add
// information, but they must always be present when normalisation changed
// the answer — otherwise the decision to treat 141 as success becomes
// irreversible after the sessions are gone.
func TestNormalisedResultKeepsTheRawSignal(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	l.CommandResult("T1", 1, "find / | head -1", CommandOutcome{
		ExitCode: 0, RawExitCode: 141, SIGPIPE: true, Latency: time.Second,
	})
	ev := decode(t, &b)[0]
	if ev["exit_code"].(float64) != 0 {
		t.Errorf("exit_code = %v, want the normalised 0", ev["exit_code"])
	}
	raw, ok := ev["raw_exit_code"]
	if !ok {
		t.Fatal("raw_exit_code dropped — the normalisation is now unauditable")
	}
	if raw.(float64) != 141 {
		t.Errorf("raw_exit_code = %v, want 141", raw)
	}
	if ev["sigpipe"] != true {
		t.Errorf("sigpipe = %v, want true", ev["sigpipe"])
	}
}

// An ordinary result should not carry a redundant duplicate of its own exit
// code on every line.
func TestUnnormalisedResultOmitsRedundantFields(t *testing.T) {
	var b bytes.Buffer
	l := New(&b, "P01")
	l.CommandResult("T1", 1, "ls", CommandOutcome{ExitCode: 0, RawExitCode: 0})
	ev := decode(t, &b)[0]
	for _, f := range []string{"raw_exit_code", "sigpipe"} {
		if _, ok := ev[f]; ok {
			t.Errorf("unnormalised result should not carry %q", f)
		}
	}
}
