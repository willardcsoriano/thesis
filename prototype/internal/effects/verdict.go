package effects

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Class is the recoverability verdict.
type Class int

const (
	// Recoverable: no lasting loss; the undo is removing or moving back what was made.
	Recoverable Class = iota
	// RecoverableWithCapture: recoverable only if a pre-image is captured first.
	RecoverableWithCapture
	// Unrecoverable: no capture of files can restore it, or it could not be determined.
	Unrecoverable
)

func (c Class) String() string {
	return [...]string{"recoverable", "recoverable-with-capture", "unrecoverable"}[c]
}

type Verdict struct {
	Class   Class
	Reasons []string
}

// Verdict decides recoverability over the effect set. Anything the analysis could
// not resolve makes the verdict unrecoverable: over-approximating destructiveness
// costs a confirmation, under-approximating it costs data.
func (a *Analysis) Verdict() Verdict {
	v := Verdict{Class: Recoverable}
	raise := func(c Class, why string) {
		if c > v.Class {
			v.Class = c
		}
		v.Reasons = append(v.Reasons, why)
	}
	for _, is := range a.Issues {
		raise(Unrecoverable, fmt.Sprintf("%s: %s", is.Kind, is.Reason))
	}
	for _, e := range a.Effects {
		switch e.Kind {
		case Read, Create:
		case Remove:
			if e.MovedTo != "" {
				continue
			}
			raise(captureClass(e), fmt.Sprintf("remove %s", e.Path))
		case Write:
			raise(captureClass(e), fmt.Sprintf("overwrite %s", e.Path))
		case Meta:
			raise(captureClass(e), fmt.Sprintf("change metadata of %s", e.Path))
		}
	}
	for _, sc := range a.States {
		if len(sc.Inverse) > 0 {
			raise(RecoverableWithCapture, sc.Change+" (undo: "+renderInverse(sc)+")")
		}
	}
	return v
}

func renderInverse(sc StateChange) string {
	var parts []string
	for _, argv := range sc.Inverse {
		parts = append(parts, strings.Join(argv, " "))
	}
	return strings.Join(parts, " && ")
}

// captureClass is RecoverableWithCapture when the target can be captured, and
// Unrecoverable when it cannot (a device, socket, or pipe has no file pre-image).
func captureClass(e Effect) Class {
	if e.Hint != "" {
		return RecoverableWithCapture
	}
	fi, err := os.Lstat(e.Path)
	if err != nil {
		return RecoverableWithCapture // absent now; nothing to lose, harmless to plan
	}
	m := fi.Mode()
	if m.IsRegular() || m.IsDir() || m&os.ModeSymlink != 0 {
		return RecoverableWithCapture
	}
	return Unrecoverable
}

// Mechanism is a capture mechanism. Each maps to an existing internal/undo one.
type Mechanism string

const (
	Trash    Mechanism = "trash"    // hardlink into trash: cost independent of size
	Content  Mechanism = "content"  // copy of the file's bytes
	Metadata Mechanism = "metadata" // mode/owner record
	GitHead  Mechanism = "git-head" // one commit id
	Inverse  Mechanism = "inverse"  // a command that puts package or service state back
)

type Capture struct {
	Mechanism Mechanism
	Path      string
	Bytes     int64 // bytes copied, for Content only
	// Inverse and Sudo are set for the Inverse mechanism, with Path holding the subject.
	Inverse [][]string
	Sudo    bool
}

// Plan is a minimum-cost cover of the capturable effects.
type Plan struct {
	Captures   []Capture
	TotalBytes int64
}

// Plan chooses, for each capturable effect, the cheapest sufficient mechanism, and
// drops captures made redundant by another. A removal is a hardlink whatever the
// size; an overwrite needs the bytes; a mode change needs a record. A path both
// overwritten and removed needs the bytes, because a hardlink shares the inode the
// write would change. A directory hardlinked as a whole covers every removal under it.
func (a *Analysis) Plan() Plan {
	type need struct{ remove, write, meta bool }
	byPath := map[string]*need{}
	var gitCaps []Capture
	for _, e := range a.Effects {
		if e.Hint == "git-head" {
			gitCaps = append(gitCaps, Capture{Mechanism: GitHead, Path: e.Path})
			continue
		}
		if e.Kind == Remove && e.MovedTo != "" || e.Kind == Create || e.Kind == Read {
			continue
		}
		n := byPath[e.Path]
		if n == nil {
			n = &need{}
			byPath[e.Path] = n
		}
		switch e.Kind {
		case Remove:
			n.remove = true
		case Write:
			n.write = true
		case Meta:
			n.meta = true
		}
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var plan Plan
	var trashedDirs []string
	covered := func(p string) bool {
		for _, d := range trashedDirs {
			if strings.HasPrefix(p, d+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	for _, p := range paths { // sorted, so ancestors precede descendants
		n := byPath[p]
		fi, err := os.Lstat(p)
		isDir := err == nil && fi.IsDir()
		switch {
		case n.write:
			b := int64(0)
			if err == nil && fi.Mode().IsRegular() {
				b = fi.Size()
			}
			plan.Captures = append(plan.Captures, Capture{Mechanism: Content, Path: p, Bytes: b})
			plan.TotalBytes += b
		case n.remove && !covered(p):
			plan.Captures = append(plan.Captures, Capture{Mechanism: Trash, Path: p})
			if isDir {
				trashedDirs = append(trashedDirs, p)
			}
		}
		// A hardlink shares the inode a mode change would alter, so metadata is
		// recorded even under a trashed directory.
		if n.meta {
			plan.Captures = append(plan.Captures, Capture{Mechanism: Metadata, Path: p})
		}
	}
	plan.Captures = append(plan.Captures, gitCaps...)
	for i := len(a.States) - 1; i >= 0; i-- { // undo in reverse order
		if sc := a.States[i]; len(sc.Inverse) > 0 {
			plan.Captures = append(plan.Captures, Capture{Mechanism: Inverse, Path: sc.Subject, Inverse: sc.Inverse, Sudo: sc.Sudo})
		}
	}
	return plan
}
