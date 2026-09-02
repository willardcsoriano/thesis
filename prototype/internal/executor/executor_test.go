package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunCapturesStdout(t *testing.T) {
	res := Run(context.Background(), "echo hello")
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if res.Stdout != "hello\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "hello\n")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestRunInRunsInTheSpecifiedDirectory(t *testing.T) {
	dir := t.TempDir()
	res := RunIn(context.Background(), dir, "pwd")
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	// Resolve symlinks on both sides: t.TempDir() on some systems returns a
	// path through a symlink (e.g. /tmp -> /private/tmp), which pwd reports
	// resolved.
	wantDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(dir): %v", err)
	}
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(res.Stdout))
	if err != nil {
		t.Fatalf("EvalSymlinks(stdout): %v", err)
	}
	if gotDir != wantDir {
		t.Errorf("pwd output = %q, want %q", gotDir, wantDir)
	}
}

func TestRunUsesCallingProcessDirectoryWhenDirEmpty(t *testing.T) {
	wantDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	res := Run(context.Background(), "pwd")
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if got := strings.TrimSpace(res.Stdout); got != wantDir {
		t.Errorf("pwd output = %q, want the calling process's cwd %q", got, wantDir)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	res := Run(context.Background(), "echo oops 1>&2")
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if res.Stderr != "oops\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "oops\n")
	}
}

func TestRunSurfacesNonZeroExit(t *testing.T) {
	res := Run(context.Background(), "exit 7")
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
}

func TestRunContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := Run(ctx, "sleep 5")
	if res.ExitCode == 0 && res.Err == nil {
		t.Fatalf("expected a cancelled command to report an error or nonzero exit, got %+v", res)
	}
}

// runWithWatchdog runs Run in a goroutine and fails the test if it does not
// return within watchdog wall-clock time — a safety net so a regression in
// Run's own timeout/kill handling fails fast with a clear message instead of
// hanging until go test's global timeout.
func runWithWatchdog(t *testing.T, ctx context.Context, cmd string, watchdog time.Duration) Result {
	t.Helper()
	done := make(chan Result, 1)
	go func() { done <- Run(ctx, cmd) }()
	select {
	case res := <-done:
		return res
	case <-time.After(watchdog):
		t.Fatalf("Run(%q) did not return within %s — it hung", cmd, watchdog)
		return Result{}
	}
}

// TestRunTimesOutOnHangingCommand exercises testing-plan.md's Layer 5
// question directly: does a context deadline actually bound execution, not
// just model-generation. waitDelay is shrunk for the duration of the test so
// the assertion stays fast regardless of the production default.
func TestRunTimesOutOnHangingCommand(t *testing.T) {
	orig := waitDelay
	waitDelay = 200 * time.Millisecond
	defer func() { waitDelay = orig }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	res := runWithWatchdog(t, ctx, "sleep 30", 3*time.Second)
	if !res.TimedOut {
		t.Errorf("TimedOut = false, want true for a command that exceeded its context deadline")
	}
	if res.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want a nonzero/negative code for a killed process")
	}
}

// TestRunHandlesLargeStdout confirms bytes.Buffer-based capture does not
// truncate or choke on multi-megabyte output.
func TestRunHandlesLargeStdout(t *testing.T) {
	const wantLen = 2_000_000
	res := runWithWatchdog(t, context.Background(), fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'a'", wantLen), 10*time.Second)
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if len(res.Stdout) != wantLen {
		t.Errorf("captured %d bytes, want %d", len(res.Stdout), wantLen)
	}
}

// TestRunHandlesNonUTF8Output confirms raw binary bytes survive capture
// unmodified — Go strings are byte sequences, not required to be valid
// UTF-8, but this makes that assumption an explicit, checked fact rather
// than an implicit one.
func TestRunHandlesNonUTF8Output(t *testing.T) {
	want := "\xff\xfe\x00\x01\x80"
	res := runWithWatchdog(t, context.Background(), `printf '\377\376\000\001\200'`, 5*time.Second)
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if res.Stdout != want {
		t.Errorf("Stdout = %q, want %q", res.Stdout, want)
	}
}

// TestRunDoesNotHangOnCommandExpectingStdin verifies (rather than assumes)
// that a command which would read from stdin gets immediate EOF, since Run
// never attaches one — exec.Cmd's documented default for a nil Stdin is
// os.DevNull, not a hang.
func TestRunDoesNotHangOnCommandExpectingStdin(t *testing.T) {
	res := runWithWatchdog(t, context.Background(), "cat", 3*time.Second)
	if res.Err != nil {
		t.Fatalf("unexpected launch error: %v", res.Err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (cat on an immediate-EOF stdin should succeed with no output)", res.ExitCode)
	}
	if res.Stdout != "" {
		t.Errorf("Stdout = %q, want empty", res.Stdout)
	}
}

// TestRunConcurrentOverlappingRuns guards against shared-state bugs between
// simultaneous invocations — relevant once M3a's persistent loop exists and
// a user could plausibly trigger overlapping steps. Run -race against this.
func TestRunConcurrentOverlappingRuns(t *testing.T) {
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("marker-%d\n", i)
			res := Run(context.Background(), fmt.Sprintf("echo marker-%d", i))
			if res.Stdout != want {
				errs <- fmt.Sprintf("goroutine %d: Stdout = %q, want %q", i, res.Stdout, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}
