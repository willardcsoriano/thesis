package effects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const maxResolveOutput = 8 << 20

// DefaultRunner runs resolution commands. The analyser has already proven each one
// read-only, so this is defence in depth: where bubblewrap works, the command runs
// with the whole filesystem mounted read-only, network and other namespaces
// unshared, and dies with its parent. Without bubblewrap it runs directly, relying
// on the proof alone. A command that exits non-zero is not an error; running out of
// time, or producing more than 8 MiB, is.
func DefaultRunner(timeout time.Duration) Runner {
	bw := bwrapPath()
	return func(ctx context.Context, wd string, argv []string) ([]byte, error) {
		// A missing binary must be an error. Under bubblewrap it would otherwise
		// surface as a non-zero exit with empty output, indistinguishable from
		// "matched nothing", and the analysis would fail open.
		if _, err := exec.LookPath(argv[0]); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		full := argv
		if bw != "" {
			full = append([]string{bw, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
				"--unshare-all", "--die-with-parent", "--chdir", wd, "--"}, argv...)
		}
		cmd := exec.CommandContext(ctx, full[0], full[1:]...)
		cmd.Dir = wd
		var out bytes.Buffer
		cmd.Stdout = &limitWriter{w: &out, n: maxResolveOutput}
		err := cmd.Run()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var lw *limitWriter
		if lw, _ = cmd.Stdout.(*limitWriter); lw != nil && lw.exceeded {
			return nil, fmt.Errorf("output exceeds %d bytes", maxResolveOutput)
		}
		var ee *exec.ExitError
		if err != nil && !errors.As(err, &ee) {
			return nil, err
		}
		return out.Bytes(), nil
	}
}

// SandboxAvailable reports whether resolvers can run under a read-only
// filesystem. Callers that want resolution only when it is sandboxed check this.
func SandboxAvailable() bool { return bwrapPath() != "" }

var (
	bwrapOnce sync.Once
	bwrapBin  string
)

// bwrapPath probes once per process: starting bubblewrap to test it costs a fork.
func bwrapPath() string {
	bwrapOnce.Do(func() { bwrapBin = probeBwrap() })
	return bwrapBin
}

func probeBwrap() string {
	p, err := exec.LookPath("bwrap")
	if err != nil {
		return ""
	}
	if exec.Command(p, "--ro-bind", "/", "/", "--unshare-all", "--die-with-parent", "true").Run() != nil {
		return ""
	}
	return p
}

type limitWriter struct {
	w        *bytes.Buffer
	n        int
	exceeded bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.w.Len()+len(p) > l.n {
		l.exceeded = true
		return len(p), nil
	}
	return l.w.Write(p)
}
