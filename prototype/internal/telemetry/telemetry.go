// Package telemetry writes the structured event log that is the user study's
// entire dataset (M7). Every claim the thesis makes about task completion
// time, error rate, and recovery behaviour is computed from these lines, so
// the design priority here is not throughput or elegance but never losing an
// event: a gap in this file cannot be reconstructed after the fact, and a
// participant session cannot be re-run.
//
// Format is JSON Lines — one self-describing object per line, appended, never
// rewritten. That choice is deliberate over a single JSON array: an array has
// to be closed to be valid, so a crash mid-session would leave an unparseable
// file, whereas a truncated JSON Lines file loses only its final line and every
// complete line before it still parses. The Python log parser reads it one line
// at a time for the same reason.
//
// Privacy: an Event carries a participant *code*, never a name, and the
// mapping from code to person lives only on the paper consent form. Command
// strings are recorded verbatim because intent-parsing accuracy is scored
// offline against them; study machines therefore hold only synthetic task data
// and no personal accounts, so a logged command cannot capture personal
// content. See the study's data-handling section for the retention rule.
package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// EventType enumerates the six event kinds the study's analysis plan expects.
// The string values are the wire format and are consumed by the offline
// parser — renaming one is a breaking change to the dataset, not a
// refactor.
type EventType string

const (
	TaskStart             EventType = "task_start"
	CommandIssued         EventType = "command_issued"
	CommandResult         EventType = "command_result"
	ConfirmationTriggered EventType = "confirmation_triggered"
	UndoInvoked           EventType = "undo_invoked"
	TaskAnswered          EventType = "task_answered"
	TaskEnd               EventType = "task_end"
)

// Condition identifies the study arm. This logger only ever runs inside
// SynapseOS, so it is always ConditionA; the field exists so that the
// combined dataset is self-describing once native-OS observations, which are
// coded by hand from screen recordings, are merged alongside it.
type Condition string

const ConditionA Condition = "A"

// Event is one logged line. Fields that do not apply to a given event type
// are omitted rather than written empty, so a reader can distinguish "no exit
// code because this is not a result event" from "exit code zero".
type Event struct {
	TimestampMs   int64     `json:"timestamp_ms"`
	Type          EventType `json:"event_type"`
	ParticipantID string    `json:"participant_id"`
	Condition     Condition `json:"condition"`
	TaskID        string    `json:"task_id"`

	Command   string `json:"command,omitempty"`
	LatencyMs *int64 `json:"latency_ms,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	// RawExitCode is the shell's unmodified status, recorded alongside the
	// normalised ExitCode so that the SIGPIPE rule can be revisited during
	// analysis without re-running sessions. SIGPIPE marks that the two
	// differ and why.
	RawExitCode *int   `json:"raw_exit_code,omitempty"`
	SIGPIPE     *bool  `json:"sigpipe,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Approved    *bool  `json:"approved,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Step        int    `json:"step,omitempty"`
}

// Logger appends events for one participant session.
//
// A nil *Logger is a valid no-op logger. That is what lets the execution loop
// be instrumented unconditionally: CLI, REPL, and TUI runs outside the study
// pass nil and pay nothing, while the study build passes a real logger, and
// neither path needs a branch at every call site. Branches at call sites are
// how events go missing.
type Logger struct {
	mu            sync.Mutex
	w             io.Writer
	sync          func() error
	participantID string
	now           func() time.Time
}

// New returns a Logger appending to w for the given participant code. If w
// implements interface{ Sync() error } — as *os.File does — every event is
// flushed to disk as it is written. Buffering would trade the study's
// irreplaceable data for speed that no interactive session needs.
func New(w io.Writer, participantID string) *Logger {
	l := &Logger{w: w, participantID: participantID, now: time.Now}
	if f, ok := w.(interface{ Sync() error }); ok {
		l.sync = f.Sync
	}
	return l
}

// Open creates or appends to a session log file at path.
func Open(path, participantID string) (*Logger, *os.File, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open session log: %w", err)
	}
	return New(f, participantID), f, nil
}

