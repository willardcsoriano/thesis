package codex

import (
	"slices"
	"testing"
)

// The cases below are Codex's own unit tests from is_dangerous_command.rs at the commit
// named in codex.go, minus the Windows-only test.

func TestRmRfIsDangerous(t *testing.T) {
	if got := Dangerous([]string{"rm", "-rf", "/"}); got != ForcedRm {
		t.Fatalf("got %v", got)
	}
}

func TestRmFIsDangerous(t *testing.T) {
	if got := Dangerous([]string{"rm", "-f", "/"}); got != ForcedRm {
		t.Fatalf("got %v", got)
	}
}

func TestForcedRmVariantsAreDangerous(t *testing.T) {
	for _, c := range [][]string{
		{"/bin/rm", "-fr", "/tmp/example"},
		{"rm", "-r", "-f", "/tmp/example"},
		{"rm", "--force", "/tmp/example"},
		{"rm", "/tmp/example", "-f"},
		{"sudo", "rm", "-rf", "/tmp/example"},
		{"env", "TARGET=/tmp/example", "rm", "-rf", "/tmp/example"},
	} {
		if got := Dangerous(c); got != ForcedRm {
			t.Errorf("%q: got %v", c, got)
		}
	}
}

func TestDeeplyNestedCommandWrappersFailClosed(t *testing.T) {
	for _, tc := range []struct {
		depth int
		want  Match
	}{{maxWrapperDepth, ForcedRm}, {maxWrapperDepth + 1, Other}} {
		c := append(slices.Repeat([]string{"env"}, tc.depth), "rm", "-rf", "/tmp/example")
		if got := Dangerous(c); got != tc.want {
			t.Errorf("depth %d: got %v, want %v", tc.depth, got, tc.want)
		}
	}
}

func TestForcedRmInComplexShellSyntaxIsDangerous(t *testing.T) {
	for _, s := range []string{
		"printf x | rm -rf /tmp/example",
		"if test -d /tmp/example; then rm --force /tmp/example; fi",
		`rm -rf "$TARGET" >/dev/null`,
		`for target in /tmp/a /tmp/b; do rm -r -f "$target"; done`,
		`echo "$(rm -rf /tmp/example)"`,
		"bash -c 'rm -rf /tmp/example'",
		"trap 'rm -rf /tmp/example' EXIT",
		`for a in '-C5a25KeRr' '--' '--json' '--bogus'; do HOME=$(mktemp -d) MDE_URL=http://127.0.0.1:1 MDE_TOKEN=x node cli/mde.cjs ls "$a" >/tmp/mde-review-out 2>/tmp/mde-review-err; code=$?; printf '%s\t%s\t%s\n' "$a" "$code" "$(tr '\n' ' ' </tmp/mde-review-err)"; rm -rf "$HOME"; done`,
	} {
		if got := Dangerous([]string{"bash", "-lc", s}); got != ForcedRm {
			t.Errorf("%s: got %v", s, got)
		}
	}
}

func TestNonForcedOrNonLiteralRmIsNotDangerous(t *testing.T) {
	for _, c := range [][]string{
		{"rm", "-r", "/tmp/example"},
		{"rm", "--", "-f"},
		{"bash", "-lc", "echo 'rm -rf /tmp/example'"},
		{"bash", "-lc", "cmd=rm; $cmd -rf /tmp/example"},
		{"bash", "-lc", "if then rm -rf /tmp/example"},
		{"env", "TARGET=/tmp/example", "rm", "-r", "/tmp/example"},
		{"bash", "-lc", "trap 'echo rm -rf /tmp/example' EXIT"},
	} {
		if got := Dangerous(c); got != None {
			t.Errorf("%q: got %v, want None", c, got)
		}
	}
}
