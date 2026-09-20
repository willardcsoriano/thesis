package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/executor"
	"synapseos/internal/ollama"
)

func TestConfirm(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"lowercase y", "y\n", true},
		{"full yes", "yes\n", true},
		{"uppercase Y", "Y\n", true},
		{"mixed case Yes", "Yes\n", true},
		{"surrounding whitespace", "  y  \n", true},
		{"explicit no", "n\n", false},
		{"empty line (bare enter)", "\n", false},
		{"garbage input", "sure\n", false},
		{"EOF with no input fails closed", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r := bufio.NewReader(strings.NewReader(tc.input))
			if got := confirm(r, &out, "proceed?"); got != tc.want {
				t.Errorf("confirm(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(out.String(), "proceed?") {
				t.Errorf("expected the prompt to be written to out, got %q", out.String())
			}
		})
	}
}

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
	got := buildStepPrompt("find pdfs", nil, "", "")
	for _, want := range []string{"Task: find pdfs", "What command should run next?"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildStepPrompt with no history = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "Steps already run") {
		t.Errorf("first-step prompt should carry no history section, got %q", got)
	}
}

func TestBuildStepPromptWithHistory(t *testing.T) {
	history := []loopStep{
		{command: "mkdir -p dest", result: executor.Result{ExitCode: 0}},
		{command: "mv *.pdf dest/", result: executor.Result{ExitCode: 1, Stderr: "mv: cannot stat '*.pdf': No such file or directory"}},
	}
	got := buildStepPrompt("organize pdfs", history, "", "")

	for _, want := range []string{
		"Task: organize pdfs",
		"1. $ mkdir -p dest",
		"exit code: 0",
		"2. $ mv *.pdf dest/",
		"exit code: 1",
		"stderr: mv: cannot stat",
		"What command should run next? Output DONE",
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
	got := buildStepPrompt("find things", history, "", "")
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected long stdout to be truncated in the prompt")
	}
}

// --- generation options (M6 step 1) ----------------------------------

// TestGenerationOptionsAlwaysSetsNumCtx guards the silent-truncation
// defect. Ollama's default num_ctx is 2048 and it enforces that by
// quietly discarding the front of an oversized prompt — no error, no
// warning, just a model that never saw its own instructions. Because the
// failure is invisible at runtime, the only place it can be caught is
// here.
func TestGenerationOptionsAlwaysSetsNumCtx(t *testing.T) {
	opts := generationOptions()

	got, ok := opts["num_ctx"]
	if !ok {
		t.Fatal("num_ctx missing from generation options — Ollama would silently truncate to 2048")
	}
	if got != defaultNumCtx {
		t.Errorf("num_ctx = %v, want %d", got, defaultNumCtx)
	}
	if defaultNumCtx <= 2048 {
		t.Errorf("defaultNumCtx = %d, which is no better than the silent default it exists to replace", defaultNumCtx)
	}
	if opts["temperature"] != 0 {
		t.Errorf("temperature = %v, want 0 — decoding must stay deterministic", opts["temperature"])
	}
}

func TestEnvIntOr(t *testing.T) {
	const key = "SYNAPSE_TEST_INT"
	cases := []struct {
		name  string
		set   bool
		value string
		want  int
	}{
		{"unset falls back", false, "", 4096},
		{"valid override", true, "16384", 16384},
		{"empty falls back", true, "", 4096},
		{"unparseable falls back", true, "banana", 4096},
		{"zero falls back", true, "0", 4096},
		{"negative falls back", true, "-1", 4096},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(key, tc.value)
			}
			if got := envIntOr(key, 4096); got != tc.want {
				t.Errorf("envIntOr(%q=%q) = %d, want %d", key, tc.value, got, tc.want)
			}
		})
	}
}

