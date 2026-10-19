package oracle

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Commands run by ExecuteIn that try to outlive it or exhaust the host.

func survivors(tag string) []string {
	var out []string
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil && bytes.Contains(b, []byte(tag)) {
			out = append(out, e.Name())
		}
	}
	return out
}

func noSurvivors(t *testing.T, tag string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		left := survivors(tag)
		if len(left) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("processes outlived the sandbox: %v", left)
			for _, p := range left {
				if pid, err := strconv.Atoi(p); err == nil {
					syscall.Kill(pid, syscall.SIGKILL)
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Daemons started every usual way die with the command, and a command
// that never exits is killed at the limit with all of them, in the default
// sandbox (the corpus's) and under HideHost.
func TestExecuteInLeavesNoSurvivors(t *testing.T) {
	bw := Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	for _, sb := range []Sandbox{{}, {HideHost: true}} {
		for _, hang := range []bool{false, true} {
			t.Run(fmt.Sprintf("hide=%v hang=%v", sb.HideHost, hang), func(t *testing.T) {
				tag := "oracle-orphan-" + strconv.FormatInt(time.Now().UnixNano(), 36)
				cmd := fmt.Sprintf(`setsid bash -c 'exec -a %[1]s sleep 300' </dev/null >/dev/null 2>&1 &
nohup bash -c 'exec -a %[1]s sleep 300' >/dev/null 2>&1 &
( ( bash -c 'exec -a %[1]s sleep 300' 2>&1 & ) & )
bash -c 'trap "" TERM HUP; exec -a %[1]s sleep 300' &
sleep 0.3`, tag)
				if hang {
					cmd += "\ntrap '' TERM INT; exec -a " + tag + " sleep 300"
				}
				start := time.Now()
				oc, err := ExecuteIn(context.Background(), bw, t.TempDir(), cmd, 2*time.Second, sb)
				if err != nil {
					t.Fatal(err)
				}
				if oc.TimedOut != hang {
					t.Errorf("timed out = %v, want %v", oc.TimedOut, hang)
				}
				if d := time.Since(start); d > 10*time.Second {
					t.Errorf("ExecuteIn took %s", d)
				}
				noSurvivors(t, tag)
			})
		}
	}
}

// Under HideHost (the agent study's per-step runner) a command cannot fork
// without bound, fill memory through /dev or /dev/shm, or create nested
// namespaces.
func TestHideHostLimits(t *testing.T) {
	bw := Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	root := t.TempDir()
	cmd := `python3 -c '
import os, time
n = 0
for i in range(5000):
    try:
        pid = os.fork()
    except OSError:
        break
    if pid == 0:
        time.sleep(30); os._exit(0)
    n += 1
print(n)' > forked
head -c 600M /dev/zero > /dev/shm/fill 2>/dev/null; stat -c %s /dev/shm/fill > shm 2>/dev/null || echo 0 > shm
echo x > /dev/planted 2>/dev/null && echo yes > devwritable
unshare -U -r true 2>/dev/null && echo yes > nested
true`
	oc, err := ExecuteIn(context.Background(), bw, root, cmd, 30*time.Second, Sandbox{HideHost: true})
	if err != nil || oc.TimedOut {
		t.Fatalf("%v %+v", err, oc)
	}
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(root, f))
		return strings.TrimSpace(string(b))
	}
	if n, _ := strconv.Atoi(read("forked")); n == 0 || n >= HiddenProcs {
		t.Errorf("forked %q processes; the limit is %d", read("forked"), HiddenProcs)
	}
	if n, _ := strconv.Atoi(read("shm")); n > HiddenSize {
		t.Errorf("/dev/shm took %d bytes; the cap is %d", n, HiddenSize)
	}
	if read("devwritable") != "" || read("nested") != "" {
		t.Errorf("/dev writable %q, nested namespace %q", read("devwritable"), read("nested"))
	}
}
