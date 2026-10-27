package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
)

// answerServer runs one step ("echo hi", then DONE) and serves the reply call
// with reply, recording whether each kind of call asked to stream. reply
// writes the reply call's whole HTTP body, so a test can break it off.
type answerServer struct {
	*httptest.Server
	mu           sync.Mutex
	stepStreamed bool
	replyStreams []bool
}

func newAnswerServer(t *testing.T, reply func(w http.ResponseWriter, stream bool)) *answerServer {
	t.Helper()
	a := &answerServer{}
	steps := []string{"echo hi", "DONE"}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct{ Stream bool }
		json.Unmarshal(body, &req)
		a.mu.Lock()
		defer a.mu.Unlock()
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			a.replyStreams = append(a.replyStreams, req.Stream)
			reply(w, req.Stream)
			return
		}
		a.stepStreamed = a.stepStreamed || req.Stream
		if len(steps) == 0 {
			t.Error("the step was asked for more often than scripted")
			return
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: steps[0], Done: true, EvalCount: 1})
		steps = steps[1:]
	}))
	return a
}

// streamReply serves parts as Ollama streams them, closed by a done record, or
// as one object when the request did not ask to stream.
func streamReply(parts ...string) func(http.ResponseWriter, bool) {
	return func(w http.ResponseWriter, stream bool) {
		if !stream {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: strings.Join(parts, ""), Done: true})
			return
		}
		for _, p := range parts {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: p})
			w.(http.Flusher).Flush()
		}
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Done: true})
	}
}

// With an event consumer the reply arrives in pieces, after everything the
// loop ran, and is then given whole: the pieces add up to exactly the Answer,
// and the Answer is the last event.
func TestRunLoopEventsStreamTheReply(t *testing.T) {
	parts := []string{"\n The fo", "lder holds ", "one fi", "le."}
	srv := newAnswerServer(t, streamReply(parts...))
	defer srv.Close()

	evs, opt := collectEvents()
	var out, errOut bytes.Buffer
	if code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "", opt); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("text was written alongside the events: %q %q", out.String(), errOut.String())
	}
	var got []string
	answers := 0
	for i, e := range *evs {
		switch e.Kind {
		case loopevent.AnswerPart:
			if answers > 0 {
				t.Errorf("event %d: a reply piece after the Answer", i)
			}
			got = append(got, e.Text)
		case loopevent.Answer:
			answers++
			if e.Text != "The folder holds one file." {
				t.Errorf("Answer = %q, want the assembled reply, trimmed", e.Text)
			}
			if i != len(*evs)-1 {
				t.Errorf("the Answer is event %d of %d, want the last", i+1, len(*evs))
			}
		case loopevent.Command, loopevent.Result:
			if len(got) > 0 {
				t.Errorf("event %d (%v) came after the reply had started", i, e.Kind)
			}
		}
	}
	if answers != 1 {
		t.Errorf("%d Answer events, want 1", answers)
	}
	if !slices.Equal(got, parts) {
		t.Errorf("reply pieces = %q, want %q", got, parts)
	}
	if srv.stepStreamed {
		t.Error("a step was asked for as a stream: commands must arrive whole")
	}
	if len(srv.replyStreams) != 1 || !srv.replyStreams[0] {
		t.Errorf("reply requests streamed = %v, want one streamed request", srv.replyStreams)
	}
}

// A reply that breaks off is not a reply. The pieces already given stay what
// they were, unconfirmed, no Answer follows them, and the task that ran is
// still reported as it ended: the reply is best effort.
func TestRunLoopEventsDropAReplyThatBreaksOff(t *testing.T) {
	cases := map[string]func(http.ResponseWriter, bool){
		// Closes without the done record Ollama always ends a stream with.
		"no done record": func(w http.ResponseWriter, _ bool) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "The folder ho"})
		},
		// A line that is not JSON in the middle of the stream.
		"garbled stream": func(w http.ResponseWriter, _ bool) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "The folder ho"})
			io.WriteString(w, "{\"response\": \"ld\n")
		},
		// The server fails after the first piece was sent.
		"connection dropped": func(w http.ResponseWriter, _ bool) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "The folder ho"})
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		},
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newAnswerServer(t, reply)
			defer srv.Close()
			evs, opt := collectEvents()
			var out, errOut bytes.Buffer
			code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, "", opt)
			if code != 0 {
				t.Errorf("exit code = %d, want 0: the command ran, only the reply failed", code)
			}
			for _, e := range *evs {
				if e.Kind == loopevent.Answer {
					t.Errorf("an Answer %q followed a reply that broke off", e.Text)
				}
			}
			if k := kinds(*evs); len(k) < 2 || k[0] != loopevent.Command || k[1] != loopevent.Result {
				t.Errorf("event kinds = %v, want the step reported first", k)
			}
		})
	}
}

// Text output cannot take back what it printed, so CLI and REPL keep asking
// for the reply whole, and print it once.
func TestRunLoopTextModeDoesNotStreamTheReply(t *testing.T) {
	srv := newAnswerServer(t, streamReply("The folder ", "holds one file."))
	defer srv.Close()
	var out, errOut bytes.Buffer
	if code := runLoop(context.Background(), ollama.New(srv.URL), "m", "say hi", neverConfirm(t), &out, &errOut, ""); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if len(srv.replyStreams) != 1 || srv.replyStreams[0] {
		t.Errorf("reply requests streamed = %v, want one request, not streamed", srv.replyStreams)
	}
	if n := strings.Count(out.String(), "The folder holds one file."); n != 1 {
		t.Errorf("the reply appears %d times in the output, want once:\n%s", n, out.String())
	}
}

// `run --json` keeps the record types a script reads: the pieces of a reply
// are not written, and the answer record carries the whole of it.
func TestJSONEmitterLeavesOutReplyPieces(t *testing.T) {
	var buf bytes.Buffer
	j := newJSONEmitter(&buf)
	j.Emit(loopevent.Event{Kind: loopevent.AnswerPart, Text: "The fo"})
	j.Emit(loopevent.Event{Kind: loopevent.AnswerPart, Text: "lder."})
	j.Emit(loopevent.Event{Kind: loopevent.Answer, Text: "The folder."})
	j.End(outcomeComplete, 0)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d records, want answer and end:\n%s", len(lines), buf.String())
	}
	var r jsonRecord
	if err := json.Unmarshal([]byte(lines[0]), &r); err != nil || r.Type != "answer" || r.Text != "The folder." {
		t.Errorf("first record = %s (%v), want the whole answer", lines[0], err)
	}
}

// Layer 7 scores what the person was told, and a reply told in pieces is told
// once.
func TestToldCountsAStreamedReplyOnce(t *testing.T) {
	evs := []loopevent.Event{
		{Kind: loopevent.AnswerPart, Text: "There are "},
		{Kind: loopevent.AnswerPart, Text: "2 warnings."},
		{Kind: loopevent.Answer, Text: "There are 2 warnings."},
	}
	if got := told(evs); got != "There are 2 warnings.\n" {
		t.Errorf("told = %q, want the reply once", got)
	}
}
