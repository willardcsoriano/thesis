// Package claudecode is the Claude Code driver of the agent command study
// (docs/agent-command-study.md, "Drivers"). It runs the real CLI headless
// inside bubblewrap and turns its stream-json output into probe.Records.
//
// The facts this package relies on about the CLI (flags, event schema, the
// token variable) were checked against the official docs and one real
// capture; NOTES.md lists each with its source.
package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"unicode/utf8"

	"synapseos/internal/probe"
)

// Session is everything Parse reads from one turn's stream.
type Session struct {
	SessionID      string // from the init event; the id a follow-up resumes
	Model          string // exact model id the CLI reports in its init event
	Version        string // CLI version (init's claude_code_version)
	PermissionMode string // as the CLI reports it; "bypassPermissions" inside the sandbox
	Result         *Result
	Records        []probe.Record
}

// Result is the stream's final result event. Nil when the stream ended
// without one (the CLI was killed, or crashed).
type Result struct {
	Subtype  string   `json:"subtype"` // "success", "error_max_turns", "error_during_execution", ...
	IsError  bool     `json:"is_error"`
	NumTurns int      `json:"num_turns"`
	Errors   []string `json:"errors,omitempty"`
	Text     string   `json:"result,omitempty"` // the final message; for an error, what went wrong
	Denials  int      `json:"-"`                // tool calls the CLI refused (permission_denials)
}

// event is the subset of a stream-json line this package reads. Unknown
// fields and unknown event types are ignored.
type event struct {
	Type           string          `json:"type"`
	Subtype        string          `json:"subtype"`
	SessionID      string          `json:"session_id"`
	Model          string          `json:"model"`
	Version        string          `json:"claude_code_version"`
	PermissionMode string          `json:"permissionMode"`
	Message        json.RawMessage `json:"message"`
	ToolUseResult  json.RawMessage `json:"tool_use_result"`
	IsError        bool            `json:"is_error"`
	NumTurns       int             `json:"num_turns"`
	Errors         []string        `json:"errors"`
	ResultText     string          `json:"result"`
	Denials        []any           `json:"permission_denials"`
}

type message struct {
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"` // a string for a plain prompt, an array of blocks otherwise
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // string, or an array of {type,text} blocks
	IsError   bool            `json:"is_error"`
}

// Parse reads one turn's stream-json and returns a Record per tool call, in
// the order the calls appear. base supplies the run, task, tier and turn;
// Parse fills Kind, Command, Step, Exit and, from the init event, the model
// and CLI versions.
func Parse(stream io.Reader, base probe.Record) ([]probe.Record, error) {
	s, err := ParseSession(stream, base)
	return s.Records, err
}

// ParseSession is Parse that also returns the session metadata.
//
// Every tool call becomes a Record. Bash is Kind "bash" with the command
// string; any other tool (Write, Edit, Read, Glob, Grep, NotebookEdit, but
// also Task, WebFetch, TodoWrite ...) is Kind = the tool's name with its
// input JSON as Command, so scoring can choose what to count. Calls made by
// a subagent appear in the main stream and are recorded too.
//
// A line that is not a JSON object in valid UTF-8 is an error, as is a
// second result event, a tool call without a name, or a Bash call whose
// input is not an object: a truncated or corrupted stream must be noticed,
// not silently shortened. The records parsed before the bad line are
// returned with the error. Empty lines and unknown event types are skipped.
//
// A tool_result goes to the oldest call with its id that has none yet, so a
// repeated id cannot move a result onto a later call; an empty id names no
// call.
func ParseSession(stream io.Reader, base probe.Record) (s Session, err error) {
	// Records are returned with an error too, completed with the init
	// metadata: a turn that went bad half way still made the calls before.
	defer s.fill()
	pending := map[string][]int{} // tool_use id -> calls still waiting for their result, oldest first
	r := bufio.NewReader(stream)
	for line := 1; ; line++ {
		raw, rerr := r.ReadBytes('\n')
		if t := bytes.TrimSpace(raw); len(t) > 0 {
			if perr := s.consume(t, base, pending); perr != nil {
				return s, fmt.Errorf("stream line %d: %w", line, perr)
			}
		}
		if errors.Is(rerr, io.EOF) {
			return s, nil
		}
		if rerr != nil {
			return s, rerr
		}
	}
}

// fill gives every record the model and CLI versions the stream reported.
func (s *Session) fill() {
	for i := range s.Records {
		if s.Records[i].ModelVersion == "" {
			s.Records[i].ModelVersion = s.Model
		}
		if s.Records[i].ToolVersion == "" {
			s.Records[i].ToolVersion = s.Version
		}
		if s.Records[i].Model == "" {
			s.Records[i].Model = s.Records[i].ModelVersion
		}
	}
}

