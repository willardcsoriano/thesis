// Package geminicli is the Gemini CLI driver of the agent command study
// (docs/agent-command-study.md, "Drivers"). It runs the real CLI headless
// inside bubblewrap and turns its stream-json output into probe.Records.
//
// It mirrors internal/probe/claudecode and reuses that package's
// CLI-independent pieces (the task workdir, the execve parser, argv
// quoting). The facts this package relies on about the CLI (flags, event
// schema, authentication) were checked against the official docs at the
// installed version's tag and against the installed bundle's source;
// NOTES.md lists each with its source.
package geminicli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"synapseos/internal/probe"
)

// ShellTool is the CLI's shell tool. Its calls are recorded as Kind "bash".
const ShellTool = "run_shell_command"

// Session is everything Parse reads from one turn's stream.
type Session struct {
	SessionID string // from the init event; the id a follow-up resumes
	Model     string // model the CLI reports in its init event (may be an alias such as "auto-...")
	Result    *Result
	Records   []probe.Record
	Warnings  []string // non-fatal "error" events (loop detected, quota warnings, ...)
	Denials   int      // tool calls the CLI's policy engine refused (error type policy_violation)
}

// Result is the stream's final result event. Nil when the stream ended
// without one: the CLI was killed, or it failed before the session started
// (an authentication failure exits 41 with no result event).
type Result struct {
	Status    string   // "success" or "error"
	ErrorType string   // the error's class name, e.g. "FatalAuthenticationError", "TerminalQuotaError"
	Message   string   // the error's message
	Models    []string // models that served requests (keys of stats.models), sorted
	ToolCalls int      // stats.tool_calls
}

// IsError reports whether the turn ended in an error.
func (r *Result) IsError() bool { return r != nil && r.Status != "success" }

