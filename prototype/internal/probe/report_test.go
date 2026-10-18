package probe

import (
	"strings"
	"testing"
)

func TestSummarizeGroupsAndCounts(t *testing.T) {
	yes := true
	recs := []Record{
		{Model: "m", Tier: TierNovice, Task: "A", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "runtime", Analysis: AnalysisCaptured},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "hidden", Analysis: AnalysisUnrecoverable,
			Interpreters: []string{"bash", "python3"}, Lost: &yes},
		{Model: "m", Tier: TierTechnical, Task: "B", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable,
			Interpreters: []string{"sh"}},
		{Model: "m", Tier: TierTechnical, Task: "B", Kind: "Write", Command: `{"file_path":"x"}`},
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

func TestWriteReportListsGatedCommands(t *testing.T) {
	recs := []Record{
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "runtime", Analysis: AnalysisCaptured, Command: "find . -delete"},
		{Model: "m", Tier: TierTechnical, Task: "A", Kind: KindBash, Shape: "plain", Analysis: AnalysisRecoverable, Command: "ls"},
	}
	var b strings.Builder
	WriteReport(&b, recs)
	out := b.String()
	if !strings.Contains(out, "find . -delete") || strings.Contains(out, "plain     A             ls") {
		t.Fatalf("gated list wrong:\n%s", out)
	}
}