// TestProposeStepSendsNumCtxOnTheWire is the end-to-end guard: it asserts
// the option actually reaches Ollama in the request body, not merely that
// the map was built correctly. A helper that returns the right map but is
// never wired into the request would pass the unit test above and still
// leave prompts being truncated.
func TestProposeStepSendsNumCtxOnTheWire(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "ls", Done: true})
	}))
	defer server.Close()

	if _, _, err := proposeStep(context.Background(), ollama.New(server.URL), "m", "task", nil, nil, nil); err != nil {
		t.Fatalf("proposeStep error: %v", err)
	}

	opts, ok := body["options"].(map[string]any)
	if !ok {
		t.Fatalf("request carried no options object; body was %#v", body)
	}
	if _, ok := opts["num_ctx"]; !ok {
		t.Errorf("num_ctx never reached the wire; options were %#v", opts)
	}
}

// The working directory is what "here" and "this folder" resolve to. Without
// it the model emits a literal placeholder (`du -sh /path/to/folder`) or
// substitutes a familiar system path (/var/log for "the log files here") —
// measured 2026-09-14 as the largest single cause of Layer 7's plain-tier
// failures. These tests fail if the line is dropped from either prompt.
func TestStepPromptStatesWorkingDirectory(t *testing.T) {
	t.Run("present with no history", func(t *testing.T) {
		got := buildStepPrompt("tidy up here", nil, "", "/home/u/work")
		if !strings.Contains(got, "Working directory: /home/u/work") {
			t.Errorf("cwd missing from prompt:\n%s", got)
		}
		if !strings.Contains(got, "Task: tidy up here") {
			t.Errorf("task missing from prompt:\n%s", got)
		}
	})

	t.Run("stated before the task", func(t *testing.T) {
		// Ordering matters: the directory is context the task is read
		// against, so it has to arrive first.
		got := buildStepPrompt("tidy up here", nil, "", "/home/u/work")
		if strings.Index(got, "Working directory:") > strings.Index(got, "Task:") {
			t.Errorf("cwd stated after the task:\n%s", got)
		}
	})

	t.Run("survives history and prior session", func(t *testing.T) {
		h := []loopStep{{command: "ls", result: executor.Result{ExitCode: 0}}}
		got := buildStepPrompt("tidy up here", h, "Earlier in this session: x", "/home/u/work")
		if !strings.Contains(got, "Working directory: /home/u/work") {
			t.Errorf("cwd missing when history present:\n%s", got)
		}
	})

	t.Run("omitted cleanly when unknown", func(t *testing.T) {
		got := buildStepPrompt("tidy up here", nil, "", "")
		if strings.Contains(got, "Working directory:") {
			t.Errorf("empty cwd should emit no line, got:\n%s", got)
		}
	})
}

func TestSystemPromptForbidsPlaceholderPaths(t *testing.T) {
	for _, want := range []string{"/path/to/folder", "placeholder", "/var/log", `"."`} {
		if !strings.Contains(loopSystemPrompt, want) {
			t.Errorf("loopSystemPrompt no longer mentions %q; the grounding rule has been weakened", want)
		}
	}
}

// Guards the seam, not the helper. buildStepPrompt can render the working
// directory perfectly while proposeStep never calls os.Getwd() and passes ""
// — every unit test above would still pass and every live prompt would go out
// ungrounded. This asserts the directory reaches the wire.
func TestProposeStepSendsWorkingDirectoryOnTheWire(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "ls", Done: true})
	}))
	defer server.Close()

	dir := t.TempDir()
	t.Chdir(dir)

	if _, _, err := proposeStep(context.Background(), ollama.New(server.URL), "m", "tidy up here", nil, nil, nil); err != nil {
		t.Fatalf("proposeStep error: %v", err)
	}

	prompt, _ := body["prompt"].(string)
	// The temp dir resolves through /tmp, which may be a symlink; compare on
	// the leaf so the assertion is about grounding, not path canonicalisation.
	leaf := filepath.Base(dir)
	if !strings.Contains(prompt, "Working directory:") || !strings.Contains(prompt, leaf) {
		t.Errorf("prompt sent to the model carries no working directory:\n%s", prompt)
	}
}

