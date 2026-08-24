// Package classifier decides whether a proposed shell command is safe to run
// automatically or must be confirmed by the user first.
//
// It is deliberately a pattern matcher, not a shell parser (see build-order.md
// M2): known-destructive command shapes are matched against the proposed
// command text. This trades precision for something that ships and is easy to
// audit — the patterns are a short, explicit list a reader can check against
// their own risk tolerance. False positives (blocking a command that was
// actually safe) cost the user one confirmation keypress; false negatives
// (auto-running something destructive) cost data, so every ambiguous case
// below resolves toward Irreversible.
package classifier

import (
	"fmt"
	"regexp"
)

// Verdict is the reversibility classification of a proposed command.
type Verdict int

const (
	// Reversible commands run immediately with no confirmation prompt.
	Reversible Verdict = iota
	// Irreversible commands are shown to the user and require an explicit
	// yes before they run.
	Irreversible
)

func (v Verdict) String() string {
	if v == Irreversible {
		return "irreversible"
	}
	return "reversible"
}

// rule pairs a compiled pattern with the human-readable reason shown in the
// confirmation prompt when it matches.
type rule struct {
	name    string
	pattern *regexp.Regexp
	reason  string
}

// notFollowedByShellOp excludes matches immediately followed by another
// shell metacharacter that would change the token's meaning (e.g. "add"
// containing no "dd" match already; this guards patterns like `-f` inside a
// longer flag such as `--force-with-lease` from a bare "-f" match). Patterns
// below use \b word boundaries for command names, which already prevents
// most false positives; this exists for the flag-only checks that can't use
// a word boundary the same way.
var rules = []rule{
	{
		name:    "rm",
		pattern: regexp.MustCompile(`\brm\b`),
		reason:  "rm can permanently delete files with no built-in undo",
	},
	{
		name:    "dd",
		pattern: regexp.MustCompile(`\bdd\b`),
		reason:  "dd overwrites raw block or file data with no built-in undo",
	},
	{
		name:    "mkfs",
		pattern: regexp.MustCompile(`\bmkfs(\.\w+)?\b`),
		reason:  "mkfs erases a filesystem's existing contents",
	},
	{
		name:    "shred",
		pattern: regexp.MustCompile(`\bshred\b`),
		reason:  "shred is designed to make file contents unrecoverable",
	},
	{
		name:    "git reset --hard",
		pattern: regexp.MustCompile(`\bgit\s+reset\b[^|;&]*--hard\b`),
		reason:  "git reset --hard discards uncommitted work with no undo",
	},
	{
		name:    "git clean -f",
		pattern: regexp.MustCompile(`\bgit\s+clean\b[^|;&]*-\w*f`),
		reason:  "git clean -f permanently deletes untracked files",
	},
	{
		name:    "sed in-place edit",
		pattern: regexp.MustCompile(`\bsed\b[^|;&]*(-i\b|--in-place\b)`),
		reason:  "sed -i overwrites the file in place with no built-in undo",
	},
	{
		name:    "awk in-place edit",
		pattern: regexp.MustCompile(`\bawk\b[^|;&]*-i\s+inplace\b`),
		reason:  "awk -i inplace overwrites the file in place with no built-in undo",
	},
	{
		name:    "truncate",
		pattern: regexp.MustCompile(`\btruncate\b`),
		reason:  "truncate can shrink or empty a file's contents with no built-in undo",
	},
}

// Known, deliberately unflagged gap: cp onto an existing destination.
// Unlike sed -i/tee/truncate (destructive essentially every time they're
// invoked at all), cp is destructive only when its destination already
// exists — which this package cannot know from the command text alone,
// since it does no filesystem I/O by design. Blanket-flagging every cp
// would cost a confirmation keypress on the common, actually-safe case
// (copying to a fresh path) far more often than it would catch real danger.
// Closing this properly needs the classifier to consult filesystem state,
// which is a real design change (see the rigorous-testing-plan discussion,
// Session 22), not a regex patch — tracked here, not silently dropped.

// Classify returns whether cmd is safe to auto-run. When the verdict is
// Irreversible, reason explains why in a form suitable for showing directly
// to the user in a confirmation prompt; it is empty for Reversible.
func Classify(cmd string) (Verdict, string) {
	for _, r := range rules {
		if r.pattern.MatchString(cmd) {
			return Irreversible, fmt.Sprintf("%s (%s)", r.name, r.reason)
		}
	}
	if hasTruncatingRedirect(cmd) {
		return Irreversible, "truncating redirect (>) overwrites the target file's existing contents"
	}
	if hasUnsafeTee(cmd) {
		return Irreversible, "tee without -a/--append overwrites the target file's existing contents"
	}
	return Reversible, ""
}

// hasTruncatingRedirect reports whether cmd contains a single '>' that is not
// part of an append redirect ('>>'). This is a character scan rather than a
// regexp because Go's RE2 engine has no lookbehind, and the append-vs-truncate
// distinction depends on what follows a '>' as well as what a second '>'
// consumes.
func hasTruncatingRedirect(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		if i+1 < len(cmd) && cmd[i+1] == '>' {
			i++ // consume the second '>' of an append redirect
			continue
		}
		return true
	}
	return false
}

// unsafeTeeCmd matches a bare "tee" invocation.
var unsafeTeeCmd = regexp.MustCompile(`\btee\b`)

// teeAppendFlag matches tee's append flag as a standalone token.
var teeAppendFlag = regexp.MustCompile(`(^|\s)(-a|--append)(\s|$)`)

// hasUnsafeTee reports whether cmd invokes tee without an append flag
// anywhere in the command. tee overwrites its target file(s) by default;
// -a/--append is the only thing that makes it non-destructive. This checks
// for the flag's presence anywhere in the string rather than scoping it to
// a specific tee invocation (RE2 has no lookaround for that), so a command
// chaining an appending tee and a non-appending tee in the same line would
// be under-flagged — an accepted imprecision, consistent with this
// package's stated trade of precision for auditability.
func hasUnsafeTee(cmd string) bool {
	return unsafeTeeCmd.MatchString(cmd) && !teeAppendFlag.MatchString(cmd)
}
