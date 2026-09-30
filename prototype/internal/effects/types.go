// Package effects derives, from a shell command's syntax, the filesystem effects it
// would have, and from those a recoverability verdict and a minimal capture plan.
//
// It exists because a hand-kept list of dangerous command names fails in three
// ways the pilot in docs/algorithms.md measured: it cannot see through wrappers
// (find -exec, xargs, loops), it does not know a command's real targets when they
// are computed at run time, and it fails open on commands nobody listed. The
// package addresses the first two directly and makes the third fail closed.
//
// Nothing here executes a mutating command. The only commands it ever runs are
// read-only resolvers that it has first proven read-only with its own analysis.
package effects

// Kind is what a command does to one path.
type Kind int

const (
	// Read is part of the model but is not emitted: reads never lose data.
	Read Kind = iota
	Create
	Write
	Remove
	Meta
)

func (k Kind) String() string {
	return [...]string{"read", "create", "write", "remove", "metadata"}[k]
}

// Effect is one effect of one command on one path.
type Effect struct {
	Kind Kind
	Path string // absolute, cleaned
	Dir  bool   // the path is (or will be) a directory
	// MovedTo is set on a Remove whose very same data survives at another path
	// (a rename or move). Such a removal needs no capture: the undo is moving it
	// back. It is never set for a transformation such as compression, whose
	// output is different bytes.
	MovedTo string
	// Hint marks effects that are not a plain path, e.g. "git-head".
	Hint   string
	Source string // text of the leaf command that produced it
}

// IssueKind says why a command could not be given a clean effect set.
type IssueKind int

const (
	// IssueUnresolved: the command is modelled, but a target could not be determined.
	IssueUnresolved IssueKind = iota
	// IssueOpaque: the command is not modelled, or is computed at run time.
	// Unknown is treated as unrecoverable — the analysis fails closed.
	IssueOpaque
	// IssueUnrecoverable: modelled, and known not to be restorable by capturing files.
	IssueUnrecoverable
)

func (k IssueKind) String() string {
	return [...]string{"unresolved", "opaque", "unrecoverable"}[k]
}

type Issue struct {
	Kind   IssueKind
	Source string
	Reason string
}

// Analysis is the raw result of analysing one command line.
type Analysis struct {
	Effects []Effect
	Issues  []Issue
	// States are reversible changes to package or service state, each with an inverse.
	States []StateChange
}

// ReadOnly reports whether the command has no effects and no unresolved parts.
// A resolver is only ever run when this holds.
func (a *Analysis) ReadOnly() bool {
	return len(a.Effects) == 0 && len(a.Issues) == 0 && len(a.States) == 0
}

func (a *Analysis) merge(b *Analysis) {
	a.Effects = append(a.Effects, b.Effects...)
	a.Issues = append(a.Issues, b.Issues...)
	a.States = append(a.States, b.States...)
}
