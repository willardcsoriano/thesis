// Package gate joins the effect analysis to the undo mechanisms: it decides
// whether a command needs confirmation, captures what the analysis says must be
// captured before the command runs, and builds the journal entry that undoes it.
//
// It is the only place that knows both internal/effects (what a command will do)
// and internal/undo (how to put it back), so the runtime and the recovery harness
// exercise the same code.
package gate

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"synapseos/internal/effects"
	"synapseos/internal/undo"
)

// Policy is when the gate asks the user.
type Policy string

const (
	// Strict asks unless the command is recoverable with no capture needed — the
	// behaviour of the gate before the analysis existed.
	Strict Policy = "strict"
	// Capture asks only when the command is unrecoverable. Anything that can be
	// captured is captured silently and undone with `synapse undo`.
	Capture Policy = "capture"
)

// Decision is the analysis of one command and what to do about it.
type Decision struct {
	Analysis *effects.Analysis
	Verdict  effects.Verdict
	Plan     effects.Plan
	Confirm  bool
	// Confident is true when the analysis found nothing it could not resolve, so
	// the effect set is complete and the plan can stand in for the legacy backups.
	Confident bool
}

// Decide analyses cmd in dir. The analyzer decides how resolution runs: a nil Run
// disables it and anything that needs it fails closed.
func Decide(ctx context.Context, an *effects.Analyzer, cmd string, p Policy) Decision {
	res := an.Analyze(ctx, cmd)
	v := res.Verdict()
	d := Decision{Analysis: res, Verdict: v, Plan: res.Plan(), Confident: len(res.Issues) == 0}
	switch p {
	case Capture:
		d.Confirm = v.Class == effects.Unrecoverable
	default:
		d.Confirm = v.Class != effects.Recoverable
	}
	return d
}

// Capture takes every capture in the plan and returns a journal entry that undoes
// the command: renames and compressions move back, what the command created is
// removed, and what it overwrote, removed, or re-moded is restored from capture.
// Call it immediately before running the command. Failures are returned beside
// whatever did succeed, because a partial safety net is better than none.
func (d Decision) Capture(dir, cmd string) (undo.Entry, []error) {
	e := undo.Entry{Timestamp: time.Now(), Command: cmd, Dir: dir}
	var errs []error

	var content, trash, meta []string
	for _, c := range d.Plan.Captures {
		switch c.Mechanism {
		case effects.Content:
			content = append(content, c.Path)
		case effects.Trash:
			trash = append(trash, c.Path)
		case effects.Metadata:
			meta = append(meta, c.Path)
		case effects.Inverse:
			for _, argv := range c.Inverse {
				e.Inverses = append(e.Inverses, undo.Inverse{Argv: argv, Sudo: c.Sudo})
			}
		case effects.GitHead:
			sha, err := undo.CaptureGitHead(c.Path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			e.GitReset = sha
		}
	}
	var cerrs []error
	if len(content) > 0 {
		e.ContentBackups, cerrs = undo.BackupContent(content)
		errs = append(errs, cerrs...)
	}
	if len(trash) > 0 {
		e.Trashed, cerrs = undo.TrashPreserve(trash)
		errs = append(errs, cerrs...)
	}
	if len(meta) > 0 {
		e.MetadataBackups, cerrs = undo.BackupMetadataPaths(meta)
		errs = append(errs, cerrs...)
	}

	moveTargets := map[string]bool{}
	for _, ef := range d.Analysis.Effects {
		if ef.Kind == effects.Remove && ef.MovedTo != "" {
			moveTargets[ef.MovedTo] = true
			e.Moves = append(e.Moves, undo.Move{OldPath: rel(dir, ef.Path), NewPath: rel(dir, ef.MovedTo)})
		}
	}
	seen := map[string]bool{}
	for _, ef := range d.Analysis.Effects {
		if ef.Kind == effects.Create && !moveTargets[ef.Path] && !seen[ef.Path] {
			seen[ef.Path] = true
			e.Created = append(e.Created, rel(dir, ef.Path))
		}
	}
	sort.Strings(e.Created)
	return e, errs
}

func rel(dir, p string) string {
	if r, err := filepath.Rel(dir, p); err == nil {
		return r
	}
	return p
}
