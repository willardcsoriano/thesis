package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run the real program end to end against a scripted fake model
// and observe what a script or an agent would: exit code, stdout, stderr, and
// the files left behind (D48).

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExitCodeDeclinedWhenStdinIsClosed(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	writeFile(t, keep, "data")
	m := newFakeModel(t, runStep("rm keep.txt"))

	r := execSynapse(t, synapseRun{args: []string{"run", "delete keep.txt"}, env: m.env(), dir: dir})
	if r.code != exitDeclined {
		t.Fatalf("exit %d, want %d (declined); stderr:\n%s", r.code, exitDeclined, r.stderr)
	}
	if !fileExists(keep) {
		t.Fatal("keep.txt was deleted although nobody approved")
	}
	if strings.Contains(r.stdout, "[y/N]") {
		t.Errorf("the confirmation prompt went to stdout: %q", r.stdout)
	}
	if !strings.Contains(r.stderr, "[y/N]") {
		t.Errorf("the confirmation prompt is not on stderr: %q", r.stderr)
	}
}

func TestExitCodeApprovedOnStdinRuns(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.txt")
	writeFile(t, gone, "data")
	m := newFakeModel(t, runStep("rm gone.txt"))

	r := execSynapse(t, synapseRun{args: []string{"run", "delete gone.txt"}, env: m.env(), dir: dir, stdin: "y\n"})
	if r.code != exitOK {
		t.Fatalf("exit %d, want 0; stderr:\n%s", r.code, r.stderr)
	}
	if fileExists(gone) {
		t.Fatal("gone.txt still exists after an approved rm")
	}
}

// Only an explicit yes approves. Anything else, including words that mean
// yes to a person but are not y/yes, declines.
func TestOnlyAnExplicitYesApproves(t *testing.T) {
	for _, answer := range []string{"\n", "n\n", "yeah\n", "Y es\n", "ok\n", "1\n", "\x00\n"} {
		dir := t.TempDir()
		keep := filepath.Join(dir, "keep.txt")
		writeFile(t, keep, "data")
		m := newFakeModel(t, runStep("rm keep.txt"))
		r := execSynapse(t, synapseRun{args: []string{"run", "delete keep.txt"}, env: m.env(), dir: dir, stdin: answer})
		if r.code != exitDeclined || !fileExists(keep) {
			t.Errorf("answer %q: exit %d, keep.txt exists=%v; want declined and kept", answer, r.code, fileExists(keep))
		}
	}
}

func TestExitCodeUnsupported(t *testing.T) {
	m := newFakeModel(t, stepDecision{Action: "unsupported"})
	r := execSynapse(t, synapseRun{args: []string{"run", "paint my house"}, env: m.env()})
	if r.code != exitUnsupported {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitUnsupported, r.stderr)
	}
}

func TestExitCodeStepLimit(t *testing.T) {
	var steps []stepDecision
	for i := 0; i <= maxLoopSteps; i++ {
		steps = append(steps, runStep("echo step"+strings.Repeat("x", i)))
	}
	m := newFakeModel(t, steps...)
	r := execSynapse(t, synapseRun{args: []string{"run", "keep echoing"}, env: m.env()})
	if r.code != exitStepLimit {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitStepLimit, r.stderr)
	}
}

func TestExitCodeModelUnreachable(t *testing.T) {
	r := execSynapse(t, synapseRun{args: []string{"run", "list files"}, env: []string{"SYNAPSE_OLLAMA=http://127.0.0.1:1"}})
	if r.code != exitModelUnavailable {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitModelUnavailable, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout carries a diagnostic: %q", r.stdout)
	}
}

func TestStdoutCarriesOnlyResults(t *testing.T) {
	m := newFakeModel(t, runStep("echo hello-from-the-task"))
	r := execSynapse(t, synapseRun{args: []string{"run", "say hello"}, env: m.env()})
	if r.code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "hello-from-the-task") {
		t.Errorf("the command's output is not on stdout: %q", r.stdout)
	}
	for _, diag := range []string{"model:", "endpoint:", "Each step may run"} {
		if strings.Contains(r.stdout, diag) {
			t.Errorf("stdout carries the diagnostic %q: %q", diag, r.stdout)
		}
	}
}

// readJSONLines parses every stdout line as JSON, failing on any that is not.
func readJSONLines(t *testing.T, stdout string) []jsonRecord {
	t.Helper()
	var recs []jsonRecord
	sc := bufio.NewScanner(strings.NewReader(stdout))
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		var r jsonRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("stdout line is not JSON: %.300q (%v)", sc.Text(), err)
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading the --json stream: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("--json wrote nothing")
	}
	return recs
}

