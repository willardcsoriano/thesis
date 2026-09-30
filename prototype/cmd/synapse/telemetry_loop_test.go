package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"synapseos/internal/ollama"
	"synapseos/internal/telemetry"
	"synapseos/internal/undo"
)

// readEvents parses a JSON Lines telemetry buffer.
func readEvents(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()
	var evs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("telemetry line is not parseable JSON: %q: %v", line, err)
		}
		evs = append(evs, m)
	}
	return evs
}

func types(evs []map[string]any) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i], _ = e["event_type"].(string)
	}
	return out
}

// M7's definition of done: every event type is written, with all fields, and
// the output is parseable by the offline log parser. This drives the real
// runLoop rather than calling the logger directly — the failure mode that
// matters is an emission site that was never wired, which only an end-to-end
// path can catch.
func TestLoopEmitsFullEventSequence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "doomed.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := scriptedOllamaServer(t, []string{"touch made.txt", "rm doomed.txt", doneSentinel})
	defer server.Close()
	client := ollama.New(server.URL)

	var log bytes.Buffer
	tel := telemetry.New(&log, "P12")
	var out, errOut bytes.Buffer

	code := runLoop(context.Background(), client, "m", "tidy up",
		func(string) bool { return true }, &out, &errOut, "",
		withTelemetry(tel, "T7"))
	if code != 0 {
		t.Fatalf("runLoop exit %d\nstderr: %s", code, errOut.String())
	}

	evs := readEvents(t, &log)
	got := types(evs)
	want := []string{
		"task_start",
		"command_issued", "command_result", // step 1, reversible
		"command_issued", "confirmation_triggered", "command_result", // step 2, gated
		"command_issued", // step 3: DONE
		"task_answered",  // the reply the user actually reads (D31)
		"task_end",
	}
	if len(got) != len(want) {
		t.Fatalf("event sequence = %v,\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %s, want %s\nfull: %v", i, got[i], want[i], got)
		}
	}

	for i, ev := range evs {
		if ev["participant_id"] != "P12" || ev["task_id"] != "T7" || ev["condition"] != "A" {
			t.Errorf("event %d lost its session identity: %v", i, ev)
		}
		if _, ok := ev["timestamp_ms"]; !ok {
			t.Errorf("event %d has no timestamp", i)
		}
	}

	// The gated step must record the command, the classifier's reason, and
	// the participant's answer — RQ2 is answered from exactly these fields.
	conf := evs[4]
	if conf["command"] != "rm doomed.txt" {
		t.Errorf("confirmation_triggered command = %v", conf["command"])
	}
	if conf["verdict"] == nil || conf["verdict"] == "" {
		t.Error("confirmation_triggered recorded no classifier reason")
	}
	if conf["approved"] != true {
		t.Errorf("confirmation_triggered approved = %v, want true", conf["approved"])
	}
	if evs[len(evs)-1]["outcome"] != "complete" {
		t.Errorf("task_end outcome = %v, want complete", evs[len(evs)-1]["outcome"])
	}
}

// A declined gate must still produce a task_end, and must record the refusal.
// Logging only completed tasks would drop every abandonment from the dataset.
func TestDeclinedConfirmationIsRecorded(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := scriptedOllamaServer(t, []string{"rm keep.txt"})
	defer server.Close()

	var log bytes.Buffer
	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "delete it",
		func(string) bool { return false }, &out, &errOut, "",
		withTelemetry(telemetry.New(&log, "P01"), "T1"))

	evs := readEvents(t, &log)
	var conf, end map[string]any
	for _, e := range evs {
		switch e["event_type"] {
		case "confirmation_triggered":
			conf = e
		case "task_end":
			end = e
		}
	}
	if conf == nil {
		t.Fatal("no confirmation_triggered event for a blocked command")
	}
	if conf["approved"] != false {
		t.Errorf("approved = %v, want false", conf["approved"])
	}
	if end == nil || end["outcome"] != "declined" {
		t.Errorf("task_end = %v, want outcome declined", end)
	}
}

// M7 explicitly requires undo telemetry on every outcome, including failure:
// "the participant tried to recover and could not" is a data point.
func TestUndoInvokedIsLoggedOnDecline(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.jsonl")
	if err := undo.AppendJournal(journal, undo.Entry{Command: "rm gone.txt", Dir: dir}); err != nil {
		t.Fatal(err)
	}

	var log bytes.Buffer
	var out, errOut bytes.Buffer
	code := runUndo(func(string) bool { return false }, &out, &errOut, journal,
		telemetry.New(&log, "P05"), "T9")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	evs := readEvents(t, &log)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1 undo_invoked: %v", len(evs), types(evs))
	}
	if evs[0]["event_type"] != "undo_invoked" || evs[0]["outcome"] != "declined" {
		t.Fatalf("event = %v, want undo_invoked/declined", evs[0])
	}
	if evs[0]["command"] != "rm gone.txt" {
		t.Errorf("command = %v", evs[0]["command"])
	}
}

