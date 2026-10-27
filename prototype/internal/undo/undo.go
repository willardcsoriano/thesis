// Package undo is a best-effort, disk-persisted safety net for CLI mode's
// executed commands, covering two distinct cases by two distinct
// mechanisms.
//
// For Reversible-classified commands (the common case — nothing was
// destructive to begin with, just inconveniently placed), it works by
// snapshotting the command's working directory before and after execution,
// diffing what appeared vs. disappeared, and pairing disappeared/appeared
// paths by basename to reconstruct moves. A path that appears with no
// matching disappeared basename is treated as pure creation (safe to remove
// on undo); a path that disappears with no matching appeared basename can't
// be safely reconstructed from a directory diff alone, so it's recorded but
// left unhandled rather than guessed at. This half does not parse bash
// syntax at all — it only ever looks at directory listings before and
// after, never the command text.
//
// For a specific, narrow slice of Irreversible-classified commands — the
// content-mutating shapes classifier.ContentMutationTargets and
// CpOverwriteTarget recognize (sed -i, awk -i inplace, truncate, a
// truncating redirect, tee without -a, cp onto an existing destination) —
// that the user explicitly confirmed anyway, BackupContent takes a
// full-content copy of the target file(s) immediately before execution, so
// Apply can restore them even though a directory diff would show nothing
// unusual (the file never disappeared, its content just changed in place —
// verified empirically, Session 25: cp overwrites an existing destination's
// same inode, exactly like sed -i, rather than unlinking and recreating it,
// which is why it needs a real content copy and not the cheaper mechanism
// below). This is the "guiltless" half of accurate-and-guiltless (Session
// 24): even a wrongly confirmed "yes" should be recoverable, not just an
// accurately blocked one.
//
// For pure deletions — rm, and (via classifier.TrashTargets and
// GitCleanDryRunCommand) git clean -f — TrashPreserve takes a cheaper
// path: a hardlink into a holding directory, not a byte-for-byte copy.
// Removing a directory entry (what rm actually does, at the syscall level)
// never touches the target's underlying data, so a hardlink taken
// beforehand keeps that data alive through the deletion at a cost
// independent of the file's size — a whole gigabyte-sized tree costs the
// same to protect as an empty one. This is specifically NOT safe for
// anything that mutates a file's content in place (cp overwrite included,
// per the inode note above) — a hardlink shares the very data being
// overwritten, so it protects nothing in that case; those commands stay on
// the content-copy path.
//
// Two more mechanisms, each suited to what actually changes: BackupMetadata
// records a file's mode/ownership (not its content, and not a deletion
// either) before a confirmed recursive chmod/chown, since only a few bytes
// per file need saving regardless of the file's size. CaptureGitHead
// records the current commit HEAD before a confirmed git reset --hard,
// since git already has its own object store — no file-level backup of any
// kind is needed, just the one commit hash Apply resets back to.
package undo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"synapseos/internal/executor"
)

// Move is one file or directory this package believes moved from OldPath to
// NewPath, both relative to the Entry's Dir.
type Move struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	// ID identifies the moved file, taken from OldPath before the command ran. When
	// set, undo moves NewPath back only if it is still that file: a move on a branch
	// that did not run, or after a part that failed, never happened, and moving
	// whatever sits at NewPath onto OldPath would destroy OldPath.
	ID *FileID `json:"id,omitempty"`
	// NewExisted records whether NewPath existed before the command ran. When the
	// file at NewPath is not the moved one and NewPath is new, what sits there was
	// made by the line (an earlier part replaced the file before moving it, as
	// sed -i does) and is removed; the original comes back from its own capture.
	NewExisted bool `json:"new_existed,omitempty"`
}

// IDOf returns the identity of the file at p, without following a symlink.
func IDOf(p string) (FileID, bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return FileID{}, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, false
	}
	return FileID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, true
}