func TestJSONStreamEndsWithTheProcessOutcome(t *testing.T) {
	cases := []struct {
		name    string
		steps   []stepDecision
		outcome string
		code    int
	}{
		{"complete", []stepDecision{runStep("printf '%s\\n' 'a<b&c'")}, outcomeComplete, exitOK},
		{"declined", []stepDecision{runStep("rm keep.txt")}, outcomeDeclined, exitDeclined},
		{"unsupported", []stepDecision{{Action: "unsupported"}}, outcomeUnsupported, exitUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "keep.txt"), "data")
			m := newFakeModel(t, c.steps...)
			r := execSynapse(t, synapseRun{args: []string{"run", "--json", "do it"}, env: m.env(), dir: dir})
			recs := readJSONLines(t, r.stdout)
			end := recs[len(recs)-1]
			if end.Type != "end" || end.Outcome != c.outcome || end.ExitCode == nil || *end.ExitCode != c.code {
				t.Fatalf("last record %+v, want end/%s/%d", end, c.outcome, c.code)
			}
			if r.code != c.code {
				t.Errorf("process exited %d but the stream says %d", r.code, c.code)
			}
			for _, rec := range recs[:len(recs)-1] {
				if rec.Type == "end" || rec.Type == "unknown" || rec.Type == "" {
					t.Errorf("unexpected record type %q in the stream", rec.Type)
				}
			}
		})
	}
}

// A result's exit code 0 must be present, not dropped as an empty field, and
// command text must survive unescaped.
func TestJSONResultKeepsZeroExitAndRawText(t *testing.T) {
	m := newFakeModel(t, runStep("printf '%s\\n' 'a<b&c'"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "print it"}, env: m.env()})
	recs := readJSONLines(t, r.stdout)
	var sawResult bool
	for _, rec := range recs {
		if rec.Type != "result" {
			continue
		}
		sawResult = true
		if rec.ExitCode == nil || *rec.ExitCode != 0 {
			t.Errorf("result exit_code = %v, want 0 present", rec.ExitCode)
		}
		if rec.Stdout != "a<b&c\n" {
			t.Errorf("result stdout = %q", rec.Stdout)
		}
	}
	if !sawResult {
		t.Fatalf("no result record in %+v", recs)
	}
	if !strings.Contains(r.stdout, `a<b&c`) {
		t.Error("--json escaped < and & instead of writing them as is")
	}
}

func TestJSONWhenTheModelIsUnreachable(t *testing.T) {
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "list files"}, env: []string{"SYNAPSE_OLLAMA=http://127.0.0.1:1"}})
	recs := readJSONLines(t, r.stdout)
	if len(recs) != 1 || recs[0].Type != "end" || recs[0].Outcome != outcomeModelUnavailable {
		t.Fatalf("records %+v, want a single end/model_unavailable", recs)
	}
	if r.code != exitModelUnavailable {
		t.Errorf("exit %d, want %d", r.code, exitModelUnavailable)
	}
}

func TestJSONConversationalReply(t *testing.T) {
	m := newFakeModel(t)
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "hello"}, env: m.env()})
	recs := readJSONLines(t, r.stdout)
	if len(recs) != 2 || recs[0].Type != "answer" || recs[0].Text == "" || recs[1].Outcome != outcomeComplete {
		t.Fatalf("records %+v, want an answer then end/complete", recs)
	}
}

// Ctrl+C during a running command stops it, records the outcome, and exits 130.
func TestInterruptStopsTheTaskAndExits130(t *testing.T) {
	dir := t.TempDir()
	m := newFakeModel(t, runStep("sleep 30"))
	start := time.Now()
	r := execSynapse(t, synapseRun{
		args: []string{"run", "--json", "wait a while"}, env: m.env(), dir: dir,
		started: func(p *os.Process) {
			go func() {
				// Long enough for the loop to reach the command; the
				// assertion below does not depend on the exact moment.
				time.Sleep(1500 * time.Millisecond)
				p.Signal(syscall.SIGINT)
			}()
		},
	})
	if took := time.Since(start); took > 20*time.Second {
		t.Fatalf("took %s: the interrupt did not stop the running command", took)
	}
	if r.code != exitInterrupted {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitInterrupted, r.stderr)
	}
	recs := readJSONLines(t, r.stdout)
	if end := recs[len(recs)-1]; end.Type != "end" || end.Outcome != outcomeInterrupted {
		t.Errorf("last record %+v, want end/interrupted", end)
	}
}

