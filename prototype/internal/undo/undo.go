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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
}

// IsNoop reports whether e has nothing to undo — no filesystem effect was
// observed (or none this package could account for). Callers should not
// journal a no-op entry.
func (e Entry) IsNoop() bool {
	return len(e.Moves) == 0 && len(e.Created) == 0 && len(e.Unhandled) == 0 &&
		len(e.ContentBackups) == 0 && len(e.Trashed) == 0 &&
		e.GitReset == "" && len(e.MetadataBackups) == 0
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

// BuildEntry diffs before (snapshotted just before cmd ran) against after
// (snapshotted just after) and pairs disappeared/appeared paths by basename
// to reconstruct Moves, Created, and Unhandled.
func BuildEntry(dir, cmd string, before, after map[string]bool) Entry {
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
	for _, p := range appeared {
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
	for _, cb := range e.ContentBackups {
		if err := copyFile(cb.BackupPath, cb.Path, cb.Mode); err != nil {
			errs = append(errs, fmt.Errorf("restoring content of %s: %w", cb.Path, err))
		}
	}

	for _, item := range e.Trashed {
		if err := os.MkdirAll(filepath.Dir(item.OriginalPath), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s: %w", item.OriginalPath, err))
			continue
		}
		if err := os.Rename(item.TrashPath, item.OriginalPath); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s from trash: %w", item.OriginalPath, err))
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

	for _, m := range e.Moves {
		oldAbs := filepath.Join(e.Dir, m.OldPath)
		newAbs := filepath.Join(e.Dir, m.NewPath)
		if err := os.MkdirAll(filepath.Dir(oldAbs), 0o755); err != nil {
			errs = append(errs, fmt.Errorf("restoring %s: %w", m.OldPath, err))
			continue
		}
		if err := os.Rename(newAbs, oldAbs); err != nil {
			errs = append(errs, fmt.Errorf("moving %s back to %s: %w", m.NewPath, m.OldPath, err))
		}
	}

	created := append([]string(nil), e.Created...)
	sort.Sort(sort.Reverse(sort.StringSlice(created)))
	for _, c := range created {
		if err := os.Remove(filepath.Join(e.Dir, c)); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", c, err))
		}
	}
	return errs
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

		backupPath := filepath.Join(backupDir, fmt.Sprintf("%d-%d-%s", time.Now().UnixNano(), i, filepath.Base(path)))
		if err := copyFile(path, backupPath, info.Mode()); err != nil {
			errs = append(errs, fmt.Errorf("backing up %s: %w", path, err))
			continue
		}
		backups = append(backups, ContentBackup{Path: path, BackupPath: backupPath, Mode: info.Mode()})
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
func hardlinkTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
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

// AppendJournal appends e to the journal at path as one JSON line.
func AppendJournal(path string, e Entry) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening undo journal %s: %w", path, err)
	}
	defer f.Close()

	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding undo entry: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("writing undo journal %s: %w", path, err)
	}
	return nil
}

// PeekLastJournal returns the most recent entry in the journal at path
// without removing it — for showing the user what an undo would do before
// they've confirmed it. ok is false (with a zero Entry) if the journal
// doesn't exist or is empty.
func PeekLastJournal(path string) (entry Entry, ok bool, err error) {
	entries, err := readJournal(path)
	if err != nil {
		return Entry{}, false, err
	}
	if len(entries) == 0 {
		return Entry{}, false, nil
	}
	return entries[len(entries)-1], true, nil
}

// PopLastJournal reads all entries from the journal at path, removes the
// last one, rewrites the file without it, and returns the popped entry. ok
// is false (with a zero Entry) if the journal doesn't exist or is empty.
// Callers that need to show the entry before committing to removing it
// should call PeekLastJournal first and only Pop once the action is
// confirmed.
func PopLastJournal(path string) (entry Entry, ok bool, err error) {
	entries, err := readJournal(path)
	if err != nil {
		return Entry{}, false, err
	}
	if len(entries) == 0 {
		return Entry{}, false, nil
	}

	last := entries[len(entries)-1]
	remaining := entries[:len(entries)-1]

	f, err := os.Create(path)
	if err != nil {
		return Entry{}, false, fmt.Errorf("rewriting undo journal %s: %w", path, err)
	}
	defer f.Close()
	for _, e := range remaining {
		b, err := json.Marshal(e)
		if err != nil {
			return Entry{}, false, fmt.Errorf("encoding undo entry: %w", err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return Entry{}, false, fmt.Errorf("rewriting undo journal %s: %w", path, err)
		}
	}
	return last, true, nil
}

func readJournal(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading undo journal %s: %w", path, err)
	}
	defer f.Close()

	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("parsing undo journal %s: %w", path, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading undo journal %s: %w", path, err)
	}
	return entries, nil
}
