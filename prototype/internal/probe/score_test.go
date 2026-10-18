package probe

import (
	"context"
	"reflect"
	"testing"

	"synapseos/internal/effects"
	"synapseos/internal/oracle"
)

var scoreFixture = []oracle.Entry{
	{Path: "a.txt", Kind: "file", Data: "a\n"},
	{Path: "b.txt", Kind: "file", Data: "b\n"},
	{Path: "logs/x.log", Kind: "file", Data: "x\n"},
	{Path: "logs/y.log", Kind: "file", Data: "y\n"},
}

func TestScore(t *testing.T) {
	cases := []struct {
		name         string
		rec          Record
		shape        string
		interpreters []string
		analysis     string
	}{
		{name: "plain read", rec: Record{Kind: KindBash, Command: "ls -la"},
			shape: "plain", analysis: AnalysisRecoverable},
		{name: "plain create", rec: Record{Kind: KindBash, Command: "mkdir new"},
			shape: "plain", analysis: AnalysisRecoverable},
		{name: "plain delete", rec: Record{Kind: KindBash, Command: "rm a.txt"},
			shape: "plain", analysis: AnalysisCaptured},
		{name: "redirect overwrite", rec: Record{Kind: KindBash, Command: "echo hi > b.txt"},
			shape: "redirect", analysis: AnalysisCaptured},
		{name: "compound", rec: Record{Kind: KindBash, Command: "mkdir out && cp a.txt out/"},
			shape: "compound", analysis: AnalysisRecoverable},
		{name: "raw device", rec: Record{Kind: KindBash, Command: "dd if=/dev/zero of=/dev/sda bs=1M count=1"},
			shape: "plain", analysis: AnalysisUnrecoverable},
		{name: "hidden script", rec: Record{Kind: KindBash, Command: `python3 -c "import os; os.remove('a.txt')"`},
			shape: "hidden", interpreters: []string{"python3"}, analysis: AnalysisUnrecoverable},
		{name: "hidden shell string", rec: Record{Kind: KindBash, Command: "bash -c 'rm b.txt'"},
			shape: "hidden", interpreters: []string{"bash"}, analysis: AnalysisCaptured},
		{name: "interpreter seen only in execve", rec: Record{Kind: KindBash, Command: "sh -c 'python3 -c pass'",
			Execve: []string{"sh", "python3"}},
			shape: "hidden", interpreters: []string{"python3", "sh"}},
		{name: "execve only, behind xargs", rec: Record{Kind: KindBash, Command: "ls logs | xargs -n1 perl -e 1",
			Execve: []string{"ls", "xargs", "/usr/bin/perl"}},
			shape: "runtime", interpreters: []string{"perl"}},
		{name: "awk in a pipeline", rec: Record{Kind: KindBash, Command: "cat a.txt | awk '{print $1}'"},
			shape: "hidden", interpreters: []string{"awk"}, analysis: AnalysisRecoverable},
		{name: "file tool", rec: Record{Kind: "write_file", Command: `{"path":"a.txt","content":"x"}`}},
		{name: "file tool rescored clears", rec: Record{Kind: "Edit", Command: `{}`, Shape: "plain", Analysis: "captured"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.rec
			if err := Score(context.Background(), &r, scoreFixture); err != nil {
				t.Fatal(err)
			}
			if r.Shape != c.shape {
				t.Errorf("Shape = %q, want %q", r.Shape, c.shape)
			}
			if !reflect.DeepEqual(r.Interpreters, c.interpreters) {
				t.Errorf("Interpreters = %v, want %v", r.Interpreters, c.interpreters)
			}
			if c.analysis != "" && r.Analysis != c.analysis {
				t.Errorf("Analysis = %q, want %q", r.Analysis, c.analysis)
			}
			if r.Kind != KindBash && r.Analysis != "" {
				t.Errorf("file tool got Analysis %q", r.Analysis)
			}
		})
	}
}

// TestScoreResolution covers a verdict that rests on running a read-only
// resolver: with the sandbox the substitution resolves to the two logs, which
// are captured; without it the analysis must fail closed.
func TestScoreResolution(t *testing.T) {
	r := Record{Kind: KindBash, Command: "rm $(ls logs/*.log)"}
	if err := Score(context.Background(), &r, scoreFixture); err != nil {
		t.Fatal(err)
	}
	want := AnalysisUnrecoverable
	if effects.SandboxAvailable() {
		want = AnalysisCaptured
	}
	if r.Shape != "runtime" || r.Analysis != want {
		t.Errorf("got shape %q analysis %q, want runtime %q", r.Shape, r.Analysis, want)
	}
}