// event is the subset of a stream-json line this package reads. Unknown
// fields and unknown event types are ignored.
type event struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id"`
	Model      string          `json:"model"`
	ToolName   string          `json:"tool_name"`
	ToolID     string          `json:"tool_id"`
	Parameters json.RawMessage `json:"parameters"`
	Status     string          `json:"status"`
	Output     *string         `json:"output"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Stats    *struct {
		ToolCalls int                        `json:"tool_calls"`
		Models    map[string]json.RawMessage `json:"models"`
	} `json:"stats"`
}

// Parse reads one turn's stream-json and returns a Record per tool call, in
// the order the calls appear. base supplies the run, task, tier, turn and
// CLI version (the stream does not carry it); Parse fills Kind, Command,
// Step, Exit and the model version.
func Parse(stream io.Reader, base probe.Record) ([]probe.Record, error) {
	s, err := ParseSession(stream, base)
	return s.Records, err
}

// ParseSession is Parse that also returns the session metadata.
//
// Every tool_use event becomes a Record. run_shell_command is Kind "bash"
// with its command string; any other tool (read_file, write_file, replace,
// list_directory, glob, grep_search, web_fetch, ...) is Kind = the tool's
// name with its parameters JSON as Command, so scoring can choose what to
// count.
//
// A line that is not a JSON object in valid UTF-8 is an error, as is a
// second result event, a tool_use without a tool name, or shell parameters
// that are not an object: a truncated or corrupted stream must be noticed,
// not silently shortened. The records parsed before the bad line are
// returned with the error. Empty lines and unknown event types are skipped.
//
// A tool_result goes to the oldest call with its id that has none yet, so a
// repeated id cannot move a result onto a later call; an empty id names no
// call.
func ParseSession(stream io.Reader, base probe.Record) (s Session, err error) {
	// Records are returned with an error too, completed with the model: a
	// turn that went bad half way still made the calls before.
	defer s.fill()
	pending := map[string][]int{} // tool_id -> calls still waiting for their result, oldest first
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

// fill gives every record the model the stream reported.
func (s *Session) fill() {
	model := s.Model
	if s.Result != nil && len(s.Result.Models) == 1 && (model == "" || strings.HasPrefix(model, "auto")) {
		// The init event names the configured model, which may be a
		// routing alias; one model serving every request is the exact id.
		model = s.Result.Models[0]
	}
	for i := range s.Records {
		if s.Records[i].ModelVersion == "" {
			s.Records[i].ModelVersion = model
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
	case "init":
		s.SessionID, s.Model = ev.SessionID, ev.Model
	case "tool_use":
		if ev.ToolName == "" {
			return errors.New("tool_use without a tool_name")
		}
		rec := base
		rec.Step = len(s.Records)
		if ev.ToolName == ShellTool {
			var in struct {
				Command string  `json:"command"`
				DirPath *string `json:"dir_path"`
			}
			if err := json.Unmarshal(ev.Parameters, &in); err != nil {
				return fmt.Errorf("%s parameters: %w", ShellTool, err)
			}
			rec.Kind, rec.Command = probe.KindBash, in.Command
			// Each call runs in a fresh subshell at dir_path (or the
			// workspace), so replay needs it to know where the command ran.
			rec.Dir = in.DirPath
		} else {
			rec.Kind, rec.Command = ev.ToolName, compact(ev.Parameters)
		}
		if ev.ToolID != "" {
			pending[ev.ToolID] = append(pending[ev.ToolID], len(s.Records))
		}
		s.Records = append(s.Records, rec)
	case "tool_result":
		if ev.Error != nil && ev.Error.Type == "policy_violation" {
			s.Denials++
		}
		waiting := pending[ev.ToolID]
		if len(waiting) == 0 {
			return nil // no call with that id is waiting: unknown, early, or answered already
		}
		i := waiting[0]
		pending[ev.ToolID] = waiting[1:]
		if s.Records[i].Kind == probe.KindBash {
			s.Records[i].Exit = shellExit(ev)
		}
	case "error":
		s.Warnings = append(s.Warnings, ev.Severity+": "+ev.Message)
	case "result":
		if s.Result != nil {
			return errors.New("a second result event")
		}
		res := &Result{Status: ev.Status}
		if ev.Error != nil {
			res.ErrorType, res.Message = ev.Error.Type, ev.Error.Message
		}
		if ev.Stats != nil {
			res.ToolCalls = ev.Stats.ToolCalls
			for m := range ev.Stats.Models {
				res.Models = append(res.Models, m)
			}
			sort.Strings(res.Models)
		}
		s.Result = res
	}
	return nil
}

var (
	// Normal mode: the result's output is the command's own output, and
	// only a command that printed nothing and failed is described.
	emptyFailure = regexp.MustCompile(`^Command exited with code: (-?\d+)$`)
	// Debug mode (-d): the output is the text the model sees, "Output: ..."
	// followed by trailer lines. Exit Code appears only when non-zero.
	trailerExit = regexp.MustCompile(`^Exit Code: (-?\d+)$`)
	trailerPGID = regexp.MustCompile(`^Process Group PGID: \d+$`)
	trailerBG   = regexp.MustCompile(`^Background PIDs: `)
	trailerSig  = regexp.MustCompile(`^Signal: `)
)

// shellExit reads the exit status from a run_shell_command tool_result.
// The stream has no exit-code field (NOTES.md, "Stream schema"), so the
// status is read only from the two texts the CLI's shell tool is known to
// produce, and is nil otherwise, never a guess:
//
//   - normal mode: a command that printed nothing and exited non-zero comes
//     back as exactly "Command exited with code: N". Any other output says
//     nothing about the status, a zero exit included.
//   - debug mode: the output ends in trailer lines. "Exit Code: N" before
//     the process-group line is the status; a successful result whose
//     trailer has the process-group line and neither an exit code nor a
//     signal exited 0.
func shellExit(ev event) *int {
	if ev.Output == nil {
		return nil
	}
	out := strings.TrimRight(*ev.Output, "\n")
	if m := emptyFailure.FindStringSubmatch(out); m != nil {
		return atoi(m[1])
	}
	if !strings.HasPrefix(out, "Output: ") {
		return nil
	}
	lines := strings.Split(out, "\n")
	i := len(lines) - 1
	if i < 1 || !trailerPGID.MatchString(lines[i]) {
		return nil // no trailer: the process never started, or this is not debug output
	}
	i--
	if i >= 1 && trailerBG.MatchString(lines[i]) {
		i--
	}
	if i >= 1 && trailerSig.MatchString(lines[i]) {
		return nil
	}
	if m := trailerExit.FindStringSubmatch(lines[i]); i >= 1 && m != nil {
		return atoi(m[1])
	}
	if ev.Status != "success" || ev.Error != nil {
		return nil
	}
	zero := 0
	return &zero
}

func atoi(s string) *int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

func compact(in json.RawMessage) string {
	if len(in) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if json.Compact(&buf, in) != nil {
		return string(in)
	}
	return buf.String()
}
