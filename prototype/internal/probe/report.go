package probe

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
)

// Shapes in reporting order, as internal/shape names them.
var Shapes = []string{"plain", "compound", "runtime", "redirect", "hidden"}

// Languages are the interpreters that mean the agent left the shell for a
// programming language. Shells (sh, bash) and awk are excluded: an agent
// running bash commands always launches them, so counting them would say
// nothing about leaving the shell.
var Languages = map[string]bool{"python": true, "python2": true, "python3": true, "perl": true, "ruby": true,
	"node": true, "nodejs": true, "php": true, "lua": true, "Rscript": true, "deno": true, "bun": true}

// Group is the summary of one model and tier.
type Group struct {
	Model, Tier string
	Tasks       int            // distinct tasks seen
	LostTasks   int            // tasks whose run destroyed existing data (oracle)
	Bash        int            // shell commands
	Tools       map[string]int // file and other tool calls, by tool name
	Shape       map[string]int // shell commands by shape
	Language    int            // shell commands that ran a programming language
	// Analysis counts the verdicts taken on a replayed and verified state
	// (StateReplayed) only: the headline numbers.
	Analysis map[string]int
	// States counts shell commands by AnalysisState ("" for records scored
	// before states existed), and Other the verdicts of every state but
	// StateReplayed, by state, so they are never pooled with the headline.
	States map[string]int
	Other  map[string]map[string]int
}

// Summarize groups scored records by model and tier, in a stable order.
func Summarize(recs []Record) []Group {
	type key struct{ model, tier string }
	groups := map[key]*Group{}
	tasks := map[key]map[string]bool{}
	lost := map[key]map[string]bool{}
	for _, r := range recs {
		k := key{r.Model, r.Tier}
		g := groups[k]
		if g == nil {
			g = &Group{Model: r.Model, Tier: r.Tier, Tools: map[string]int{}, Shape: map[string]int{}, Analysis: map[string]int{},
				States: map[string]int{}, Other: map[string]map[string]int{}}
			groups[k], tasks[k], lost[k] = g, map[string]bool{}, map[string]bool{}
		}
		tasks[k][r.Task] = true
		if r.Lost != nil && *r.Lost {
			lost[k][r.Task] = true
		}
		if r.Kind != KindBash {
			g.Tools[r.Kind]++
			continue
		}
		g.Bash++
		g.Shape[r.Shape]++
		st := effectiveState(r)
		g.States[st]++
		if st == StateReplayed {
			g.Analysis[r.Analysis]++
		} else {
			if g.Other[st] == nil {
				g.Other[st] = map[string]int{}
			}
			g.Other[st][r.Analysis]++
		}
		if slices.ContainsFunc(r.Interpreters, func(s string) bool { return Languages[s] }) {
			g.Language++
		}
	}
	var out []Group
	for k, g := range groups {
		g.Tasks, g.LostTasks = len(tasks[k]), len(lost[k])
		out = append(out, *g)
	}
	tierRank := func(t string) int {
		if i := slices.Index(Tiers, t); i >= 0 {
			return i
		}
		return len(Tiers)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return tierRank(out[i].Tier) < tierRank(out[j].Tier)
	})
	return out
}

// effectiveState is the state a record's verdict counts under. A record from
// a task run that did not complete (status.jsonl says incomplete,
// interrupted, error, fatal, or has no line for it) is reported under
// "run-<status>" whatever its replay state: the run is missing steps, so its
// verdicts are not headline evidence.
func effectiveState(r Record) string {
	if r.RunStatus != "" && r.RunStatus != RunComplete {
		return "run-" + r.RunStatus
	}
	return r.AnalysisState
}

// verdicts is the reporting order of analysis verdicts.
var verdicts = []string{AnalysisRecoverable, AnalysisCaptured, AnalysisUnrecoverable}

// WriteReport prints the summary, then every shell command the analysis would
// not let run unasked (captured or unrecoverable), so the destructive ones can
// be read alongside the counts, then every step whose verdict was not taken on
// a replayed and verified state, with the reason.
//
// The analysis line counts StateReplayed verdicts only, as a share of those
// commands. Verdicts on any other state (unverified, diverged, the starting
// fixture) are printed on their own lines under the state's name and never
// added to it.
func WriteReport(w io.Writer, recs []Record) {
	for _, g := range Summarize(recs) {
		fmt.Fprintf(w, "== %s / %s: %d tasks, %d lost data, %d shell commands, %d other tool calls\n",
			g.Model, g.Tier, g.Tasks, g.LostTasks, g.Bash, sum(g.Tools))
		fmt.Fprintf(w, "   shape:     %s\n", counts(g.Shape, Shapes, g.Bash))
		fmt.Fprintf(w, "   states:    %s\n", counts(g.States, States, g.Bash))
		replayed := g.States[StateReplayed]
		fmt.Fprintf(w, "   analysis:  %s  [on replayed state, %d of %d shell commands]\n",
			counts(g.Analysis, verdicts, replayed), replayed, g.Bash)
		for _, st := range stateOrder(g.Other) {
			fmt.Fprintf(w, "   not headline, %s: %s\n", stateName(st), counts(g.Other[st], verdicts, 0))
		}
		fmt.Fprintf(w, "   language:  %s of shell commands ran python/perl/node/...\n", pct(g.Language, g.Bash))
		if len(g.Tools) > 0 {
			fmt.Fprintf(w, "   tools:     %s\n", counts(g.Tools, nil, 0))
		}
	}
	fmt.Fprintln(w, "\n== shell commands the analysis would gate (captured or unrecoverable)")
	for _, r := range recs {
		if r.Kind == KindBash && (r.Analysis == AnalysisCaptured || r.Analysis == AnalysisUnrecoverable) {
			fmt.Fprintf(w, "%-13s %-17s %-10s %-9s %-13s %s\n", r.Analysis, stateName(r.AnalysisState), r.Tier, r.Shape, r.Task, oneLine(r.Command))
		}
	}
	fmt.Fprintln(w, "\n== steps not analysed on a replayed, verified state")
	for _, r := range recs {
		if r.AnalysisState != StateReplayed {
			fmt.Fprintf(w, "%-17s %-10s %-13s %d.%d %-6s %s\n      %s\n", stateName(r.AnalysisState), r.Tier, r.Task,
				r.Turn, r.Step, r.Kind, oneLine(r.Command), oneLine(r.AnalysisNote))
		}
	}
}

// stateOrder returns the keys of m in States order, unknown ones last.
func stateOrder(m map[string]map[string]int) []string {
	var out []string
	for _, s := range States {
		if _, ok := m[s]; ok {
			out = append(out, s)
		}
	}
	var rest []string
	for s := range m {
		if !slices.Contains(States, s) {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// stateName names a state for printing; records scored before states existed
// have none.
func stateName(s string) string {
	if s == "" {
		return "(no state)"
	}
	return s
}

func counts(m map[string]int, order []string, total int) string {
	keys := append([]string(nil), order...)
	var rest []string
	for k := range m {
		if !slices.Contains(keys, k) {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	var parts []string
	for _, k := range keys {
		if m[k] == 0 {
			continue
		}
		name := k
		if name == "" {
			name = "(none)"
		}
		if total > 0 {
			parts = append(parts, fmt.Sprintf("%s %d (%s)", name, m[k], pct(m[k], total)))
		} else {
			parts = append(parts, fmt.Sprintf("%s %d", name, m[k]))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func pct(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// oneLine prints agent-steered text with terminal escapes neutralised and
// never cuts a character in half (open-problems row 46).
func oneLine(s string) string { return OneLine(s, 160) }
