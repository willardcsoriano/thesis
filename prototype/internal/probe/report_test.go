package probe

import (
	"strings"
	"testing"
)

func TestSummarizeGroupsAndCounts(t *testing.T) {
	yes := true
	rp := StateReplayed
	recs := []Record{
		{Model: "m", Tier: TierNovice, Task: "A", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable, AnalysisState: rp},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "runtime", Analysis: AnalysisCaptured, AnalysisState: rp},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "hidden", Analysis: AnalysisUnrecoverable, AnalysisState: rp,
			Interpreters: []string{"bash", "python3"}, Lost: &yes},
		{Model: "m", Tier: TierTechnical, Task: "B", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable, AnalysisState: rp,
			Interpreters: []string{"sh"}},
		{Model: "m", Tier: TierTechnical, Task: "B", Kind: "Write", Command: `{"file_path":"x"}`, AnalysisState: rp},
	}
	gs := Summarize(recs)
	if len(gs) != 2 || gs[0].Tier != TierTechnical || gs[1].Tier != TierNovice {
		t.Fatalf("groups not ordered by tier: %+v", gs)
	}
	g := gs[0]
	if g.Tasks != 2 || g.LostTasks != 1 || g.Bash != 3 || g.Tools["Write"] != 1 {
		t.Fatalf("counts wrong: %+v", g)
	}
	// A shell is not a language: only the python3 command counts.
	if g.Language != 1 {
		t.Fatalf("language = %d, want 1", g.Language)
	}
	if g.Shape["runtime"] != 1 || g.Analysis[AnalysisCaptured] != 1 || g.Analysis[AnalysisUnrecoverable] != 1 {
		t.Fatalf("shape/analysis wrong: %+v", g)
	}
}

// TestSummarizeKeepsNonReplayedApart: a verdict on any state but a replayed,
// verified one never reaches the headline counts, whichever state it is,
// including records scored before states existed.
func TestSummarizeKeepsNonReplayedApart(t *testing.T) {
	var recs []Record
	for _, st := range []string{StateReplayed, StateUnverified, StateDiverged, StateUnreconstructable, StateInitial, ""} {
		recs = append(recs, Record{Model: "m", Tier: TierNovice, Task: "A", Kind: KindBash, Shape: "plain",
			Analysis: AnalysisCaptured, AnalysisState: st})
	}
	recs[3].Analysis = "" // unreconstructable carries no verdict
	g := Summarize(recs)[0]
	if g.Analysis[AnalysisCaptured] != 1 || len(g.Analysis) != 1 {
		t.Fatalf("headline = %v, want only the replayed verdict", g.Analysis)
	}
	for _, st := range []string{StateUnverified, StateDiverged, StateInitial, ""} {
		if g.Other[st][AnalysisCaptured] != 1 {
			t.Errorf("Other[%q] = %v", st, g.Other[st])
		}
	}
	if g.Other[StateUnreconstructable][""] != 1 || g.States[StateReplayed] != 1 || g.Bash != 6 {
		t.Errorf("states %v other %v", g.States, g.Other)
	}

	var b strings.Builder
	WriteReport(&b, recs)
	out := b.String()
	for _, want := range []string{
		"analysis:  captured 1 (100%)  [on replayed state, 1 of 6 shell commands]",
		"not headline, replay-unverified: captured 1",
		"not headline, replay-diverged: captured 1",
		"not headline, initial: captured 1",
		"not headline, (no state): captured 1",
		"not headline, unreconstructable: (none) 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteReportListsGatedCommands(t *testing.T) {
	recs := []Record{
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "runtime", Analysis: AnalysisCaptured, Command: "find . -delete", AnalysisState: StateReplayed},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable, Command: "ls", AnalysisState: StateReplayed},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "plain", Command: "rm x", AnalysisState: StateUnreconstructable,
			AnalysisNote: "step 0.1 Edit: old_string does not occur exactly"},
	}
	var b strings.Builder
	WriteReport(&b, recs)
	out := b.String()
	if !strings.Contains(out, "find . -delete") || strings.Contains(out, "plain     A             ls") {
		t.Fatalf("gated list wrong:\n%s", out)
	}
	_, notReplayed, _ := strings.Cut(out, "== steps not analysed")
	if !strings.Contains(notReplayed, "rm x") || !strings.Contains(notReplayed, "old_string does not occur") || strings.Contains(notReplayed, "find . -delete") {
		t.Fatalf("not-replayed list wrong:\n%s", notReplayed)
	}
}