func TestInvalidConfigurationIsRefusedBeforeTheModel(t *testing.T) {
	for _, kv := range []string{
		"SYNAPSE_ANALYSIS=strcit",
		"SYNAPSE_RESOLVE=sometimes",
		"SYNAPSE_NUM_CTX=0",
		"SYNAPSE_NUM_CTX=8k",
		"SYNAPSE_KEEP_ALIVE=forever",
		"SYNAPSE_OLLAMA=localhost:11434",
		"SYNAPSE_OLLAMA=ftp://host",
		"SYNAPSE_SESSION_LOG=/tmp/x.jsonl",
		"SYNAPSE_NUM_CTX=16",
		"SYNAPSE_MODEL=two words",
		"SYNAPSE_MODEL=qwen\nx",
		"SYNAPSE_MODEL= ",
		"SYNAPSE_OLLAMA=http://h:99999",
		"SYNAPSE_OLLAMA=http://h/?q=1",
		"SYNAPSE_OLLAMA=http://h/#f",
	} {
		m := newFakeModel(t)
		env := append(m.env(), kv)
		if strings.HasPrefix(kv, "SYNAPSE_OLLAMA=") {
			env = []string{kv}
		}
		r := execSynapse(t, synapseRun{args: []string{"run", "list files"}, env: env})
		if r.code != exitUsage {
			t.Errorf("%s: exit %d, want %d; stderr:\n%s", kv, r.code, exitUsage, r.stderr)
		}
		if m.steps.Load() != 0 {
			t.Errorf("%s: the model was asked for a step", kv)
		}
		name := kv[:strings.IndexByte(kv, '=')]
		if !strings.Contains(r.stderr, name) {
			t.Errorf("%s: the error does not name the variable: %q", kv, r.stderr)
		}
	}
}

func TestValidConfigurationIsAccepted(t *testing.T) {
	m := newFakeModel(t)
	env := append(m.env(), "SYNAPSE_ANALYSIS=Capture", "SYNAPSE_RESOLVE=never",
		"SYNAPSE_NUM_CTX=4096", "SYNAPSE_KEEP_ALIVE=-1", "SYNAPSE_MODEL=anything")
	r := execSynapse(t, synapseRun{args: []string{"run", "hello"}, env: env})
	if r.code != exitOK {
		t.Fatalf("exit %d; stderr:\n%s", r.code, r.stderr)
	}
}

// undo is the safety net: a bad setting meant for the model must not block it.
func TestUndoIgnoresModelConfiguration(t *testing.T) {
	r := execSynapse(t, synapseRun{args: []string{"undo"}, env: []string{"SYNAPSE_ANALYSIS=bogus", "SYNAPSE_OLLAMA=nonsense"}})
	if r.code != exitOK || !strings.Contains(r.stdout, "Nothing to undo") {
		t.Fatalf("exit %d, stdout %q; want 0 and nothing to undo", r.code, r.stdout)
	}
}

// Every accepted value of the analysis switches is one their parsers act on.
func TestEnvChoicesMatchTheirParsers(t *testing.T) {
	for _, v := range analysisChoices {
		t.Setenv("SYNAPSE_ANALYSIS", v)
		p, on := analysisPolicy()
		switch strings.ToLower(v) {
		case "strict":
			if !on || p != "strict" {
				t.Errorf("SYNAPSE_ANALYSIS=%s gave %q on=%v", v, p, on)
			}
		case "capture":
			if !on || p != "capture" {
				t.Errorf("SYNAPSE_ANALYSIS=%s gave %q on=%v", v, p, on)
			}
		default:
			if on {
				t.Errorf("SYNAPSE_ANALYSIS=%s left the analysis on", v)
			}
		}
	}
	for _, v := range resolveChoices {
		if err := oneOf(resolveChoices)(v); err != nil {
			t.Errorf("%s refused", v)
		}
	}
}

func TestHelpDocumentsEveryVariableAndExitCode(t *testing.T) {
	var b strings.Builder
	printUsage(&b)
	help := b.String()
	for _, e := range envVars {
		if !strings.Contains(help, e.name) {
			t.Errorf("help does not document %s", e.name)
		}
	}
	codes := []int{exitOK, exitFailed, exitUsage, exitDeclined, exitUnsupported, exitStepLimit, exitModelUnavailable, exitNotUndoable, exitInterrupted}
	// A stop by signal exits 128 plus its number, so each signal synapse
	// handles is an exit code too: the hand-written list above missed 129 and
	// 143 (ship review, 2026-10-27).
	for _, sig := range stopSignals {
		codes = append(codes, 128+int(sig.(syscall.Signal)))
	}
	for _, code := range codes {
		if !strings.Contains(help, "\n  "+strconv.Itoa(code)+" ") {
			t.Errorf("help does not list exit code %d", code)
		}
	}
}

