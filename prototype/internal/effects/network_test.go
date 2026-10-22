package effects

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"synapseos/internal/testenv"
)

// Defect 13: a resolver runs without the network, so a producer that reads it prints
// nothing there, which was read as "nothing to remove" while the real run removed
// what the server or name lookup returned. The runner now observes socket(2) in every
// process of a resolver and refuses the result. TestRunnerRefuses* exercise that
// observation directly, and TestNetworkProducersAskBecauseTheTracerRefuses pins the
// end-to-end path by asserting the refusal reason is ErrOutsideInput: were the tracer
// to break, the sandbox's own network isolation would still fail these commands, but
// with a different error, so a class-only check would pass without testing anything.

// TestMain lets the test binary stand in for programs the analysis has never heard of:
// with EFFECTS_TEST_HELPER set it acts as that program instead of running the tests.
func TestMain(m *testing.M) {
	switch os.Getenv("EFFECTS_TEST_HELPER") {
	case "":
		os.Exit(m.Run())
	case "net-client":
		// An unknown network client: it connects (to TEST-NET-1, which nothing answers
		// inside the sandbox) and, finding no server, prints nothing.
		if c, err := net.DialTimeout("tcp", "192.0.2.1:80", 200*time.Millisecond); err == nil {
			c.Close()
		}
		os.Exit(0)
	case "raw-socket":
		// A socket that is opened and never connected still counts: it is the attempt.
		if fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_DGRAM, 0); err == nil {
			syscall.Close(fd)
		}
		os.Exit(0)
	case "unix-socket":
		// A local service is outside the files too (a name cache, a resolver daemon):
		// here one the test listens as, at EFFECTS_TEST_SOCKET.
		if c, err := net.Dial("unix", os.Getenv("EFFECTS_TEST_SOCKET")); err == nil {
			c.Close()
		}
		os.Exit(0)
	case "abstract-socket":
		// An abstract address belongs to the network namespace, which the sandbox replaces.
		if c, err := net.Dial("unix", "@synapse-effects-test"); err == nil {
			c.Close()
		}
		os.Exit(0)
	case "nscd-probe":
		// What glibc does before reading /etc/passwd: ask a name cache that is not
		// there. The same path is absent in the real run, so this is not outside input.
		if c, err := net.Dial("unix", "/var/run/nscd-not-here/socket"); err == nil {
			c.Close()
		}
		os.Stdout.WriteString("a.txt\n")
		os.Exit(0)
	case "offline":
		os.Stdout.WriteString("a.txt\n")
		os.Exit(0)
	}
	os.Exit(2)
}

func runHelper(t *testing.T, role string) ([]byte, error) {
	t.Helper()
	t.Setenv("EFFECTS_TEST_HELPER", role)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return DefaultRunner(defaultTestTimeout)(context.Background(), t.TempDir(), []string{self})
}

func TestRunnerRefusesAnyProgramThatReachesOutside(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "service.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	t.Setenv("EFFECTS_TEST_SOCKET", sock)
	for _, role := range []string{"net-client", "raw-socket", "unix-socket", "abstract-socket"} {
		t.Run(role, func(t *testing.T) {
			out, err := runHelper(t, role)
			if !errors.Is(err, ErrOutsideInput) {
				t.Fatalf("a program that reaches outside the files must fail with ErrOutsideInput, got output %q, err %v", out, err)
			}
		})
	}
	for _, role := range []string{"offline", "nscd-probe"} {
		t.Run(role, func(t *testing.T) {
			out, err := runHelper(t, role)
			if err != nil || string(out) != "a.txt\n" {
				t.Fatalf("a program that reads only files resolves: got %q, %v", out, err)
			}
		})
	}
}

func TestRunnerRefusesSocketsInAnyDescendant(t *testing.T) {
	run := DefaultRunner(defaultTestTimeout)
	for _, script := range []string{
		"(sleep 0; exec 3<>/dev/udp/127.0.0.1/9) & wait; echo a.txt",   // a background child
		"echo a.txt | (cat; exec 3<>/dev/tcp/127.0.0.1/9) 2>/dev/null", // a later pipeline stage
		"x=$(getent hosts example.com); echo a.txt",                    // a substitution whose output is not used
	} {
		out, err := run(context.Background(), t.TempDir(), []string{"bash", "-c", script})
		if !errors.Is(err, ErrOutsideInput) {
			t.Errorf("%s: got output %q, err %v; want ErrOutsideInput", script, out, err)
		}
	}
}

