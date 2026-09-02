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
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
		pattern: gitResetHardPattern,
		reason:  "git reset --hard discards uncommitted work — recoverable via the commit HEAD pointed at before it ran",
	},
	{
		name:    "git clean -f",
		pattern: gitCleanForcePattern,
		reason:  "git clean -f permanently deletes untracked files — recoverable via a pre-deletion trash copy",
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
	{
		name:    "chmod recursive",
		pattern: regexp.MustCompile(`\bchmod\b[^|;&]*(-[a-zA-Z]*R[a-zA-Z]*\b|--recursive\b)`),
		reason:  "a recursive permission change across a whole directory tree can lock out access as broadly as a deletion — recoverable via a pre-change record of each file's original mode",
	},
	{
		name:    "chown recursive",
		pattern: regexp.MustCompile(`\bchown\b[^|;&]*(-[a-zA-Z]*R[a-zA-Z]*\b|--recursive\b)`),
		reason:  "a recursive ownership change across a whole directory tree can lock out access as broadly as a deletion — recoverable via a pre-change record of each file's original owner",
	},
	{
		name:    "eval",
		pattern: regexp.MustCompile(`\beval\b`),
		reason:  "eval executes a dynamically constructed string this classifier cannot inspect before it runs",
	},
	{
		name:    "pkill",
		pattern: regexp.MustCompile(`\bpkill\b`),
		reason:  "pkill resolves its target by pattern match against running processes rather than a specific known one, risking killing the wrong process",
	},
	{
		name:    "fuser kill",
		pattern: regexp.MustCompile(`\bfuser\b[^|;&]*-[a-zA-Z]*k[a-zA-Z]*\b`),
		reason:  "fuser -k resolves its target by resource lookup (port, file, mount) rather than a specific known process, risking killing the wrong one",
	},
}

// Deliberately unclassified: chmod/chown on a specific, named path (no -R).
// Blast radius is one file, matching the same low-stakes calculus that keeps
// most of this package's rules narrow — flagging every chmod +x would cost a
// confirmation keypress on one of the most common, benign shell idioms far
// more often than it would catch real danger.

// Formerly a documented gap (Session 22): cp onto an existing destination.
// Unlike sed -i/tee/truncate (destructive essentially every time they're
// invoked at all), cp is destructive only when its destination already
// exists — which plain text matching cannot know, since Classify does no
// filesystem I/O by design. Closed in Session 23 via ClassifyForDir, a
// separate function that does consult the filesystem — kept apart from
// Classify rather than adding I/O to it, since a filesystem-aware check
// needs a working directory and is a fundamentally different (and harder to
// unit test) kind of function than pure text matching. See
// cpOverwritesExisting below.