// docs/cli.md quotes the help text; the quote must be the program's.
func TestCLIDocMatchesHelp(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	const begin, end = "<!-- help:begin -->\n```\n", "```\n<!-- help:end -->"
	i, j := strings.Index(doc, begin), strings.Index(doc, end)
	if i < 0 || j < i {
		t.Fatal("docs/cli.md has no help:begin/help:end block")
	}
	var b strings.Builder
	printUsage(&b)
	if got := doc[i+len(begin) : j]; got != b.String() {
		t.Errorf("docs/cli.md's help text differs from `synapse help`; paste the current output between the markers.\n--- doc ---\n%s\n--- program ---\n%s", got, b.String())
	}
}

// Ctrl+C while a one-shot task waits at a confirmation prompt ends it at once,
// with nothing run and the outcome recorded, even though stdin is still open.
func TestInterruptAtAConfirmationPromptExitsAtOnce(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	writeFile(t, keep, "data")
	m := newFakeModel(t, runStep("rm keep.txt"))
	start := time.Now()
	r := execSynapse(t, synapseRun{
		args: []string{"run", "--json", "delete keep.txt"}, env: m.env(), dir: dir, holdStdin: true,
		started: func(p *os.Process) {
			go func() {
				time.Sleep(1500 * time.Millisecond) // well past reaching the prompt
				p.Signal(syscall.SIGINT)
			}()
		},
	})
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("took %s: the prompt kept waiting after Ctrl+C", took)
	}
	if r.code != exitInterrupted {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitInterrupted, r.stderr)
	}
	if !fileExists(keep) {
		t.Fatal("keep.txt was deleted")
	}
	recs := readJSONLines(t, r.stdout)
	var sawApproval bool
	for _, rec := range recs {
		sawApproval = sawApproval || rec.Type == "approval"
	}
	if !sawApproval {
		t.Fatalf("the run never reached the prompt, so this tested nothing: %+v", recs)
	}
	if end := recs[len(recs)-1]; end.Outcome != outcomeInterrupted {
		t.Errorf("last record %+v, want end/interrupted", end)
	}
}

// Two quick Ctrl+Cs during a running command end the process promptly either
// way: the first cancels the task, and a second, after the handler stands
// down, gets the default behaviour.
func TestDoubleInterruptEndsPromptly(t *testing.T) {
	m := newFakeModel(t, runStep("sleep 30"))
	start := time.Now()
	r := execSynapse(t, synapseRun{
		args: []string{"run", "pause for half a minute"}, env: m.env(),
		started: func(p *os.Process) {
			go func() {
				time.Sleep(1500 * time.Millisecond)
				p.Signal(syscall.SIGINT)
				time.Sleep(50 * time.Millisecond)
				p.Signal(syscall.SIGINT)
			}()
		},
	})
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("took %s after two interrupts", took)
	}
	if r.code != exitInterrupted {
		t.Fatalf("exit %d, want %d; stderr:\n%s", r.code, exitInterrupted, r.stderr)
	}
}

// D49: the analysis may clear a list confirmation only when the list's reason
// is one of its character scans and the analysis proves the command
// recoverable with nothing to capture. Each case runs the real program with
// stdin closed, so any confirmation is declined and shows as exit 3.
func TestGateClearsOnlyTheListsTextScans(t *testing.T) {
	cases := []struct {
		name, cmd string
		asks      bool
		check     func(t *testing.T, dir string)
	}{
		{"quoted > is not a redirect", `printf '%s\n' 'a>b'`, false, nil},
		{"redirect to a new file is recoverable", "echo hi > new.txt", false, func(t *testing.T, dir string) {
			if b, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(b) != "hi\n" {
				t.Errorf("new.txt = %q, want the command to have run", b)
			}
		}},
		{"tee to a new file is recoverable", "echo hi | tee new.txt", false, nil},
		{"redirect over an existing file still asks", "echo hi > existing.txt", true, func(t *testing.T, dir string) {
			if b, _ := os.ReadFile(filepath.Join(dir, "existing.txt")); string(b) != "keep\n" {
				t.Errorf("existing.txt = %q, want it untouched", b)
			}
		}},
		{"a list command rule still asks though the analysis says recoverable", "rm -f doesnotexist.txt", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "existing.txt"), "keep\n")
			m := newFakeModel(t, runStep(c.cmd))
			r := execSynapse(t, synapseRun{args: []string{"run", "do it"}, env: m.env(), dir: dir})
			want := exitOK
			if c.asks {
				want = exitDeclined
			}
			if r.code != want {
				t.Fatalf("%s: exit %d, want %d; stderr:\n%s", c.cmd, r.code, want, r.stderr)
			}
			if c.check != nil {
				c.check(t, dir)
			}
		})
	}
}

