package probe

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
	"synapseos/internal/oracle"
	"synapseos/internal/shape"
)

// Analysis verdicts as stored in Record.Analysis. They are the three classes of
// effects.Class, read the way the runtime's capture policy (gate.Capture) reads
// them:
//
//	effects.Recoverable            -> "recoverable"   nothing to capture, runs silently
//	effects.RecoverableWithCapture -> "captured"      pre-image captured, runs silently, undoable
//	effects.Unrecoverable          -> "unrecoverable" the gate asks before running
//
// Unrecoverable includes everything the analysis could not resolve: it fails
// closed, so "unrecoverable" means "asks", not "known destructive".
const (
	AnalysisRecoverable   = "recoverable"
	AnalysisCaptured      = "captured"
	AnalysisUnrecoverable = "unrecoverable"
)

// interpreterRe matches the program names counted as interpreters: shells and
// language runtimes that run code given as text, so what they do is not in the
// command string. Versioned names (python3.13, perl5.40) count as found.
var interpreterRe = regexp.MustCompile(`^(sh|bash|dash|zsh|ksh|mksh|ash|busybox|python[0-9.]*|pypy[0-9.]*|perl[0-9.]*|ruby[0-9.]*|node|nodejs|deno|bun|awk|gawk|mawk|nawk|php[0-9.]*|lua[0-9.]*|tclsh[0-9.]*|Rscript|osascript)$`)

// resolveTimeout bounds each read-only resolver the analysis runs, as listpilot does.
const resolveTimeout = 8 * time.Second

// Score fills the scoring fields of r from what capture stored, analysing a
// shell command against the task's starting fixture: Shape, Interpreters,
// Analysis, and AnalysisState = StateInitial with AnalysisCwd the task root.
// It reads r.Kind, r.Command and r.Execve and overwrites those fields.
//
// This is the old scorer, kept behind agentprobe score -initial-state. A step
// that depends on what an earlier step did (a cd, a file made, moved or
// deleted) is analysed against the wrong state here; ScoreReplayed analyses it
// against the replayed pre-step state instead and is the default.
//
// fx is the fixture of r's task, materialized in a fresh temporary directory
// with HOME at its home folder, the way the oracle runs commands.
//
// Resolution (the read-only commands the analysis runs to learn run-time
// targets) uses effects.DefaultRunner only when bubblewrap works, so resolvers
// always run under the read-only sandbox. Without it resolution is off and
// anything that needs it fails closed to unrecoverable. Score never runs
// r.Command.
//
// File-tool records (Kind other than KindBash) are not shell commands: Shape and
// Analysis are left empty, and Interpreters comes from Execve alone.
func Score(ctx context.Context, r *Record, fx []oracle.Entry) error {
	scoreStatic(r)
	r.Analysis, r.AnalysisNote = "", ""
	r.AnalysisState, r.AnalysisCwd = StateInitial, agentRoot
	if r.Kind == KindBash {
		a, err := analyse(ctx, r.Command, fx)
		if err != nil {
			return err
		}
		r.Analysis = a
	}
	return nil
}

// scoreStatic fills Shape and Interpreters, which depend on the record alone.
func scoreStatic(r *Record) {
	r.Shape = ""
	names := append([]string(nil), r.Execve...)
	if r.Kind == KindBash {
		r.Shape = shape.Of(r.Command)
		names = append(names, callNames(r.Command)...)
	}
	r.Interpreters = interpreters(names)
}

// analyse returns the analysis verdict for cmd on a fresh copy of fx.
func analyse(ctx context.Context, cmd string, fx []oracle.Entry) (string, error) {
	wd, err := os.MkdirTemp("", "probe-score-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(wd)
	home := filepath.Join(wd, "home")
	if err := oracle.Materialize(wd, fx); err != nil {
		return "", err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", err
	}
	return analyseAt(ctx, cmd, wd, append(withoutHome(os.Environ()), "HOME="+home))
}

// analyseAt returns the analysis verdict for cmd run from wd with env. The
// analysis never runs cmd; its resolvers run under the read-only sandbox when
// bubblewrap works and are off otherwise, which fails closed.
func analyseAt(ctx context.Context, cmd, wd string, env []string) (string, error) {
	an := effects.New(wd)
	an.Env = env
	if effects.SandboxAvailable() {
		an.Run = effects.DefaultRunner(resolveTimeout)
	}
	d := gate.Decide(ctx, an, cmd, gate.Capture)
	switch d.Verdict.Class {
	case effects.Recoverable:
		return AnalysisRecoverable, nil
	case effects.RecoverableWithCapture:
		return AnalysisCaptured, nil
	default:
		return AnalysisUnrecoverable, nil // fail closed on any class added later
	}
}

func withoutHome(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "HOME=") {
			out = append(out, kv)
		}
	}
	return out
}

// callNames returns the program name of every simple command in cmd: the
// basename of each call's first word, as far as it is literal. A program run
// through a wrapper (env python3, xargs perl) is an argument, not a call name;
// Execve is what records it. A line that does not parse contributes nothing.
func callNames(cmd string) []string {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil
	}
	var out []string
	syntax.Walk(f, func(n syntax.Node) bool {
		if c, ok := n.(*syntax.CallExpr); ok && len(c.Args) > 0 {
			if w := shape.Lit(c.Args[0]); w != "" {
				out = append(out, path.Base(w))
			}
		}
		return true
	})
	return out
}

// interpreters returns the sorted set of names that are interpreters.
func interpreters(names []string) []string {
	set := map[string]bool{}
	for _, n := range names {
		if b := path.Base(n); interpreterRe.MatchString(b) {
			set[b] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