// Known, deliberately unflagged gap: shell-variable indirection. A command
// like `CMD=rm; $CMD file.txt` never contains the literal substring "rm" in
// the text actually executed — it's introduced through a variable binding
// this package does not track. Closing this needs real variable resolution,
// which is exactly the shell-parsing this package trades away for
// auditability (see the package doc comment) — accepted, not silently
// dropped (Session 23).
//
// Known, accepted false positive: a path or filename that happens to
// contain "dd" as its own token, e.g. `mv /mnt/dd-backup ~/archive/` —
// hyphens aren't word characters, so \bdd\b matches "dd" inside
// "dd-backup" as if it were the dd command. Costs an unnecessary
// confirmation keypress, never a false negative, so it's left as-is rather
// than chased with a fragile "was dd actually the command name" heuristic.

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
	if hasFetchExec(cmd) {
		return Irreversible, "fetching remote content and running it through a shell interpreter executes arbitrary unreviewed code"
	}
	if hasDecodeExec(cmd) {
		return Irreversible, "decoding content and running it through a shell interpreter executes arbitrary unreviewed code"
	}
	if hasDynamicKillTarget(cmd) {
		return Irreversible, "this kills a process resolved at runtime rather than a specific known PID, risking the wrong target"
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

// teeAppendFlag matches tee's append flag as a standalone token, including
// when combined with other short flags in either order (-ai, -ia, both
// equivalent to -a -i under standard getopt combining) — not just a bare
// -a/--append, so a genuinely safe combined-flag invocation isn't
// over-flagged.
var teeAppendFlag = regexp.MustCompile(`(^|\s)(-[a-zA-Z]*a[a-zA-Z]*|--append)(\s|$)`)

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

// fetchCmd matches curl or wget invocations.
var fetchCmd = regexp.MustCompile(`\b(curl|wget)\b`)

// shellInterpreterInvocation matches sh/bash/zsh/dash/ash invoked as its own
// command — preceded by a shell operator, whitespace, or the start of the
// command, and followed the same way. Deliberately not a bare \b match:
// found live (Session 23) that \bsh\b alone false-matches the file
// extension in "installer.sh", since "." counts as a word boundary just
// like whitespace does. Requiring an actual operator/whitespace/edge on
// both sides catches both the piped shape (curl ... | sh) and the
// download-then-run shape (wget ... && bash file) without that false
// positive.
var shellInterpreterInvocation = regexp.MustCompile(`(^|[\s;&|])(sudo\s+)?(sh|bash|zsh|dash|ash)($|[\s;&|])`)

// hasFetchExec reports whether cmd fetches remote content and also invokes
// a shell interpreter on it — either piped directly (curl ... | sh) or
// downloaded then run as a separate step (wget ... && bash file), the shape
// found live to be at least as common a model output as the piped form.
// This is a fetch-and-execute risk, not filesystem reversibility in the
// usual sense this package otherwise checks, but running arbitrary
// unreviewed remote code is at least as consequential as anything else it
// flags (see docs/decisions.md D22).
func hasFetchExec(cmd string) bool {
	return fetchCmd.MatchString(cmd) && shellInterpreterInvocation.MatchString(cmd)
}

// base64Decode matches a base64 decode invocation (short or long flag).
var base64Decode = regexp.MustCompile(`\bbase64\b[^|;&]*(-d\b|--decode\b)`)

// hasDecodeExec reports whether cmd decodes base64 content and also invokes
// a shell interpreter — the same fetch-and-execute concern as hasFetchExec,
// applied to an inline-encoded payload instead of a remote URL.
// Deliberately narrow: this catches the decode-then-execute *shape*, not
// arbitrary obfuscation — a payload hidden behind command substitution or a
// custom encoding this package doesn't know to look for is a known,
// accepted blind spot (see the package-level gap notes above), since a
// regex matcher cannot generically decode and inspect content the way a
// real shell parser could.
func hasDecodeExec(cmd string) bool {
	return base64Decode.MatchString(cmd) && shellInterpreterInvocation.MatchString(cmd)
}

// xargsIntoKill matches xargs feeding a kill command in the same pipeline
// segment — e.g. `... | xargs kill -9` — a dynamically resolved target,
// same danger shape as the existing "rm inside pipeline" case.
var xargsIntoKill = regexp.MustCompile(`\bxargs\b[^|;&]*\bkill\b`)

// commandSubstitution matches $(...) or `...` — used here to detect a kill
// target resolved by substitution (kill $(pgrep myapp)) rather than a
// literal, already-known PID.
var commandSubstitution = regexp.MustCompile("\\$\\(|`")

// bareKill matches a "kill" invocation as its own command.
var bareKill = regexp.MustCompile(`\bkill\b`)

// hasDynamicKillTarget reports whether cmd kills a process resolved at
// runtime (via a piped lookup or command substitution) rather than a
// specific PID already known when the command was written. Deliberately
// narrow: a literal `kill -9 1234` is not flagged — the number is already
// fixed in the command text, same distinction the classifier draws
// elsewhere between a known target and one resolved dynamically.
func hasDynamicKillTarget(cmd string) bool {
	return bareKill.MatchString(cmd) && (xargsIntoKill.MatchString(cmd) || commandSubstitution.MatchString(cmd))
}

// ClassifyForDir extends Classify with one additional, filesystem-aware
// check: whether a proposed cp would silently overwrite an existing
// destination file. wd resolves cp's relative destination argument — pass
// the working directory the command will actually execute in. Every other
// verdict is identical to Classify; this function exists separately (rather
// than adding I/O to Classify) because a filesystem check is a genuinely
// different, harder-to-unit-test kind of operation than pure text matching,
// and every other call site (the adversarial corpus, live-model testing)
// wants Classify's cheap, deterministic, no-I/O behavior unchanged.
func ClassifyForDir(cmd, wd string) (Verdict, string) {
	if v, reason := Classify(cmd); v == Irreversible {
		return v, reason
	}
	if dst, overwrites := cpOverwritesExisting(cmd, wd); overwrites {
		return Irreversible, fmt.Sprintf("cp overwrites an existing file (%s already exists and would be replaced with no built-in undo)", dst)
	}
	return Reversible, ""
}

// cpInvocation isolates a cp command's own segment of the line, stopping at
// the next shell operator — the same [^|;&]* boundary convention the rest
// of this package uses to avoid crossing into an unrelated pipeline stage.
var cpInvocation = regexp.MustCompile(`\bcp\b[^|;&]*`)

// cpOverwritesExisting reports whether cmd is a cp invocation whose
// destination already exists as a file in wd. Copying into an existing
// *directory* is normal, expected cp behavior (the source lands inside it,
// nothing is replaced) and is not flagged — only a destination that exists
// as a file in its own right is destructive. Deliberately approximate, not
// a full getopt parser: flags are dropped wholesale rather than matched
// against cp's actual option set, and a command this can't confidently
// parse (fewer than two positional arguments — e.g. a bare "cp -r" with a
// variable, or output truncated mid-generation) is treated as safe rather
// than guessed at, consistent with this package's stated trade of
// precision for auditability.
func cpOverwritesExisting(cmd, wd string) (string, bool) {
	seg := cpInvocation.FindString(cmd)
	if seg == "" {
		return "", false
	}
	fields := tokenizeShellWords(seg)
	var args []string
	for _, f := range fields[1:] { // fields[0] is "cp" itself
		if strings.HasPrefix(f, "-") {
			continue
		}
		args = append(args, f)
	}
	if len(args) < 2 {
		return "", false
	}
	dst := args[len(args)-1]
	path := dst
	if !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return dst, true
}

// CpOverwriteTarget returns the absolute path of the file a cp invocation
// would overwrite, if any — the same check ClassifyForDir uses internally
// to reach its verdict, exposed separately and resolved to an absolute
// path so a caller that already has that Irreversible verdict can find out
// which file to protect before the command runs.
//
// This target belongs on internal/undo's content-copy path (BackupContent),
// not its cheaper trash/hardlink path (TrashPreserve): verified empirically
// (Session 25) that cp overwrites an existing destination's same inode in
// place rather than unlinking and recreating it, so a hardlink taken
// beforehand would share the very data being overwritten and protect
// nothing.
func CpOverwriteTarget(cmd, wd string) (string, bool) {
	dst, overwrites := cpOverwritesExisting(cmd, wd)
	if !overwrites {
		return "", false
	}
	if filepath.IsAbs(dst) {
		return dst, true
	}
	return filepath.Join(wd, dst), true
}

// sedSegment, awkSegment, and truncateSegment isolate each command's own
// segment of the line, the same [^|;&]* boundary convention cpInvocation
// uses, so ContentMutationTargets doesn't reach into an unrelated pipeline
// stage when picking out that command's own arguments.
var sedSegment = regexp.MustCompile(`\bsed\b[^|;&]*`)
var awkSegment = regexp.MustCompile(`\bawk\b[^|;&]*`)
var truncateSegment = regexp.MustCompile(`\btruncate\b[^|;&]*`)
var teeSegment = regexp.MustCompile(`\btee\b[^|;&]*`)

// sedInPlaceFlag and awkInPlaceFlag are the same flag patterns the "sed
// in-place edit"/"awk in-place edit" rules above match, factored out so
// ContentMutationTargets can test for them within an already-isolated
// segment without recompiling or duplicating the full \bsed\b[^|;&]*...
// pattern.
var sedInPlaceFlag = regexp.MustCompile(`-i\b|--in-place\b`)
var awkInPlaceFlag = regexp.MustCompile(`-i\s+inplace\b`)

// ContentMutationTargets returns the file paths a known content-mutating
// command shape (sed -i, awk -i inplace, truncate, a truncating redirect,
// or tee without -a) would overwrite, resolved to absolute paths against
// wd. It exists for internal/undo to back up a file's content immediately
// before a confirmed Irreversible command runs — the "guiltless" half of
// accurate-and-guiltless (Session 24): even a wrongly confirmed "yes"
// should be recoverable, not just an accurately blocked one.
//
// Deliberately narrow, matching this package's stated trade of precision
// for auditability: for sed/awk/truncate, the *last* non-flag token in the
// command's own segment is treated as its target file, which covers the
// common single-file case but misses sed/awk's ability to edit several
// files in one invocation (sed -i 's/x/y/' a.txt b.txt only backs up
// b.txt) — an accepted gap, not silently dropped, since a false negative
// here only means no backup for the missed file, not misclassification.
// tee is the one shape genuinely and routinely multi-target (tee writes
// every argument simultaneously by design), so every non-flag token after
// tee is returned, not just the last. A target that isn't a flag but
// happens to not be a real file (a malformed command, or a script argument
// mistaken for a filename) is returned anyway — BackupContent's job, not
// this function's, to discover it doesn't exist and skip it.
//
// Known, accepted gap: a flag that takes its own value as a separate token
// (truncate -s 0 file.txt) is indistinguishable from a bare flag by this
// heuristic, since it only knows "starts with -", not which flags consume
// the token after them. This can only misidentify a value as the target
// when it's genuinely the last token with no real file after it (truncate
// -s 0 alone, no file argument, is invalid usage anyway); the common case
// (a real trailing file argument) is unaffected since that file is still
// last. Fixing this properly needs each command's actual flag arity, which
// is real getopt-parsing knowledge this package trades away by design.
func ContentMutationTargets(cmd, wd string) []string {
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(wd, p)
	}
	lastNonFlagToken := func(seg string) []string {
		tokens := tokenizeShellWords(seg)
		var last string
		for _, tok := range tokens[1:] { // tokens[0] is the command name itself
			if !strings.HasPrefix(tok, "-") {
				last = tok
			}
		}
		if last == "" {
			return nil
		}
		return []string{resolve(last)}
	}

	if seg := sedSegment.FindString(cmd); seg != "" && sedInPlaceFlag.MatchString(seg) {
		return lastNonFlagToken(seg)
	}
	if seg := awkSegment.FindString(cmd); seg != "" && awkInPlaceFlag.MatchString(seg) {
		return lastNonFlagToken(seg)
	}
	if seg := truncateSegment.FindString(cmd); seg != "" {
		return lastNonFlagToken(seg)
	}
	if idx := lastTruncatingRedirectIndex(cmd); idx != -1 {
		tokens := tokenizeShellWords(cmd[idx+1:])
		if len(tokens) > 0 {
			return []string{resolve(tokens[0])}
		}
		return nil
	}
	if hasUnsafeTee(cmd) {
		seg := teeSegment.FindString(cmd)
		tokens := tokenizeShellWords(seg)
		var targets []string
		for _, tok := range tokens[1:] { // tokens[0] is "tee" itself
			if !strings.HasPrefix(tok, "-") {
				targets = append(targets, resolve(tok))
			}
		}
		return targets
	}
	return nil
}

