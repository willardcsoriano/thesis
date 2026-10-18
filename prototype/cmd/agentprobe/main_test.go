package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
	"synapseos/internal/probe/geminicli"
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
	for _, driver := range []string{"claude", "gemini"} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), []string{driver, "-tasks", suite, "-tiers", "expert"}, env(nil), &out, &errb); code != 2 {
			t.Errorf("%s: exit %d for unknown tier", driver, code)
		}
	}
}

func TestRejectsUnknownDriver(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"codex", "-tasks", suite}, env(nil), &out, &errb); code != 2 || !strings.Contains(errb.String(), "gemini") {
		t.Errorf("exit %d: %s", code, errb.String())
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

func TestGeminiRefusesWithoutCredential(t *testing.T) {
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"gemini", "-tasks", suite, "-out", t.TempDir()}, env(nil), &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	for _, want := range []string{"no credential configured", "GEMINI_CLI_HOME=$HOME/.agentprobe-gemini gemini", "-oauth-creds", "export " + geminicli.APIKeyEnv + "="} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, errb.String())
		}
	}
}

func TestGeminiRefusesTwoCredentialsAndBadFile(t *testing.T) {
	dir := t.TempDir()
	creds := filepath.Join(dir, "oauth_creds.json")
	if err := os.WriteFile(creds, []byte(`{"refresh_token":"1//TEST"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`not json SECRET`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"gemini", "-tasks", suite, "-oauth-creds", creds, "-dry-run"},
		env(map[string]string{geminicli.APIKeyEnv: "k"}), &out, &errb); code != 2 || !strings.Contains(errb.String(), "pass only one") {
		t.Errorf("two credentials: exit %d: %s", code, errb.String())
	}
	errb.Reset()
	if code := run(context.Background(), []string{"gemini", "-tasks", suite, "-oauth-creds", bad}, env(nil), &out, &errb); code != 2 || strings.Contains(errb.String(), "SECRET") {
		t.Errorf("bad file: exit %d: %s", code, errb.String())
	}
}

func TestGeminiDryRunRedactsAndRunsNothing(t *testing.T) {
	if oracle.Bwrap() == "" {
		t.Skip("bubblewrap unavailable")
	}
	if _, err := geminicli.ResolveInstall(); err != nil {
		t.Skip(err)
	}
	const secret = "AIzaSECRET-TEST"
	dir := t.TempDir()
	creds := filepath.Join(dir, "oauth_creds.json")
	if err := os.WriteFile(creds, []byte(`{"refresh_token":"1//REFRESH-SECRET"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		env  map[string]string
		want []string
	}{
		{"apikey", nil, map[string]string{geminicli.APIKeyEnv: secret}, []string{geminicli.APIKeyEnv + "=<redacted>", `"selectedType": "gemini-api-key"`}},
		{"oauth", []string{"-oauth-creds", creds}, nil, []string{"oauth_creds.json: a copy of " + creds, `"selectedType": "oauth-personal"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			args := append([]string{"gemini", "-tasks", suite, "-tiers", "technical,novice", "-dry-run", "-out", dir, "-model", "gemini-2.5-pro"}, tc.args...)
			if code := run(context.Background(), args, env(tc.env), &out, &errb); code != 0 {
				t.Fatalf("exit %d: %s", code, errb.String())
			}
			s := out.String()
			for _, bad := range []string{secret, "REFRESH-SECRET"} {
				if strings.Contains(s, bad) || strings.Contains(errb.String(), bad) {
					t.Fatal("dry run printed a credential")
				}
			}
			for _, want := range append([]string{"bwrap", "--tmpfs /home", "--chdir " + geminicli.SandboxCwd, "NO_BROWSER=true",
				"--approval-mode yolo", "--skip-trust", "--model gemini-2.5-pro", "--resume '<session-uuid>'", "tiny-1 novice turn 1", `"maxSessionTurns": 25`}, tc.want...) {
				if !strings.Contains(s, want) {
					t.Errorf("dry run lacks %q:\n%s", want, s)
				}
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("dry run wrote into the output folder: %v", entries)
			}
		})
	}
}

func TestStopEndsTheRun(t *testing.T) {
	tasks, err := probe.LoadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	fatal := func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
		calls++
		return nil, fmt.Errorf("x: %w", geminicli.ErrFatal)
	}
	var errb bytes.Buffer
	stop := func(err error) bool { return errors.Is(err, geminicli.ErrFatal) }
	if code := runTasks(context.Background(), fatal, t.TempDir(), tasks, []string{"technical", "novice"}, stop, &errb); code != 1 || calls != 1 {
		t.Errorf("exit %d after %d calls, want 1 after 1: %s", code, calls, errb.String())
	}
	calls = 0
	if code := runTasks(context.Background(), fatal, t.TempDir(), tasks, []string{"technical", "novice"}, nil, &errb); code != 1 || calls != 2 {
		t.Errorf("without stop: exit %d after %d calls, want 1 after 2", code, calls)
	}
}
