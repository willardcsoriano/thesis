package toolcall

import (
	"errors"
	"testing"
)

// offered mirrors what the CLI actually declares.
func offered(name string) bool {
	switch name {
	case "run_bash", "find_files", "move_files", "delete_files", "rename_files", "copy_file":
		return true
	}
	return false
}

// Every case in this test was captured from a live qwen2.5-coder reply on
// 2026-09-15, not invented. That matters: a parser tested against the shapes
// its author imagined is a parser tested against nothing.
func TestParseRealRepliesFromTheModel(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantCmd string
	}{
		{
			name:    "bare call, clean arguments",
			content: `{"name": "run_bash", "arguments": {"command": "du -sh ."}}`,
			wantCmd: "du -sh .",
		},
		{
			name:    "schema echoed around the value",
			content: `{"name": "run_bash", "arguments": {"command": {"type": "string", "value": "wc -l log.txt"}}}`,
			wantCmd: "wc -l log.txt",
		},
		{
			name: "duplicate key: schema first, then the real value",
			// Go's decoder keeps the last occurrence, which here is the value.
			content: `{"name": "run_bash", "arguments": {"command": {"type": "string", "description": "The bash command to run."}, "command": "du -sh ."}}`,
			wantCmd: "du -sh .",
		},
		{
			name:    "wrapped in a markdown fence",
			content: "here you go:\n```json\n{\"name\": \"run_bash\", \"arguments\": {\"command\": \"ls -la\"}}\n```",
			wantCmd: "ls -la",
		},
		{
			name:    "nested under function, mirroring the native wire shape",
			content: `{"function": {"name": "run_bash", "arguments": {"command": "df -h"}}}`,
			wantCmd: "df -h",
		},
		{
			name:    "braces inside the command must not end the scan",
			content: `{"name": "run_bash", "arguments": {"command": "awk '{print $2}' data.csv"}}`,
			wantCmd: "awk '{print $2}' data.csv",
		},
		{
			name:    "prose before the call",
			content: `I'll check that for you. {"name": "run_bash", "arguments": {"command": "uptime"}}`,
			wantCmd: "uptime",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call, err := Parse(tc.content, offered)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if call.Name != "run_bash" {
				t.Errorf("tool = %q, want run_bash", call.Name)
			}
			got, _ := call.Args["command"].(string)
			if got != tc.wantCmd {
				t.Errorf("command = %q, want %q", got, tc.wantCmd)
			}
		})
	}
}

func TestParseProseIsNotAnError(t *testing.T) {
	// The conversational half of the fork. Captured live: "thanks, that
	// worked" produced exactly this, with no JSON at all.
	for _, s := range []string{
		"You're welcome! If you need anything else, feel free to ask.",
		"Hello! How can I help you with this machine?",
		"",
		"I can reach files, processes, and packages — but not graphical apps.",
	} {
		if _, err := Parse(s, offered); !errors.Is(err, ErrNoCall) {
			t.Errorf("Parse(%q) error = %v, want ErrNoCall", s, err)
		}
	}
}

func TestParseRejectsInventedTools(t *testing.T) {
	// Captured live: "hello" produced a call to print_message, a function
	// that was never offered. Dispatching on name alone would turn a
	// hallucinated name into an action.
	content := `{"name": "print_message", "arguments": {"message": "Hello!"}}`
	call, err := Parse(content, offered)
	if call != nil {
		t.Fatalf("invented tool was accepted: %+v", call)
	}
	var unknown *UnknownToolError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want *UnknownToolError", err)
	}
	if unknown.Name != "print_message" {
		t.Errorf("reported name = %q, want print_message", unknown.Name)
	}
	// It must not be mistaken for prose: the model meant to act, and showing
	// a user this raw JSON would be worse than either branch.
	if errors.Is(err, ErrNoCall) {
		t.Error("an invented tool call was reported as prose")
	}
}

func TestParseRejectsSchemaWithNoValue(t *testing.T) {
	// The unrecoverable shape: the schema arrived where the value belonged,
	// and no value came with it. Guessing here would fabricate a command.
	content := `{"name": "run_bash", "arguments": {"command": {"type": "string", "description": "The bash command to run."}}}`
	call, err := Parse(content, offered)
	if call != nil {
		t.Fatalf("unusable arguments were accepted: %+v", call)
	}
	if err == nil || errors.Is(err, ErrNoCall) {
		t.Fatalf("error = %v, want a usable-arguments failure", err)
	}
}

func TestExtractJSONBalancesBraces(t *testing.T) {
	// The scan must survive shell syntax, which is full of braces, and must
	// not be fooled by a brace inside a quoted string.
	got := extractJSON(`prefix {"a": "x{y}z", "b": {"c": 1}} suffix`)
	want := `{"a": "x{y}z", "b": {"c": 1}}`
	if got != want {
		t.Errorf("extractJSON() = %q, want %q", got, want)
	}
	if extractJSON("no json here at all") != "" {
		t.Error("extractJSON found an object in prose")
	}
	if extractJSON(`{"unterminated": `) != "" {
		t.Error("extractJSON returned an unbalanced object")
	}
}