// lastTruncatingRedirectIndex returns the byte index of the last '>' in cmd
// that isn't part of an append redirect ('>>'), or -1 if there isn't one.
// Same character-scan rationale as hasTruncatingRedirect (RE2 has no
// lookbehind) — this variant additionally returns the position, since
// ContentMutationTargets needs to know what follows it.
func lastTruncatingRedirectIndex(cmd string) int {
	last := -1
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		if i+1 < len(cmd) && cmd[i+1] == '>' {
			i++
			continue
		}
		last = i
	}
	return last
}

// rmSegment isolates an rm command's own segment of the line, the same
// [^|;&]* boundary convention cpInvocation uses.
var rmSegment = regexp.MustCompile(`\brm\b[^|;&]*`)

// TrashTargets returns every file or directory path an rm invocation would
// remove, resolved to absolute paths against wd, for internal/undo's
// TrashPreserve to hardlink into a holding directory before a confirmed rm
// runs. Unlike ContentMutationTargets' single-target shapes, every non-flag
// token is returned — rm genuinely can remove several named paths in one
// invocation (rm a.txt b.txt c.txt deletes all three), and unlike sed/tee's
// last-token-wins ambiguity, there's no script/expression argument to
// confuse a target with.
//
// Scoped to rm alone: this is the one shape where deletion means removing a
// directory entry without touching the target's own data (see
// internal/undo's package doc for why that specific property is what makes
// the cheap hardlink-based path safe) — shred is deliberately excluded
// (its entire purpose is making content unrecoverable, which a trash copy
// would defeat), and dd/mkfs operate on raw block/device data a directory
// entry can't represent at all.
//
// Known, accepted gap: an end-of-options marker (rm -- -oddly-named-file)
// isn't recognized, so a literal filename starting with "-" after "--" is
// dropped along with the real flags — the same precision-for-auditability
// trade this package makes throughout rather than implementing a real
// getopt parser.
func TrashTargets(cmd, wd string) []string {
	seg := rmSegment.FindString(cmd)
	if seg == "" {
		return nil
	}
	tokens := tokenizeShellWords(seg)
	var targets []string
	for _, tok := range tokens[1:] { // tokens[0] is "rm" itself
		if strings.HasPrefix(tok, "-") {
			continue
		}
		if filepath.IsAbs(tok) {
			targets = append(targets, tok)
		} else {
			targets = append(targets, filepath.Join(wd, tok))
		}
	}
	return targets
}

