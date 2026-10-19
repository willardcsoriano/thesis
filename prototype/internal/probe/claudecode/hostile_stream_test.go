package claudecode

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"synapseos/internal/probe"
)

// Hostile streams. A stream comes from a CLI driven by a remote model, so
// anything a broken or adversarial CLI could print is fair input. The parser
// must not panic or hang, must keep every tool call it has already seen,
// and must fail on a line that is not well-formed JSON.

const bashUse = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"rm -r old"}}]}}`

// parseHostile parses in under a deadline, so a hang fails instead of
// stalling the suite, and turns a panic into a failure naming the input.
func parseHostile(t *testing.T, in []byte) (Session, error) {
	t.Helper()
	type out struct {
		s   Session
		err error
		pan any
	}
	done := make(chan out, 1)
	go func() {
		var o out
		defer func() { o.pan = recover(); done <- o }()
		o.s, o.err = ParseSession(bytes.NewReader(in), base())
	}()
	select {
	case o := <-done:
		if o.pan != nil {
			t.Fatalf("parser panicked: %v", o.pan)
		}
		return o.s, o.err
	case <-time.After(20 * time.Second):
		t.Fatal("parser hung")
	}
	return Session{}, nil
}

func lines(ls ...string) []byte { return []byte(strings.Join(ls, "\n") + "\n") }

// A malformed line is an error, and the calls before it survive it: the
// caller keeps them with the error instead of dropping a turn's records.
func TestHostileMalformedKeepsEarlierCalls(t *testing.T) {
	for name, bad := range map[string]string{
		"truncated last line":     `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b","name":"Bash","inp`,
		"raw NUL in a string":     "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"id\":\"b\",\"name\":\"Bash\",\"input\":{\"command\":\"rm \x00x\"}}]}}",
		"bare NUL":                "\x00",
		"invalid UTF-8":           "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"id\":\"b\",\"name\":\"Bash\",\"input\":{\"command\":\"rm \xff\xfe\"}}]}}",
		"deep nesting":            `{"type":"assistant","x":` + strings.Repeat("[", 200000) + strings.Repeat("]", 200000) + `}`,
		"tool_use without a name": `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"b","input":{}}]}}`,
		"second result event":     `{"type":"result","subtype":"success","is_error":false,"num_turns":1}` + "\n" + `{"type":"result","subtype":"error_during_execution","is_error":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			in := lines(initLine, bashUse)
			if name == "second result event" {
				in = append(in, []byte(bad+"\n")...)
			} else {
				in = append(in, []byte(bad)...)
			}
			s, err := parseHostile(t, in)
			if err == nil {
				t.Fatalf("no error; records %+v result %+v", s.Records, s.Result)
			}
			if len(s.Records) != 1 || s.Records[0].Command != "rm -r old" {
				t.Errorf("the call seen before the bad line was dropped: %+v", s.Records)
			}
			if s.Records != nil && s.Records[0].ModelVersion != "m-1" {
				t.Errorf("records returned with an error must still carry the init metadata: %+v", s.Records[0])
			}
		})
	}
}

