package probe

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"synapseos/internal/oracle"
)

// Traced is what Trace saw of one command.
type Traced struct {
	Outcome  oracle.Outcome
	Duration time.Duration
	// Execve is the basename of argv[0] of every successful execve the command
	// made, in the order strace logged them, with the sandbox's wrapper shell
	// removed (see Trace).
	Execve        []string
	Before, After oracle.Snapshot
	Verdict       oracle.Verdict
}

// Lost reports whether something that existed before the command was removed,
// overwritten or re-moded (oracle.Label's "C").
func (t Traced) Lost() bool { return t.Verdict.Label == "C" }

// Apply copies the capture fields into r: Exit, DurationMS, Execve and Lost. A
// timed-out run has no exit code, so Exit stays nil.
func (t Traced) Apply(r *Record) {
	r.Exit = nil
	if !t.Outcome.TimedOut {
		code := t.Outcome.ExitCode
		r.Exit = &code
	}
	r.DurationMS = t.Duration.Milliseconds()
	r.Execve = t.Execve
	lost := t.Lost()
	r.Lost = &lost
}

// Strace is the strace binary, or empty when it is not installed. The sandbox
// mounts the host root read-only, so the host's strace is the one that runs.
func Strace() string {
	p, err := exec.LookPath("strace")
	if err != nil {
		return ""
	}
	return p
}

// Trace runs cmd in root, an already materialized task folder, inside the
// oracle's sandbox (oracle.ExecuteIn: only root writable, no network, empty
// stdin, a hard time limit) with strace in front of the sandbox's shell. It
// snapshots root before and after and labels the difference, whatever the exit
// status: a command that fails half way may still have destroyed something.
//
// The sandbox always hides the host (oracle.Sandbox.HideHost): the commands
// come from a remote model and what they print goes back to its provider, so
// no home, temp file, username, hostname or environment variable of the
// operator may be readable. Root is seen at oracle.HiddenRoot, and /tmp is a
// tmpfs that dies with the run, so a write there is neither kept nor labelled.
//
// The trace goes to a private temporary directory outside root, bound writable
// at traceDir (under the masked /run, after the masks so it is not shadowed)
// for strace alone, so the task folder's snapshot never contains it. The
// command could in principle write there too; nothing else is kept in it.
//
// Exclusions from Execve: strace is started by bubblewrap, not traced, so its
// own exec never appears. The first successful execve in the trace is the
// sandbox's wrapper shell (bash -c "<ulimits>; <sudo shim>; cmd") and is
// dropped. Everything after it, including a bash the command itself started,
// is kept.
//
// Duration is wall time of the whole sandboxed run, including bubblewrap and
// strace start-up.
func Trace(ctx context.Context, bwrap, strace, root, cmd string, limit time.Duration) (Traced, error) {
	if bwrap == "" || strace == "" {
		return Traced{}, fmt.Errorf("tracing needs bubblewrap and strace")
	}
	if d := hiddenParent(strace); d != "" {
		return Traced{}, fmt.Errorf("strace at %s is under %s, which the sandbox hides", strace, d)
	}
	dir, err := os.MkdirTemp("", "probe-trace-")
	if err != nil {
		return Traced{}, err
	}
	defer os.RemoveAll(dir)

	var t Traced
	if t.Before, err = oracle.Observe(root); err != nil {
		return Traced{}, err
	}
	start := time.Now()
	t.Outcome, err = oracle.ExecuteIn(ctx, bwrap, root, cmd, limit, oracle.Sandbox{
		Wrap:       []string{strace, "-f", "-qq", "-s", "4096", "-e", "trace=execve", "-e", "signal=none", "-o", traceDir + "/execve"},
		WritableAt: []oracle.Mount{{Host: dir, At: traceDir}},
		HideHost:   true,
	})
	t.Duration = time.Since(start)
	if err != nil {
		return Traced{}, err
	}
	if t.After, err = oracle.Observe(root); err != nil {
		return Traced{}, err
	}
	t.Verdict = oracle.Label(t.Before, t.After)

	f, err := os.Open(filepath.Join(dir, "execve"))
	if err != nil {
		return Traced{}, fmt.Errorf("reading the trace: %w", err)
	}
	defer f.Close()
	execs, err := parseExecve(f)
	if err != nil {
		return Traced{}, err
	}
	if len(execs) == 0 {
		return Traced{}, fmt.Errorf("trace holds no execve, not even the sandbox shell")
	}
	t.Execve = execs[1:]
	if len(t.Execve) == 0 {
		t.Execve = nil
	}
	return t, nil
}

// traceDir is where Trace's log directory is seen inside the sandbox. It is
// under /run, which HideHost replaces with a tmpfs, so bubblewrap can create it.
const traceDir = "/run/probe-trace"

// hiddenParent returns the oracle.HiddenDirs entry that p, resolved, lies
// under, or "" if none: an executable there would be masked in the sandbox.
func hiddenParent(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	for _, d := range oracle.HiddenDirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return d
		}
	}
	return ""
}

// parseExecve reads strace -f output and returns the basename of argv[0] of
// every execve that returned 0, in order. Lines look like
//
//	7  execve("/usr/bin/python3", ["python3", "-c", "pass"], 0x55e... /* 67 vars */) = 0
//
// and, when another process interleaves, are split into
//
//	7  execve("/usr/bin/python3", ["python3", "-c", "pass"], 0x55e... <unfinished ...>
//	7  <... execve resumed>) = 0
func parseExecve(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	pending := map[string]string{} // pid -> argv[0] of an unfinished execve
	var out []string
	for sc.Scan() {
		pid, rest, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		switch {
		case strings.HasPrefix(rest, "execve("):
			name := argv0(rest)
			if strings.HasSuffix(rest, "<unfinished ...>") {
				pending[pid] = name
				continue
			}
			if succeeded(rest) {
				out = append(out, name)
			}
		case strings.HasPrefix(rest, "<... execve resumed>"):
			name, had := pending[pid]
			delete(pending, pid)
			if had && succeeded(rest) {
				out = append(out, name)
			}
		}
	}
	return out, sc.Err()
}

// succeeded reports whether a completed syscall line returned 0.
func succeeded(line string) bool {
	i := strings.LastIndex(line, ") = ")
	return i >= 0 && strings.HasPrefix(line[i+len(") = "):], "0")
}

// argv0 returns the basename of argv[0] from an execve line, falling back to the
// path argument when argv is empty or unreadable.
func argv0(line string) string {
	s := strings.TrimPrefix(line, "execve(")
	file, s := cString(s)
	s = strings.TrimPrefix(s, ", ")
	if strings.HasPrefix(s, "[") {
		if a, _ := cString(s[1:]); a != "" {
			return path.Base(a)
		}
	}
	return path.Base(file)
}

// cString reads one strace-quoted string from the start of s and returns it
// unescaped with the remainder of s. strace escapes like C, which Go's quoting
// rules accept; anything it cannot unquote is returned raw.
func cString(s string) (string, string) {
	if !strings.HasPrefix(s, `"`) {
		return "", s
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			raw := s[:i+1]
			rest := strings.TrimPrefix(s[i+1:], "...") // truncated by -s
			if v, err := strconv.Unquote(raw); err == nil {
				return v, rest
			}
			return raw[1 : len(raw)-1], rest
		}
	}
	return "", s
}