// gitResetHardPattern and gitCleanForcePattern are the same patterns the
// "git reset --hard"/"git clean -f" rules above match, factored out so
// IsGitResetHard/IsGitCleanForce/GitCleanDryRunCommand can reuse them
// without duplicating or recompiling the pattern.
var gitResetHardPattern = regexp.MustCompile(`\bgit\s+reset\b[^|;&]*--hard\b`)
var gitCleanForcePattern = regexp.MustCompile(`\bgit\s+clean\b[^|;&]*-\w*f`)
var gitCleanSegment = regexp.MustCompile(`\bgit\s+clean\b[^|;&]*`)

// IsGitResetHard reports whether cmd contains a git reset --hard
// invocation — internal/undo uses this to decide whether to capture the
// repository's current HEAD commit before a confirmed reset runs.
func IsGitResetHard(cmd string) bool {
	return gitResetHardPattern.MatchString(cmd)
}

// IsGitCleanForce reports whether cmd contains a git clean -f invocation.
func IsGitCleanForce(cmd string) bool {
	return gitCleanForcePattern.MatchString(cmd)
}

// GitCleanDryRunCommand returns a dry-run variant of cmd's git clean -f
// invocation — inserting -n, which git honors as an override even when -f
// is also present (verified empirically, Session 26: `git clean -n -f`
// removes nothing, only reports what would be removed) — for
// internal/undo to learn exactly which untracked paths a confirmed git
// clean -f would remove before anything is actually removed, the same way
// TrashTargets already knows rm's targets directly from its argument list.
// ok is false if cmd has no git clean -f invocation to transform.
func GitCleanDryRunCommand(cmd string) (string, bool) {
	seg := gitCleanSegment.FindString(cmd)
	if seg == "" || !gitCleanForcePattern.MatchString(seg) {
		return "", false
	}
	dryRunSeg := strings.Replace(seg, "clean", "clean -n", 1)
	return strings.Replace(cmd, seg, dryRunSeg, 1), true
}

