package effects

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const maxResolveOutput = 8 << 20

// DefaultRunner runs resolution commands. The analyser has already proven each one
// read-only, so the sandbox is defence in depth: where bubblewrap works, the command
// runs with the whole filesystem mounted read-only, network and other namespaces
// unshared, and dies with its parent. Without bubblewrap it runs directly, relying
// on the proof alone. A command that exits non-zero is not an error; running out of
// time, or producing more than 8 MiB, is.
//
// Every command also runs under a system-call trace (nettrace.go), and one that takes
// input from outside the files, in any of its processes, fails with ErrOutsideInput:
// a socket to the network or a name server, a connection to a local daemon. In the
// sandbox such a command gets nothing, or something else, where the real run gets the
// server's answer, so what it printed cannot stand for what it will print
// (docs/analyzer-defects.md, defect 13). Without a working tracer every run fails
// with ErrNoTracer, so nothing is resolved and every line that needs it asks.
func DefaultRunner(timeout time.Duration) Runner {
	bw := bwrapPath()
	tr := tracer()
	return func(ctx context.Context, wd string, argv []string) ([]byte, error) {
		// A missing binary must be an error. Under bubblewrap it would otherwise
		// surface as a non-zero exit with empty output, indistinguishable from
		// "matched nothing", and the analysis would fail open.
		if _, err := exec.LookPath(argv[0]); err != nil {
			return nil, err
		}
		if tr == nil {
			return nil, ErrNoTracer
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		full := tr.wrap(argv)
		if bw != "" {
			full = append(sandboxArgv(bw, wd), full...)
		}
		var out bytes.Buffer
		lw := &limitWriter{w: &out, n: maxResolveOutput}
		res, err := tr.run(ctx, wd, full, lw)
		// Checked first: the run is stopped at the first outside input, and a read from
		// the network can otherwise outlast the timeout.
		if res.outside != "" {
			return nil, fmt.Errorf("%w: %s", ErrOutsideInput, res.outside)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, err
		}
		if lw.exceeded {
			return nil, fmt.Errorf("output exceeds %d bytes", maxResolveOutput)
		}
		return out.Bytes(), nil
	}
}

// sandboxArgv is the bubblewrap prefix a resolver runs under in wd.
func sandboxArgv(bw, wd string) []string {
	return []string{bw, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
		"--unshare-all", "--die-with-parent", "--chdir", wd, "--"}
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
