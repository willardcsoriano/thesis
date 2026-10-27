package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"synapseos/internal/gate"
	"synapseos/internal/loopevent"
)

// Fuzz targets for every parser of untrusted input (docs/cli-breaking.md,
// Part 1). Each checks an invariant that must hold for any input, not an
// expected output. `go test` runs the seeds; the box runs them with -fuzz.

func FuzzParseArgs(f *testing.F) {
	for _, s := range []string{"", "run\x00list files", "--\x00--help", "undo\x00--dry-run", "tui\x00scratch", "run\x00--json\x00--\x00-x", "help", "\x00", "run\x00 \x00\t"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, joined string) {
		var args []string
		if joined != "" {
			args = strings.Split(joined, "\x00")
		}
		inv, err := parseArgs(args)
		if err != nil {
			if !isUsageError(err) {
				t.Fatalf("parseArgs(%q): non-usage error %v", args, err)
			}
			return
		}
		if inv.mode == modeRun {
			if args[0] != "run" && args[0] != "--" {
				t.Fatalf("parseArgs(%q) made a task without run or --", args)
			}
			if inv.task == "" || inv.task != strings.TrimSpace(inv.task) {
				t.Fatalf("parseArgs(%q): task %q is empty or padded", args, inv.task)
			}
		} else if inv.task != "" || inv.json {
			t.Fatalf("parseArgs(%q): %+v carries run fields outside run", args, inv)
		}
	})
}

func FuzzCheckEnv(f *testing.F) {
	for i, e := range envVars {
		f.Add(i, e.def)
		f.Add(i, "")
		f.Add(i, " "+e.def)
	}
	f.Fuzz(func(t *testing.T, idx int, value string) {
		if idx < 0 {
			idx = -idx
		}
		e := envVars[idx%len(envVars)]
		env := map[string]string{e.name: value}
		if e.name == "SYNAPSE_SESSION_LOG" {
			env["SYNAPSE_PARTICIPANT"] = "P01"
		}
		err := checkEnv(func(k string) string { return env[k] })
		if err != nil || value == "" {
			return
		}
		// Accepted: the value must be one its reader takes as meant.
		switch e.name {
		case "SYNAPSE_ANALYSIS", "SYNAPSE_RESOLVE":
			choices := analysisChoices
			if e.name == "SYNAPSE_RESOLVE" {
				choices = resolveChoices
			}
			found := false
			for _, c := range choices {
				found = found || strings.EqualFold(c, value)
			}
			if !found {
				t.Fatalf("%s=%q accepted but is not a listed choice", e.name, value)
			}
		case "SYNAPSE_NUM_CTX":
			if n, err := strconv.Atoi(value); err != nil || n <= 0 {
				t.Fatalf("%s=%q accepted but is not a positive integer", e.name, value)
			}
		case "SYNAPSE_OLLAMA":
			u, err := url.Parse(value)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				t.Fatalf("%s=%q accepted but is not an http(s) URL with a host", e.name, value)
			}
		}
	})
}

func FuzzConfirmLine(f *testing.F) {
	for _, s := range []string{"y\n", "yes\n", "Y", " yes \nn\n", "", "\n", "no\n", "y es\n", "\x00y\n", "ｙ\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		first, _, _ := strings.Cut(input, "\n")
		answer := strings.ToLower(strings.TrimSpace(first))
		want := input != "" && (answer == "y" || answer == "yes")
		got := confirmLine(context.Background(), newLineSource(strings.NewReader(input)), &bytes.Buffer{}, "ok?")
		if got != want {
			t.Fatalf("confirmLine(%q) = %v, want %v", input, got, want)
		}
	})
}

func FuzzJSONEmitter(f *testing.F) {
	f.Add("hello", "out\n", 0)
	f.Add("a\x00b\nc\"d", "\xff\xfe", 141)
	f.Add("\x1b[2J", "", -1)
	f.Fuzz(func(t *testing.T, text, stdout string, code int) {
		var buf bytes.Buffer
		em := newJSONEmitter(&buf)
		em.Emit(loopevent.Event{Kind: loopevent.Result, Command: text, Stdout: stdout, ExitCode: code})
		em.Emit(loopevent.Event{Kind: loopevent.Answer, Text: text})
		em.End(outcomeComplete, exitOK)
		sc := bufio.NewScanner(&buf)
		sc.Buffer(nil, 1<<24)
		var recs []jsonRecord
		for sc.Scan() {
			var r jsonRecord
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				t.Fatalf("invalid JSON line %q: %v", sc.Text(), err)
			}
			recs = append(recs, r)
		}
		if len(recs) != 3 {
			t.Fatalf("%d lines for 3 events", len(recs))
		}
		if recs[0].ExitCode == nil || *recs[0].ExitCode != code {
			t.Fatalf("exit code %v, want %d", recs[0].ExitCode, code)
		}
		if utf8.ValidString(text) && recs[1].Text != text {
			t.Fatalf("answer text %q did not round-trip as %q", text, recs[1].Text)
		}
	})
}

// The model's reply is untrusted input. Decoding it must never panic, and the
// command taken from free text must be part of that text, never something new.
func FuzzModelReply(f *testing.F) {
	for _, s := range []string{
		`{"action":"run","command":"ls"}`, `{"action":"done","command":""}`, "```bash\nls -la\n```",
		"`ls`", "$ df -h", `{"action":"run"}`, "{", "", "DONE", `{"action":"run","command":"a\u0000b"}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		decodeStepDecision(raw)
		if c := cleanCommand(raw); !strings.Contains(raw, c) {
			t.Fatalf("cleanCommand(%q) = %q, which is not part of the reply", raw, c)
		}
	})
}

// The analysis runs on every command the model proposes. decideFailClosed
// turns a panic there into a confirmation, so this calls the analysis itself,
// to find the panics that safety net would otherwise hide. Resolution is off
// so the fuzzer never runs anything.
func FuzzAnalysisDecision(f *testing.F) {
	for _, s := range []string{"ls", "rm -rf /", "find . -exec rm {} +", "echo $((1/0))", "a=(", "<<EOF", "${x:?}", "cat <(ls) | xargs -0 rm", "\x00", "for i in *; do mv \"$i\" x; done"} {
		f.Add(s)
	}
	dir := f.TempDir()
	f.Fuzz(func(t *testing.T, cmd string) {
		t.Setenv("SYNAPSE_RESOLVE", "never")
		decide(context.Background(), newAnalyzer(dir), cmd, gate.Strict)
	})
}
