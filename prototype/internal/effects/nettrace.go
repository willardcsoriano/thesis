package effects

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// A resolver runs a read-only command now and reads its output as what the command
// will print when the line runs. That holds only for input both runs see alike: the
// files, which the sandbox mounts read-only as they are. A command that talks to a
// server, a name server, or a daemon gets its input from outside the files; the
// sandbox has no network, so there it gets nothing, and an empty answer was read as
// "nothing to remove" (defect 13). A list of client names cannot close that, since
// any program may open a socket (getent, git ls-remote, gawk's /inet files, bash's
// /dev/tcp), so the runner watches what the command does instead.
//
// Every resolver runs under strace, following every process it starts. Input from
// outside the files needs a socket, and every socket starts with socket(2),
// socketcall(2) (its 32-bit entry), or an io_uring, which can open one without that
// call: no socket is inherited (the runner passes only pipes), and open(2) cannot
// open a socket file. The trace is judged call by call (outsideInput):
//
//   - a socket of any family but AF_UNIX (the network, netlink, raw packets), and
//     any io_uring, is outside input, whether or not it ever connects: trying is
//     what counts, because in the sandbox it fails where the real run succeeds;
//   - an AF_UNIX socket is local, and is outside input only once it reaches past
//     the files: a connect(2) that succeeds (a live daemon answers: a name cache, a
//     resolver, a bus), any connect to an abstract address or to a path under /dev
//     or /proc (the two trees the sandbox replaces, so its answer there is not the
//     real one), any bind(2), and any send with an explicit address;
//   - a connect to a socket path that fails with ENOENT or ECONNREFUSED is not: the
//     sandbox mounts the real filesystem, so no daemon listens at that path in the
//     real run either. This is glibc asking for nscd before reading /etc/passwd,
//     which tar, rsync, and ls -l do on every run.
//
// Anything in the trace these rules do not recognise is outside input, so a new
// strace output format fails closed. The trace goes to a pipe on descriptor 3, which
// the traced command does not get, and a trace with no successful execve(2) means
// the command did not run under the tracer, which is an error.
//
// --seccomp-bpf makes the traced processes stop only at the traced calls, so tracing
// costs little (a find over /usr runs about 1.1 times as long, against 14 times
// under plain ptrace). Its one gap, in strace(1): a call refused or killed by the
// traced program's own seccomp filter is not reported. Such a call made no socket,
// so no outside input reached the program, and the same program refuses the same
// call when the line really runs. If the filter cannot be installed strace stops at
// every call, which is slower and no less sound.

// ErrOutsideInput is returned when a resolution command took, or tried to take,
// input from outside the files.
var ErrOutsideInput = errors.New("it reaches outside the files (network, name lookup, or a local service), so what it prints now is not what it will print then")

// ErrNoTracer is returned by every run when no working strace was found: without
// it a command that reads the network cannot be told from one that found nothing.
var ErrNoTracer = errors.New("strace is not available, so whether a resolver reads the network cannot be observed")

type runnerTracer struct {
	path string
	bpf  bool
}

type traceResult struct {
	outside string // the first traced call that took outside input, or ""
}

const traceFD = 3

var (
	tracePID     = regexp.MustCompile(`^(\d+)\s+`)
	traceResumed = regexp.MustCompile(`^<\.\.\. (\w+) resumed>`)
	traceCall    = regexp.MustCompile(`^(\w+)\(`)
	traceUnix    = regexp.MustCompile(`^connect\(\d+, \{sa_family=AF_UNIX, sun_path="((?:[^"\\]|\\.)*)"\}, \d+\) = -1 (E[A-Z]+) `)
)

func (t *runnerTracer) wrap(argv []string) []string {
	// --kill-on-exit (strace 6.6) kills every tracee when strace exits or is killed,
	// as the runner does on a timeout or the first outside input. Without it a
	// tracee runs on untraced, and strace's "<detached ...>" could hide a socket.
	// --seccomp-bpf turns it on by itself; it is passed so the plain-ptrace
	// fallback holds too.
	full := []string{t.path, "-f", "-qq", "--kill-on-exit", "-e", "signal=none", "-s", "256",
		"-e", "trace=execve,socket,socketcall,connect,bind,sendto,sendmsg,sendmmsg,io_uring_setup",
		"-o", fmt.Sprintf("/dev/fd/%d", traceFD)}
	if t.bpf {
		full = append(full, "--seccomp-bpf")
	}
	// The traced command must not hold the trace's descriptor.
	full = append(full, "--", "bash", "-c", fmt.Sprintf(`exec "$@" %d>&-`, traceFD), "bash")
	return append(full, argv...)
}

// run starts full, whose innermost command is t.wrap's, and reads the trace as it
// comes. The first outside input stops the run: its result is refused whatever else
// happens, and a read from the network could otherwise wait out the timeout.
func (t *runnerTracer) run(ctx context.Context, wd string, full []string, stdout io.Writer) (traceResult, error) {
	var res traceResult
	r, w, err := os.Pipe()
	if err != nil {
		return res, err
	}
	defer r.Close()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	cmd.Dir = wd
	cmd.Stdout = stdout
	cmd.ExtraFiles = []*os.File{w} // descriptor 3
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		w.Close()
		return res, err
	}
	w.Close()
	var tr traceReader
	done := make(chan struct{})
	go func() {
		defer close(done)
		br := bufio.NewReaderSize(r, 64<<10)
		for {
			line, err := br.ReadString('\n')
			if s := tr.line(strings.TrimRight(line, "\n")); s != "" && res.outside == "" {
				res.outside = s
				stop()
			}
			if err != nil {
				return
			}
		}
	}()
	werr := cmd.Wait()
	<-done
	if res.outside == "" {
		// A syscall left unfinished when the trace ends never had its result read, so
		// whether it reached outside is unknown: fail closed and count it.
		res.outside = tr.flush()
	}
	if res.outside != "" {
		return res, nil
	}
	var ee *exec.ExitError
	if werr != nil && !errors.As(werr, &ee) {
		return res, werr
	}
	if !tr.execOK {
		return res, errors.New("the resolver did not start under the tracer")
	}
	return res, nil
}

