// Package undo is a best-effort, disk-persisted safety net for CLI mode's
// auto-run (reversible-classified) commands.
//
// It does not parse bash syntax and does not attempt to reconstruct deleted
// file *content* — irreversible-classified commands (rm, dd, sed -i, ...)
// are already gated by internal/classifier and stay out of scope here by
// design; this package only has to undo things that were never destructive
// to begin with, just inconveniently placed. It works by snapshotting the
// command's working directory before and after execution, diffing what
// appeared vs. disappeared, and pairing disappeared/appeared paths by
// basename to reconstruct moves. A path that appears with no matching
// disappeared basename is treated as pure creation (safe to remove on
// undo); a path that disappears with no matching appeared basename can't be
// safely reconstructed without a content backup this package doesn't keep,
// so it's recorded but left unhandled rather than guessed at.
package undo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Move is one file or directory this package believes moved from OldPath to
// NewPath, both relative to the Entry's Dir.
type Move struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// Entry records one reversible command's filesystem effect, well enough to
// reverse it. Dir is the absolute directory the command ran in; Moves,
// Created, and Unhandled paths are all relative to it.
type Entry struct {
	Timestamp time.Time `json:"timestamp"`
	Command   string    `json:"command"`
	Dir       string    `json:"dir"`
	Moves     []Move    `json:"moves,omitempty"`
	Created   []string  `json:"created,omitempty"`
	Unhandled []string  `json:"unhandled,omitempty"`
}

// IsNoop reports whether e has nothing to undo — no filesystem effect was
// observed (or none this package could account for). Callers should not
// journal a no-op entry.
func (e Entry) IsNoop() bool {
	return len(e.Moves) == 0 && len(e.Created) == 0 && len(e.Unhandled) == 0
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
