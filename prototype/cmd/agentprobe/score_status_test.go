package main

import (
	"context"
	"encoding/json"
	"io"
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

// runTasks writes one status line per task run, so a second line for the
// same task and tier is a corrupt or mixed file. Neither line may win
// silently: the later "complete" would put an interrupted run in the
// headline, the earlier would hide a rerun.
func TestApplyStatusRefusesADuplicateTaskRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.jsonl")
	writeLines(t, path,
		Status{Task: "A", Tier: "technical", Status: "interrupted"},
		Status{Task: "A", Tier: "novice", Status: "complete"},
		Status{Task: "A", Tier: "technical", Status: "complete"})
	recs := []probe.Record{{Task: "A", Tier: "technical"}}
	err := applyStatus(recs, path)
	if err == nil || !strings.Contains(err.Error(), ":3:") || !strings.Contains(err.Error(), "second status line") {
		t.Fatalf("err = %v, want the duplicate on line 3 named", err)
	}
	if recs[0].RunStatus != "" {
		t.Errorf("a refused file still marked the record %q", recs[0].RunStatus)
	}
}

// The duplicate's only source in the driver: a tier given twice would run
// every task twice on it. It is refused before anything runs.
func TestParseTiersRefusesADuplicate(t *testing.T) {
	if _, err := parseTiers("technical,novice,technical"); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("err = %v", err)
	}
	if got, err := parseTiers("technical, novice"); err != nil || strings.Join(got, ",") != "technical,novice" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestApplyStatusRefusesAMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.jsonl")
	writeLines(t, path, map[string]string{"task": "A", "tier": "technical"})
	if err := applyStatus([]probe.Record{{Task: "A"}}, path); err == nil {
		t.Fatal("a status line without a status must be an error, not 'complete'")
	}
}

// A run keeps the tasks it ran, and scoring uses them: editing the suite
// afterwards must not rescore the run against fixtures it never saw.
func TestRunKeepsItsSuiteAndScoreUsesIt(t *testing.T) {
	tasks, err := probe.LoadTasks(suite)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	none := func(context.Context, probe.Task, string) ([]probe.Record, error) { return nil, nil }
	if code := runTasks(context.Background(), none, out, tasks, []string{"technical"}, nil, io.Discard); code != 0 {
		t.Fatalf("run: exit %d", code)
	}
	kept, err := probe.LoadTasks(filepath.Join(out, runSuite))
	if err != nil {
		t.Fatalf("the run's copy does not load: %v", err)
	}
	if len(kept) != len(tasks) || kept[0].ID != tasks[0].ID {
		t.Fatalf("the run kept %d tasks, want %d", len(kept), len(tasks))
	}

	if got, err := suiteFor(out, ""); err != nil || got != filepath.Join(out, runSuite) {
		t.Errorf("no -tasks: %q %v, want the run's own copy", got, err)
	}
	if got, err := suiteFor(out, suite); err != nil || got != filepath.Join(out, runSuite) {
		t.Errorf("-tasks naming the same suite: %q %v", got, err)
	}

	// The suite edited after the run: one fixture changed.
	edited := filepath.Join(t.TempDir(), "edited.jsonl")
	changed := append([]probe.Task(nil), tasks...)
	changed[0].Fixture = append(changed[0].Fixture[:0:0], changed[0].Fixture...)
	changed[0].Fixture[0].Data += "edited"
	writeLines(t, edited, toAny(changed)...)
	if _, err := suiteFor(out, edited); err == nil {
		t.Error("scoring against an edited suite was allowed")
	}

	// A run from before runs kept a copy needs -tasks, and gets it as given.
	old := t.TempDir()
	if _, err := suiteFor(old, ""); err == nil {
		t.Error("an old run with no copy and no -tasks was scored against nothing")
	}
	if got, err := suiteFor(old, edited); err != nil || got != edited {
		t.Errorf("old run with -tasks: %q %v", got, err)
	}
}

func toAny[T any](xs []T) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}