// chmodSegment, chownSegment, and recursiveFlag isolate a chmod/chown
// invocation's own segment and its recursive flag, the same patterns the
// "chmod recursive"/"chown recursive" rules above already match, factored
// out for reuse rather than duplicated.
var chmodSegment = regexp.MustCompile(`\bchmod\b[^|;&]*`)
var chownSegment = regexp.MustCompile(`\bchown\b[^|;&]*`)
var recursiveFlag = regexp.MustCompile(`-[a-zA-Z]*R[a-zA-Z]*\b|--recursive\b`)

// RecursivePermissionTargets returns every path a recursive chmod or chown
// invocation would apply to, resolved to absolute paths against wd, for
// internal/undo's metadata-backup mechanism to record each path's current
// mode/owner before the change runs. The first non-flag token — chmod's
// MODE or chown's OWNER[:GROUP] spec — is skipped; every remaining
// non-flag token is a target, since both commands accept multiple targets
// in one invocation the same way rm does.
func RecursivePermissionTargets(cmd, wd string) []string {
	seg := chmodSegment.FindString(cmd)
	if seg == "" || !recursiveFlag.MatchString(seg) {
		seg = chownSegment.FindString(cmd)
		if seg == "" || !recursiveFlag.MatchString(seg) {
			return nil
		}
	}

	var positional []string
	for _, tok := range tokenizeShellWords(seg)[1:] { // [0] is the command name itself
		if !strings.HasPrefix(tok, "-") {
			positional = append(positional, tok)
		}
	}
	if len(positional) < 2 { // need at least a mode/owner spec plus one target
		return nil
	}

	var targets []string
	for _, tok := range positional[1:] { // positional[0] is the mode/owner spec
		if filepath.IsAbs(tok) {
			targets = append(targets, tok)
		} else {
			targets = append(targets, filepath.Join(wd, tok))
		}
	}
	return targets
}

