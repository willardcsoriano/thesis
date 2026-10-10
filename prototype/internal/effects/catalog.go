package effects

import (
	"fmt"
	"sort"
	"strings"
)

// ruleNotes says, for each entry in the mutating rule table, what effects the rule
// derives. It is documentation, kept beside the code it describes:
// TestCatalogCoversEveryRule fails when a rule has no note or a note has no rule, and
// TestCatalogDocIsCurrent fails when docs/recoverability-analysis.md no longer matches.
var ruleNotes = map[string]string{
	"rm":       "removes each existing operand; a directory only with -r; -r on / or a top-level directory is unrecoverable",
	"unlink":   "as rm",
	"rmdir":    "removes each operand that is an empty directory; a non-empty one fails and changes nothing",
	"mv":       "moves each source: onto a new name, a create plus a removal that undo reverses by moving back; onto an existing name, an overwrite (captured); -n skips; -b also makes a backup file",
	"cp":       "copies each source: an existing destination is overwritten (captured), a new one created; directories only with -r/-a, walked file by file; --parents creates the intermediate directories; -b makes a backup file",
	"install":  "as cp; -d creates directories",
	"ln":       "creates each link; -f over an existing file removes that file (captured whole, not through the new link)",
	"mkdir":    "creates each missing directory, and with -p each missing parent",
	"touch":    "creates each missing file; an existing file only gets new times, which lose nothing; -c creates nothing",
	"chmod":    "changes the mode of each operand, and with -R of everything under it (captured as a metadata record); an octal mode equal to the current one is no change",
	"chown":    "changes ownership of each operand, recursively with -R (metadata record)",
	"chgrp":    "as chown",
	"sed":      "-i overwrites each file, and -i.SUF also leaves a backup; a script with w, W, or e is opaque; otherwise read-only",
	"awk":      "-i inplace overwrites each file; a program with system(), a redirected print, or a piped getline is opaque; otherwise read-only",
	"gawk":     "as awk",
	"mawk":     "as awk",
	"nawk":     "as awk",
	"truncate": "overwrites each operand, creating it unless -c",
	"tee":      "writes each file operand (- and devices excepted)",
	"dd":       "writes the of= file; a raw device is unrecoverable",
	"shred":    "unrecoverable whenever a target exists: it destroys content on purpose",
	"sort":     "-o FILE writes FILE; otherwise read-only",
	"uniq":     "a second operand is an output file and is written",
	"gzip":     "writes the compressed file and removes the original unless -k; the removal is captured, because the output is different bytes, not a move; -c, -t, -l are read-only",
	"gunzip":   "as gzip, in reverse",
	"bzip2":    "as gzip",
	"bunzip2":  "as gzip, in reverse",
	"xz":       "as gzip",
	"unxz":     "as gzip, in reverse",
	"zstd":     "as gzip",
	"unzstd":   "as gzip, in reverse",
	"curl":     "-o, -O, -D, -c write their files; a request that sends data or uses a method other than GET or HEAD is unrecoverable; -K and trace options are unresolved",
	"wget":     "creates the file named by the URL (index.html for a trailing slash), or NAME.1 when NAME exists, since wget never replaces by default; -N and -c overwrite; -O writes its file; -o and -a write the log; recursive and server-named downloads are unresolved",
	"tar":      "create, append, update write the archive, and --remove-files removes the inputs (captured); extract is resolved by tar -tf and writes or creates each member; an archive on standard input is unresolved; list and diff are read-only",
	"unzip":    "resolved by unzip -Z1: writes or creates each member; -j drops directories; -n never overwrites",
	"rsync":    "resolved by rsync --dry-run --itemize-changes: creates, overwrites, and --delete removals; a remote host is unrecoverable; options that change the source, run a remote command, or write a file even in a dry run are unresolved",
	"git":      "per subcommand: reset --hard captures HEAD and every dirty tracked file; clean is resolved by clean -n; checkout, restore, switch -f and stash capture the dirty tracked files they overwrite; checkout -b and stash record an inverse; rm and mv as the file rules; history rewrites, forced pushes, and branch or tag deletion are unrecoverable; an unknown subcommand is opaque",
	"rename":   "resolved by rename -n (both the util-linux and the Perl tool): each rename is a move, and onto an existing name also an overwrite",
	"prename":  "as rename",
}

// compositionHandlers are the constructs the analysis resolves through rather than
// treating as a single command. Compose=false makes each of them opaque.
var compositionHandlers = []struct{ name, how string }{
	{"transparent wrappers", "the wrapper's own options are skipped and the command it runs is analysed in its place; sudo and doas mark it privileged, so its targets are never resolved as the current user"},
	{"find -delete, -exec, -execdir, -ok, -okdir", "the expression is rewritten so each action prints which action fired on which path, run read-only, and the inner command is analysed once per match (or once with all matches, for +)"},
	{"xargs", "the producer feeding it is proven read-only, run, and its output split exactly as GNU xargs would (-0, -d, -I, -n, -L, -r, NUL handling); the command is analysed per resulting invocation"},
	{"sh, bash, zsh, dash, ksh, ash -c", "the string is parsed and analysed as a command line of its own; a script file or code piped in is not visible and asks"},
	{"eval", "its arguments, joined by spaces, are analysed as a command line"},
	{"for loops", "the item list is expanded and the body analysed once per item with the variable bound, up to the loop cap"},
}