// Telemetry is opt-in: ordinary CLI/REPL/TUI use passes no logger and the
// loop must behave identically.
func TestLoopWithoutTelemetryIsUnaffected(t *testing.T) {
	t.Chdir(t.TempDir())
	server := scriptedOllamaServer(t, []string{"touch a.txt", doneSentinel})
	defer server.Close()
	var out, errOut bytes.Buffer
	if code := runLoop(context.Background(), ollama.New(server.URL), "m", "make a file",
		neverConfirm(t), &out, &errOut, ""); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}

// This is the test whose absence let M7 ship logging nothing. The earlier
// tests in this file call runLoop with withTelemetry directly, which verifies
// the loop's side of the seam and says nothing about whether any entry point
// actually passes the option — and none of them did. Driving runREPL through
// its real environment configuration is what catches that.
func TestREPLEntryPointActuallyEmitsTelemetry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.jsonl")
	t.Setenv("SYNAPSE_SESSION_LOG", logPath)
	t.Setenv("SYNAPSE_PARTICIPANT", "P42")
	t.Setenv("SYNAPSE_TASK_ID", "T1")

	server := scriptedOllamaServer(t, []string{"true", doneSentinel})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runREPL(context.Background(), ollama.New(server.URL), "m", "",
		strings.NewReader("do something harmless\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("no session log written by the REPL entry point: %v", err)
	}
	var buf bytes.Buffer
	buf.Write(raw)
	evs := readEvents(t, &buf)
	if len(evs) == 0 {
		t.Fatal("session log is empty — the entry point did not pass withTelemetry")
	}
	if evs[0]["participant_id"] != "P42" || evs[0]["task_id"] != "T1" {
		t.Errorf("session identity not threaded through: %v", evs[0])
	}
	if got := types(evs); got[0] != "task_start" {
		t.Errorf("first event = %s, want task_start (full: %v)", got[0], got)
	}
}

// REPL and TUI run a whole task set inside one process, so the task pointer
// has to be advanceable mid-session; otherwise every event in a participant's
// session is attributed to one task and per-task timing is unrecoverable.
func TestTaskCommandRetargetsSubsequentEvents(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.jsonl")
	t.Setenv("SYNAPSE_SESSION_LOG", logPath)
	t.Setenv("SYNAPSE_PARTICIPANT", "P42")
	t.Setenv("SYNAPSE_TASK_ID", "T1")

	server := scriptedOllamaServer(t, []string{"true", doneSentinel, "true", doneSentinel})
	defer server.Close()

	var out, errOut bytes.Buffer
	runREPL(context.Background(), ollama.New(server.URL), "m", "",
		strings.NewReader("first task\ntask T5\nsecond task\n"), &out, &errOut)

	raw, _ := os.ReadFile(logPath)
	var buf bytes.Buffer
	buf.Write(raw)
	evs := readEvents(t, &buf)

	seen := map[string]bool{}
	for _, e := range evs {
		seen[e["task_id"].(string)] = true
	}
	if !seen["T1"] || !seen["T5"] {
		t.Fatalf("task ids recorded = %v, want both T1 and T5", seen)
	}
	if !strings.Contains(out.String(), "Now recording events under task T5") {
		t.Error("the task command gave no confirmation to the facilitator")
	}
}

// The defect this pins produced "UNSUPPORTEDstep 1: UNSUPPORTED" in the TUI:
// the model's output was streamed into the transcript and then the status
// line printed the same command again.
func TestStreamingModeDoesNotEchoTheCommandTwice(t *testing.T) {
	t.Chdir(t.TempDir())
	server := streamingOllamaServer(t, []string{"UNSUPPORTED"})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do the impossible",
		neverConfirm(t), &out, &errOut, "", withTokenStreaming(&out))

	if n := strings.Count(out.String(), "UNSUPPORTED"); n != 1 {
		t.Fatalf("command rendered %d times, want 1:\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), "step 1: UNSUPPORTED") {
		t.Errorf("streamed output did not land under its step label:\n%s", out.String())
	}
}