// networkFixture is the standard fixture with a file named like the host the
// commands ask about, so a lookup that printed its own name would remove it.
func networkFixture(t *testing.T) string {
	t.Helper()
	wd := fixture(t)
	if err := os.WriteFile(filepath.Join(wd, "example.com"), []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestProducersThatReadTheNetworkAsk(t *testing.T) {
	wd := networkFixture(t)
	const gawkNet = `gawk 'BEGIN { s = "/inet/tcp/0/example.com/80"; print "GET /" |& s; while ((s |& getline l) > 0) print l }'`
	producers := []string{
		"getent hosts example.com",
		"getent ahosts example.com",
		"host example.com",
		"nslookup example.com",
		"dig +short example.com",
		"git ls-remote https://example.com/repo.git",
		"cat < /dev/tcp/example.com/80",
		"cat < /dev/udp/127.0.0.1/53",
		"curl -s https://example.com/list",
		"wget -qO- https://example.com/list",
		gawkNet,
		"getent hosts example.com | sort",
		"echo example.com | (cat; getent hosts example.com >/dev/null)",
	}
	var cases []ruleCase
	for _, p := range producers {
		cases = append(cases,
			ruleCase{p + " | xargs rm -f", true, Unrecoverable, nil, []string{"remove example.com"}},
			ruleCase{p + ` | while read f; do rm -f "$f"; done`, true, Unrecoverable, nil, nil},
			ruleCase{p + ` | while IFS= read -r f; do rm -f -- "$f"; done`, true, Unrecoverable, nil, nil},
			ruleCase{"rm -f $(" + p + ")", true, Unrecoverable, nil, nil},
			ruleCase{"f=$(" + p + `); rm -f "$f"`, true, Unrecoverable, nil, nil},
		)
	}
	runRuleCasesIn(t, wd, cases)
}

// TestNetworkProducersAskBecauseTheTracerRefuses makes the tracer the thing under
// test. Each producer is a command the analysis treats as read-only, so the resolver
// runs it, and only the socket tracer stops it; the forms are the two that reach the
// tracer (a bare substitution and xargs), not the ones a prior unknown-variable or
// awk-program rule already refuses. The assertion is on the ErrOutsideInput reason,
// not merely the class: with the tracer disabled the sandbox's network isolation
// still fails these, but with another error, which this then catches.
func TestNetworkProducersAskBecauseTheTracerRefuses(t *testing.T) {
	wd := networkFixture(t)
	producers := []string{
		"getent hosts example.com",
		"getent ahosts example.com",
		"host example.com",
		"nslookup example.com",
		"dig +short example.com",
		"git ls-remote https://example.com/repo.git",
		"cat < /dev/tcp/example.com/80",
		"cat < /dev/udp/127.0.0.1/53",
		"curl -s https://example.com/list",
		"wget -qO- https://example.com/list",
		"getent hosts example.com | sort",
	}
	for _, p := range producers {
		for _, cmd := range []string{"rm -f $(" + p + ")", p + " | xargs rm -f"} {
			t.Run(cmd, func(t *testing.T) {
				a := analyze(t, wd, cmd, true)
				if v := a.Verdict(); v.Class != Unrecoverable {
					t.Fatalf("class = %s, want unrecoverable; issues: %+v", v.Class, a.Issues)
				}
				found := false
				for _, is := range a.Issues {
					if strings.Contains(is.Reason, ErrOutsideInput.Error()) {
						found = true
					}
				}
				if !found {
					t.Fatalf("asked, but not because the tracer saw a socket; issues: %+v", a.Issues)
				}
			})
		}
	}
}

func TestOfflineProducersStillResolve(t *testing.T) {
	wd := networkFixture(t)
	if err := os.WriteFile(filepath.Join(wd, "list.txt"), []byte("a.txt\nb.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRuleCasesIn(t, wd, []ruleCase{
		{"find . -name '*.tmp' | xargs rm", true, RecoverableWithCapture, []string{"remove one.tmp", "remove sub/two.tmp"}, []string{"remove example.com"}},
		{"ls *.txt | xargs rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"cat list.txt | xargs rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{`cat list.txt | while read f; do rm "$f"; done`, true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{`rm $(cat list.txt)`, true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{`find logs -type f | while IFS= read -r f; do rm -- "$f"; done`, true, RecoverableWithCapture, []string{"remove logs/x.log", "remove logs/y.log"}, nil},
	})
}

func TestTraceJudgement(t *testing.T) {
	for _, tc := range []struct {
		lines   []string
		outside bool
	}{
		{[]string{`7 socket(AF_INET, SOCK_DGRAM|SOCK_CLOEXEC|SOCK_NONBLOCK, IPPROTO_IP) = 4`}, true},
		{[]string{`7 socket(AF_INET6, SOCK_STREAM, IPPROTO_TCP) = -1 EAFNOSUPPORT (Address family not supported by protocol)`}, true},
		{[]string{`7 socket(AF_NETLINK, SOCK_RAW|SOCK_CLOEXEC, NETLINK_ROUTE) = 3`}, true},
		{[]string{`7 io_uring_setup(8, {flags=0}) = 3`}, true},
		{[]string{`7 socketcall(0x1, [2, 1, 0]) = 3`}, true},
		{[]string{`7 bind(3, {sa_family=AF_UNIX, sun_path=@"x"}, 4) = 0`}, true},
		{[]string{`7 socket(AF_UNIX, SOCK_STREAM|SOCK_CLOEXEC|SOCK_NONBLOCK, 0) = 4`}, false},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110) = -1 ENOENT (No such file or directory)`}, false},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/run/stale.sock"}, 110) = -1 ECONNREFUSED (Connection refused)`}, false},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/run/systemd/resolve/io.systemd.Resolve"}, 110) = 0`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path=@"/tmp/.X11-unix/X0"}, 20) = -1 ECONNREFUSED (Connection refused)`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/dev/log"}, 110) = -1 ENOENT (No such file or directory)`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/run/../dev/log"}, 110) = -1 ENOENT (No such file or directory)`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="rel.sock"}, 110) = -1 ENOENT (No such file or directory)`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/run/x.sock"}, 110) = -1 EACCES (Permission denied)`}, true},
		{[]string{`7 connect(4, {sa_family=AF_INET, sin_port=htons(80), sin_addr=inet_addr("1.2.3.4")}, 16) = -1 ENETUNREACH (Network is unreachable)`}, true},
		{[]string{`7 sendto(3, "<13>x", 5, MSG_NOSIGNAL, {sa_family=AF_UNIX, sun_path="/dev/log"}, 110) = -1 ENOENT (No such file or directory)`}, true},
		{[]string{`7 sendto(3, "x", 1, 0, NULL, 0) = 1`}, false},
		{[]string{`7 sendmsg(3, {msg_name=NULL, msg_namelen=0, msg_iov=[{iov_base="x", iov_len=1}], msg_iovlen=1, msg_controllen=0, msg_flags=0}, 0) = 1`}, false},
		// A call split by another process's output is judged whole.
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110 <unfinished ...>`,
			`8 execve("/usr/bin/x", ["x"], 0x0 /* 3 vars */) = 0`,
			`7 <... connect resumed>) = 0`}, true},
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110 <unfinished ...>`,
			`7 <... connect resumed>) = -1 ENOENT (No such file or directory)`}, false},
		{[]string{`7 <... connect resumed>) = -1 ENOENT (No such file or directory)`}, true},
		// A syscall left unfinished when the trace ends never had its result read, so
		// whether it reached outside is unknown: it is counted, fail closed.
		{[]string{`7 connect(4, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110 <unfinished ...>`}, true},
		{[]string{`7 socket(AF_UNIX, SOCK_STREAM|SOCK_CLOEXEC, 0 <unfinished ...>`}, true},
		// What strace may print that these rules do not know is outside input.
		{[]string{`7 something new`}, true},
	} {
		var tr traceReader
		got := false
		for _, l := range tc.lines {
			if tr.line(l) != "" {
				got = true
			}
		}
		// Whatever is still unfinished at the end of the trace is judged too.
		if tr.flush() != "" {
			got = true
		}
		if got != tc.outside {
			t.Errorf("%q: outside input = %v, want %v", tc.lines, got, tc.outside)
		}
	}
}