// Escaped NULs and control characters are valid JSON; they are kept
// byte-exact, not stripped, so the record is the command the agent ran.
func TestHostileEscapesKeptExact(t *testing.T) {
	s, err := parseHostile(t, lines(initLine,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"printf '\u001b]52;c;aGk=\u0007' \u0000; rm x"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 1 || s.Records[0].Command != "printf '\x1b]52;c;aGk=\x07' \x00; rm x" {
		t.Errorf("records = %+v", s.Records)
	}
}

// A 50 MB command on one line is parsed whole, not cut at a buffer size.
func TestHostileHugeLine(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 50 MB line")
	}
	cmd := "echo " + strings.Repeat("A", 50<<20)
	s, err := parseHostile(t, lines(initLine,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"`+cmd+`"}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 1 || s.Records[0].Command != cmd || s.Result == nil {
		t.Errorf("huge line lost: %d records, result %v", len(s.Records), s.Result)
	}
}

// Ids: a result for an id nobody used, a result before its call, an empty
// id, and an id used twice. Each result goes to at most the one call it
// names, the earliest still waiting for one, and never to an unrelated call.
func TestHostileToolIDs(t *testing.T) {
	use := func(id, cmd string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":"` + cmd + `"}}]}}`
	}
	res := func(id, content string, isErr bool) string {
		e := "false"
		if isErr {
			e = "true"
		}
		return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"` + content + `","is_error":` + e + `}]}}`
	}
	s, err := parseHostile(t, lines(initLine,
		res("early", "Exit code 9", true), // before its call: nothing to attach to
		use("early", "first"),
		use("", "no-id"),
		res("", "Exit code 4", true), // an empty id names no call
		use("dup", "dup-1"),
		use("dup", "dup-2"),
		res("dup", "Exit code 1", true),
		res("dup", "Exit code 2", true),
		res("ghost", "Exit code 3", true),
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 4 {
		t.Fatalf("records = %+v", s.Records)
	}
	exit := func(i int) any {
		if s.Records[i].Exit == nil {
			return nil
		}
		return *s.Records[i].Exit
	}
	if exit(0) != nil || exit(1) != nil {
		t.Errorf("a result attached to the wrong call: first=%v no-id=%v", exit(0), exit(1))
	}
	if exit(2) != 1 || exit(3) != 2 {
		t.Errorf("duplicate ids: dup-1=%v dup-2=%v, want 1 and 2", exit(2), exit(3))
	}
}

// A call the model makes through a server-side or MCP tool is still a tool
// call the stream shows, so it is recorded, not skipped as an unknown block.
func TestHostileOtherToolBlocksRecorded(t *testing.T) {
	s, err := parseHostile(t, lines(initLine,
		`{"type":"assistant","message":{"content":[{"type":"server_tool_use","id":"w","name":"web_search","input":{"query":"x"}},{"type":"mcp_tool_use","id":"m","name":"fs_delete","server_name":"fs","input":{"path":"/"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 2 || s.Records[0].Kind != "web_search" || s.Records[1].Kind != "fs_delete" {
		t.Errorf("records = %+v", s.Records)
	}
}

// Shapes a hostile CLI can give the known fields: wrong JSON types where an
// object is expected are errors; null and absent are not.
func TestHostileFieldShapes(t *testing.T) {
	for in, wantErr := range map[string]bool{
		`{"type":"assistant","message":"text"}`:                                                              true,
		`{"type":"assistant","message":{"content":{"a":1}}}`:                                                 false, // not an array: no tool traffic
		`{"type":"assistant","message":null}`:                                                                false,
		`{"type":"assistant","message":{"content":[1,2]}}`:                                                   true,
		`{"type":"assistant","message":{"content":null}}`:                                                    false,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":{"x":1}}]}}`: false,
		`{"type":"result","num_turns":"many"}`:                                                               true,
		`{"type":"system","subtype":"init","session_id":7}`:                                                  true,
		`[]`:         true,
		`"a string"`: true,
		`null`:       true,
	} {
		_, err := parseHostile(t, lines(initLine, bashUse, in))
		if (err != nil) != wantErr {
			t.Errorf("%s: err = %v, want error %v", in, err, wantErr)
		}
	}
}

// Many events and unknown types in volume: linear, no hang, nothing lost.
func TestHostileVolume(t *testing.T) {
	var b strings.Builder
	b.WriteString(initLine + "\n")
	for i := 0; i < 20000; i++ {
		b.WriteString(`{"type":"stream_event","event":{"type":"content_block_delta"}}` + "\n")
		b.WriteString(bashUse + "\n")
	}
	s, err := parseHostile(t, []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 20000 || s.Records[19999].Step != 19999 {
		t.Errorf("got %d records", len(s.Records))
	}
	_ = probe.KindBash
}
