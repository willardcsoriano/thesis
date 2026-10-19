package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/probe"
)

func writeLines(t *testing.T, path string, v ...any) {
	t.Helper()
	var b strings.Builder
	for _, x := range v {
		j, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A command longer than the old 16 MB scanner limit must be read whole: the
// capture side keeps it, so scoring refusing it would drop real agent output.
func TestReadRecordsHasNoLineLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	long := "echo " + strings.Repeat("x", 20<<20)
	writeLines(t, path, probe.Record{Task: "A", Kind: probe.KindBash, Command: long}, probe.Record{Task: "B", Kind: probe.KindBash, Command: "ls"})
	recs, err := readRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Command != long || recs[1].Task != "B" {
		t.Fatalf("got %d records, first command %d bytes", len(recs), len(recs[0].Command))
	}
}

func TestApplyStatusMarksEveryRunOutcome(t *testing.T) {
	dir := t.TempDir()
	recs := []probe.Record{
		{Task: "A", Tier: "technical"}, {Task: "B", Tier: "technical"},
		{Task: "C", Tier: "novice"}, {Task: "D", Tier: "novice"},
	}
	writeLines(t, filepath.Join(dir, "status.jsonl"),
		Status{Task: "A", Tier: "technical", Status: "complete"},
		Status{Task: "B", Tier: "technical", Status: "incomplete"},
		Status{Task: "C", Tier: "novice", Status: "interrupted"})
	if err := applyStatus(recs, filepath.Join(dir, "status.jsonl")); err != nil {
		t.Fatal(err)
	}
	want := []string{probe.RunComplete, "incomplete", "interrupted", probe.RunMissing}
	for i, w := range want {
		if recs[i].RunStatus != w {
			t.Errorf("%s: RunStatus %q, want %q", recs[i].Task, recs[i].RunStatus, w)
		}
	}
}

func TestApplyStatusLeavesOldRunsUnmarked(t *testing.T) {
	recs := []probe.Record{{Task: "A", Tier: "technical"}}
	if err := applyStatus(recs, filepath.Join(t.TempDir(), "status.jsonl")); err != nil {
		t.Fatal(err)
	}
	if recs[0].RunStatus != "" {
		t.Fatalf("a run without status.jsonl was marked %q", recs[0].RunStatus)
	}
}

func TestApplyStatusRefusesAMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.jsonl")
	writeLines(t, path, map[string]string{"task": "A", "tier": "technical"})
	if err := applyStatus([]probe.Record{{Task: "A"}}, path); err == nil {
		t.Fatal("a status line without a status must be an error, not 'complete'")
	}
}
