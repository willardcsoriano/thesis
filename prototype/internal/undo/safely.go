package undo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synapseos/internal/executor"
)

// Kept is something undo set aside before changing it: what was at Path is now
// at TrashPath, in case the person still needs it.
type Kept struct {
	Path      string
	TrashPath string
	// Cleared: the path was moved out of the way; otherwise a linked copy was
	// kept and the undo then removed or reused the path.
	Cleared bool
}

// SafeResult is what ApplySafely did besides undoing.
type SafeResult struct {
	Kept []Kept
	// GitStash names a stash commit holding the uncommitted changes a git
	// restore would have discarded; empty when the tree was clean.
	GitStash string
	Errs     []error
	// Aborted is true when ApplySafely stopped before changing anything
	// because it could not keep a current version; the entry is not undone.
	Aborted bool
}

// keptAction says how a path is set aside before Apply runs.
type keptAction int

const (
	keepOnly  keptAction = iota // linked into the trash; Apply still removes or reuses it
	keepClear                   // linked into the trash and cleared, so Apply restores into an empty slot
)

type plannedKeep struct {
	path   string
	action keptAction
}

// planKeeps lists what currently exists where undo will remove, overwrite or
// restore something (docs/cli-breaking.md, findings atk1-08, atk1-09, atk2-01
// to 03, atk4-05). Undo used to destroy those paths whatever they held, so a
// file the person had written since the command ran was lost without warning.
//
// Two kinds of path are never set aside, because Apply relies on finding them
// where they are: a file that is itself one a move carried (Apply moves it
// back by its identity), and a captured original still in place (Apply keeps
// it). A path met twice is set aside once, with the stronger action.
func planKeeps(e Entry) []plannedKeep {
	targets := restoreTargets(e)
	moved := map[FileID]bool{}
	moveDest := map[string]bool{}
	for _, m := range e.Moves {
		if m.ID != nil {
			moved[*m.ID] = true
		}
		// Where a move put a file is where Apply looks for it, whether or
		// not the journal recorded its identity.
		moveDest[filepath.Join(e.Dir, m.NewPath)] = true
	}
	protected := func(abs string) bool {
		if targets.isOriginal(abs) {
			return true
		}
		id, ok := IDOf(abs)
		return ok && moved[id]
	}
	order := []string{}
	action := map[string]keptAction{}
	add := func(abs string, a keptAction) {
		if _, err := os.Lstat(abs); err != nil || protected(abs) {
			return
		}
		prev, seen := action[abs]
		if !seen {
			order = append(order, abs)
		}
		if !seen || a > prev {
			action[abs] = a
		}
	}
	for _, item := range e.Trashed {
		add(item.OriginalPath, keepClear) // restored from the trash into this slot
	}
	for _, cb := range e.ContentBackups {
		add(cb.Path, keepClear) // its old content is written here
		if cb.LinkTarget != "" {
			add(cb.LinkTarget, keepClear) // and, through a link, here
		}
	}
	for _, m := range e.Moves {
		add(filepath.Join(e.Dir, m.OldPath), keepClear) // the moved file comes back here
		add(filepath.Join(e.Dir, m.NewPath), keepOnly)  // removed if it is not the moved file
	}
	created := map[string]bool{}
	for _, c := range e.Created {
		created[filepath.Join(e.Dir, c)] = true
	}
	for _, c := range e.Created {
		abs := filepath.Join(e.Dir, c)
		add(abs, keepOnly) // removed as something the command made
		// Anything added since inside a folder the command made would stop
		// that folder from being removed, leaving the undo half done
		// (finding r2-atk6-02); it is moved out of the way first.
		if fi, err := os.Lstat(abs); err == nil && fi.IsDir() {
			entries, _ := os.ReadDir(abs)
			for _, en := range entries {
				if child := filepath.Join(abs, en.Name()); !created[child] && !moveDest[child] {
					add(child, keepClear)
				}
			}
		}
	}
	out := make([]plannedKeep, 0, len(order))
	for _, p := range order {
		out = append(out, plannedKeep{p, action[p]})
	}
	return out
}

// WillSetAside lists, for the confirmation shown before an undo, every path
// whose current version ApplySafely will move to the trash before undoing.
func WillSetAside(e Entry) []string {
	var out []string
	for _, k := range planKeeps(e) {
		out = append(out, k.path)
	}
	return out
}

