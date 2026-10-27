package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"synapseos/internal/ollama"
)

// runMainEnv makes the test binary run main() (see TestMain).
const runMainEnv = "SYNAPSE_TEST_RUN_MAIN"

func TestParseArgsAccepts(t *testing.T) {
	cases := []struct {
		args []string
		want invocation
	}{
		{[]string{"help"}, invocation{mode: modeHelp}},
		{[]string{"-h"}, invocation{mode: modeHelp}},
		{[]string{"--help"}, invocation{mode: modeHelp}},
		{[]string{"version"}, invocation{mode: modeVersion}},
		{[]string{"--version"}, invocation{mode: modeVersion}},
		{[]string{"repl"}, invocation{mode: modeREPL}},
		{[]string{"scratch"}, invocation{mode: modeScratch}},
		{[]string{"undo"}, invocation{mode: modeUndo}},
		{[]string{"demo"}, invocation{mode: modeDemo}},
		{[]string{"tui"}, invocation{mode: modeTUI}},
		{[]string{"tui", "scratch"}, invocation{mode: modeTUIScratch}},
		{[]string{"run", "list", "the", "files"}, invocation{mode: modeRun, task: "list the files"}},
		{[]string{"run", "list the files"}, invocation{mode: modeRun, task: "list the files"}},
		{[]string{"run", "  padded  "}, invocation{mode: modeRun, task: "padded"}},
		{[]string{"run", "--", "-rf is a flag"}, invocation{mode: modeRun, task: "-rf is a flag"}},
		{[]string{"run", "undo", "--help"}, invocation{mode: modeRun, task: "undo --help"}},
		{[]string{"--", "--help"}, invocation{mode: modeRun, task: "--help"}},
		{[]string{"--", "list", "files"}, invocation{mode: modeRun, task: "list files"}},
		{[]string{"run", "--json", "list files"}, invocation{mode: modeRun, task: "list files", json: true}},
		{[]string{"run", "--json", "--", "-x"}, invocation{mode: modeRun, task: "-x", json: true}},
		{[]string{"run", "list", "--json"}, invocation{mode: modeRun, task: "list --json"}},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if err != nil {
			t.Errorf("parseArgs(%q): unexpected error %v", c.args, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseArgs(%q) = %+v, want %+v", c.args, got, c.want)
		}
	}
}

// Every one of these was, before parseArgs, joined into a task and sent to the
// model. Each must now be refused as a usage error.
func TestParseArgsRefuses(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"list", "the", "files"},
		{"--json"},
		{"-x"},
		{"--help", "extra"},
		{"help", "me"},
		{"version", "--short"},
		{"undo", "--dry-run"},
		{"undo", "that"},
		{"repl", "now"},
		{"scratch", "x"},
		{"demo", "x"},
		{"tui", "fullscreen"},
		{"tui", "scratch", "x"},
		{"run"},
		{"run", ""},
		{"run", "   ", "\t"},
		{"run", "--json", "--json", "list files"},
		{"run", "--json"},
		{"run", "--json", "--"},
		{"run", "--yes", "list files"},
		{"run", "-"},
		{"run", "--"},
		{"--"},
		{"--", "  "},
		{"Run", "list files"},
		{"UNDO"},
		{""},
	}
	for _, args := range cases {
		got, err := parseArgs(args)
		if err == nil {
			t.Errorf("parseArgs(%q) = %+v, want a usage error", args, got)
			continue
		}
		if !isUsageError(err) {
			t.Errorf("parseArgs(%q): error %v is not a usage error", args, err)
		}
	}
}

func TestIsUsageErrorRejectsOtherErrors(t *testing.T) {
	if isUsageError(errors.New("boom")) {
		t.Fatal("a plain error was classified as a usage error")
	}
}

// runSynapse executes the real program (main, via TestMain) with args, against
// a fake Ollama that counts every request it receives and answers none.
func runSynapse(t *testing.T, args ...string) (stdout, stderr string, code int, requests int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "this fake model answers nothing", http.StatusInternalServerError)
	}))
	defer srv.Close()
	r := execSynapse(t, synapseRun{args: args, env: []string{"SYNAPSE_OLLAMA=" + srv.URL}})
	return r.stdout, r.stderr, r.code, hits.Load()
}

// synapseRun is one execution of the real program.
type synapseRun struct {
	args  []string
	env   []string // added to a clean SYNAPSE_* environment
	stdin string
	// holdStdin keeps stdin open after stdin is written, as a person who has
	// not answered yet would, until the process exits.
	holdStdin bool
	stdinFile string // a file to use as stdin, such as /dev/zero
	// closeStdout closes the read end of stdout right after start, as
	// `synapse run … | head -1` does once head has its line.
	closeStdout bool
	dir         string // working directory; a fresh temp dir when empty
	home        string // $HOME; a fresh temp dir when empty
	// started, when set, is called with the running process, e.g. to signal it.
	started func(p *os.Process)
}

type synapseResult struct {
	stdout, stderr string
	code           int
	maxRSSKiB      int64 // peak resident memory of synapse (children excluded)
}

