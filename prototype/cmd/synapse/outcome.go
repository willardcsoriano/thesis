package main

// A task's outcome is recorded once, as the study log's task_end outcome, and
// the process exit code is derived from it here, so the two cannot disagree.
// Before this table runLoop returned only 0 or 1 and a declined confirmation
// exited 0, so a script read "the user said no" as success.
const (
	outcomeComplete        = "complete"
	outcomeNothingToDo     = "nothing_to_do"
	outcomeDeclined        = "declined"
	outcomeUnsupported     = "unsupported"
	outcomeStepLimit       = "step_limit"
	outcomeProposeError    = "propose_error"
	outcomeExecutionError  = "execution_error"
	outcomeRepeatedFailure = "repeated_failure"
	// outcomeRepeatedSuccess: the model proposed the command that had just
	// succeeded instead of DONE, so the loop stopped and answered. Exits 0
	// like complete, but a study counts it apart: the model did not finish.
	outcomeRepeatedSuccess = "repeated_success"
	// outcomeReversedStep: the model proposed moving back what the step before
	// had just moved, so it was not run and the task was answered. Exits 0, and
	// is counted apart from complete for the same reason as repeated_success.
	outcomeReversedStep = "reversed_step"
	outcomeInterrupted  = "interrupted"
	// outcomeGaveUp: a command was tried, failed, and the model found no other.
	outcomeGaveUp = "gave_up"
	// outcomeCaptureFailed: the copy undo depends on could not be saved, so the
	// command did not run.
	outcomeCaptureFailed = "capture_failed"
	// outcomeNotUndoable: the task ran, but a step's undo entry could not be
	// written, so what it did cannot be taken back.
	outcomeNotUndoable = "not_undoable"
	// outcomeNoWorkingDir: the working folder no longer exists; nothing ran.
	outcomeNoWorkingDir = "no_working_directory"
	// outcomeModelUnavailable is the pre-flight check failing, before any
	// task starts; it never appears in the study log.
	outcomeModelUnavailable = "model_unavailable"
	outcomeIncomplete       = "incomplete" // left by a path that set nothing; a bug if ever seen
)

// Exit codes. 2 is exitUsage (cli.go). 130 follows the shell's 128+SIGINT.
const (
	exitOK               = 0
	exitFailed           = 1
	exitDeclined         = 3
	exitUnsupported      = 4
	exitStepLimit        = 5
	exitModelUnavailable = 6
	// exitNotUndoable: the task ran, but a step could not be recorded for
	// undo. Not 1, which would say the command failed (it did not), and not
	// 0, which would say undo can take it back (it cannot).
	exitNotUndoable = 7
	exitInterrupted = 130
)

// exitCodeFor maps an outcome to the process exit code. An outcome it does not
// know is a failure, never success.
func exitCodeFor(outcome string) int {
	switch outcome {
	case outcomeComplete, outcomeNothingToDo, outcomeRepeatedSuccess, outcomeReversedStep:
		return exitOK
	case outcomeDeclined:
		return exitDeclined
	case outcomeUnsupported:
		return exitUnsupported
	case outcomeStepLimit:
		return exitStepLimit
	case outcomeProposeError, outcomeModelUnavailable:
		return exitModelUnavailable
	case outcomeInterrupted:
		return exitInterrupted
	case outcomeNotUndoable:
		return exitNotUndoable
	}
	return exitFailed
}