// ApplySafely is the undo the person runs. It sets aside, in the trash, the
// current version of every path the undo will change, saves uncommitted git
// changes a git restore would discard, and then runs Apply unchanged: the
// same restore the sandboxed recovery check measures (cmd/recoverycheck),
// into slots that hold nothing the person could still need. Nothing undo
// touches is destroyed: it is either restored or kept.
func ApplySafely(e Entry) SafeResult {
	var res SafeResult
	// Every saved copy the undo restores from must still exist; otherwise it
	// would set the current versions aside and then fail, half done
	// (finding r2-atk6-05).
	for _, cb := range e.ContentBackups {
		if _, err := os.Lstat(cb.BackupPath); err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: the saved copy of %s is missing (%s)", cb.Path, cb.BackupPath))
		}
	}
	for _, it := range e.Trashed {
		if _, err := os.Lstat(it.TrashPath); err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: the saved copy of %s is missing (%s)", it.OriginalPath, it.TrashPath))
		}
	}
	if len(res.Errs) > 0 {
		res.Aborted = true
		return res
	}
	// Uncommitted git changes are saved before anything is set aside, so the
	// stash holds the person's edits, not the moves made below (r2-atk6-01).
	if e.GitReset != "" {
		stash, err := stashUncommitted(e.Dir)
		if err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: could not save the uncommitted changes in %s first: %w", e.Dir, err))
			res.Aborted = true
			return res
		}
		res.GitStash = stash
	}
	trashDir, err := DefaultTrashDir()
	if err != nil {
		res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: the trash, where current versions are kept, is unavailable: %w", err))
		res.Aborted = true
		return res
	}
	// Two passes, so a failure leaves the tree as it was. Every current version
	// is kept first; only when all are kept is anything cleared. A path that
	// then cannot be cleared puts back the ones already cleared, and the undo
	// is reported aborted, so runUndo returns the entry to the journal and
	// "nothing was undone" stays true (review of the 2026-10-27 ship: a clear
	// that failed used to return with the entry gone and Aborted unset).
	type keptAt struct {
		k    plannedKeep
		info os.FileInfo
		dst  string
	}
	var kept []keptAt
	for i, k := range planKeeps(e) {
		info, err := os.Lstat(k.path)
		if err != nil {
			continue
		}
		dst := filepath.Join(trashDir, fmt.Sprintf("%d-%d-undo-kept-%s", time.Now().UnixNano(), i, filepath.Base(k.path)))
		if err := preserveViaHardlink(k.path, dst, info); err != nil {
			for _, c := range kept {
				os.RemoveAll(c.dst) // a second link to a file still in place
			}
			os.RemoveAll(dst)
			res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: could not keep the current %s before undoing: %w", k.path, err))
			res.Aborted = true
			return res
		}
		kept = append(kept, keptAt{k, info, dst})
	}
	var cleared []keptAt
	for _, c := range kept {
		res.Kept = append(res.Kept, Kept{Path: c.k.path, TrashPath: c.dst, Cleared: c.k.action == keepClear})
		if c.k.action != keepClear {
			continue
		}
		if err := os.RemoveAll(c.k.path); err != nil {
			res.Errs = append(res.Errs, fmt.Errorf("nothing was undone: could not clear %s (its current version is kept at %s): %w", c.k.path, c.dst, err))
			for _, b := range cleared {
				if perr := preserveViaHardlink(b.dst, b.k.path, b.info); perr != nil {
					res.Errs = append(res.Errs, fmt.Errorf("%s could not be put back; its current version is kept at %s: %w", b.k.path, b.dst, perr))
				}
			}
			res.Aborted = true
			return res
		}
		cleared = append(cleared, c)
	}
	res.Errs = append(res.Errs, Apply(e)...)
	return res
}

// stashUncommitted records the working tree's uncommitted changes as a stash
// entry without touching the tree, and returns its commit, or "" when there
// is nothing uncommitted. The stash is listed by `git stash list`.
func stashUncommitted(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := executor.RunIn(ctx, dir, "git stash create 'kept by synapse undo'")
	if r.Err != nil || r.ExitCode != 0 {
		return "", fmt.Errorf("git stash create: %v %s", r.Err, strings.TrimSpace(r.Stderr))
	}
	sha := strings.TrimSpace(r.Stdout)
	if sha == "" {
		return "", nil
	}
	r = executor.RunIn(ctx, dir, "git stash store -m 'kept by synapse undo' "+sha)
	if r.Err != nil || r.ExitCode != 0 {
		return "", fmt.Errorf("git stash store: %v %s", r.Err, strings.TrimSpace(r.Stderr))
	}
	return sha, nil
}
