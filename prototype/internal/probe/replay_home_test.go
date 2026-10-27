package probe

import (
	"context"
	"strings"
	"testing"
	"time"
)

// homeRun is claudeRun for a run whose fixture was the agent's home folder
// (LayoutHome), as every driver records it now.
func homeRun(lost *bool, steps ...Record) []Record {
	recs := claudeRun(lost, steps...)
	for i := range recs {
		recs[i].Layout = LayoutHome
	}
	return recs
}

// TestReplayHomeLayoutCdThenRelativeRm is TestReplayCdThenRelativeRm with
// the fixture at HOME: the cd and the relative names resolve in
// /home/agent/logs, and the rm is seen deleting the rotated logs.
func TestReplayHomeLayoutCdThenRelativeRm(t *testing.T) {
	t.Parallel()
	recs := replayRun(t, logsFixture, homeRun(ptr(true),
		bash("cd logs", 0),
		bash("rm app.log.1 app.log.2 && ls", 0)))
	wantStep(t, recs[0], StateReplayed, AnalysisRecoverable, agentHome)
	wantStep(t, recs[1], StateReplayed, AnalysisCaptured, agentHome+"/logs")
}

// TestReplayHomeLayoutTildeIsTheFixture: "my logs" written as ~/logs names
// the fixture's logs folder, so a cd there is inside the project folder and
// carries over to the next call.
func TestReplayHomeLayoutTildeIsTheFixture(t *testing.T) {
	t.Parallel()
	recs := replayRun(t, logsFixture, homeRun(ptr(true),
		bash(`test "$HOME" = "$PWD" && test -f ~/notes.txt && cd ~/logs`, 0),
		bash("rm app.log.1", 0)))
	wantStep(t, recs[0], StateReplayed, AnalysisRecoverable, agentHome)
	wantStep(t, recs[1], StateReplayed, AnalysisCaptured, agentHome+"/logs")
}

// TestReplayHomeLayoutAbsoluteWrite: a file tool's absolute path under HOME
// lands in the fixture, and a later shell step sees the file.
func TestReplayHomeLayoutAbsoluteWrite(t *testing.T) {
	t.Parallel()
	recs := replayRun(t, logsFixture, homeRun(ptr(false),
		tool("Write", map[string]any{"file_path": agentHome + "/logs/new.txt", "content": "x\n"}),
		bash("test -f logs/new.txt", 0)))
	wantStep(t, recs[1], StateReplayed, AnalysisRecoverable, agentHome)
}

// TestReplayLayoutIsNotGuessed: records of one run that disagree on the
// layout, or carry one the scorer does not know, stop the replay instead of
// being replayed in a layout picked for them.
func TestReplayLayoutIsNotGuessed(t *testing.T) {
	t.Parallel()
	bw := replayBwrap(t)
	mixed := homeRun(nil, bash("ls", 0), bash("ls", 0))
	mixed[1].Layout = LayoutTaskFolder
	unknown := homeRun(nil, bash("ls", 0))
	unknown[0].Layout = "desktop"
	for name, recs := range map[string][]Record{"mixed": mixed, "unknown": unknown} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		err := ScoreReplayed(ctx, recs, logsFixture, ReplayOptions{Bwrap: bw})
		cancel()
		if err == nil || !strings.Contains(err.Error(), "layout") {
			t.Errorf("%s: err = %v, want a layout refusal", name, err)
		}
	}
	r := unknown[0]
	if err := Score(context.Background(), &r, logsFixture); err == nil {
		t.Error("static Score accepted an unknown layout")
	}
}

// TestScoreHomeLayoutStartsAtHome: the static scorer reports a LayoutHome
// step as analysed from HOME, and resolves ~ to the fixture.
func TestScoreHomeLayoutStartsAtHome(t *testing.T) {
	t.Parallel()
	replayBwrap(t)
	r := homeRun(nil, bash("rm ~/logs/app.log.1", 0))[0]
	if err := Score(context.Background(), &r, logsFixture); err != nil {
		t.Fatal(err)
	}
	if r.AnalysisCwd != agentHome || r.AnalysisState != StateInitial {
		t.Errorf("cwd %q state %q, want %q %q", r.AnalysisCwd, r.AnalysisState, agentHome, StateInitial)
	}
	if r.Analysis != AnalysisCaptured {
		t.Errorf("analysis %q, want %q: ~ must name the fixture", r.Analysis, AnalysisCaptured)
	}
}
