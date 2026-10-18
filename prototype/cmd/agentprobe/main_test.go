package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"synapseos/internal/oracle"
	"synapseos/internal/probe/claudecode"
)

const suite = "../../internal/probe/claudecode/testdata/tasks.jsonl"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestRefusesWithoutToken(t *testing.T) {
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"claude", "-tasks", suite, "-out", t.TempDir()}, env(nil), &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	for _, want := range []string{claudecode.TokenEnv + " is not set", "claude setup-token", "export " + claudecode.TokenEnv + "="} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, errb.String())
		}
	}
}

func TestRejectsUnknownTier(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"claude", "-tasks", suite, "-tiers", "expert"}, env(nil), &out, &errb); code != 2 {
		t.Errorf("exit %d for unknown tier", code)
	}
}

func TestDryRunRedactsAndRunsNothing(t *testing.T) {
	if oracle.Bwrap() == "" {
		t.Skip("bubblewrap unavailable")
	}
	if _, err := claudecode.ResolveInstall(); err != nil {
		t.Skip(err)
	}
	const secret = "sk-ant-oat01-SECRET-TEST"
	var out, errb bytes.Buffer
	dir := t.TempDir()
	code := run(context.Background(), []string{"claude", "-tasks", suite, "-tiers", "technical,novice", "-dry-run", "-out", dir, "-model", "sonnet"},
		env(map[string]string{claudecode.TokenEnv: secret}), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	s := out.String()
	if strings.Contains(s, secret) || strings.Contains(errb.String(), secret) {
		t.Fatal("dry run printed the token")
	}
	for _, want := range []string{claudecode.TokenEnv + "=<redacted>", "bwrap", "--tmpfs /home", "--chdir " + claudecode.SandboxCwd,
		"--dangerously-skip-permissions", "--model sonnet", "--resume '<session-uuid>'", "tiny-1 novice turn 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("dry run lacks %q:\n%s", want, s)
		}
	}
}