func (s *Session) consume(raw []byte, base probe.Record, pending map[string][]int) error {
	if !utf8.Valid(raw) {
		return errors.New("not valid UTF-8")
	}
	if raw[0] != '{' {
		return errors.New("not a JSON object")
	}
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return err
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			s.SessionID, s.Model, s.Version, s.PermissionMode = ev.SessionID, ev.Model, ev.Version, ev.PermissionMode
		}
	case "assistant":
		blocks, err := contentBlocks(ev.Message)
		if err != nil {
			return err
		}
		model := messageModel(ev.Message)
		for _, b := range blocks {
			if !toolCall[b.Type] {
				continue
			}
			if b.Name == "" {
				return fmt.Errorf("%s block without a name", b.Type)
			}
			rec := base
			rec.Step = len(s.Records)
			if model != "" && rec.ModelVersion == "" {
				rec.ModelVersion = model
			}
			if b.Name == "Bash" {
				var in struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(b.Input, &in); err != nil {
					return fmt.Errorf("Bash input: %w", err)
				}
				rec.Kind, rec.Command = probe.KindBash, in.Command
			} else {
				rec.Kind, rec.Command = b.Name, compact(b.Input)
			}
			if b.ID != "" {
				pending[b.ID] = append(pending[b.ID], len(s.Records))
			}
			s.Records = append(s.Records, rec)
		}
	case "user":
		blocks, err := contentBlocks(ev.Message)
		if err != nil {
			return err
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			waiting := pending[b.ToolUseID]
			if len(waiting) == 0 {
				continue // no call with that id is waiting: unknown, early, or answered already
			}
			i := waiting[0]
			pending[b.ToolUseID] = waiting[1:]
			if s.Records[i].Kind == probe.KindBash {
				s.Records[i].Exit = bashExit(b, ev.ToolUseResult, len(blocks) == 1)
			}
		}
	case "result":
		if s.Result != nil {
			return errors.New("a second result event")
		}
		s.Result = &Result{Subtype: ev.Subtype, IsError: ev.IsError, NumTurns: ev.NumTurns, Errors: ev.Errors,
			Text: ev.ResultText, Denials: len(ev.Denials)}
	}
	return nil
}

// toolCall are the content block types that are a call the model made: the
// CLI's own tools, and server-side and MCP tools, which are calls too even
// though no command of ours runs them.
var toolCall = map[string]bool{"tool_use": true, "server_tool_use": true, "mcp_tool_use": true}

func contentBlocks(msg json.RawMessage) ([]block, error) {
	if len(msg) == 0 {
		return nil, nil
	}
	var m message
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil, fmt.Errorf("message: %w", err)
	}
	c := bytes.TrimSpace(m.Content)
	if len(c) == 0 || c[0] != '[' {
		return nil, nil // a plain-text prompt, not tool traffic
	}
	var bs []block
	if err := json.Unmarshal(c, &bs); err != nil {
		return nil, fmt.Errorf("message content: %w", err)
	}
	return bs, nil
}

func messageModel(msg json.RawMessage) string {
	var m message
	_ = json.Unmarshal(msg, &m)
	return m.Model
}

var exitLine = regexp.MustCompile(`^Exit code (-?\d+)`)

// bashExit reads the exit status from a Bash tool_result. The CLI does not
// report the status as a field (BashOutput has none), so it is inferred from
// what the CLI was observed to emit: a failing command comes back with
// is_error true and content starting "Exit code N"; a command that exited 0
// comes back with is_error false and a BashOutput object that was not
// interrupted and not moved to the background. Anything else is nil, never
// a guess. structured is the event's tool_use_result, which describes the
// block only when the event carries a single result (own says so).
func bashExit(b block, structured json.RawMessage, own bool) *int {
	text := resultText(b.Content)
	if m := exitLine.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return &n
		}
	}
	if b.IsError || !own {
		return nil
	}
	var out struct {
		Interrupted      *bool  `json:"interrupted"`
		BackgroundTaskID string `json:"backgroundTaskId"`
	}
	if json.Unmarshal(structured, &out) != nil || out.Interrupted == nil || *out.Interrupted || out.BackgroundTaskID != "" {
		return nil
	}
	zero := 0
	return &zero
}

func resultText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(c, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				return p.Text
			}
		}
	}
	return ""
}

func compact(in json.RawMessage) string {
	var buf bytes.Buffer
	if json.Compact(&buf, in) != nil {
		return string(in)
	}
	return buf.String()
}
