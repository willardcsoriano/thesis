package executor

import (
	"context"
	"strings"
	"testing"
)

// TestBashSyntaxRuns guards the shell choice. The loop prompts the model for
// bash; if Run dispatches through /bin/sh (dash on Debian) these commands
// fail as syntax errors that look like model failures.
func TestBashSyntaxRuns(t *testing.T) {
	for _, tc := range []struct{ name, cmd, want string }{
		{"double-bracket test", `if [[ "abc" == a* ]]; then echo yes; fi`, "yes"},
		{"brace expansion", `echo {1..5}`, "1 2 3 4 5"},
		{"array", `a=(x y z); echo ${a[1]}`, "y"},
		{"process substitution", `cat <(echo hi)`, "hi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Run(context.Background(), tc.cmd)
			if r.ExitCode != 0 {
				t.Fatalf("exit %d, stderr=%q — shell rejected bash syntax", r.ExitCode, r.Stderr)
			}
			if got := strings.TrimSpace(r.Stdout); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Without pipefail a pipeline reports only its last stage, so a failed
// producer reads as success. That is a silent false success, and it biases
// the study's execution-error count in the flattering direction.
func TestPipelineFailureIsVisible(t *testing.T) {
	r := Run(context.Background(), "ls /definitely-not-here | wc -l")
	if r.ExitCode == 0 {
		t.Fatalf("failed pipeline reported success (exit %d) — pipefail is not in effect", r.ExitCode)
	}
	if r.RawExitCode == 0 {
		t.Errorf("RawExitCode = 0, want the shell's real nonzero status")
	}
	if r.SIGPIPE {
		t.Error("a genuine failure was misclassified as SIGPIPE")
	}
}

// pipefail's cost: a consumer that exits early kills its producer with
// SIGPIPE, which is an artifact of the pipeline shape rather than a failure.
// Normalising it is what makes pipefail usable here.
func TestEarlyExitPipelineIsNotCountedAsFailure(t *testing.T) {
	r := Run(context.Background(), "yes | head -1")
	if r.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0: an early-exit pipeline is not a failure", r.ExitCode)
	}
	if r.RawExitCode != 141 {
		t.Fatalf("RawExitCode = %d, want 141 (128+SIGPIPE) preserved for analysis", r.RawExitCode)
	}
	if !r.SIGPIPE {
		t.Error("SIGPIPE flag not set, so the normalisation is unauditable in the log")
	}
}

// Ordinary success and ordinary failure must be untouched by the above.
func TestPlainExitCodesAreUnchanged(t *testing.T) {
	if r := Run(context.Background(), "true"); r.ExitCode != 0 || r.RawExitCode != 0 || r.SIGPIPE {
		t.Errorf("true: %+v", r)
	}
	if r := Run(context.Background(), "exit 3"); r.ExitCode != 3 || r.RawExitCode != 3 {
		t.Errorf("exit 3: ExitCode=%d RawExitCode=%d", r.ExitCode, r.RawExitCode)
	}
}

// grep exiting 1 for "no lines matched" is a real answer, not an artifact, and
// must not be swallowed the way SIGPIPE is.
func TestNoMatchIsStillReported(t *testing.T) {
	r := Run(context.Background(), "echo hello | grep zzz | cat")
	if r.ExitCode == 0 {
		t.Error("a no-match pipeline reported success; only SIGPIPE should be normalised")
	}
}
