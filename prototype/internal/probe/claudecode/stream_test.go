package claudecode

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"synapseos/internal/probe"
)

func base() probe.Record {
	return probe.Record{Run: "r1", Driver: "cli", Profile: "native", Task: "t1", Tier: probe.TierTechnical, Turn: 2}
}

// The real capture from Claude Code 2.1.302 (scrubbed): two Bash calls, one
// exiting 0 and one exiting 2, then a text answer and a result.
func TestParseRealCapture(t *testing.T) {
	f, err := os.Open("testdata/ls_two_commands.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, err := ParseSession(f, base())
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "claude-opus-5-5" || s.Version != "2.1.302" || s.SessionID != "session_id-1" {
		t.Fatalf("init metadata = %q %q %q", s.Model, s.Version, s.SessionID)
	}
	if s.Result == nil || s.Result.Subtype != "success" || s.Result.IsError {
		t.Fatalf("result = %+v", s.Result)
	}
	if len(s.Records) != 2 {
		t.Fatalf("got %d records, want 2: %+v", len(s.Records), s.Records)
	}
	want := []struct {
		cmd  string
		exit int
	}{{"ls -la", 0}, {"ls missing-file", 2}}
	for i, w := range want {
		r := s.Records[i]
		if r.Kind != probe.KindBash || r.Command != w.cmd || r.Step != i || r.Turn != 2 || r.Task != "t1" {
			t.Errorf("record %d = %+v", i, r)
		}
		if r.Exit == nil || *r.Exit != w.exit {
			t.Errorf("record %d exit = %v, want %d", i, r.Exit, w.exit)
		}
		if r.ModelVersion != "claude-opus-5-5" || r.ToolVersion != "2.1.302" || r.Model != "claude-opus-5-5" {
			t.Errorf("record %d versions = %q %q %q", i, r.Model, r.ModelVersion, r.ToolVersion)
		}
	}
}

const initLine = `{"type":"system","subtype":"init","session_id":"s","model":"m-1","claude_code_version":"9.9.9","permissionMode":"bypassPermissions"}`

func parse(t *testing.T, lines ...string) Session {
	t.Helper()
	b := base()
	b.Model = "sonnet" // what the operator asked for is kept
	s, err := ParseSession(strings.NewReader(strings.Join(lines, "\n")), b)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseFileToolsAndOrder(t *testing.T) {
	s := parse(t, initLine,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"a","name":"Write","input":{"file_path":"x.txt", "content":"1"}},{"type":"tool_use","id":"b","name":"Bash","input":{"command":"cat x.txt"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"ok","is_error":false}]},"tool_use_result":{"type":"create"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"c","name":"Edit","input":{"file_path":"x.txt","old_string":"1","new_string":"2"}}]}}`,
	)
	if len(s.Records) != 3 {
		t.Fatalf("records = %+v", s.Records)
	}
	if r := s.Records[0]; r.Kind != "Write" || r.Command != `{"file_path":"x.txt","content":"1"}` || r.Exit != nil || r.Step != 0 {
		t.Errorf("write = %+v", r)
	}
	if r := s.Records[1]; r.Kind != probe.KindBash || r.Command != "cat x.txt" || r.Exit != nil || r.Step != 1 {
		t.Errorf("bash without a result must have nil exit: %+v", r)
	}
	if r := s.Records[2]; r.Kind != "Edit" || r.Step != 2 {
		t.Errorf("edit = %+v", r)
	}
	for _, r := range s.Records {
		if r.Model != "sonnet" || r.ModelVersion != "m-1" || r.ToolVersion != "9.9.9" {
			t.Errorf("versions = %q %q %q", r.Model, r.ModelVersion, r.ToolVersion)
		}
	}
	if s.PermissionMode != "bypassPermissions" || s.Result != nil {
		t.Errorf("mode %q result %+v", s.PermissionMode, s.Result)
	}
}

func TestParseExitInference(t *testing.T) {
	use := func(id string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":"c` + id + `"}}]}}`
	}
	s := parse(t, initLine,
		use("1"), `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"1","content":[{"type":"text","text":"Exit code 127\nnope"}],"is_error":true}]}}`,
		use("2"), `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"2","content":"Permission denied by hook","is_error":true}]}}`,
		use("3"), `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"3","content":"","is_error":false}]},"tool_use_result":{"stdout":"","stderr":"","interrupted":true}}`,
		use("4"), `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"4","content":"started","is_error":false}]},"tool_use_result":{"stdout":"","interrupted":false,"backgroundTaskId":"bg1"}}`,
		use("5"), `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"5","content":"fine","is_error":false}]},"tool_use_result":{"stdout":"fine","stderr":"","interrupted":false}}`,
		`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":5,"errors":["max turns"]}`,
	)
	want := []string{"127", "nil", "nil", "nil", "0"}
	for i, w := range want {
		got := "nil"
		if e := s.Records[i].Exit; e != nil {
			got = strconv.Itoa(*e)
		}
		if got != w {
			t.Errorf("record %d exit = %s, want %s", i, got, w)
		}
	}
	if s.Result == nil || s.Result.Subtype != "error_max_turns" || !s.Result.IsError || s.Result.NumTurns != 5 {
		t.Errorf("result = %+v", s.Result)
	}
}

// Two results in one event: the event's tool_use_result cannot be attributed,
// so a success there is not inferred; an explicit "Exit code" still is.
func TestParseSharedResultEvent(t *testing.T) {
	s := parse(t, initLine,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"true"}},{"type":"tool_use","id":"b","name":"Bash","input":{"command":"false"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"","is_error":false},{"type":"tool_result","tool_use_id":"b","content":"Exit code 1","is_error":true}]},"tool_use_result":{"interrupted":false}}`,
	)
	if s.Records[0].Exit != nil {
		t.Errorf("shared event must not infer exit 0: %v", *s.Records[0].Exit)
	}
	if e := s.Records[1].Exit; e == nil || *e != 1 {
		t.Errorf("explicit exit code lost: %v", e)
	}
}

func TestParseSkipsUnknownAndBlank(t *testing.T) {
	s := parse(t, "", initLine, `{"type":"rate_limit_event","rate_limit_info":{}}`, `{"type":"stream_event","event":{}}`,
		`{"type":"user","message":{"role":"user","content":"a plain prompt"}}`, "   ")
	if len(s.Records) != 0 || s.Model != "m-1" {
		t.Errorf("got %+v", s)
	}
}

func TestParseMalformedIsError(t *testing.T) {
	for _, in := range []string{
		initLine + "\n" + `{"type":"assistant","message":{"content":[`, // truncated mid-line
		"not json",
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":"oops"}]}}`,
	} {
		if _, err := Parse(strings.NewReader(in), base()); err == nil {
			t.Errorf("no error for %q", in)
		}
	}
}