// ContentBackup records a full-content backup of one file taken immediately
// before a confirmed content-mutating command ran (sed -i, awk -i inplace,
// truncate, a truncating redirect, tee without -a — see
// classifier.ContentMutationTargets), letting Apply restore the file even
// though the command itself has no undo of its own. Unlike Moves/Created,
// which BuildEntry infers automatically from a before/after directory diff
// for any Reversible command, a ContentBackup only exists for a command the
// classifier flagged Irreversible that the user explicitly confirmed
// anyway — the "guiltless" half of accurate-and-guiltless (Session 24):
// even a wrongly confirmed "yes" should be recoverable, not just an
// accurately blocked one.
type ContentBackup struct {
	Path       string      `json:"path"`        // absolute path to the file that was backed up
	BackupPath string      `json:"backup_path"` // absolute path to the saved copy
	Mode       os.FileMode `json:"mode"`        // the original file's permissions, restored alongside its content
	// LinkText and LinkTarget are set when Path was a symlink: its own text,
	// and the file it resolved to, whose content BackupPath holds. A write
	// through the link (`echo x > link`) changes the target; `sed -i link`
	// replaces the link with a file. Undo restores both: the target's content
	// and the link itself (findings r2-atk7-01, r2-atk6-03).
	LinkText   string `json:"link_text,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

// TrashedItem records a file or directory tree preserved via hardlink (see
// TrashPreserve) immediately before a confirmed deletion ran. Unlike
// ContentBackup, this is a directory-entry-removal case — rm never touches
// the target's underlying data — so preservation costs nothing proportional
// to size.
type TrashedItem struct {
	OriginalPath string `json:"original_path"` // absolute path the item lived at before deletion
	TrashPath    string `json:"trash_path"`    // absolute path it's preserved at, restorable by moving it back
}

// MetadataBackup records one file's permission mode and ownership,
// captured immediately before a confirmed recursive chmod/chown ran over
// the directory tree containing it (see classifier.RecursivePermissionTargets).
// The cheapest of the three per-file backup shapes: a few bytes of
// metadata regardless of the file's own size, since chmod/chown never
// touch a file's content or its directory entry, only these two
// properties.
type MetadataBackup struct {
	Path string      `json:"path"`
	Mode os.FileMode `json:"mode"`
	UID  int         `json:"uid"`
	GID  int         `json:"gid"`
}

// Entry records one command's filesystem effect, well enough to reverse it.
// Dir is the absolute directory the command ran in; Moves, Created, and
// Unhandled paths are relative to it. ContentBackups', Trashed's, and
// MetadataBackups' paths are absolute (they come from classifier functions
// that already resolve against the working directory) since a
// confirmed-Irreversible command's target isn't necessarily inside Dir the
// way a Reversible command's snapshot diff always is. GitReset is the
// commit HEAD pointed at in the repository at Dir immediately before a
// confirmed git reset --hard ran there — empty when not applicable.
type Entry struct {
	Timestamp       time.Time        `json:"timestamp"`
	Command         string           `json:"command"`
	Dir             string           `json:"dir"`
	Moves           []Move           `json:"moves,omitempty"`
	Created         []string         `json:"created,omitempty"`
	Unhandled       []string         `json:"unhandled,omitempty"`
	ContentBackups  []ContentBackup  `json:"content_backups,omitempty"`
	Trashed         []TrashedItem    `json:"trashed,omitempty"`
	GitReset        string           `json:"git_reset,omitempty"`
	MetadataBackups []MetadataBackup `json:"metadata_backups,omitempty"`
	// Inverses put package or service state back by running a command.
	Inverses []Inverse `json:"inverses,omitempty"`
	// Preexisting gives the identity, before the command ran, of each Created path
	// that already existed then: the analysis records a creation where it expects
	// an earlier part of the line to have moved or removed the path, and if that part
	// did not happen, what stands there after undo is the original and is kept.
	Preexisting map[string]FileID `json:"preexisting,omitempty"`
}

// Inverse is one command that undoes a change to package or service state.
type Inverse struct {
	Argv []string `json:"argv"`
	Sudo bool     `json:"sudo,omitempty"`
}

// String renders the command as the user would type it, for the manual fallback.
func (i Inverse) String() string {
	argv := i.Argv
	if i.Sudo {
		argv = append([]string{"sudo"}, argv...)
	}
	return strings.Join(argv, " ")
}

// KeepObserved drops from e what the command was expected to do but did not:
// a creation or a move whose result exists nowhere after it ran, following
// later moves of the same path. The journal used to record the analysis's
// prediction, so a command that failed before doing anything left an entry
// whose undo reported "move back" for files that never moved and "Undo
// complete" with nothing done, and that entry stood in front of the person's
// real last change (finding r3-atk9-03). Captures stay: they were taken
// before the command ran and restore whatever it did manage.
func (e Entry) KeepObserved() Entry {
	exists := func(p string) bool {
		_, err := os.Lstat(filepath.Join(e.Dir, p))
		return err == nil
	}
	// A path the command moved on again is not gone: for mv a b; mv b c the
	// file the first move made is the one the second moved, so a->b is kept
	// when b's chain of later moves ends at a path that exists. Judging each
	// move by its own destination dropped a->b, and undo then left the file
	// at b and reported success (ship review, 2026-10-27).
	reaches := func(start string, from int) bool {
		at, seen := start, map[string]bool{}
		for !exists(at) {
			if seen[at] {
				return false
			}
			seen[at] = true
			next := -1
			for j := from; j < len(e.Moves); j++ {
				if e.Moves[j].OldPath == at {
					next = j
					break
				}
			}
			if next < 0 {
				return false
			}
			at, from = e.Moves[next].NewPath, next+1
		}
		return true
	}
	var created []string
	for _, c := range e.Created {
		if reaches(c, 0) {
			created = append(created, c)
		}
	}
	var moves []Move
	for i, m := range e.Moves {
		if reaches(m.NewPath, i+1) {
			moves = append(moves, m)
		}
	}
	e.Created, e.Moves = created, moves
	return e
}

// Restorable reports whether undoing e would put anything back. An entry
// holding only Unhandled changes records that something happened that could
// not be captured, and restores nothing.
func (e Entry) Restorable() bool {
	return len(e.Moves) > 0 || len(e.Created) > 0 || len(e.ContentBackups) > 0 || len(e.Trashed) > 0 ||
		e.GitReset != "" || len(e.MetadataBackups) > 0 || len(e.Inverses) > 0
}

// IsNoop reports whether e has nothing to undo — no filesystem effect was
// observed (or none this package could account for). Callers should not
// journal a no-op entry.
func (e Entry) IsNoop() bool {
	return len(e.Moves) == 0 && len(e.Created) == 0 && len(e.Unhandled) == 0 &&
		len(e.ContentBackups) == 0 && len(e.Trashed) == 0 &&
		e.GitReset == "" && len(e.MetadataBackups) == 0 && len(e.Inverses) == 0
}

// Snapshot lists dir's contents up to one level of nesting (dir's direct
// children, plus one level inside any subdirectory), returning the set of
// paths found, relative to dir. Depth is deliberately shallow: full
// recursion would make every reversible command pay a filesystem-walk cost
// proportional to however much unrelated data happens to live nearby. One
// level is enough to see files moved into (or already inside) a newly
// created subfolder, which is the common "mkdir dest && mv *.log dest"
// shape this package exists to handle.
func Snapshot(dir string) (map[string]bool, error) {
	paths := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", dir, err)
	}
	for _, e := range entries {
		paths[e.Name()] = true
		if !e.IsDir() {
			continue
		}
		inner, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue // permission issue etc. on a subdirectory: skip it, not fatal to the whole snapshot
		}
		for _, ie := range inner {
			paths[filepath.Join(e.Name(), ie.Name())] = true
		}
	}
	return paths, nil
}

// FileID identifies a file independent of its name: the same device and inode
// before and after means the same data, so a path that disappeared and a path
// that appeared with the same FileID is a rename or move, whatever either is called.
type FileID struct{ Dev, Ino uint64 }

// SnapshotIDs is Snapshot's companion: the FileID of each path Snapshot would
// list, from lstat so a symlink is identified as itself. Take both before the
// command runs and both after, and pass them to BuildEntryIDs.
func SnapshotIDs(dir string) (map[string]FileID, error) {
	ids := map[string]FileID{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", dir, err)
	}
	record := func(rel, abs string) {
		if fi, err := os.Lstat(abs); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				ids[rel] = FileID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}
			}
		}
	}
	for _, e := range entries {
		record(e.Name(), filepath.Join(dir, e.Name()))
		if !e.IsDir() {
			continue
		}
		inner, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, ie := range inner {
			rel := filepath.Join(e.Name(), ie.Name())
			record(rel, filepath.Join(dir, rel))
		}
	}
	return ids, nil
}

// BuildEntry diffs before (snapshotted just before cmd ran) against after
// (snapshotted just after) and pairs disappeared/appeared paths by basename
// to reconstruct Moves, Created, and Unhandled. Pairing by basename alone
// cannot see a rename to a new name; prefer BuildEntryIDs.
func BuildEntry(dir, cmd string, before, after map[string]bool) Entry {
	return BuildEntryIDs(dir, cmd, before, after, nil, nil)
}

// BuildEntryIDs is BuildEntry with file identity. Paths that vanished and
// appeared with the same FileID are paired first, so `mv notes.txt renamed.txt`
// is a move and is undone by moving it back, where basename pairing alone would
// record "renamed.txt" as created and delete it on undo. Whatever identity
// cannot pair falls back to the basename rule, which still covers moves across
// filesystems (a new inode). Nil id maps give plain BuildEntry behaviour.
func BuildEntryIDs(dir, cmd string, before, after map[string]bool, beforeIDs, afterIDs map[string]FileID) Entry {
	var appeared, disappeared []string
	for p := range after {
		if !before[p] {
			appeared = append(appeared, p)
		}
	}
	for p := range before {
		if !after[p] {
			disappeared = append(disappeared, p)
		}
	}
	sort.Strings(appeared)
	sort.Strings(disappeared)

	disappearedByBase := map[string][]string{}
	for _, p := range disappeared {
		b := filepath.Base(p)
		disappearedByBase[b] = append(disappearedByBase[b], p)
	}

	var moves []Move
	var created []string
	used := map[string]bool{}
	usedAppeared := map[string]bool{}

	if beforeIDs != nil && afterIDs != nil {
		byID := map[FileID][]string{}
		for _, p := range disappeared {
			if id, ok := beforeIDs[p]; ok {
				byID[id] = append(byID[id], p)
			}
		}
		for _, p := range appeared {
			id, ok := afterIDs[p]
			if !ok {
				continue
			}
			for _, cand := range byID[id] {
				if !used[cand] {
					used[cand] = true
					usedAppeared[p] = true
					moves = append(moves, Move{OldPath: cand, NewPath: p})
					break
				}
			}
		}
	}

	// A renamed directory drags every path one level inside it along with it in
	// the snapshot. Those nested pairs are implied by the directory's own move, and
	// undoing them separately recreates the old directory first and then fails to
	// move the directory back onto it.
	moves = dropNestedMoves(moves)

	for _, p := range appeared {
		if usedAppeared[p] {
			continue
		}
		b := filepath.Base(p)
		matched := ""
		for _, candidate := range disappearedByBase[b] {
			if !used[candidate] {
				matched = candidate
				break
			}
		}
		if matched != "" {
			used[matched] = true
			moves = append(moves, Move{OldPath: matched, NewPath: p})
		} else {
			created = append(created, p)
		}
	}

	var unhandled []string
	for _, p := range disappeared {
		if !used[p] {
			unhandled = append(unhandled, p)
		}
	}

	return Entry{
		Timestamp: time.Now(),
		Command:   cmd,
		Dir:       dir,
		Moves:     moves,
		Created:   created,
		Unhandled: unhandled,
	}
}

// dropNestedMoves removes a move whose old and new paths are each inside the old
// and new paths of another move, by the same relative remainder.
func dropNestedMoves(moves []Move) []Move {
	var out []Move
	for i, m := range moves {
		nested := false
		for j, outer := range moves {
			if i == j {
				continue
			}
			if strings.HasPrefix(m.OldPath, outer.OldPath+"/") && strings.HasPrefix(m.NewPath, outer.NewPath+"/") &&
				strings.TrimPrefix(m.OldPath, outer.OldPath) == strings.TrimPrefix(m.NewPath, outer.NewPath) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, m)
		}
	}
	return out
}

// Apply reverses e: moves are moved back to their original location, and
// created paths are removed. It keeps going after an individual failure so
// one bad step doesn't strand the rest of an otherwise-undoable entry, and
// returns every error encountered (nil if everything succeeded). Created
// paths are removed deepest-first (reverse lexical order) so a file inside
// a newly created directory is removed before the now-hopefully-empty
// directory itself; a directory that isn't actually empty at that point
// fails os.Remove rather than being force-deleted, since this package would
// rather report a stuck cleanup than guess wrong and delete something it
// doesn't understand.
func Apply(e Entry) []error {
	var errs []error

	// Order matters, and each step below exists because the step before it would
	// otherwise undo it:
	//  0. A created path that a capture restores is removed first (see below).
	//  1. Moves go back first. A move onto an existing name leaves the moved
	//     data sitting at the destination; restoring the destination's old
	//     content before moving it back would write the old bytes into the
	//     moved file and destroy it.
	//  2. Trash is restored next; it puts back whole paths.
	//  3. git reset --hard runs before content is restored. A hard reset
	//     discards working-tree changes, so restoring a dirty file first and
	//     resetting after would erase exactly what was just restored.
	//  4. Content and metadata are restored last, on top of the settled tree.
	//  5. Other created paths are removed once nothing needs them.
	//
	// A created path that a capture restores (the line removed a file and made one
	// of the same name) is removed before the restore, not after, or removing "what
	// the command created" would delete the restored original. A path that is still
	// the captured original, because the part that would have removed it never ran,
	// is kept.
	created := append([]string(nil), e.Created...)
	sort.Sort(sort.Reverse(sort.StringSlice(created)))
	targets := restoreTargets(e)
	var late []string
	for _, c := range created {
		abs := filepath.Join(e.Dir, c)
		if !targets.covers(abs) {
			late = append(late, c)
			continue
		}
		if targets.isOriginal(abs) {
			continue
		}
		if err := os.Remove(abs); err != nil {
			// Not yet empty, or not there until a move puts its directory back.
			late = append(late, c)
		}
	}

	// Moves go back by identity: each moved file returns to the origin of the move
	// that carried it, wherever it ended up, so the order in which the line's parts
	// ran (a pipeline's stages run at once) does not matter. What sits at a move's
	// destination that is no moved file, and was not there before, was made by the
	// line and is removed.
	byID := map[FileID]Move{}
	for _, m := range e.Moves {
		if m.ID != nil {
			byID[*m.ID] = m
		}
	}
	for i := len(e.Moves) - 1; i >= 0; i-- {
		m := e.Moves[i]
		newAbs := filepath.Join(e.Dir, m.NewPath)
		back := m
		if m.ID != nil {
			id, ok := IDOf(newAbs)
			if !ok {
				continue // nothing there: the move never happened, or its file went on
			}
			if id != *m.ID {
				other, known := byID[id]
				if !known {
					// Its data, if an original, comes back from its own capture.
					if !m.NewExisted {
						if err := os.Remove(newAbs); err != nil {
							errs = append(errs, fmt.Errorf("removing %s: %w", m.NewPath, err))
						}
					}
					continue
				}
				back = other
			}
		}
		if err := moveBack(newAbs, filepath.Join(e.Dir, back.OldPath)); err != nil {
			errs = append(errs, fmt.Errorf("moving %s back to %s: %w", m.NewPath, back.OldPath, err))
		}
	}

	for _, item := range e.Trashed {
		if err := os.MkdirAll(filepath.Dir(item.OriginalPath), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s: %w", item.OriginalPath, err))
			continue
		}
		if err := restoreTrashed(item); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s from trash: %w", item.OriginalPath, err))
		}
	}

	if e.GitReset != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result := executor.RunIn(ctx, e.Dir, "git reset --hard "+e.GitReset)
		cancel()
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("restoring git HEAD to %s: %w", e.GitReset, result.Err))
		} else if result.ExitCode != 0 {
			errs = append(errs, fmt.Errorf("restoring git HEAD to %s: exit %d: %s", e.GitReset, result.ExitCode, strings.TrimSpace(result.Stderr)))
		}
	}

	// Package and service state goes back by running the inverse command. It runs
	// after trash so that configuration files restored from trash are already in
	// place (the inverse install keeps them), and before content restore. They run
	// in the order stored, which the journal writer makes the reverse of the changes.
	for _, inv := range e.Inverses {
		if err := inverseRunner(inv); err != nil {
			errs = append(errs, fmt.Errorf("undoing package or service state failed (%w); run it yourself: %s", err, inv))
		}
	}

	// A file restored from trash is the original inode. If a path whose content is
	// about to be restored is that same inode (an earlier part of the line moved the
	// file there and a later part failed), writing in place would rewrite the
	// restored original too, so that path is unlinked and written afresh.
	restored := map[FileID]bool{}
	for _, item := range e.Trashed {
		if id, ok := IDOf(item.OriginalPath); ok {
			restored[id] = true
		}
	}
	for _, cb := range e.ContentBackups {
		if id, ok := IDOf(cb.Path); ok && restored[id] {
			if err := os.Remove(cb.Path); err != nil {
				errs = append(errs, fmt.Errorf("separating %s from a restored file: %w", cb.Path, err))
				continue
			}
		}
		if err := restoreContent(cb); err != nil {
			errs = append(errs, fmt.Errorf("restoring content of %s: %w", cb.Path, err))
		}
	}

	for _, mb := range e.MetadataBackups {
		if err := os.Chmod(mb.Path, mb.Mode); err != nil {
			errs = append(errs, fmt.Errorf("restoring mode of %s: %w", mb.Path, err))
		}
		if err := os.Chown(mb.Path, mb.UID, mb.GID); err != nil {
			errs = append(errs, fmt.Errorf("restoring ownership of %s: %w", mb.Path, err))
		}
	}

	for _, c := range late {
		abs := filepath.Join(e.Dir, c)
		if targets.isOriginal(abs) || targets.contentRestored(abs) {
			continue // what is there now is the restored original
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("removing %s: %w", c, err))
		}
	}
	return errs
}

// restoreSet is what Apply puts back: the paths captures restore, and the identity of
// each captured original that may still be in place.
type restoreSet struct {
	paths     []string
	originals map[string]FileID
	content   map[string]bool
	// held maps the identity of every file a capture holds, inside a trashed or
	// moved directory as well as at its top, to the path it belongs at. A file is an
	// original only at that path; the same file under another name (a second link
	// the line left behind) is a leftover.
	held map[FileID]string
}

func restoreTargets(e Entry) restoreSet {
	r := restoreSet{originals: map[string]FileID{}, content: map[string]bool{}, held: map[FileID]string{}}
	for _, m := range e.Moves {
		abs := filepath.Join(e.Dir, m.OldPath)
		r.paths = append(r.paths, abs)
		if m.ID != nil {
			r.originals[abs] = *m.ID
		}
	}
	for _, m := range e.Moves {
		if m.ID != nil {
			r.held[*m.ID] = filepath.Join(e.Dir, m.OldPath)
		}
	}
	for rel, id := range e.Preexisting {
		r.originals[filepath.Join(e.Dir, rel)] = id
	}
	for _, t := range e.Trashed {
		_ = filepath.WalkDir(t.TrashPath, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if id, ok := IDOf(p); ok {
					if rel, rerr := filepath.Rel(t.TrashPath, p); rerr == nil {
						r.held[id] = filepath.Join(t.OriginalPath, rel)
					}
				}
			}
			return nil
		})
		r.paths = append(r.paths, t.OriginalPath)
		if id, ok := IDOf(t.TrashPath); ok {
			if fi, err := os.Lstat(t.TrashPath); err == nil && !fi.IsDir() {
				r.originals[t.OriginalPath] = id // a file's capture is a hardlink to it
			}
		}
	}
	for _, cb := range e.ContentBackups {
		r.paths = append(r.paths, cb.Path)
		r.content[cb.Path] = true
	}
	return r
}

func (r restoreSet) contentRestored(abs string) bool { return r.content[abs] }

// covers reports whether abs is, or lies under, a path a capture restores.
func (r restoreSet) covers(abs string) bool {
	for _, p := range r.paths {
		if abs == p || strings.HasPrefix(abs, p+"/") {
			return true
		}
	}
	return false
}

// isOriginal reports whether abs is the very file a capture holds, at the path it
// belongs at.
func (r restoreSet) isOriginal(abs string) bool {
	id, ok := IDOf(abs)
	if !ok {
		return false
	}
	if want, ok := r.originals[abs]; ok && id == want {
		return true
	}
	return r.held[id] == abs
}

// moveBack moves a moved file or directory from where it is to where it was. A
// directory of the same name made again after the move is merged into, as for a
// directory restored from trash; a file made at the vacated name after the move
// (mv logs tmp/ ; echo y > logs) is replaced, since the line made it.
func moveBack(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	err := os.Rename(from, to)
	if err == nil {
		return nil
	}
	src, serr := os.Lstat(from)
	dst, derr := os.Lstat(to)
	if serr != nil || derr != nil || !src.IsDir() {
		return err
	}
	if dst.IsDir() {
		if merr := mergeTree(from, to, src.Mode().Perm()); merr != nil {
			return merr
		}
		return os.Remove(from)
	}
	if rerr := os.Remove(to); rerr != nil {
		return err
	}
	return os.Rename(from, to)
}

// restoreTrashed moves a trashed item back. When a directory of the same name exists
// again (the line made it anew, or the removal never ran), the trashed tree is merged
// into it: each entry missing from it is moved back, and the directory gets its
// original mode. An entry present in both is left as it is.
func restoreTrashed(item TrashedItem) error {
	err := os.Rename(item.TrashPath, item.OriginalPath)
	if err == nil {
		return nil
	}
	src, serr := os.Lstat(item.TrashPath)
	dst, derr := os.Lstat(item.OriginalPath)
	if serr != nil || derr != nil || !src.IsDir() || !dst.IsDir() {
		return err
	}
	return mergeTree(item.TrashPath, item.OriginalPath, src.Mode().Perm())
}

func mergeTree(from, into string, mode os.FileMode) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	var errs []error
	for _, en := range entries {
		s, d := filepath.Join(from, en.Name()), filepath.Join(into, en.Name())
		di, derr := os.Lstat(d)
		switch {
		case os.IsNotExist(derr):
			if err := os.Rename(s, d); err != nil {
				errs = append(errs, err)
			}
		case derr == nil && en.IsDir() && di.IsDir():
			if si, err := os.Lstat(s); err == nil {
				if err := mergeTree(s, d, si.Mode().Perm()); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	if err := os.Chmod(into, mode); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// inverseRunner is how Apply runs an inverse; a variable so tests can observe
// and order-check the calls without executing a package manager.
var inverseRunner = runInverse

// restoreContent puts a file's bytes and permission bits back. If the command replaced
// the file with a symlink (ln -sf), writing through it would overwrite the link's
// target and leave the link in place, so the link is removed first. The mode is set
// explicitly afterwards, because a command that recreated the file (git checkout,
// install) gave it a mode of its own and opening an existing file keeps that mode.
func restoreContent(cb ContentBackup) error {
	if cb.LinkText != "" {
		return restoreThroughLink(cb)
	}
	if fi, err := os.Lstat(cb.Path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(cb.Path); err != nil {
			return fmt.Errorf("removing the link that replaced %s: %w", cb.Path, err)
		}
	}
	if err := copyFile(cb.BackupPath, cb.Path, cb.Mode); err != nil {
		return err
	}
	if err := os.Chmod(cb.Path, cb.Mode); err != nil {
		return fmt.Errorf("restoring mode of %s: %w", cb.Path, err)
	}
	return nil
}

// restoreThroughLink restores a file that was reached through a symlink: the
// target gets its old content back, and the path becomes that same link again
// if the command replaced it.
func restoreThroughLink(cb ContentBackup) error {
	if err := os.MkdirAll(filepath.Dir(cb.LinkTarget), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(cb.LinkTarget); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(cb.LinkTarget); err != nil {
			return fmt.Errorf("removing the link that replaced %s: %w", cb.LinkTarget, err)
		}
	}
	if err := copyFile(cb.BackupPath, cb.LinkTarget, cb.Mode); err != nil {
		return err
	}
	if err := os.Chmod(cb.LinkTarget, cb.Mode); err != nil {
		return fmt.Errorf("restoring mode of %s: %w", cb.LinkTarget, err)
	}
	if text, err := os.Readlink(cb.Path); err == nil && text == cb.LinkText {
		return nil // the link is still there
	}
	if err := os.Remove(cb.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing what replaced the link %s: %w", cb.Path, err)
	}
	if err := os.Symlink(cb.LinkText, cb.Path); err != nil {
		return fmt.Errorf("restoring the link %s: %w", cb.Path, err)
	}
	return nil
}

// runInverse runs one inverse command attached to the terminal, so a privilege
// prompt can be answered. Time-limited so a wedged package manager cannot hang undo.
func runInverse(inv Inverse) error {
	if len(inv.Argv) == 0 {
		return errors.New("empty command")
	}
	argv := inv.Argv
	if inv.Sudo && os.Geteuid() != 0 {
		argv = append([]string{"sudo"}, argv...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// DefaultJournalPath is ~/.synapse/undo.log, creating the parent directory
// if needed.
func DefaultJournalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	dir := filepath.Join(home, ".synapse")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return filepath.Join(dir, "undo.log"), nil
}

// DefaultContentBackupDir is ~/.synapse/content-backups, creating it if
// needed — where BackupContent saves the full-content copies ContentBackup
// entries point at.
func DefaultContentBackupDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	dir := filepath.Join(home, ".synapse", "content-backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

// BackupContent copies each existing regular file in paths into
// DefaultContentBackupDir, returning one ContentBackup per file that
// existed and was copied successfully. A path that doesn't exist as a
// regular file (classifier.ContentMutationTargets's own heuristic can
// return a false positive — a script argument mistaken for a filename, or
// a genuinely nonexistent target) is skipped, not an error: there's simply
// nothing to protect. A real copy failure (permission denied, disk full)
// is collected and returned alongside whatever backups did succeed, rather
// than aborting the whole batch — callers should still journal the
// backups that succeeded rather than lose them over one failure.
//
// Call this immediately before running a confirmed content-mutating
// command, using classifier.ContentMutationTargets to get paths — the
// backup has to happen before execution, since afterward the original
// content is exactly what's gone.
func BackupContent(paths []string) ([]ContentBackup, []error) {
	backupDir, err := DefaultContentBackupDir()
	if err != nil {
		return nil, []error{err}
	}

	var backups []ContentBackup
	var errs []error
	for i, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue // nothing to protect: not a real file (or not there at all)
		}
		cb := ContentBackup{Path: path, Mode: info.Mode()}
		if li, err := os.Lstat(path); err == nil && li.Mode()&os.ModeSymlink != 0 {
			text, terr := os.Readlink(path)
			target, rerr := filepath.EvalSymlinks(path)
			if terr != nil || rerr != nil {
				errs = append(errs, fmt.Errorf("backing up %s: resolving the link: %v %v", path, terr, rerr))
				continue
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			cb.LinkText, cb.LinkTarget = text, target
		}

		cb.BackupPath = filepath.Join(backupDir, fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), i, filepath.Base(path)))
		if err := copyFile(path, cb.BackupPath, info.Mode()); err != nil {
			errs = append(errs, fmt.Errorf("backing up %s: %w", path, err))
			continue
		}
		backups = append(backups, cb)
	}
	return backups, errs
}

// DefaultTrashDir is ~/.synapse/trash, creating it if needed — where
// TrashPreserve hardlinks (or, on a cross-device fallback, copies) the
// items ContentBackup's ContentBackups counterpart, TrashedItem, points at.
func DefaultTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	dir := filepath.Join(home, ".synapse", "trash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	return dir, nil
}

// TrashPreserve preserves each existing path (file or directory tree) by
// hardlinking it into DefaultTrashDir immediately before a confirmed
// deletion runs, returning one TrashedItem per path preserved
// successfully. A nonexistent path is skipped, not an error — nothing to
// protect, same convention as BackupContent.
//
// This is deliberately not a copy: hardlinking a regular file, or
// recreating a directory's structure while hardlinking every regular file
// inside it, shares the same underlying data rather than duplicating it,
// so the cost is independent of how much data the path contains — a
// directory holding gigabytes costs the same to preserve as an empty one.
// This is only correct for commands that remove a directory entry without
// touching the target's data (rm, at the syscall level) — see the package
// doc comment for why this must never be used for a command that mutates a
// file's content in place (that path stays on BackupContent instead).
//
// A path on a different filesystem than the trash directory can't be
// hardlinked (EXDEV) — falls back to an actual copy for that path only,
// the one case where preservation genuinely costs proportional to size.
func TrashPreserve(paths []string) ([]TrashedItem, []error) {
	trashDir, err := DefaultTrashDir()
	if err != nil {
		return nil, []error{err}
	}

	var items []TrashedItem
	var errs []error
	for i, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			continue // nothing to protect: not there
		}

		trashPath := filepath.Join(trashDir, fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), i, filepath.Base(path)))
		if err := preserveViaHardlink(path, trashPath, info); err != nil {
			errs = append(errs, fmt.Errorf("preserving %s: %w", path, err))
			continue
		}
		items = append(items, TrashedItem{OriginalPath: path, TrashPath: trashPath})
	}
	return items, errs
}

// BackupMetadata walks each directory tree in dirs — dirs themselves
// included — recording every entry's current mode and ownership
// immediately before a confirmed recursive chmod/chown runs over it (see
// classifier.RecursivePermissionTargets). Only metadata is recorded, never
// content, so the cost is a few bytes per file regardless of the file's
// own size. A walk error partway through one dir is reported but doesn't
// stop the others in dirs from being attempted; whatever entries were
// already recorded before the failure are still returned, since a partial
// backup is strictly better than none.
func BackupMetadata(dirs []string) ([]MetadataBackup, []error) {
	var backups []MetadataBackup
	var errs []error
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("could not read ownership metadata for %s", path)
			}
			backups = append(backups, MetadataBackup{Path: path, Mode: info.Mode(), UID: int(stat.Uid), GID: int(stat.Gid)})
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("backing up permissions under %s: %w", dir, err))
		}
	}
	return backups, errs
}

// BackupMetadataPaths records the mode and ownership of exactly the paths given,
// without walking into them. BackupMetadata walks each tree, which is right when
// the caller names a directory and lets chmod -R reach everything under it; a
// caller that has already listed every affected path (internal/gate) would pay
// for the same walk once per path.
func BackupMetadataPaths(paths []string) ([]MetadataBackup, []error) {
	var backups []MetadataBackup
	var errs []error
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			continue // not there: nothing to protect
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			errs = append(errs, fmt.Errorf("could not read ownership metadata for %s", p))
			continue
		}
		backups = append(backups, MetadataBackup{Path: p, Mode: info.Mode(), UID: int(stat.Uid), GID: int(stat.Gid)})
	}
	return backups, errs
}

// CaptureGitHead returns the commit HEAD currently points at in the git
// repository at dir, for a caller to record on Entry.GitReset immediately
// before a confirmed git reset --hard runs there. Apply restores it via
// `git reset --hard <sha>` — git already has its own object store, so a
// single commit hash is a complete, correct snapshot with no file-level
// backup needed at all.
func CaptureGitHead(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result := executor.RunIn(ctx, dir, "git rev-parse HEAD")
	if result.Err != nil {
		return "", result.Err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse HEAD exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return strings.TrimSpace(result.Stdout), nil
}

// preserveViaHardlink hardlinks src to dst if src is a regular file, or
// recreates src's directory structure at dst while hardlinking every
// regular file inside it. Falls back to a real copy per-file only on
// EXDEV (src and dst on different filesystems).
func preserveViaHardlink(src, dst string, info os.FileInfo) error {
	if info.IsDir() {
		return hardlinkTree(src, dst)
	}
	if err := linkFunc(src, dst); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return copyFile(src, dst, info.Mode())
		}
		return err
	}
	return nil
}

// linkFunc is os.Link, indirected so tests can simulate the EXDEV
// cross-device case without needing two real filesystems — a genuine
// EXDEV isn't reproducible inside a single-filesystem test sandbox, but
// the fallback behavior it triggers still needs real coverage.
var linkFunc = os.Link

// hardlinkTree walks srcDir, recreating its directory structure at dstDir
// and hardlinking each regular file it contains rather than copying data.
// isTrashRoot reports whether dir is synapse's trash directory.
func isTrashRoot(dir string) bool {
	home, err := os.UserHomeDir()
	return err == nil && filepath.Clean(dir) == filepath.Join(home, ".synapse", "trash")
}

func hardlinkTree(srcDir, dstDir string) error {
	// Directory modes are applied after the walk, deepest first: a directory
	// captured as read-only could not otherwise receive its own children, and
	// a hardlink shares its file's mode but a new directory would come back as
	// 0755 whatever it was.
	type dirMode struct {
		path string
		mode os.FileMode
	}
	var dirs []dirMode
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		// The destination itself, when the tree being kept contains the
		// trash (`rm -rf ~/.synapse`), is skipped: walking into it would
		// copy the copy into itself until the path grew too long (finding
		// r2-atk5-06).
		if d.IsDir() && (path == dstDir || isTrashRoot(path)) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			dirs = append(dirs, dirMode{target, info.Mode().Perm()})
			return os.MkdirAll(target, 0o755)
		}
		if err := linkFunc(path, target); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				info, statErr := d.Info()
				if statErr != nil {
					return statErr
				}
				return copyFile(path, target, info.Mode())
			}
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if cerr := os.Chmod(dirs[i].path, dirs[i].mode); cerr != nil {
			return cerr
		}
	}
	return nil
}

// copyFile copies src to dst, creating dst with the given mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}

// The journal is shared by every synapse process of one user, so each change
// to it holds an exclusive lock on a sibling file (path + ".lock") for its
// whole read-modify-write. The lock lives beside the journal, not on it,
// because a pop replaces the journal file and a lock on the old file would
// not cover the new one. A rewrite goes to a temporary file that is synced
// and renamed into place, so a crash leaves the old journal or the new one,
// never a half-written one. A line that does not parse (an append cut off by
// a crash, or damage) is kept as it is and skipped, so one bad line cannot
// disable every undo.

// ErrJournalChanged is returned by PopLastJournalIf when the newest entry is
// no longer the one the caller showed the person, because another process
// appended or undid something in between.
var ErrJournalChanged = errors.New("the undo journal changed while the undo was being confirmed")

// withJournalLock runs fn holding the journal's exclusive lock.
func withJournalLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("locking undo journal %s: %w", path, err)
	}
	l, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("locking undo journal %s: %w", path, err)
	}
	defer l.Close()
	if err := syscall.Flock(int(l.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking undo journal %s: %w", path, err)
	}
	defer syscall.Flock(int(l.Fd()), syscall.LOCK_UN)
	return fn()
}

// JournalWritable reports whether an entry could be appended to the journal
// at path now: its lock and the journal itself open for writing. It creates
// them if absent and writes nothing.
func JournalWritable(path string) error {
	return withJournalLock(path, func() error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening undo journal %s: %w", path, err)
		}
		return f.Close()
	})
}

// AppendJournal appends e to the journal at path as one JSON line.
func AppendJournal(path string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding undo entry: %w", err)
	}
	return withJournalLock(path, func() error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return fmt.Errorf("opening undo journal %s: %w", path, err)
		}
		// A line cut off by a crash has no newline; without one written first,
		// this entry would be glued onto it and lost with it (finding atk4-09).
		line := append(b, '\n')
		if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
			last := make([]byte, 1)
			if _, err := f.ReadAt(last, fi.Size()-1); err == nil && last[0] != '\n' {
				line = append([]byte{'\n'}, line...)
			}
		}
		if _, err := f.Write(line); err != nil {
			f.Close()
			return fmt.Errorf("writing undo journal %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("writing undo journal %s: %w", path, err)
		}
		return nil
	})
}

// PeekLastJournal returns the most recent entry in the journal at path
// without removing it — for showing the user what an undo would do before
// they've confirmed it. ok is false (with a zero Entry) if the journal
// doesn't exist or holds no readable entry.
func PeekLastJournal(path string) (entry Entry, ok bool, err error) {
	lines, err := readJournal(path)
	if err != nil {
		return Entry{}, false, err
	}
	if i := lastEntry(lines); i >= 0 {
		return *lines[i].entry, true, nil
	}
	return Entry{}, false, nil
}

// UnreadableJournalLines counts the journal lines that do not parse as an
// entry and are therefore skipped, so a caller can say so.
func UnreadableJournalLines(path string) (int, error) {
	lines, err := readJournal(path)
	n := 0
	for _, l := range lines {
		if l.entry == nil {
			n++
		}
	}
	return n, err
}

// PopLastJournal removes the most recent entry from the journal at path and
// returns it. ok is false (with a zero Entry) if there is none. Callers that
// show the entry before removing it should use PopLastJournalIf, so that an
// entry another process added in the meantime is not removed unapplied.
func PopLastJournal(path string) (entry Entry, ok bool, err error) {
	err = withJournalLock(path, func() error {
		var e error
		entry, ok, e = popLocked(path, nil)
		return e
	})
	return entry, ok, err
}

// PopLastJournalIf removes the most recent entry only if it is still want,
// the entry the caller peeked and showed. Otherwise it removes nothing and
// returns ErrJournalChanged.
func PopLastJournalIf(path string, want Entry) error {
	return withJournalLock(path, func() error {
		_, ok, err := popLocked(path, &want)
		if err == nil && !ok {
			return ErrJournalChanged
		}
		return err
	})
}

// popLocked removes the last readable entry, if it equals want when want is
// set, keeping every other line, readable or not, byte for byte. The caller
// holds the lock.
func popLocked(path string, want *Entry) (Entry, bool, error) {
	lines, err := readJournal(path)
	if err != nil {
		return Entry{}, false, err
	}
	i := lastEntry(lines)
	if i < 0 {
		return Entry{}, false, nil
	}
	if want != nil {
		a, _ := json.Marshal(lines[i].entry)
		b, _ := json.Marshal(want)
		if !bytes.Equal(a, b) {
			return Entry{}, false, nil
		}
	}
	popped := *lines[i].entry
	tmp, err := os.CreateTemp(filepath.Dir(path), ".undo-journal-*")
	if err != nil {
		return Entry{}, false, fmt.Errorf("rewriting undo journal %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	w := bufio.NewWriter(tmp)
	for j, l := range lines {
		if j == i {
			continue
		}
		w.Write(l.raw)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("rewriting undo journal %s: %w", path, err)
	}
	return popped, true, nil
}

// journalLine is one line of the journal: its bytes, and its entry when it
// parses.
type journalLine struct {
	raw   []byte
	entry *Entry
}

func lastEntry(lines []journalLine) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].entry != nil {
			return i
		}
	}
	return -1
}

// readJournal reads every non-empty line. A line of any length is read whole;
// one that does not parse is returned with a nil entry.
func readJournal(path string) ([]journalLine, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading undo journal %s: %w", path, err)
	}
	defer f.Close()

	var lines []journalLine
	r := bufio.NewReader(f)
	for {
		raw, rerr := r.ReadBytes('\n')
		raw = bytes.TrimRight(raw, "\n")
		if len(bytes.TrimSpace(raw)) > 0 {
			l := journalLine{raw: raw}
			var e Entry
			if json.Unmarshal(raw, &e) == nil {
				l.entry = &e
			}
			lines = append(lines, l)
		}
		if rerr == io.EOF {
			return lines, nil
		}
		if rerr != nil {
			return nil, fmt.Errorf("reading undo journal %s: %w", path, rerr)
		}
	}
}