// traceReader joins the halves of calls strace splits when processes interleave
// ("connect(4, ... <unfinished ...>" and later "<... connect resumed>) = 0") and
// judges each whole call.
type traceReader struct {
	pending map[string]string
	execOK  bool
}

// flush reports a syscall that was left unfinished when the trace ended. Its result
// was never read, so whether it reached outside is unknown, and the analysis fails
// closed by treating it as outside input. It returns "" when nothing is pending.
func (tr *traceReader) flush() string {
	for _, head := range tr.pending {
		return head + " <unfinished ...>"
	}
	return ""
}

// line takes one line of the trace and returns it if it shows outside input.
func (tr *traceReader) line(l string) string {
	if l == "" {
		return ""
	}
	pid := ""
	if m := tracePID.FindStringSubmatch(l); m != nil {
		pid, l = m[1], l[len(m[0]):]
	}
	if tr.pending == nil {
		tr.pending = map[string]string{}
	}
	if head, ok := strings.CutSuffix(l, " <unfinished ...>"); ok {
		tr.pending[pid] = head
		return ""
	}
	if m := traceResumed.FindStringSubmatch(l); m != nil {
		head, ok := tr.pending[pid]
		delete(tr.pending, pid)
		if !ok || !strings.HasPrefix(head, m[1]+"(") {
			return l // a resumption of nothing seen: not understood
		}
		l = head + l[len(m[0]):]
	}
	if strings.HasPrefix(l, "execve(") {
		if strings.HasSuffix(l, " = 0") {
			tr.execOK = true
		}
		return ""
	}
	if outsideInput(l) {
		return l
	}
	return ""
}

// outsideInput judges one whole traced call other than execve, by the rules in the
// comment at the top of this file.
func outsideInput(call string) bool {
	m := traceCall.FindStringSubmatch(call)
	if m == nil {
		return true
	}
	switch m[1] {
	case "socket":
		return !strings.HasPrefix(call, "socket(AF_UNIX,")
	case "connect":
		u := traceUnix.FindStringSubmatch(call)
		if u == nil {
			return true // succeeded, failed otherwise, abstract, or not AF_UNIX
		}
		if u[2] != "ENOENT" && u[2] != "ECONNREFUSED" {
			return true
		}
		return !realSocketPath(u[1])
	case "sendto", "sendmsg", "sendmmsg":
		return strings.Contains(call, "sa_family=")
	}
	return true // bind, socketcall, io_uring_setup, anything unexpected
}

// realSocketPath reports whether a socket path as strace prints it names the same
// file inside the sandbox as outside: an absolute path, with no escapes, that never
// passes under /dev or /proc, before or after following a symbolic link. Those two
// trees are the sandbox's own, so a path through them (the host's /dev/log, a link
// into /dev) is not the file the real run reaches.
func realSocketPath(p string) bool {
	if !filepath.IsAbs(p) || strings.Contains(p, `\`) {
		return false
	}
	p = filepath.Clean(p)
	for hops := 0; hops < 40; hops++ {
		if under(p, "/dev") || under(p, "/proc") {
			return false
		}
		parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
		cur, linked := "/", false
		for i, c := range parts {
			next := filepath.Join(cur, c)
			fi, err := os.Lstat(next)
			if err != nil {
				return true // missing from here on, in both views
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(next)
				if err != nil {
					return false
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(cur, target)
				}
				p = filepath.Clean(filepath.Join(append([]string{target}, parts[i+1:]...)...))
				linked = true
				break
			}
			cur = next
		}
		if !linked {
			return true
		}
	}
	return false // the links do not end
}

func under(p, dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }

var (
	tracerOnce sync.Once
	tracerVal  *runnerTracer
)

// tracer probes once per process for a strace that both runs a command and reports
// a socket, under bubblewrap when that is how commands will run: a tracer that ran
// but saw nothing would make every network read look like an empty result.
func tracer() *runnerTracer {
	tracerOnce.Do(func() { tracerVal = probeTracer(bwrapPath()) })
	return tracerVal
}

func probeTracer(bw string) *runnerTracer {
	p, err := exec.LookPath("strace")
	if err != nil {
		return nil
	}
	for _, bpf := range []bool{true, false} {
		t := &runnerTracer{path: p, bpf: bpf}
		if t.works(bw) {
			return t
		}
	}
	return nil
}

func (t *runnerTracer) works(bw string) bool {
	run := func(argv ...string) (traceResult, bool) {
		full := t.wrap(argv)
		if bw != "" {
			full = append(sandboxArgv(bw, "/"), full...)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := t.run(ctx, "/", full, io.Discard)
		return res, err == nil
	}
	quiet, ok := run("true")
	if !ok || quiet.outside != "" {
		return false
	}
	// A UDP socket to the discard port: the socket(2) call is what must be seen, and
	// it is made whether or not anything is listening.
	loud, ok := run("bash", "-c", "exec 9<>/dev/udp/127.0.0.1/9")
	return ok && loud.outside != ""
}
