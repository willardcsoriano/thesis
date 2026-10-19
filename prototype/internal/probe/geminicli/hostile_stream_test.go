package geminicli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Hostile streams, as for claudecode (hostile_stream_test.go there): no
// panic, no hang, every call already seen is kept, and a line that is not a
// well-formed JSON object is an error.

const shellUse = `{"type":"tool_use","tool_name":"run_shell_command","tool_id":"a","parameters":{"command":"rm -r old"}}`

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

func TestHostileMalformedKeepsEarlierCalls(t *testing.T) {
	for name, bad := range map[string]string{
		"truncated last line":      `{"type":"tool_use","tool_name":"run_shell_command","tool_id":"b","param`,
		"raw NUL in a string":      "{\"type\":\"tool_use\",\"tool_name\":\"run_shell_command\",\"tool_id\":\"b\",\"parameters\":{\"command\":\"rm \x00x\"}}",
		"bare NUL":                 "\x00",
		"invalid UTF-8":            "{\"type\":\"tool_use\",\"tool_name\":\"run_shell_command\",\"tool_id\":\"b\",\"parameters\":{\"command\":\"rm \xff\"}}",
		"deep nesting":             `{"type":"tool_use","x":` + strings.Repeat("[", 200000) + strings.Repeat("]", 200000) + `}`,
		"tool_use without a name":  `{"type":"tool_use","tool_id":"b","parameters":{}}`,
		"second result event":      `{"type":"result","status":"success"}` + "\n" + `{"type":"result","status":"error","error":{"type":"FatalAuthenticationError","message":"x"}}`,
		"null line":                `null`,
		"error that is not a type": `{"type":"tool_result","tool_id":"a","status":"error","error":"boom"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, err := parseHostile(t, append(lines(initLine, shellUse), []byte(bad+"\n")...))
			if err == nil {
				t.Fatalf("no error; records %+v result %+v", s.Records, s.Result)
			}
			if len(s.Records) != 1 || s.Records[0].Command != "rm -r old" {
				t.Errorf("the call seen before the bad line was dropped: %+v", s.Records)
			}
			if len(s.Records) == 1 && s.Records[0].ModelVersion != "gemini-2.5-pro" {
				t.Errorf("records returned with an error must still carry the model: %+v", s.Records[0])
			}
		})
	}
}

func TestHostileEscapesKeptExact(t *testing.T) {
	s, err := parseHostile(t, lines(initLine,
		`{"type":"tool_use","tool_name":"run_shell_command","tool_id":"a","parameters":{"command":"printf '\u001b]52;c;aGk=\u0007' \u0000; rm x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 1 || s.Records[0].Command != "printf '\x1b]52;c;aGk=\x07' \x00; rm x" {
		t.Errorf("records = %+v", s.Records)
	}
}

func TestHostileHugeLine(t *testing.T) {
	cmd := "echo " + strings.Repeat("A", 50<<20)
	s, err := parseHostile(t, lines(initLine,
		`{"type":"tool_use","tool_name":"run_shell_command","tool_id":"a","parameters":{"command":"`+cmd+`"}}`,
		`{"type":"result","status":"success"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 1 || s.Records[0].Command != cmd || s.Result == nil {
		t.Errorf("huge line lost: %d records, result %v", len(s.Records), s.Result)
	}
}

func TestHostileToolIDs(t *testing.T) {
	use := func(id, cmd string) string {
		return `{"type":"tool_use","tool_name":"run_shell_command","tool_id":"` + id + `","parameters":{"command":"` + cmd + `"}}`
	}
	res := func(id string, code int) string {
		return `{"type":"tool_result","tool_id":"` + id + `","status":"error","output":"Command exited with code: ` + string(rune('0'+code)) + `"}`
	}
	s, err := parseHostile(t, lines(initLine,
		res("early", 9),
		use("early", "first"),
		use("", "no-id"),
		res("", 4),
		use("dup", "dup-1"),
		use("dup", "dup-2"),
		res("dup", 1),
		res("dup", 2),
		res("ghost", 3),
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

func TestHostileVolume(t *testing.T) {
	var b strings.Builder
	b.WriteString(initLine + "\n")
	for i := 0; i < 20000; i++ {
		b.WriteString(`{"type":"message","role":"assistant","content":"x","delta":true}` + "\n")
		b.WriteString(shellUse + "\n")
	}
	s, err := parseHostile(t, []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records) != 20000 || s.Records[19999].Step != 19999 {
		t.Errorf("got %d records", len(s.Records))
	}
}