// readOnlyConditional are the commands readOnlyForm accepts only in some forms.
var readOnlyConditional = []struct{ name, when string }{
	{"sed", "without -i and without w, W, e in the script"},
	{"awk, gawk, mawk, nawk", "without -i inplace, and with a program that has no system(), redirected print, or piped getline"},
	{"sort", "without -o"},
	{"uniq", "with at most one operand"},
	{"xxd", "with at most one operand and without -r"},
	{"tar", "in list (-t) or diff (-d) mode"},
	{"unzip", "with -l, -Z, -t, -v, -p, -z, or -c"},
	{"curl", "with no output option and nothing sent"},
	{"wget", "with --spider or -O -"},
	{"git", "with a read-only subcommand (status, log, diff, show, ...) or the listing form of branch, tag, remote, config, stash, reflog, worktree, submodule"},
	{"base64, base32", "without -o"},
	{"date", "without -s or --set"},
	{"hostname", "with no operand"},
}

// dryRunAdapters are the fixed read-only commands the analysis runs to resolve targets.
var dryRunAdapters = []struct{ forCmd, runs string }{
	{"command substitution", "the substitution body, via bash -c, once the analysis finds it has no effects, no privilege, and nothing nondeterministic or written earlier on the line"},
	{"xargs", "the producer pipeline, via bash -c, under the same conditions"},
	{"find", "find with every action rewritten to -printf and every printing action to -true; refused if any action token survives"},
	{"tar -x", "tar -tf ARCHIVE [members]"},
	{"unzip", "unzip -Z1 ARCHIVE"},
	{"rsync", "rsync ARGS --dry-run --itemize-changes"},
	{"rename", "rename -n ARGS, reading both output streams"},
	{"git clean", "git clean -n, with repository hooks, fsmonitor, and external diff disabled"},
	{"git reset --hard, checkout, restore, switch, stash", "git diff --name-only HEAD, with the same settings disabled"},
	{"git checkout -b", "git symbolic-ref / rev-parse HEAD, to know where to switch back"},
	{"apt, apt-get", "apt-get -s VERB ...; then apt-cache policy and dpkg-query for the versions to restore"},
	{"dpkg", "dpkg-query -W, dpkg-deb -f"},
	{"systemctl, service", "systemctl is-active and is-enabled"},
}

// CatalogMarkdown renders the analysis's tables as Markdown: the rule map in
// docs/recoverability-analysis.md is exactly this text.
func CatalogMarkdown() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	modelled := map[string]bool{"apt": true, "apt-get": true, "dpkg": true, "systemctl": true, "service": true}
	w("Generated from `prototype/internal/effects` by `make rulemap`; a test fails when this section and the code disagree. ")
	w("In total: %d commands with a mutating rule, %d transparent wrappers, %d package, service, and system-state tools (%d of them modelled with inverses), %d commands that are always unrecoverable, %d interpreters, and %d commands that are read-only in every form.\n\n",
		len(rules), len(wrappers), len(stateTools), len(modelled), len(unrecoverableNames), len(interpreters), len(roAlways))

	w("#### Composition: what the analysis resolves through\n\n| Construct | How |\n|---|---|\n")
	for _, h := range compositionHandlers {
		w("| %s | %s |\n", h.name, h.how)
	}
	w("\nTransparent wrappers: %s.\n\n", codeList(keys(wrappers)))

	w("#### Mutating rules\n\n| Command | Effects derived |\n|---|---|\n")
	for _, k := range keys(rules) {
		w("| `%s` | %s |\n", k, ruleNotes[k])
	}

	w("\n#### Resolvers: the only commands the analysis runs\n\n| For | Runs |\n|---|---|\n")
	for _, d := range dryRunAdapters {
		w("| %s | %s |\n", d.forCmd, d.runs)
	}

	w("\n#### Package, service, and system state\n\nRead-only forms (listing, status, simulation) pass. ")
	w("Of the rest, %s are modelled: the tool is asked what would change, and the change is recorded with the commands that reverse it. Every other tool asks, for the reason shown.\n\n", codeList(keys(modelled)))
	w("| Tool | When it is not read-only |\n|---|---|\n")
	for _, k := range keys(stateTools) {
		why := stateTools[k].why
		if modelled[k] {
			why = "modelled, with an inverse"
		}
		w("| `%s` | %s |\n", k, why)
	}

	w("\n#### Always unrecoverable\n\n| Reason | Commands |\n|---|---|\n")
	byWhy := map[string][]string{}
	for k, why := range unrecoverableNames {
		byWhy[why] = append(byWhy[why], k)
	}
	byWhy["creates a filesystem"] = append(byWhy["creates a filesystem"], "mkfs.*")
	for _, why := range keys(byWhy) {
		sort.Strings(byWhy[why])
		w("| %s | %s |\n", why, codeList(byWhy[why]))
	}
	w("\nInterpreters, opaque by design (and unrecoverable when fed code through a pipe): %s.\n", codeList(keys(interpreters)))

	w("\n#### Read-only\n\nRead-only in every form: %s.\n\n", codeList(keys(roAlways)))
	w("Read-only only in some forms:\n\n| Command | Read-only when |\n|---|---|\n")
	for _, r := range readOnlyConditional {
		w("| `%s` | %s |\n", r.name, r.when)
	}
	w("\nAny other command is unknown, and unknown asks.\n")
	return b.String()
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func codeList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = "`" + strings.ReplaceAll(x, "|", `\|`) + "`"
	}
	return strings.Join(q, ", ")
}
