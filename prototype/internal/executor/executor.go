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
)

// Result is the outcome of running a command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// Err is set when the process could not be started or run at all (for
	// example the shell binary is missing). It is distinct from a nonzero
	// ExitCode, which means the command ran and reported failure normally.
	Err error
}

// Run executes cmd through "sh -c", capturing stdout and stderr separately.
// ctx controls cancellation; pass context.Background() for no timeout.
func Run(ctx context.Context, cmd string) Result {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

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
