package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A suite line whose id or prompt could not be used safely is refused at
// load time, before a run starts, not when its task comes up mid-run.
func TestLoadTasksRefusesHostileIDsAndPrompts(t *testing.T) {
	good := func(id, prompt string) string {
		p := `"` + prompt + `"`
		return `{"id":"` + id + `","partition":"E","sources":{"technical":"authored","interface":"authored","plain":"authored","novice":"authored"},"prompts":{"technical":` + p + `,"interface":"x","plain":"x","novice":"x"}}`
	}
	for name, line := range map[string]string{
		"traversal id":   good("../../etc/x", "x"),
		"slash id":       good("a/b", "x"),
		"dot id":         good("..", "x"),
		"dash id":        good("-x", "x"),
		"newline id":     good(`a\nb`, "x"),
		"flag prompt":    good("a", "--help"),
		"NUL prompt":     good("a", `x\u0000y`),
		"huge prompt":    good("a", strings.Repeat("x", MaxArg+1)),
		"flag follow-up": strings.TrimSuffix(good("a", "x"), "}") + `,"follow_ups":["-rf"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "s.jsonl")
			if err := os.WriteFile(p, []byte(line+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadTasks(p); err == nil {
				t.Errorf("loaded %.80s", line)
			}
		})
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(good("E-01_a.b", " -leading space is fine")+"\n"), 0o644)
	if _, err := LoadTasks(p); err != nil {
		t.Errorf("a safe line was refused: %v", err)
	}
}

func TestCheckID(t *testing.T) {
	for _, id := range []string{"20261019T120000Z-claude", "batch1", "a.b_c-d"} {
		if err := CheckID("run", id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	for _, id := range []string{"", ".", "..", "../x", "x/..", "/x", "x/y", "-x", ".x", "x y", "x\x00"} {
		if CheckID("run", id) == nil {
			t.Errorf("%q accepted", id)
		}
	}
}
