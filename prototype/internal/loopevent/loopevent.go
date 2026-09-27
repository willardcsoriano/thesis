// Package loopevent is the typed seam between the task loop and an interface
// that wants to present its progress itself.
//
// The loop used to narrate everything as text on one io.Writer, so a command,
// its output, the answer, and a warning arrived as the same undifferentiated
// stream and an interface could only tell them apart by scraping line prefixes
// (open-problems.md, row 2). A writer that also implements Emitter receives the
// same information as events instead, and the loop then writes none of it as
// text. Writers that do not implement Emitter get the unchanged text, which is
// what keeps CLI and REPL output identical.
package loopevent

import "time"

// Kind says what an Event is.
type Kind int

const (
	// Command: the model chose a command; Step, Command, Tokens and Latency are set.
	// It is announced before the command is checked or run.
	Command Kind = iota
	// Result: what the last Command did; Stdout, Stderr, ExitCode, TimedOut, NotRun.
	Result
	// Answer: the plain-language reply to the task; Text.
	Answer
	// Notice: something the person should read now; Text.
	Notice
	// Problem: something went wrong; Text says what in plain words, Detail says why.
	Problem
	// Note: bookkeeping only worth showing when the person asks for details; Text.
	Note
	// Approval: a command is about to ask for confirmation; Command, and Text
	// carrying the reason and any warning.
	Approval
)

// Event is one thing the loop wants the interface to know.
type Event struct {
	Kind    Kind
	Step    int
	Command string
	Text    string
	Detail  string

	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
	// NotRun marks a Result for a command that was refused before it ran.
	NotRun bool

	Tokens  int
	Latency time.Duration
}

// Emitter is implemented by a writer whose owner wants events.
type Emitter interface {
	Emit(Event)
}