// With the analysis off there is nothing to clear the list with: it asks.
func TestGateWithoutAnalysisKeepsTheList(t *testing.T) {
	m := newFakeModel(t, runStep(`printf '%s\n' 'a>b'`))
	r := execSynapse(t, synapseRun{args: []string{"run", "print it"}, env: append(m.env(), "SYNAPSE_ANALYSIS=off")})
	if r.code != exitDeclined {
		t.Fatalf("exit %d, want %d (the list alone asks)", r.code, exitDeclined)
	}
}

// A session log that cannot be written is refused before anything runs
// (finding atk4-01), and a task over the length limit is a usage error.
func TestUnusableSessionLogAndOversizedTaskAreRefused(t *testing.T) {
	base := t.TempDir()
	for _, path := range []string{base, filepath.Join(base, "missing", "log.jsonl")} {
		m := newFakeModel(t)
		r := execSynapse(t, synapseRun{args: []string{"run", "hello"}, env: append(m.env(), "SYNAPSE_SESSION_LOG="+path, "SYNAPSE_PARTICIPANT=P01")})
		if r.code != exitUsage || m.steps.Load() != 0 {
			t.Errorf("SESSION_LOG=%s: exit %d, %d model steps; want %d and none", path, r.code, m.steps.Load(), exitUsage)
		}
	}
	m := newFakeModel(t)
	r := execSynapse(t, synapseRun{args: []string{"run", strings.Repeat("a", maxTaskBytes+1)}, env: m.env()})
	if r.code != exitUsage || m.steps.Load() != 0 {
		t.Errorf("oversized task: exit %d, %d model steps", r.code, m.steps.Load())
	}
}

// undo refuses a broken study-log setting with the usage exit code, like every
// other command (finding r2-atk8-05), and a device is not a log (r2-atk8-06).
func TestUndoAndLogDevicesFollowTheConfigContract(t *testing.T) {
	for _, env := range [][]string{
		{"SYNAPSE_SESSION_LOG=/tmp/x.jsonl"},
		{"SYNAPSE_SESSION_LOG=/dev/full", "SYNAPSE_PARTICIPANT=P01"},
	} {
		r := execSynapse(t, synapseRun{args: []string{"undo"}, env: env})
		if r.code != exitUsage {
			t.Errorf("undo with %v: exit %d, want %d", env, r.code, exitUsage)
		}
	}
	r := execSynapse(t, synapseRun{args: []string{"run", "hello"}, env: []string{"SYNAPSE_SESSION_LOG=/dev/full", "SYNAPSE_PARTICIPANT=P01", "SYNAPSE_OLLAMA=http://127.0.0.1:1"}})
	if r.code != exitUsage {
		t.Errorf("run with the log on /dev/full: exit %d, want %d", r.code, exitUsage)
	}
}

// A password in the endpoint is never printed (finding r2-atk8-03).
func TestEndpointPasswordIsNeverPrinted(t *testing.T) {
	// Both paths that print the endpoint: the banner (a reachable model) and
	// the connection error (an unreachable one).
	m := newFakeModel(t)
	reachable := strings.Replace(m.srv.URL, "http://", "http://user:hunter2@", 1)
	for _, url := range []string{reachable, "http://user:hunter2@127.0.0.1:1/"} {
		r := execSynapse(t, synapseRun{args: []string{"run", "list the files"}, env: []string{"SYNAPSE_OLLAMA=" + url}})
		if strings.Contains(r.stdout+r.stderr, "hunter2") {
			t.Errorf("the password was printed with SYNAPSE_OLLAMA=%s:\n%s", url, r.stderr)
		}
		if url == reachable && !strings.Contains(r.stderr, "endpoint:") {
			t.Errorf("the banner did not print, so its masking was not exercised:\n%s", r.stderr)
		}
	}
}
