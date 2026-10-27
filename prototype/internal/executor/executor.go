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
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Result is the outcome of running a command.
type Result struct {
	Stdout string
	Stderr string
	// ExitCode is the shell's status with one normalisation applied: a
	// SIGPIPE death (141) under pipefail is reported as 0. See RawExitCode
	// and SIGPIPE below for why, and use RawExitCode when the unmodified
	// value matters.
	ExitCode int
	// RawExitCode is exactly what the shell returned, before any
	// normalisation. The study's offline analysis records both so that the
	// normalisation below can be revisited without re-running sessions —
	// a logger must never destroy the signal it was built to capture.
	RawExitCode int
	// SIGPIPE reports that the command was terminated by a broken pipe
	// (status 141) rather than failing. With pipefail enabled this is
	// overwhelmingly an artifact of a pipeline whose consumer exits early
	// — "find … | head -1" kills find once head has what it needs — not a
	// failure of the command. Output is captured into a buffer here, so the
	// final stage of a pipeline cannot itself see a closed pipe; a 141
	// therefore effectively always originates upstream.
	SIGPIPE bool
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
	// StdoutDropped and StderrDropped count the bytes RunCapped did not keep
	// from the middle of each stream. Zero for Run and RunIn, which keep all.
	StdoutDropped, StderrDropped int64
}

// waitDelay bounds how long Run waits for a killed process's I/O pipes to
// drain before forcing them closed. Without this, exec.Cmd's default
// (WaitDelay unset) means a context-triggered kill can still leave Run
// hanging indefinitely if the killed process orphaned a grandchild that
// holds stdout/stderr open — the kill signal alone does not guarantee Run
// returns promptly. A var, not a const, so tests can shrink it.
var waitDelay = 5 * time.Second

// Run executes cmd through "bash -c" in the calling process's current
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
	return run(ctx, dir, cmd, 0)
}

// RunCapped is Run for output nobody controls: the model's commands. It keeps
// the first and last limit/2 bytes of each stream and counts the rest, so a
// command that prints gigabytes costs a bounded amount of memory. The kept
// text says how much was left out. Internal callers whose output feeds a
// backup (a git clean preview) use Run, which keeps everything, because a
// truncated list there would silently shrink the backup.
func RunCapped(ctx context.Context, cmd string, limit int) Result {
	return run(ctx, "", cmd, limit)
}

// run is RunIn with an optional per-stream capture limit (0 keeps all).
func run(ctx context.Context, dir, cmd string, limit int) Result {
	// bash, not sh: the model is prompted to emit bash, and on Debian
	// /bin/sh is dash, which rejects bash-only syntax ([[ ]], arrays,
	// brace expansion, process substitution). Running generated bash under
	// dash turns a valid command into a syntax error that looks like a
	// model failure, which would corrupt the study's intent-parsing error
	// counts. Keep this in step with the prompts in cmd/synapse.
	//
	// pipefail, because the model emits pipelines constantly and without it
	// a pipeline reports the status of its *last* stage only: "ls /missing |
	// wc -l" exits 0 even though ls failed. That is a silent false success,
	// and it biases the study's execution-error count in the flattering
	// direction — the worst possible direction for a thesis measuring
	// whether the system works. The cost is that early-exit pipelines now
	// surface SIGPIPE, which is handled by the normalisation below rather
	// than by leaving real failures invisible.
	c := exec.CommandContext(ctx, "bash", "-o", "pipefail", "-c", cmd)
	c.Dir = dir
	c.WaitDelay = waitDelay

	stdout, stderr := newCapture(limit), newCapture(limit)
	c.Stdout = stdout
	c.Stderr = stderr

	err := c.Run()

	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), StdoutDropped: stdout.dropped, StderrDropped: stderr.dropped}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.RawExitCode = 0
	case errors.As(err, &exitErr):
		res.RawExitCode = exitErr.ExitCode()
	default:
		// The process never ran (e.g. bash not found, context cancelled
		// before start) — there is no exit code to report.
		res.Err = err
		res.RawExitCode = -1
	}

	res.ExitCode = res.RawExitCode
	// 141 is 128+SIGPIPE. Only normalise it when the command did not time
	// out: a killed-on-deadline process reports its own signal status and
	// must keep reading as a failure.
	if res.RawExitCode == 141 && !res.TimedOut {
		res.SIGPIPE = true
		res.ExitCode = 0
	}

	return res
}

// capture is an io.Writer that keeps everything, or, with a limit, the first
// and last limit/2 bytes and a count of what fell between.
type capture struct {
	limit   int
	head    bytes.Buffer
	tail    []byte // ring of the last limit/2 bytes once head is full
	next    int    // where the ring writes next
	full    bool   // the ring has wrapped
	dropped int64
}

func newCapture(limit int) *capture { return &capture{limit: limit} }

func (c *capture) Write(p []byte) (int, error) {
	n := len(p)
	if c.limit <= 0 {
		return c.head.Write(p)
	}
	half := c.limit / 2
	if room := half - c.head.Len(); room > 0 {
		k := min(room, len(p))
		c.head.Write(p[:k])
		p = p[k:]
	}
	if len(p) == 0 {
		return n, nil
	}
	if c.tail == nil {
		c.tail = make([]byte, 0, half)
	}
	for _, b := range p {
		if len(c.tail) < half {
			c.tail = append(c.tail, b)
			continue
		}
		c.full = true
		c.tail[c.next] = b
		c.next = (c.next + 1) % half
		c.dropped++
	}
	return n, nil
}

func (c *capture) String() string {
	if c.tail == nil {
		return c.head.String()
	}
	var b strings.Builder
	b.Write(c.head.Bytes())
	if c.dropped > 0 {
		fmt.Fprintf(&b, "\n[... %d bytes not shown ...]\n", c.dropped)
	}
	if c.full {
		b.Write(c.tail[c.next:])
		b.Write(c.tail[:c.next])
	} else {
		b.Write(c.tail)
	}
	return b.String()
}
