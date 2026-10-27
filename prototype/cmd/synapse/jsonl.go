package main

import (
	"encoding/json"
	"io"
	"sync"

	"synapseos/internal/loopevent"
)

// jsonRecord is one line of `synapse run --json`. Every line carries "type";
// the other fields appear only where that type sets them, except exit_code,
// which is a pointer so a successful command's 0 is written rather than
// dropped. The last line is always {"type":"end",...} with the task's outcome
// and the process's exit code, so a consumer never has to infer how it ended.
type jsonRecord struct {
	Type      string `json:"type"`
	Step      int    `json:"step,omitempty"`
	Command   string `json:"command,omitempty"`
	Text      string `json:"text,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	NotRun    bool   `json:"not_run,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
}

// jsonEmitter writes loop events as JSON Lines. It is safe for concurrent use:
// the loop and an interrupt watcher may both report.
type jsonEmitter struct {
	mu  sync.Mutex
	enc *json.Encoder
	err error // the first write error; later writes are skipped
}

func newJSONEmitter(w io.Writer) *jsonEmitter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // commands contain < > &; keep them readable
	return &jsonEmitter{enc: enc}
}

func (j *jsonEmitter) write(r jsonRecord) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return
	}
	j.err = j.enc.Encode(r)
}

// Emit records one loop event. The pieces of a streamed reply are not
// written: a consumer of `run --json` reads records, not a live screen, and the
// answer record that follows them carries the whole reply. Keeping them out
// also leaves the record types a script sees what they were.
func (j *jsonEmitter) Emit(e loopevent.Event) {
	if e.Kind == loopevent.AnswerPart {
		return
	}
	r := jsonRecord{
		Type:      e.Kind.String(),
		Step:      e.Step,
		Command:   e.Command,
		Text:      e.Text,
		Detail:    e.Detail,
		Stdout:    e.Stdout,
		Stderr:    e.Stderr,
		TimedOut:  e.TimedOut,
		NotRun:    e.NotRun,
		Tokens:    e.Tokens,
		LatencyMS: e.Latency.Milliseconds(),
	}
	if e.Kind == loopevent.Result {
		code := e.ExitCode
		r.ExitCode = &code
	}
	j.write(r)
}

// End writes the closing record.
func (j *jsonEmitter) End(outcome string, exitCode int) {
	j.write(jsonRecord{Type: "end", Outcome: outcome, ExitCode: &exitCode})
}

// Err reports the first write failure, such as a closed pipe.
func (j *jsonEmitter) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.err
}
