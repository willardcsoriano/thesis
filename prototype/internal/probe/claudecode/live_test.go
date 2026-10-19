//go:build live

package claudecode

import (
	"context"
	"os"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/testenv"
)

// TestLiveTinyTask runs the real CLI, sandboxed, on the tiny test task. It
// spends subscription quota, so it needs -tags live and the token:
//
//	CLAUDE_CODE_OAUTH_TOKEN=... go test -tags live -run Live ./internal/probe/claudecode
func TestLiveTinyTask(t *testing.T) {
	token := os.Getenv(TokenEnv)
	if token == "" {
		t.Skipf("%s not set", TokenEnv)
	}
	bw := oracle.Bwrap()
	if bw == "" {
		testenv.Unavailable(t, "bubblewrap unavailable")
	}
	in, err := ResolveInstall()
	if err != nil {
		t.Skip(err)
	}
	tasks, err := probe.LoadTasks("testdata/tasks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	n, err := ResolveNetwork(NetIsolated)
	if err == nil {
		err = n.Probe()
	}
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Sandbox: Sandbox{Bwrap: bw, Prlimit: Prlimit(), Net: n, Install: in}, Token: token, Model: os.Getenv("AGENTPROBE_MODEL"),
		MaxTurns: 8, TurnTimeout: 5 * time.Minute, RunID: "live-test", Out: t.TempDir(), Log: testLog{t}}
	recs, err := r.RunTask(context.Background(), tasks[0], probe.TierTechnical)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("no tool call recorded")
	}
	for _, rec := range recs {
		t.Logf("turn %d step %d %s: %s exit=%v", rec.Turn, rec.Step, rec.Kind, rec.Command, rec.Exit)
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
