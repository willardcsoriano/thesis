// Package oracle produces ground truth for the recoverability corpus by running
// each command in a sandbox on a fixture and reading what changed.
//
// The label of a command is a fact about one command on one state, taken from the
// filesystem before and after the run, not a judgement about the command. It is
// independent of internal/effects by construction: it shares no code with the
// analysis and never consults it, so agreement between the two is evidence.
//
// What it can see is the observable filesystem effect inside the fixture. What it
// cannot see (a remote host, a mount, a signal, the clipboard) is handled by the
// caller as an "external" partition that is never executed.
package oracle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

// Entry is one thing in a fixture. Kind is "file", "dir" or "link".
type Entry struct {
	Path   string `json:"p"`
	Kind   string `json:"k"`
	Data   string `json:"d,omitempty"`
	Target string `json:"t,omitempty"`
	Mode   uint32 `json:"m,omitempty"`
}

// Materialize creates the fixture under root. Parents are created as needed.
func Materialize(root string, fx []Entry) error {
	for _, e := range fx {
		full := filepath.Join(root, e.Path)
		switch e.Kind {
		case "dir":
			if err := os.MkdirAll(full, 0o755); err != nil {
				return err
			}
		case "link":
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(e.Target, full); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			mode := fs.FileMode(0o644)
			if e.Mode != 0 {
				mode = fs.FileMode(e.Mode)
			}
			if err := os.WriteFile(full, []byte(e.Data), mode); err != nil {
				return err
			}
			if err := os.Chmod(full, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

// Node is what is observed at one path.
type Node struct {
	Kind     byte // 'f' file, 'd' dir, 'l' link, 'o' other
	Hash     string
	Target   string
	Perm     fs.FileMode
	UID, GID uint32
	Dev, Ino uint64
}

// Snapshot maps a path relative to the fixture root to what is there.
type Snapshot map[string]Node

// Observe walks root and records every path. Modification times are deliberately
// not recorded: touching a file changes nothing a user could lose.
func Observe(root string) (Snapshot, error) {
	s := Snapshot{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // vanished or unreadable mid-walk; the diff will show it
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir // repository internals are not user files; the working tree is
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return nil
		}
		n := Node{Perm: fi.Mode().Perm() | fi.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			n.UID, n.GID, n.Dev, n.Ino = st.Uid, st.Gid, uint64(st.Dev), st.Ino
		}
		switch m := fi.Mode(); {
		case m&fs.ModeSymlink != 0:
			n.Kind = 'l'
			n.Target, _ = os.Readlink(p)
		case m.IsDir():
			n.Kind = 'd'
		case m.IsRegular():
			n.Kind = 'f'
			n.Hash = hashFile(p)
		default:
			n.Kind = 'o'
		}
		s[rel] = n
		return nil
	})
	return s, err
}

func hashFile(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return "?"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "?"
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Verdict is the ground-truth label of one run.
type Verdict struct {
	// Label is "R" when nothing that existed before was lost or altered (paths may
	// have been created or moved), and "C" when something was removed, overwritten,
	// or re-moded and so needs a pre-image to come back.
	Label   string
	Reason  string
	Changed []string // paths that were lost or altered, sorted
}

// Label compares the state before and after a run. A path that disappeared but whose
// very same inode now sits at another path was moved, not lost. Everything else
// that disappeared, or whose bytes, kind, link target, permission bits or owner
// changed, is a loss.
func Label(before, after Snapshot) Verdict {
	type key struct{ dev, ino uint64 }
	now := map[key]bool{}
	for p, n := range after {
		if _, existed := before[p]; !existed {
			now[key{n.Dev, n.Ino}] = true
		}
	}
	var changed []string
	reason := ""
	note := func(why string) {
		if reason == "" {
			reason = why
		}
	}
	for p, b := range before {
		a, ok := after[p]
		switch {
		case !ok:
			if now[key{b.Dev, b.Ino}] {
				continue // moved
			}
			changed = append(changed, p)
			note("removed " + p)
		case a.Kind != b.Kind:
			changed = append(changed, p)
			note("replaced " + p)
		case a.Kind == 'f' && a.Hash != b.Hash:
			changed = append(changed, p)
			note("rewrote " + p)
		case a.Kind == 'l' && a.Target != b.Target:
			changed = append(changed, p)
			note("retargeted " + p)
		case a.Perm != b.Perm || a.UID != b.UID || a.GID != b.GID:
			changed = append(changed, p)
			note("changed mode or owner of " + p)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		return Verdict{Label: "R", Reason: "nothing that existed was lost"}
	}
	return Verdict{Label: "C", Reason: reason, Changed: changed}
}

// Outcome is how a run ended. Only a clean exit yields a label.
type Outcome struct {
	ExitCode int
	TimedOut bool
	Stderr   string
}

// Clean reports whether the run finished normally with exit status 0.
func (o Outcome) Clean() bool { return !o.TimedOut && o.ExitCode == 0 }

// Bwrap is the sandbox binary; empty means unavailable.
func Bwrap() string {
	p, err := exec.LookPath("bwrap")
	if err != nil {
		return ""
	}
	if exec.Command(p, "--ro-bind", "/", "/", "--unshare-all", "--die-with-parent", "true").Run() != nil {
		return ""
	}
	return p
}

// sudoShim makes privilege prefixes transparent: the fixture is owned by the
// current user, so what the command does to it is the same with or without root.
const sudoShim = `sudo() { while [ $# -gt 0 ]; do case "$1" in -u|-g|-h|-p|-C|-D|-r|-t|-T|-U) shift 2;; -*) shift;; *) break;; esac; done; "$@"; }; doas() { sudo "$@"; }; `

// Execute runs cmd with root as its working directory under bubblewrap: the rest
// of the filesystem read-only, no network, no other namespaces shared, standard
// input empty, and a hard time limit. Only root is writable. Where a command
// writes elsewhere it fails, and the caller excludes it rather than labelling it.
func Execute(ctx context.Context, bwrap, root, cmd string, limit time.Duration) (Outcome, error) {
	if bwrap == "" {
		return Outcome{}, errors.New("bubblewrap is required to run the oracle")
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Outcome{}, err
	}
	script := "ulimit -v 4000000 -f 200000 2>/dev/null; " + sudoShim + cmd
	c := exec.CommandContext(ctx, bwrap,
		"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
		"--bind", root, root, "--unshare-all", "--die-with-parent",
		"--chdir", root, "--setenv", "HOME", home, "--setenv", "PWD", root,
		"--", "bash", "-c", script)
	var errBuf limited
	c.Stderr = &errBuf
	c.Stdout = io.Discard
	c.Stdin = nil
	err := c.Run()
	out := Outcome{Stderr: strings.TrimSpace(errBuf.String())}
	if ctx.Err() != nil {
		out.TimedOut = true
		return out, nil
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		out.ExitCode = ee.ExitCode()
	default:
		return out, fmt.Errorf("running the sandbox: %w", err)
	}
	return out, nil
}

type limited struct{ b []byte }

func (l *limited) Write(p []byte) (int, error) {
	if len(l.b) < 4096 {
		l.b = append(l.b, p...)
	}
	return len(p), nil
}
func (l *limited) String() string { return string(l.b) }

// Run materializes fx in a fresh directory, runs cmd, and labels the result.
// setup, when set, runs first on the host in the fixture (used to create a git
// repository). The directory is removed afterwards.
func Run(ctx context.Context, bwrap string, fx []Entry, setup, cmd string, limit time.Duration) (Verdict, Outcome, error) {
	root, err := os.MkdirTemp("", "oracle-")
	if err != nil {
		return Verdict{}, Outcome{}, err
	}
	defer os.RemoveAll(root)
	if err := Materialize(root, fx); err != nil {
		return Verdict{}, Outcome{}, err
	}
	if setup != "" {
		sc := exec.CommandContext(ctx, "bash", "-c", setup)
		sc.Dir = root
		if out, err := sc.CombinedOutput(); err != nil {
			return Verdict{}, Outcome{}, fmt.Errorf("fixture setup: %v: %s", err, out)
		}
	}
	before, err := Observe(root)
	if err != nil {
		return Verdict{}, Outcome{}, err
	}
	oc, err := Execute(ctx, bwrap, root, cmd, limit)
	if err != nil || !oc.Clean() {
		return Verdict{}, oc, err
	}
	after, err := Observe(root)
	if err != nil {
		return Verdict{}, oc, err
	}
	return Label(before, after), oc, nil
}