func execSynapse(t *testing.T, r synapseRun) synapseResult {
	t.Helper()
	cmd := exec.Command(os.Args[0], r.args...)
	// Start from this process's environment minus every SYNAPSE_* variable,
	// so a developer's own settings cannot change what the test observes.
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "SYNAPSE_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env,
		runMainEnv+"=1",
		// The shipped default, strict analysis, is what a user runs, so the
		// CLI contract is tested under it. TestMain sets SYNAPSE_ANALYSIS=off
		// for the older loop tests; it is stripped above with the rest.
	)
	home := r.home
	if home == "" {
		home = t.TempDir() // the undo journal must never touch the real home
	}
	cmd.Env = append(cmd.Env, "HOME="+home)
	cmd.Env = append(cmd.Env, r.env...)
	cmd.Dir = r.dir
	if cmd.Dir == "" {
		cmd.Dir = t.TempDir()
	}
	var hold io.WriteCloser
	if r.holdStdin {
		w, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		hold = w
		defer w.Close()
	} else if r.stdinFile != "" {
		f, err := os.Open(r.stdinFile)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cmd.Stdin = f
	} else {
		cmd.Stdin = strings.NewReader(r.stdin) // EOF after it: every further prompt is declined
	}
	var out, errb bytes.Buffer
	cmd.Stderr = &errb
	var stdoutPipe io.ReadCloser
	if r.closeStdout {
		p, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdoutPipe = p
	} else {
		cmd.Stdout = &out
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting synapse %q: %v", r.args, err)
	}
	if stdoutPipe != nil {
		stdoutPipe.Close()
	}
	if hold != nil && r.stdin != "" {
		io.WriteString(hold, r.stdin)
	}
	if r.started != nil {
		r.started(cmd.Process)
	}
	err := cmd.Wait()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running synapse %q: %v", r.args, err)
		}
		code = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			code = 128 + int(ws.Signal())
		}
	}
	res := synapseResult{stdout: out.String(), stderr: errb.String(), code: code}
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		res.maxRSSKiB = ru.Maxrss
	}
	return res
}

// fakeModel is an Ollama stand-in that serves /api/tags and answers each
// step request with the next scripted decision; the plain-language reply
// (D31) gets a fixed text without consuming one. Past the script it answers
// "done", so a test's script is exactly the commands it expects to run.
type fakeModel struct {
	srv   *httptest.Server
	steps atomic.Int64
}

func newFakeModel(t *testing.T, decisions ...stepDecision) *fakeModel {
	t.Helper()
	f := &fakeModel{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("You are reporting the outcome of a task")) {
			json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: "fake answer", Done: true, EvalCount: 1})
			return
		}
		i := int(f.steps.Add(1)) - 1
		d := stepDecision{Action: "done"}
		if i < len(decisions) {
			d = decisions[i]
		}
		raw, _ := json.Marshal(d)
		json.NewEncoder(w).Encode(ollama.GenerateResponse{Response: string(raw), Done: true, EvalCount: 1})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModel) env() []string { return []string{"SYNAPSE_OLLAMA=" + f.srv.URL} }

func runStep(cmd string) stepDecision { return stepDecision{Action: "run", Command: cmd} }

// The defect this parser fixes, observed end to end: a refused command line
// exits 2, prints usage to stderr, and never sends a single request to the model.
func TestRefusedCommandLinesNeverReachTheModel(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"list", "the", "files"},
		{"undo", "--dry-run"},
		{"--json"},
		{"run"},
		{"tui", "fullscreen"},
	} {
		stdout, stderr, code, requests := runSynapse(t, args...)
		if code != exitUsage {
			t.Errorf("synapse %q exited %d, want %d", args, code, exitUsage)
		}
		if requests != 0 {
			t.Errorf("synapse %q sent %d request(s) to the model, want 0", args, requests)
		}
		if stdout != "" {
			t.Errorf("synapse %q wrote to stdout on a usage error: %q", args, stdout)
		}
		if !strings.Contains(stderr, "Usage:") {
			t.Errorf("synapse %q: stderr carries no usage text: %q", args, stderr)
		}
	}
}

func TestHelpAndVersionNeverReachTheModel(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--version"}, {"version"}} {
		stdout, _, code, requests := runSynapse(t, args...)
		if code != 0 || requests != 0 {
			t.Errorf("synapse %q: exit %d, %d model request(s); want 0 and 0", args, code, requests)
		}
		if strings.TrimSpace(stdout) == "" {
			t.Errorf("synapse %q printed nothing on stdout", args)
		}
	}
	stdout, _, _, _ := runSynapse(t, "--help")
	for _, want := range []string{defaultModel, "synapse run", "Exit codes"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
	if v, _, _, _ := runSynapse(t, "version"); !strings.HasPrefix(v, "synapse ") {
		t.Errorf("version output %q does not start with \"synapse \"", v)
	}
}

// The positive side: an accepted task does reach the model.
func TestRunReachesTheModel(t *testing.T) {
	_, _, _, requests := runSynapse(t, "run", "list the files here")
	if requests == 0 {
		t.Fatal("synapse run sent no request to the model")
	}
}

// Every way the Makefile starts synapse is a command line the parser accepts.
// The CLI contract (D48) changed what synapse accepts, and make run and make
// task kept the old forms, which then exited 2 (ship review, 2026-10-27).
func TestMakefileInvocationsParse(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\$\(SYNAPSE\)([^\n;&|]*)`)
	found := 0
	for _, m := range re.FindAllStringSubmatch(string(mk), -1) {
		var args []string
		for _, f := range strings.Fields(m[1]) {
			args = append(args, strings.Trim(strings.ReplaceAll(f, "$(TASK)", "find big files"), `"`))
		}
		found++
		if _, err := parseArgs(args); err != nil {
			t.Errorf("Makefile runs synapse %q, which the CLI refuses: %v", strings.Join(args, " "), err)
		}
	}
	if found < 5 {
		t.Fatalf("found only %d synapse invocations in the Makefile; the scan is broken", found)
	}
}
