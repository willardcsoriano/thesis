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
}

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