// A task with nothing run yet must never be offered DONE. Layer 7's technical
// tier fell 100% -> 58.3% on 2026-09-14 when adding the working directory
// routed first steps through the with-history trailer: every failure was the
// model answering DONE before running anything, because a phrasing like
// "du -sh ." already reads as a finished command. loopSystemPrompt states the
// same rule; this keeps the rendered prompt honest about it.
func TestFirstStepPromptDoesNotOfferDone(t *testing.T) {
	cases := []struct {
		name          string
		prior, cwd    string
		history       []loopStep
		wantDoneOffer bool
	}{
		{name: "bare first step", wantDoneOffer: false},
		{name: "first step with cwd", cwd: "/home/u/work", wantDoneOffer: false},
		{name: "first step with prior session", prior: "Earlier in this session: x", wantDoneOffer: false},
		{name: "first step with both", prior: "Earlier in this session: x", cwd: "/home/u/work", wantDoneOffer: false},
		{
			name:          "after a step has run",
			cwd:           "/home/u/work",
			history:       []loopStep{{command: "ls", result: executor.Result{ExitCode: 0}}},
			wantDoneOffer: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildStepPrompt("du -sh .", tc.history, tc.prior, tc.cwd)
			offered := strings.Contains(got, "DONE")
			if offered != tc.wantDoneOffer {
				t.Errorf("DONE offered = %v, want %v\nprompt:\n%s", offered, tc.wantDoneOffer, got)
			}
			if !tc.wantDoneOffer && !strings.Contains(got, "What command should run next?") {
				t.Errorf("first-step prompt does not ask for a command:\n%s", got)
			}
		})
	}
}

// Greetings and capability questions are answered locally, per D31. Before
// this existed they fell through to the model, which emitted UNSUPPORTED and
// told the user their "hello" was a visual task like editing images — the
// first thing anyone types, and so the system's first impression.
func TestAnswerConversational(t *testing.T) {
	t.Run("greetings are answered", func(t *testing.T) {
		for _, in := range []string{"hello", "Hello", "  hi  ", "hey", "Good morning", "hello!", "hi."} {
			var b strings.Builder
			if !answerConversational(in, &b) {
				t.Errorf("%q was not recognised as a greeting", in)
			}
			if b.Len() == 0 {
				t.Errorf("%q produced no reply", in)
			}
		}
	})

	t.Run("capability questions are answered without the model", func(t *testing.T) {
		var b strings.Builder
		if !answerConversational("what can you do", &b) {
			t.Fatal("capability question not handled")
		}
		got := b.String()
		for _, want := range []string{"command line", "cannot", "undo"} {
			if !strings.Contains(got, want) {
				t.Errorf("capability reply missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("identity questions are answered locally, never improvised", func(t *testing.T) {
		// Found in live testing 2026-09-15: "what ai model are you" and "are
		// you human" both reached the shell-capability message. Answering
		// them from the model would be worse — a 3B coder model improvising
		// its own identity is the confabulation risk of row 15 aimed at the
		// one subject the system should be authoritative about.
		for _, in := range []string{"what ai model are you", "are you human", "are you an ai", "are you chatgpt"} {
			var b strings.Builder
			if !answerConversational(in, &b) {
				t.Errorf("%q was not answered locally", in)
			}
			got := b.String()
			if !strings.Contains(got, "not a person") {
				t.Errorf("%q: reply does not say it is a program:\n%s", in, got)
			}
			if !strings.Contains(got, "leaves this computer") {
				t.Errorf("%q: reply omits that inference is local:\n%s", in, got)
			}
		}
	})

	t.Run("a task with a greeting attached is still a task", func(t *testing.T) {
		// The dangerous direction: swallowing real work because it opened
		// politely. Exact matching on the normalised input is what prevents it.
		for _, in := range []string{
			"hi, delete the logs",
			"hello world",
			"say hi to the user",
			"help me find the largest file",
			"what can you do about the disk space",
		} {
			var b strings.Builder
			if answerConversational(in, &b) {
				t.Errorf("%q was swallowed as conversation; it is a task", in)
			}
		}
	})
}

// TestMain turns the effect analysis off for the tests that predate it, so they
// keep exercising the list classifier they were written against. Tests of the
// analysis set SYNAPSE_ANALYSIS themselves.
func TestMain(m *testing.M) {
	if os.Getenv("SYNAPSE_ANALYSIS") == "" {
		os.Setenv("SYNAPSE_ANALYSIS", "off")
	}
	os.Exit(m.Run())
}