// When the model wraps its answer in fences, the streamed text and the
// command that will actually run differ — and the user must be shown the one
// that will run.
func TestStreamingShowsTheCanonicalCommandWhenItDiffers(t *testing.T) {
	t.Chdir(t.TempDir())
	server := streamingOllamaServer(t, []string{"```bash\n", "touch made.txt\n", "```"})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "make a file",
		neverConfirm(t), &out, &errOut, "", withTokenStreaming(&out))

	if !strings.Contains(out.String(), "command: touch made.txt") {
		t.Errorf("cleaned command not shown though it differs from the streamed text:\n%s", out.String())
	}
}

// ...but on the common path, where they are identical, it must not be
// repeated — that repetition is the original bug.
func TestStreamingOmitsTheCanonicalLineWhenIdentical(t *testing.T) {
	t.Chdir(t.TempDir())
	server := streamingOllamaServer(t, []string{"true"})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do a thing",
		neverConfirm(t), &out, &errOut, "", withTokenStreaming(&out))

	if strings.Contains(out.String(), "command: true") {
		t.Errorf("redundant canonical line printed for an unchanged command:\n%s", out.String())
	}
}

// D31: the system answers, rather than leaving the user to read raw output.
// This is the defect that made the interface feel unresponsive in live use —
// asking it to count files produced "4" and nothing else.
func TestCompletedTaskIsAnsweredInProse(t *testing.T) {
	t.Chdir(t.TempDir())
	server := scriptedOllamaServer(t, []string{"true", doneSentinel})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "how many files are here",
		neverConfirm(t), &out, &errOut, "")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), testAnswerText) {
		t.Fatalf("the task completed without answering the user:\n%s", out.String())
	}
}

// The evidence layer must survive the answer layer. vision.md's principle is
// that the answer is the surface and the raw output sits beneath it — not that
// the raw output is replaced.
func TestAnswerDoesNotReplaceTheEvidence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	server := scriptedOllamaServer(t, []string{"echo marker-value", doneSentinel})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "print the marker",
		neverConfirm(t), &out, &errOut, "")

	got := out.String()
	for _, want := range []string{
		"echo marker-value", // the command that ran
		"marker-value",      // its actual output
		"exit code: 0",      // its status
		testAnswerText,      // and the answer on top
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q from the transcript:\n%s", want, got)
		}
	}
}

// A failing summariser must not turn a successful task into a failed one. The
// work is already done by the time the answer is requested.
func TestAnswerFailureDoesNotFailTheTask(t *testing.T) {
	t.Chdir(t.TempDir())
	var call int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		i := int(atomic.AddInt32(&call, 1)) - 1
		resp := []string{"true", doneSentinel}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: resp[min(i, 1)], EvalCount: 1})
	}))
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "do a thing",
		neverConfirm(t), &out, &errOut, "")
	if code != 0 {
		t.Fatalf("a failed summary failed the whole task: exit %d", code)
	}
	if !strings.Contains(out.String(), "Task complete") {
		t.Error("task completion was not reported when the summary failed")
	}
	if !strings.Contains(errOut.String(), "could not summarise") {
		t.Errorf("the summary failure was swallowed silently:\n%s", errOut.String())
	}
}

// Tests that script an exact number of model calls opt out.
func TestAnswerCanBeSuppressed(t *testing.T) {
	t.Chdir(t.TempDir())
	server := scriptedOllamaServer(t, []string{"true", doneSentinel})
	defer server.Close()

	var out, errOut bytes.Buffer
	runLoop(context.Background(), ollama.New(server.URL), "m", "do a thing",
		neverConfirm(t), &out, &errOut, "", withoutAnswer())
	if strings.Contains(out.String(), testAnswerText) {
		t.Error("withoutAnswer() did not suppress the reply")
	}
}

// A task that fails needs an explanation more than a task that succeeds: the
// user is left with a wall of failed commands and no statement of what went
// wrong. This was missed in the first implementation, which answered only on
// the success path, and was found by running the binary rather than by a test.
func TestFailedTaskIsAlsoAnswered(t *testing.T) {
	t.Chdir(t.TempDir())
	// Never emits DONE, and each proposal differs so the loop exhausts its
	// step cap rather than stopping at the repeated-failure guard — this test
	// is about the step-limit path specifically.
	server := scriptedOllamaServer(t, []string{
		"ls /nonexistent-a", "ls /nonexistent-b", "ls /nonexistent-c",
		"ls /nonexistent-d", "ls /nonexistent-e",
	})
	defer server.Close()

	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "list a missing directory",
		neverConfirm(t), &out, &errOut, "")
	if code == 0 {
		t.Fatal("expected a nonzero exit for a task that never completed")
	}
	if !strings.Contains(out.String(), testAnswerText) {
		t.Fatalf("a failed task left the user with no explanation:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "step limit reached") {
		t.Error("the mechanical failure reason was lost")
	}
}