func TestSocketPathThroughALinkIntoDev(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("/dev", filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if realSocketPath(filepath.Join(dir, "d", "log")) {
		t.Fatal("a socket path that leads under /dev through a link names a different file in the sandbox")
	}
	if !realSocketPath(filepath.Join(dir, "absent", "socket")) {
		t.Fatal("a socket path under a missing directory is absent in the real run too")
	}
}

// TestTracerLeavesNoTraceeRunning kills strace while the traced command still has a
// child running, and checks the child died with it within seconds, long before its
// own 60-second sleep would end. A tracee strace detached would
// run on untraced, so a "<detached ...>" line could hide a socket opened later; the
// trace judge accepts that line only because this holds, with or without the seccomp
// filter (which turns --kill-on-exit on by itself).
func TestTracerLeavesNoTraceeRunning(t *testing.T) {
	p, err := exec.LookPath("strace")
	if err != nil {
		testenv.Unavailable(t, "strace is not installed")
	}
	for _, bpf := range []bool{true, false} {
		t.Run(fmt.Sprintf("bpf=%v", bpf), func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pid")
			tr := &runnerTracer{path: p, bpf: bpf}
			full := tr.wrap([]string{"bash", "-c", `sleep 60 & echo $! > "$1"; wait`, "bash", pidFile})
			cmd := exec.Command(full[0], full[1:]...)
			devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer devnull.Close()
			cmd.ExtraFiles = []*os.File{devnull} // the trace's descriptor 3
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var pid int
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if b, err := os.ReadFile(pidFile); err == nil && len(b) > 1 {
					fmt.Sscan(string(b), &pid)
					break
				}
			}
			if pid == 0 {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatal("the traced child never started")
			}
			// The runner stops strace with SIGKILL (its context is cancelled on a
			// timeout or on the first outside input). Without PTRACE_O_EXITKILL the
			// kernel then lets every tracee run on, untraced.
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			cmd.Wait()
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				// Signal 0 also succeeds on a zombie, so read its state instead.
				st, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
				if err != nil || strings.Contains(string(st), ") Z ") {
					return // gone
				}
			}
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("pid %d outlived strace: a detached tracee runs on untraced", pid)
		})
	}
}
