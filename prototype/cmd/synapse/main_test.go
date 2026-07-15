package main

import (
	"strings"
	"testing"

	"synapseos/internal/executor"
)

func TestCleanCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare command", "ls -la", "ls -la"},
		{"surrounding whitespace", "  ls -la  \n", "ls -la"},
		{"triple backtick fence no lang", "```\nls -la\n```", "ls -la"},
		{"triple backtick fence with lang", "```bash\nls -la\n```", "ls -la"},
		{"inline single backticks", "`ls -la`", "ls -la"},
		{"inline single backticks with whitespace", "  `ls -la`  ", "ls -la"},
		{"unsupported sentinel untouched", "UNSUPPORTED", "UNSUPPORTED"},
		{"single backtick alone is not stripped", "`", "`"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanCommand(tc.in); got != tc.want {
				t.Errorf("cleanCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 500); got != "short" {
		t.Errorf("truncate of a short string should be unchanged, got %q", got)
	}
	long := strings.Repeat("x", 600)
	got := truncate(long, 500)
	if len(got) <= 500 {
		t.Errorf("expected truncated output to include the marker beyond the limit, got len %d", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("x", 500)) {
		t.Errorf("truncate should preserve the first n bytes verbatim")
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncated output should say so, got %q", got)
	}
}

func TestBuildStepPromptFirstStep(t *testing.T) {
	got := buildStepPrompt("find pdfs", nil)
	want := "Task: find pdfs"
	if got != want {
		t.Errorf("buildStepPrompt with no history = %q, want %q", got, want)
	}
}

func TestBuildStepPromptWithHistory(t *testing.T) {
	history := []loopStep{
		{command: "mkdir -p dest", result: executor.Result{ExitCode: 0}},
		{command: "mv *.pdf dest/", result: executor.Result{ExitCode: 1, Stderr: "mv: cannot stat '*.pdf': No such file or directory"}},
	}
	got := buildStepPrompt("organize pdfs", history)

	for _, want := range []string{
		"Task: organize pdfs",
		"1. $ mkdir -p dest",
		"exit code: 0",
		"2. $ mv *.pdf dest/",
		"exit code: 1",
		"stderr: mv: cannot stat",
		"What is the next command? Output DONE",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("buildStepPrompt output missing %q\ngot:\n%s", want, got)
		}
	}
}

func TestBuildStepPromptTruncatesLongOutput(t *testing.T) {
	history := []loopStep{
		{command: "find /", result: executor.Result{ExitCode: 0, Stdout: strings.Repeat("line\n", 1000)}},
	}
	got := buildStepPrompt("find things", history)
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected long stdout to be truncated in the prompt")
	}
}
