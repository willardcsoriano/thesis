// Package recoverycheck holds the tree snapshot and diff used by cmd/recoverycheck
// to decide whether an undo restored a directory exactly.
//
// Two things are deliberately not compared. Modification times are not restored by
// any undo mechanism in this project, so comparing them would fail every case for
// a reason that is not what is being tested. And inside .git only HEAD and refs/
// are compared: git reset rewrites the index, ORIG_HEAD and the reflog, none of
// which an undo that resets HEAD back can be expected to reproduce byte for byte,
// while HEAD and the refs are what "the commit history is back" means.
package recoverycheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Node is what is recorded about one path.
type Node struct {
	Type string // "file", "dir", "symlink", "other"
	Mode fs.FileMode
	Size int64
	Sum  string // sha256 of a regular file's bytes, or "unreadable: ..."
	Link string // symlink target
}

// Tree maps a path relative to the root to its Node.
type Tree map[string]Node

func skipGit(rel string, isDir bool) (skip, prune bool) {
	if !strings.HasPrefix(rel, ".git/") {
		return false, false
	}
	if rel == ".git/HEAD" || rel == ".git/refs" || strings.HasPrefix(rel, ".git/refs/") {
		return false, false
	}
	return true, isDir
}

// Snapshot records every path under root, hidden files included.
func Snapshot(root string) (Tree, error) {
	t := Tree{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip, prune := skipGit(rel, d.IsDir()); skip {
			if prune {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		n := Node{Mode: fi.Mode()}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			n.Type = "symlink"
			n.Link, _ = os.Readlink(p)
		case fi.IsDir():
			n.Type = "dir"
		case fi.Mode().IsRegular():
			n.Type = "file"
			n.Size = fi.Size()
			n.Sum = sumFile(p)
		default:
			n.Type = "other"
		}
		t[rel] = n
		return nil
	})
	return t, err
}

func sumFile(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unreadable: " + err.Error()
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Diff lists every difference between want and got, sorted, empty when equal.
func Diff(want, got Tree) []string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []string
	for _, k := range names {
		w, wok := want[k]
		g, gok := got[k]
		switch {
		case wok && !gok:
			out = append(out, "missing "+k)
		case !wok && gok:
			out = append(out, "extra "+k)
		case w.Type != g.Type:
			out = append(out, fmt.Sprintf("type %s: %s -> %s", k, w.Type, g.Type))
		case w.Mode != g.Mode:
			out = append(out, fmt.Sprintf("mode %s: %v -> %v", k, w.Mode, g.Mode))
		case w.Type == "file" && w.Sum != g.Sum:
			out = append(out, "content "+k)
		case w.Type == "symlink" && w.Link != g.Link:
			out = append(out, fmt.Sprintf("link %s: %s -> %s", k, w.Link, g.Link))
		}
	}
	return out
}

// Bytes is the total size of the regular files in t: the cost of copying the tree.
func Bytes(t Tree) int64 {
	var n int64
	for _, v := range t {
		if v.Type == "file" {
			n += v.Size
		}
	}
	return n
}