// ddSegment and mkfsSegment isolate a dd/mkfs invocation's own segment.
var ddSegment = regexp.MustCompile(`\bdd\b[^|;&]*`)
var mkfsSegment = regexp.MustCompile(`\bmkfs(\.\w+)?\b[^|;&]*`)

// RawWriteOverwriteTarget returns the absolute path of an existing regular
// file a dd or mkfs invocation would overwrite, if any. dd and mkfs write
// into an existing regular-file target's own inode rather than replacing
// it — the same in-place-mutation shape sed -i and cp's overwrite already
// get content-backup for (cp verified empirically, Session 25) — but only
// when the target actually is a regular file. dd/mkfs's overwhelmingly
// common real target is a raw block device, not a regular file at all;
// this deliberately returns ok=false for anything that isn't a regular
// file (a device, a directory, or a path that doesn't exist yet and so has
// nothing to protect) rather than attempting to back up a device's data,
// which a bounded pre-execution backup cannot meaningfully do (see
// docs/safety-model.md).
func RawWriteOverwriteTarget(cmd, wd string) (string, bool) {
	var target string
	if seg := ddSegment.FindString(cmd); seg != "" {
		for _, tok := range tokenizeShellWords(seg) {
			if v, ok := strings.CutPrefix(tok, "of="); ok {
				target = v
				break
			}
		}
	} else if seg := mkfsSegment.FindString(cmd); seg != "" {
		tokens := tokenizeShellWords(seg)
		for _, tok := range tokens[1:] {
			if !strings.HasPrefix(tok, "-") {
				target = tok
			}
		}
	}
	if target == "" {
		return "", false
	}

	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// tokenizeShellWords splits s into words on unquoted whitespace, respecting
// single and double quotes as POSIX shells do for a plain literal (no
// variable expansion, no escape sequences inside quotes — this package does
// not track variable values by design, see the gap notes above). Good
// enough for path arguments in a proposed command; not a general shell
// lexer.
func tokenizeShellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var inSingle, inDouble, has bool

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle, has = true, true
		case c == '"':
			inDouble, has = true, true
		case c == ' ' || c == '\t':
			if has {
				words = append(words, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(c)
			has = true
		}
	}
	if has {
		words = append(words, cur.String())
	}
	return words
}
