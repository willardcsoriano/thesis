package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// events renders test2json events, one per line.
func events(t *testing.T, es ...event) string {
	t.Helper()
	var b strings.Builder
	for _, e := range es {
		j, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return b.String()
}

func ran(pkg, test, action, out string) []event {
	return []event{{Action: "run", Package: pkg, Test: test}, {Action: "output", Package: pkg, Test: test, Output: out}, {Action: action, Package: pkg, Test: test}}
}

func gate(t *testing.T, allow, in string) (int, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(p, []byte(allow), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := run([]string{"-allow", p}, strings.NewReader(in), &out, &errb)
	return code, out.String() + errb.String()
}

const allowOne = "# comment\n\nsynapseos/x TestHelper helper process only\n"

func TestGate(t *testing.T) {
	pass := ran("synapseos/x", "TestA", "pass", "ok\n")
	helper := ran("synapseos/x", "TestHelper", "skip", "    x_test.go:9: helper process only\n")
	stray := ran("synapseos/y", "TestB/sub", "skip", "    y_test.go:3: bubblewrap unavailable\n")
	failed := ran("synapseos/y", "TestC", "fail", "    y_test.go:5: boom\n")
	noTests := []event{{Action: "skip", Package: "synapseos/empty"}}
	cat := func(ess ...[]event) []event {
		var out []event
		for _, es := range ess {
			out = append(out, es...)
		}
		return out
	}
	for _, c := range []struct {
		name  string
		allow string
		in    string
		code  int
		want  []string
	}{
		{"all pass, listed skip", allowOne, events(t, cat(pass, helper, noTests)...), 0, []string{"allowed skip: synapseos/x TestHelper (helper process only)", "2 tests ran, 0 failed, 1 skipped (1 allowed)"}},
		{"a skip not listed", allowOne, events(t, cat(pass, helper, stray)...), 1, []string{"--- SKIP not in the allowlist: synapseos/y TestB/sub", "bubblewrap unavailable"}},
		{"a failure", allowOne, events(t, cat(pass, helper, failed)...), 1, []string{"--- FAIL: synapseos/y TestC", "boom"}},
		{"a listed test that never ran is stale", allowOne + "synapseos/x TestGone gone\n", events(t, cat(pass, helper)...), 1, []string{"stale allowlist entry, no such test ran: synapseos/x TestGone"}},
		{"a listed test that ran and passed is fine", allowOne, events(t, cat(pass, ran("synapseos/x", "TestHelper", "pass", ""))...), 0, nil},
		{"a build failure", allowOne, events(t, cat(pass, helper, []event{{Action: "build-output", ImportPath: "synapseos/z [synapseos/z.test]", Output: "z.go:1: syntax error\n"},
			{Action: "build-fail", ImportPath: "synapseos/z [synapseos/z.test]"}, {Action: "fail", Package: "synapseos/z"}})...), 1, []string{"--- BUILD FAILED: synapseos/z", "syntax error", "--- FAIL: package synapseos/z"}},
		{"text that is not an event", allowOne, events(t, cat(pass, helper)...) + "panic: something\n", 1, []string{"not a go test -json event: panic: something"}},
		{"an entry without a reason", "synapseos/x TestHelper\n", events(t, helper...), 2, []string{"with a reason"}},
		{"an entry listed twice", allowOne + allowOne, events(t, helper...), 2, []string{"listed twice"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, out := gate(t, c.allow, c.in)
			if code != c.code {
				t.Errorf("exit %d, want %d:\n%s", code, c.code, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

// The real stream: a package whose test skips is caught by the gate run on
// go test -json's own output, not only on hand-written events.
func TestGateOnRealGoTest(t *testing.T) {
	if os.Getenv("SKIPGATE_CHILD") != "" {
		t.Skip("a stray skip")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		if gobin = filepath.Join(os.Getenv("HOME"), ".local/go/bin/go"); !fileExists(gobin) {
			t.Fatal("go not found on PATH or at ~/.local/go/bin/go; the gate needs the toolchain that runs it")
		}
	}
	cmd := exec.Command(gobin, "test", "-count=1", "-json", "-run", "^TestGateOnRealGoTest$", ".")
	cmd.Env = append(os.Environ(), "SKIPGATE_CHILD=1")
	stream, err := cmd.Output()
	if err != nil {
		t.Fatalf("go test: %v: %s", err, stream)
	}
	code, out := gate(t, "", string(stream))
	if code != 1 || !strings.Contains(out, "--- SKIP not in the allowlist: synapseos/cmd/skipgate TestGateOnRealGoTest") || !strings.Contains(out, "a stray skip") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	code, out = gate(t, "synapseos/cmd/skipgate TestGateOnRealGoTest the child's own skip\n", string(stream))
	if code != 0 {
		t.Errorf("listed: exit %d:\n%s", code, out)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
