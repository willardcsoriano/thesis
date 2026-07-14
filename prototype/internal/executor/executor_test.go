package executor

import (
	"context"
	"testing"
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
