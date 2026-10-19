//go:build live

package geminicli

import (
	"context"
	"os"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
)

// TestLiveTinyTask runs the real CLI, sandboxed, on the tiny test task. It
// spends quota, so it needs -tags live and a credential: either the path of
// an oauth_creds.json (the personal subscription) or an AI Studio key.
//
//	AGENTPROBE_GEMINI_OAUTH_CREDS=/path/to/oauth_creds.json go test -tags live -run Live ./internal/probe/geminicli
//	GEMINI_API_KEY=... go test -tags live -run Live ./internal/probe/geminicli
func TestLiveTinyTask(t *testing.T) {
	a := Auth{APIKey: os.Getenv(APIKeyEnv), Project: os.Getenv(ProjectEnv)}
	if p := os.Getenv("AGENTPROBE_GEMINI_OAUTH_CREDS"); p != "" {
		creds, err := LoadOAuthCreds(p)
		if err != nil {
			t.Fatal(err)
		}
		a.OAuthCreds, a.APIKey = creds, ""
	}
	if a.Type() == "" {
		t.Skip("no credential: set AGENTPROBE_GEMINI_OAUTH_CREDS or " + APIKeyEnv)
	}
	bw := oracle.Bwrap()
	if bw == "" {
		t.Skip("bubblewrap unavailable")
	}
	in, err := ResolveInstall()
	if err != nil {
		t.Skip(err)
	}
	tasks, err := probe.LoadTasks("testdata/tasks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	n, err := claudecode.ResolveNetwork(claudecode.NetIsolated)
	if err == nil {
		err = n.Probe()
	}
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Sandbox: Sandbox{Bwrap: bw, Prlimit: claudecode.Prlimit(), Net: n, Install: in}, Auth: a, Model: os.Getenv("AGENTPROBE_MODEL"),
		MaxTurns: 8, Debug: os.Getenv("AGENTPROBE_GEMINI_DEBUG") != "", TurnTimeout: 5 * time.Minute,
		RunID: "live-test", Out: t.TempDir(), Log: testLog{t}}
	recs, err := r.RunTask(context.Background(), tasks[0], probe.TierTechnical)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("no tool call recorded")
	}
	for _, rec := range recs {
		t.Logf("turn %d step %d %s: %s exit=%v model=%s", rec.Turn, rec.Step, rec.Kind, rec.Command, rec.Exit, rec.ModelVersion)
		if rec.ToolVersion == "" || rec.ModelVersion == "" {
			t.Errorf("versions missing: %+v", rec)
		}
	}
	if recs[len(recs)-1].Lost == nil {
		t.Error("no task-level verdict on the last record")
	}
}

type testLog struct{ t *testing.T }

func (l testLog) Write(p []byte) (int, error) { l.t.Log(string(p)); return len(p), nil }