// emit writes one event. Errors are returned rather than swallowed so a
// caller running a real session can fail loudly at startup if the log is
// unwritable — discovering that after a participant has left is the failure
// this package exists to prevent.
func (l *Logger) emit(ev Event) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ev.TimestampMs = l.now().UnixMilli()
	ev.ParticipantID = l.participantID
	ev.Condition = ConditionA
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", ev.Type, err)
	}
	if _, err := l.w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s event: %w", ev.Type, err)
	}
	if l.sync != nil {
		return l.sync()
	}
	return nil
}

// The methods below are deliberately one per event type rather than a single
// exported Log(Event). A shared entry point makes it possible to emit a
// command_result with no exit code, or a confirmation with no verdict; these
// signatures make the required fields of each event type unforgeable.

func (l *Logger) TaskStart(taskID, prompt string) error {
	return l.emit(Event{Type: TaskStart, TaskID: taskID, Detail: prompt})
}

func (l *Logger) CommandIssued(taskID string, step int, command string) error {
	return l.emit(Event{Type: CommandIssued, TaskID: taskID, Step: step, Command: command})
}

// CommandOutcome carries everything known about how one command ended. It is
// a struct rather than a parameter list because the study's analysis plan is
// still open on how to treat edge cases, and a struct can gain a field
// without every call site in the tree changing shape.
type CommandOutcome struct {
	// ExitCode is the status after SIGPIPE normalisation.
	ExitCode int
	// RawExitCode is what the shell actually returned.
	RawExitCode int
	// SIGPIPE reports that the command died of a broken pipe, which under
	// pipefail is an early-exit-pipeline artifact rather than a failure.
	SIGPIPE bool
	Latency time.Duration
}

func (l *Logger) CommandResult(taskID string, step int, command string, o CommandOutcome) error {
	ms := o.Latency.Milliseconds()
	ev := Event{
		Type: CommandResult, TaskID: taskID, Step: step, Command: command,
		ExitCode: &o.ExitCode, LatencyMs: &ms,
	}
	// Only recorded when it adds information: a raw code identical to the
	// normalised one would be noise on every successful line.
	if o.RawExitCode != o.ExitCode || o.SIGPIPE {
		ev.RawExitCode = &o.RawExitCode
		ev.SIGPIPE = &o.SIGPIPE
	}
	return l.emit(ev)
}

// ConfirmationTriggered records that the gate stopped a command, and what the
// participant then chose. Both outcomes are logged: a declined confirmation is
// a substantive observation about whether the gate is understood, not an
// absence of data.
func (l *Logger) ConfirmationTriggered(taskID string, step int, command, verdict string, approved bool) error {
	return l.emit(Event{
		Type: ConfirmationTriggered, TaskID: taskID, Step: step,
		Command: command, Verdict: verdict, Approved: &approved,
	})
}

// UndoOutcome distinguishes the three ways an undo attempt ends. Logging only
// successes would bias the record toward the system working: "the participant
// tried to recover and could not" is exactly what the study needs to see.
type UndoOutcome string

const (
	UndoApplied  UndoOutcome = "applied"
	UndoDeclined UndoOutcome = "declined"
	UndoFailed   UndoOutcome = "failed"
)

func (l *Logger) UndoInvoked(taskID, command string, outcome UndoOutcome, detail string) error {
	return l.emit(Event{
		Type: UndoInvoked, TaskID: taskID, Command: command,
		Outcome: string(outcome), Detail: detail,
	})
}

// TaskAnswered records the natural-language reply the user was actually shown
// (D31). Logged because the study scores whether the system answered the
// request correctly, and that cannot be recovered from the raw command output
// alone — two runs can produce identical stdout and answer the question
// differently.
func (l *Logger) TaskAnswered(taskID, answer string) error {
	return l.emit(Event{Type: TaskAnswered, TaskID: taskID, Detail: answer})
}

func (l *Logger) TaskEnd(taskID, outcome string, steps int) error {
	return l.emit(Event{Type: TaskEnd, TaskID: taskID, Outcome: outcome, Step: steps})
}
