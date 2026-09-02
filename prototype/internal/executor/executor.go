// Package executor runs a proposed shell command and captures its outcome.
//
// It has no opinion about whether a command is safe to run — the classifier
// package decides that, and by the time Run is called (auto-run or after
// user confirmation) that decision has already been made. Keeping the two
// concerns in separate packages means the confirmation gate can be tested
// without spawning real subprocesses, and Run can be tested without pulling
// in the classifier's pattern list.
package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Result is the outcome of running a command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// TimedOut is true when ctx's deadline was reached and the process was
	// killed as a result. ExitCode is -1 in this case (the process was
	// killed by signal, not exited normally) but that alone is
	// indistinguishable from other kill signals, so callers that want to
	// report "this timed out" specifically should check this field rather
	// than inferring it from ExitCode.
	TimedOut bool
	// Err is set when the process could not be started or run at all (for
	// example the shell binary is missing). It is distinct from a nonzero
	// ExitCode, which means the command ran and reported failure normally.
	Err error
}

// waitDelay bounds how long Run waits for a killed process's I/O pipes to
// drain before forcing them closed. Without this, exec.Cmd's default
// (WaitDelay unset) means a context-triggered kill can still leave Run
// hanging indefinitely if the killed process orphaned a grandchild that
// holds stdout/stderr open — the kill signal alone does not guarantee Run
// returns promptly. A var, not a const, so tests can shrink it.
var waitDelay = 5 * time.Second

// Run executes cmd through "sh -c" in the calling process's current
// working directory, capturing stdout and stderr separately. ctx controls
// cancellation; pass context.Background() for no timeout. A context with a
// deadline is what makes a hung command a bounded failure (TimedOut)
// instead of freezing the caller forever.
func Run(ctx context.Context, cmd string) Result {
	return RunIn(ctx, "", cmd)
}

// RunIn is Run, but the command runs in dir instead of the calling
// process's current directory. An empty dir behaves exactly like Run
// (os/exec's own default when Cmd.Dir is unset). This exists for callers
// that need to run a command against a specific directory recorded earlier
// — internal/undo's git-based restores, which may run long after the
// original command and from a different working directory than the one
// it needs to act on.
func RunIn(ctx context.Context, dir, cmd string) Result {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = dir
	c.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		// The process never ran (e.g. sh not found, context cancelled before
		// start) — there is no exit code to report.
		res.Err = err
		res.ExitCode = -1
	}

	return res
}
