package geminicli

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"synapseos/internal/probe"
)

func base() probe.Record {
	return probe.Record{Run: "r1", Driver: "cli", Profile: "native", Task: "t1", Tier: probe.TierTechnical, Turn: 2, ToolVersion: "0.49.0"}
}

func exitString(e *int) string {
	if e == nil {
		return "nil"
	}
	return strconv.Itoa(*e)
}

// testdata/ls_two_commands.jsonl is hand-made: no authenticated CLI was
// available when the driver was built (NOTES.md, "Provenance of the test
// stream"). Each line follows the emitter in the installed 0.49.0 bundle.
func TestParseFixtureStream(t *testing.T) {
	f, err := os.Open("testdata/ls_two_commands.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := ParseSession(f, base())
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionID != "11111111-2222-4333-8444-555555555555" || s.Model != "auto-gemini-3" {
		t.Fatalf("init = %q %q", s.SessionID, s.Model)
	}
	if s.Result == nil || s.Result.IsError() || s.Result.ToolCalls != 3 {
		t.Fatalf("result = %+v", s.Result)
	}
	want := []struct{ kind, cmd, exit string }{
		{probe.KindBash, "ls -la", "nil"}, // non-empty output says nothing about the status
		{probe.KindBash, "ls missing-file 2>/dev/null", "2"},
		{"read_file", `{"file_path":"a.txt"}`, "nil"},
	}
	if len(s.Records) != len(want) {
		t.Fatalf("records = %+v", s.Records)
	}
	for i, w := range want {
		r := s.Records[i]
		if r.Kind != w.kind || r.Command != w.cmd || r.Step != i || r.Turn != 2 || exitString(r.Exit) != w.exit {
			t.Errorf("record %d = %+v exit %s, want %+v", i, r, exitString(r.Exit), w)
		}
		// The init model is a routing alias; the single model in stats is the exact id.
		if r.ModelVersion != "gemini-3-flash-preview" || r.Model != "gemini-3-flash-preview" || r.ToolVersion != "0.49.0" {
			t.Errorf("record %d versions = %q %q %q", i, r.Model, r.ModelVersion, r.ToolVersion)
		}
	}
}

const initLine = `{"type":"init","session_id":"s","model":"gemini-2.5-pro"}`

func parse(t *testing.T, lines ...string) Session {
	t.Helper()
	b := base()
	b.Model = "pro" // what the operator asked for is kept
	s, err := ParseSession(strings.NewReader(strings.Join(lines, "\n")), b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func shell(id, cmd string) string {
	return `{"type":"tool_use","tool_name":"run_shell_command","tool_id":"` + id + `","parameters":{"command":` + strconv.Quote(cmd) + `}}`
}

func result(id, status, output string) string {
	return `{"type":"tool_result","tool_id":"` + id + `","status":"` + status + `","output":` + strconv.Quote(output) + `}`
}

func TestParseExitInference(t *testing.T) {
	s := parse(t, initLine,
		// normal mode
		shell("1", "false"), result("1", "success", "Command exited with code: 1"),
		shell("2", "true"), result("2", "success", ""),
		shell("3", "echo Command exited with code: 9; exit 0"), result("3", "success", "x\nCommand exited with code: 9"),
		shell("4", "sleep 9"), result("4", "success", "Command terminated by signal: SIGTERM"),
		// debug mode: "Output: ..." then trailer lines
		shell("5", "ls"), result("5", "success", "Output: a.txt\nProcess Group PGID: 4242"),
		shell("6", "ls nope"), result("6", "success", "Output: ls: cannot access 'nope'\nExit Code: 2\nProcess Group PGID: 4243"),
		shell("7", "sleep 1 & ls"), result("7", "success", "Output: (empty)\nExit Code: 3\nBackground PIDs: 77\nProcess Group PGID: 4244"),
		shell("8", "kill -9 $$"), result("8", "success", "Output: (empty)\nSignal: 9\nProcess Group PGID: 4245"),
		shell("9", "nosuch"), `{"type":"tool_result","tool_id":"9","status":"error","output":"Output: (empty)\nError: spawn failed\nProcess Group PGID: 1","error":{"type":"shell_execute_error","message":"spawn failed"}}`,
		shell("10", "cat big"), result("10", "success", "Output: Exit Code: 5 is just text"),
		shell("11", "never answered"),
	)
	want := []string{"1", "nil", "nil", "nil", "0", "2", "3", "nil", "nil", "nil", "nil"}
	if len(s.Records) != len(want) {
		t.Fatalf("records = %+v", s.Records)
	}
	for i, w := range want {
		if got := exitString(s.Records[i].Exit); got != w {
			t.Errorf("record %d (%s) exit = %s, want %s", i, s.Records[i].Command, got, w)
		}
	}
	for _, r := range s.Records {
		if r.Model != "pro" || r.ModelVersion != "gemini-2.5-pro" {
			t.Errorf("versions = %q %q", r.Model, r.ModelVersion)
		}
	}
}

func TestParseFileToolsDenialsAndWarnings(t *testing.T) {
	s := parse(t, initLine,
		`{"type":"message","role":"assistant","content":"ok","delta":true}`,
		`{"type":"tool_use","tool_name":"write_file","tool_id":"w","parameters":{"file_path":"x.txt", "content":"1"}}`,
		`{"type":"tool_result","tool_id":"w","status":"error","output":"denied","error":{"type":"policy_violation","message":"denied by policy"}}`,
		`{"type":"tool_use","tool_name":"replace","tool_id":"e","parameters":{"file_path":"x.txt","old_string":"1","new_string":"2"}}`,
		`{"type":"tool_use","tool_name":"list_directory","tool_id":"l"}`,
		`{"type":"error","severity":"warning","message":"Loop detected, stopping execution"}`,
		`{"type":"result","status":"error","error":{"type":"FatalTurnLimitedError","message":"Reached max session turns"},"stats":{"tool_calls":2,"models":{"b":{},"a":{}}}}`,
	)
	if len(s.Records) != 3 {
		t.Fatalf("records = %+v", s.Records)
	}
	if r := s.Records[0]; r.Kind != "write_file" || r.Command != `{"file_path":"x.txt","content":"1"}` || r.Exit != nil {
		t.Errorf("write = %+v", r)
	}
	if r := s.Records[1]; r.Kind != "replace" || r.Step != 1 {
		t.Errorf("replace = %+v", r)
	}
	if r := s.Records[2]; r.Kind != "list_directory" || r.Command != "{}" {
		t.Errorf("list_directory without parameters = %+v", r)
	}
	if s.Denials != 1 || len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "Loop detected") {
		t.Errorf("denials %d warnings %q", s.Denials, s.Warnings)
	}
	if !s.Result.IsError() || s.Result.ErrorType != "FatalTurnLimitedError" || strings.Join(s.Result.Models, ",") != "a,b" {
		t.Errorf("result = %+v", s.Result)
	}
	// Two models served the turn, so the init model stays.
	if s.Records[0].ModelVersion != "gemini-2.5-pro" {
		t.Errorf("model version = %q", s.Records[0].ModelVersion)
	}
}

func TestParseSkipsUnknownAndBlank(t *testing.T) {
	s := parse(t, "", initLine, `{"type":"message","role":"user","content":"hi"}`, `{"type":"future_event","x":1}`, "   ")
	if len(s.Records) != 0 || s.Model != "gemini-2.5-pro" || s.Result != nil {
		t.Errorf("got %+v", s)
	}
}

func TestParseMalformedIsError(t *testing.T) {
	for _, in := range []string{
		initLine + "\n" + `{"type":"tool_use","tool_name":"run_shell_command","param`, // truncated mid-line
		"Opening authentication page in your browser. Do you want to continue? [Y/n]: ",
		`{"type":"tool_use","tool_name":"run_shell_command","tool_id":"a","parameters":"oops"}`,
	} {
		if _, err := Parse(strings.NewReader(in), base()); err == nil {
			t.Errorf("no error for %q", in)
		}
	}
}

// Each Gemini shell call runs at its dir_path, so the record must keep it;
// without it replay cannot place the command.
func TestShellCallKeepsDirPath(t *testing.T) {
	stream := `{"type":"init","session_id":"s","model":"gemini-x"}
{"type":"tool_use","tool_name":"run_shell_command","tool_id":"1","parameters":{"command":"ls","dir_path":"logs"}}
{"type":"tool_use","tool_name":"run_shell_command","tool_id":"2","parameters":{"command":"pwd"}}
`
	s, err := ParseSession(strings.NewReader(stream), probe.Record{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 2 || s.Records[0].Dir == nil || *s.Records[0].Dir != "logs" || s.Records[1].Dir != nil {
		t.Fatalf("dir_path not kept: %+v", s.Records)
	}
}
