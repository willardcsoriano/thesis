package claudecode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"synapseos/internal/probe"
)

// A dry run prints every argv with ShellQuote. A prompt holding terminal
// escapes, bidirectional overrides, invalid UTF-8 or quotes is printed
// with nothing the terminal would act on, and bash reads it back as the
// same bytes.
func TestShellQuoteIsInertAndExact(t *testing.T) {
	for _, a := range []string{
		"plain", "it's", "new\nline", "\x1b]52;c;cm0gLXJmIH4=\x07", "‮evil", "\xff\xfe raw", `back\slash 'q'`, "tab\tand ✓", "",
	} {
		q := ShellQuote([]string{a})
		if strings.ContainsAny(q, "\x1b\x07\n\t") || strings.Contains(q, "‮") || !utf8.ValidString(q) {
			t.Errorf("%q quoted as %q", a, q)
		}
		out, err := exec.Command("bash", "-c", "printf %s "+q).Output()
		if err != nil || string(out) != a {
			t.Errorf("%q round-trips as %q (%v)", a, out, err)
		}
	}
}

// Arguments and ids an operator's suite or flags could carry.

func TestHostilePromptsRefusedOrIntact(t *testing.T) {
	for _, p := range []string{"-rf /", "--help", "a\x00b", strings.Repeat("x", probe.MaxArg+1)} {
		if _, err := ClaudeArgs(p, "id", 0, "", 0); err == nil {
			t.Errorf("prompt %.20q accepted", p)
		}
	}
	if _, err := ClaudeArgs("ok", "id", 0, "--dangerously-load", 0); err == nil {
		t.Error("a model value starting with '-' accepted")
	}
	if _, err := ClaudeArgs(strings.Repeat("x", probe.MaxArg), "id", 0, "", 0); err != nil {
		t.Errorf("a prompt of exactly MaxArg bytes refused: %v", err)
	}
}

// Prompts with shell metacharacters, newlines, a leading space before a
// dash, and the longest one exec allows reach the CLI byte for byte: the
// fake CLI hashes its last argument.
func TestHostilePromptsReachTheCLIExactly(t *testing.T) {
	for _, p := range []string{
		"$(touch /tmp/pwned) `id` ; rm -rf ~ && echo '\"' | > x",
		"line one\nline two\r\n\ttabbed",
		" -not a flag",
		"ünïcödé ✓ ‮ reversed",
		strings.Repeat("long prompt ", probe.MaxArg/12),
	} {
		r := hostileRunner(t, streamInit+`
for last; do :; done
printf 'sha=%s\n' "$(printf '%s' "$last" | sha256sum | cut -d' ' -f1)" >&2
`+streamResult)
		task := oneTurnTask
		task.Prompts = map[string]string{probe.TierTechnical: p}
		var log bytes.Buffer
		r.Log = &log
		if _, err := r.RunTask(context.Background(), task, probe.TierTechnical); err != nil {
			t.Fatalf("%.30q: %v", p, err)
		}
		sum := sha256.Sum256([]byte(p))
		got, _ := os.ReadFile(filepath.Join(r.Out, "raw", task.ID+"-technical-t0.stderr"))
		if !strings.Contains(string(got), "sha="+hex.EncodeToString(sum[:])) {
			t.Errorf("%.30q did not arrive intact: %s", p, got)
		}
		if _, err := os.Stat("/tmp/pwned"); err == nil {
			t.Fatal("the prompt was run by a shell")
		}
	}
}

// A task id names raw files under the run's output; one that is a path
// must be refused before anything is written, not resolved outside it.
func TestHostileTaskIDsCannotLeaveTheOutput(t *testing.T) {
	r := testRunner(t, testSandbox(t, ""))
	w := Workdir{Root: t.TempDir()}
	for _, id := range []string{"../escape", "a/b", "/abs", "..", ".", ".hidden", "-flag", "", "a\x00b", "a\nb", strings.Repeat("a", 129)} {
		task := oneTurnTask
		task.ID = id
		if _, err := r.Plan(task, probe.TierTechnical, w, "s"); err == nil {
			t.Errorf("task id %q accepted", id)
		}
		if _, err := r.RunTask(context.Background(), task, probe.TierTechnical); err == nil {
			t.Errorf("RunTask ran task id %q", id)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Out), "escape-technical-t0.jsonl")); err == nil {
		t.Error("a raw file was written outside the run's output")
	}
}

// The Redactor catches a secret split across writes at every offset, and
// one written a byte at a time, and passes everything else through.
func TestRedactorAcrossWrites(t *testing.T) {
	text := "before " + fakeToken + " middle " + fakeToken + fakeToken + " after"
	want := strings.ReplaceAll(text, fakeToken, RedactedText)
	for cut := 0; cut <= len(text); cut++ {
		var out bytes.Buffer
		r := NewRedactor(&out, []string{fakeToken, "short"})
		r.Write([]byte(text[:cut]))
		r.Write([]byte(text[cut:]))
		if err := r.Flush(); err != nil {
			t.Fatal(err)
		}
		if out.String() != want {
			t.Fatalf("cut at %d: %q", cut, out.String())
		}
	}
	var out bytes.Buffer
	r := NewRedactor(&out, []string{fakeToken})
	for i := 0; i < len(text); i++ {
		r.Write([]byte{text[i]})
	}
	r.Flush()
	if out.String() != want {
		t.Errorf("byte at a time: %q", out.String())
	}
}

// Text from the CLI cannot drive the operator's terminal through the log.
func TestOneLineNeutralisesTerminalControl(t *testing.T) {
	in := "ok \x1b]52;c;cm0gLXJmIH4=\x07 \x1b[2J ‮gnp.exe \x9b31m \xff end\n\nnext"
	got := OneLine(in)
	for _, bad := range []string{"\x1b", "\x07", "‮", "\x9b", "\xff", "\n"} {
		if strings.Contains(got, bad) {
			t.Errorf("OneLine kept %q: %q", bad, got)
		}
	}
	if !strings.HasPrefix(got, "ok ") || !strings.HasSuffix(got, "end next") {
		t.Errorf("OneLine = %q", got)
	}
	long := OneLine(strings.Repeat("é", 300))
	if !strings.HasSuffix(long, "...") || !strings.HasPrefix(long, "é") || strings.ContainsRune(long, '�') {
		t.Errorf("long text cut badly: %q", long)
	}
}

// An output folder outside the hidden directories is masked in the
// sandbox; one inside them is hidden already and not listed twice.
func TestOutputFolderMasked(t *testing.T) {
	r := testRunner(t, testSandbox(t, ""))
	w := Workdir{Root: t.TempDir()}
	for out, masked := range map[string]bool{"/usr/share": true, r.Out: false} {
		r.Out = out
		turns, err := r.Plan(oneTurnTask, probe.TierTechnical, w, "s")
		if err != nil {
			t.Fatal(err)
		}
		argv := turns[0].Argv
		i := slices.Index(argv, out)
		if got := i > 0 && argv[i-1] == "--tmpfs"; got != masked {
			t.Errorf("out %s masked = %v, want %v: %v", out, got, masked, argv)
		}
	}
}
