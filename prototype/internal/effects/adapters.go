package effects

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A dry-run adapter learns what a command would touch by running a form of that
// command which cannot change anything: git clean -n, rsync -n, tar -t, unzip -Z1,
// git diff --name-only. Each is fixed here and never assembled from user text
// beyond its operands.

// dry runs argv in the call's directory and returns its output, or records why it
// could not.
func (st *state) dry(c *call, argv []string) ([]byte, bool) {
	if st.a.Run == nil {
		st.unresolved(c, "resolution is disabled")
		return nil, false
	}
	if c.sc.wd == unknownWD {
		st.unresolved(c, errUnknownWD.Error())
		return nil, false
	}
	if c.priv {
		st.unresolved(c, "runs with elevated privilege, so its targets cannot be resolved as the current user")
		return nil, false
	}
	out, err := st.a.Run(st.ctx, c.sc.wd, argv)
	if err != nil {
		st.unresolved(c, "could not resolve targets with "+argv[0]+": "+err.Error())
		return nil, false
	}
	return out, true
}

func lines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// ---- tar ----

// tarOperand is a name on a tar command line and the -C directory in effect where it
// appears (empty for the working directory). GNU tar applies each -C to what follows it.
type tarOperand struct{ name, dir string }

// tarCmd is a parsed tar command line.
type tarCmd struct {
	mode     byte // c x t r u d A, or D for --delete
	file     string
	dir      string // the last -C
	operands []tarOperand
	short    map[byte]string   // short options seen, with their values
	long     map[string]string // long options seen, with their values
	strip    int               // --strip-components; -1 when it could not be read
}

// tarValueLetters are GNU tar's short options that take a value.
const tarValueLetters = "fCTXbHIKLNVFg"

// parseTar reads a GNU tar command line: the old bundled first argument (cvzf a.tar),
// short clusters, and long options, each -C in order.
func parseTar(args []string) tarCmd {
	t := tarCmd{short: map[byte]string{}, long: map[string]string{}}
	setMode := func(ch byte) {
		if strings.IndexByte("cxtrudA", ch) >= 0 {
			t.mode = ch
		}
	}
	short := func(ch byte, v string) {
		t.short[ch] = v
		switch ch {
		case 'f':
			t.file = v
		case 'C':
			if t.dir != "" && !filepath.IsAbs(v) {
				v = filepath.Join(t.dir, v)
			}
			t.dir = v
		default:
			setMode(ch)
		}
	}
	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idx := 1
		for j := 0; j < len(args[0]); j++ {
			ch := args[0][j]
			v := ""
			if strings.IndexByte(tarValueLetters, ch) >= 0 && idx < len(args) {
				v = args[idx]
				idx++
			}
			short(ch, v)
		}
		i = idx
	}
	longValue := setOf(`file directory files-from exclude-from exclude transform xform strip-components to-command
use-compress-program checkpoint-action info-script new-volume-script rsh-command rmt-command listed-incremental
blocking-factor format label owner group mode mtime newer after-date newer-mtime tape-length`)
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			for _, r := range args[i+1:] {
				t.operands = append(t.operands, tarOperand{r, t.dir})
			}
			i = len(args)
		case strings.HasPrefix(a, "--"):
			name, val, has := strings.Cut(a[2:], "=")
			if !has && longValue[name] && i+1 < len(args) {
				i++
				val = args[i]
			}
			t.long[name] = val
			switch name {
			case "file":
				t.file = val
			case "directory":
				short('C', val)
			case "create":
				t.mode = 'c'
			case "extract", "get":
				t.mode = 'x'
			case "list":
				t.mode = 't'
			case "append":
				t.mode = 'r'
			case "update":
				t.mode = 'u'
			case "diff", "compare":
				t.mode = 'd'
			case "concatenate", "catenate":
				t.mode = 'A'
			case "delete":
				t.mode = 'D'
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				ch := a[j]
				if strings.IndexByte(tarValueLetters, ch) < 0 {
					short(ch, "")
					continue
				}
				// The value is what is left of this cluster, or the next argument.
				v := a[j+1:]
				if v == "" && i+1 < len(args) {
					i++
					v = args[i]
				}
				short(ch, v)
				j = len(a)
			}
		default:
			t.operands = append(t.operands, tarOperand{a, t.dir})
		}
	}
	if v, ok := t.long["strip-components"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			n = -1
		}
		t.strip = n
	}
	return t
}

// tarArgs finds the mode letter, archive file, -C directory, and remaining operands.
func tarArgs(args []string) (mode byte, file, dir string, rest []string) {
	t := parseTar(args)
	for _, o := range t.operands {
		rest = append(rest, o.name)
	}
	return t.mode, t.file, t.dir, rest
}

func (t tarCmd) has(short byte, long ...string) bool {
	if _, ok := t.short[short]; ok && short != 0 {
		return true
	}
	for _, l := range long {
		if _, ok := t.long[l]; ok {
			return true
		}
	}
	return false
}

// compressorNames are the programs tar -I may name without the analysis asking: each
// only filters standard input to standard output when tar runs it.
var compressorNames = setOf(`gzip pigz bzip2 pbzip2 lbzip2 xz pixz zstd pzstd lz4 lzip plzip lzop lzma brotli`)

func ruleTar(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	t := parseTar(c.args)
	if t.mode == 0 || t.mode == 't' || t.mode == 'd' {
		return
	}
	// Options that make tar run another program, whose effects are not visible here.
	for _, l := range []string{"to-command", "info-script", "new-volume-script", "rsh-command", "rmt-command"} {
		if _, ok := t.long[l]; ok {
			st.opaque(c, "tar --"+l+" runs a program whose effects are not modelled")
			return
		}
	}
	if _, ok := t.short['F']; ok {
		st.opaque(c, "tar -F runs a script whose effects are not modelled")
		return
	}
	if strings.Contains(t.long["checkpoint-action"], "exec") {
		st.opaque(c, "tar --checkpoint-action=exec runs a command whose effects are not modelled")
		return
	}
	prog, isProg := t.short['I']
	if v, ok := t.long["use-compress-program"]; ok {
		prog, isProg = v, true
	}
	if f := strings.Fields(prog); isProg && (len(f) == 0 || !compressorNames[filepath.Base(f[0])]) {
		st.opaque(c, "tar -I runs a program that is not a known compressor")
		return
	}
	if t.file != "" && t.file != "-" && !t.has(0, "force-local") {
		if host, _, remote := strings.Cut(t.file, ":"); remote && !strings.Contains(host, "/") {
			st.unrec(c, "the archive is on a remote host")
			return
		}
	}
	at := func(o tarOperand) string {
		if o.dir == "" {
			return c.abs(o.name)
		}
		return absIn(&scope{wd: c.abs(o.dir)}, o.name)
	}
	switch t.mode {
	case 'c', 'r', 'u', 'A', 'D':
		if t.file != "" && t.file != "-" {
			st.writeTo(c, c.abs(t.file))
		}
		if g, ok := t.short['g']; ok && g != "" {
			st.writeTo(c, c.abs(g))
		} else if g, ok := t.long["listed-incremental"]; ok && g != "" {
			st.writeTo(c, c.abs(g))
		}
		if t.mode == 'c' || t.mode == 'r' || t.mode == 'u' {
			if t.has(0, "remove-files") {
				if t.has('T', "files-from") {
					st.unresolved(c, "tar --remove-files with names read from a file: which files it deletes cannot be listed")
					return
				}
				// Whatever the archive goes to (a file, standard output, the default
				// device), the inputs are deleted once archived. The archive holds a
				// copy, not the same bytes, so each removal is captured.
				for _, o := range t.operands {
					if p := at(o); st.exists(p) {
						st.add(c, Effect{Kind: Remove, Path: p})
					}
				}
			}
		}
	case 'x':
		if t.file == "" || t.file == "-" {
			st.unresolved(c, "the archive comes from standard input")
			return
		}
		if t.has('P', "absolute-names") || t.has(0, "transform", "xform") || t.strip < 0 {
			st.unresolved(c, "tar rewrites or keeps the member names, so where it extracts cannot be listed")
			return
		}
		if !st.archiveReadable(c, c.abs(t.file)) {
			return
		}
		var members []string
		for _, o := range t.operands {
			members = append(members, o.name)
		}
		out, ok := st.dry(c, append([]string{"tar", "-tf", c.abs(t.file)}, members...))
		if !ok {
			return
		}
		base := c.sc.wd
		if t.dir != "" {
			base = c.abs(t.dir)
		}
		if t.has('O', "to-stdout") {
			return // members go to standard output
		}
		keep := t.has('k', "keep-old-files", "skip-old-files")
		st.extractInto(c, base, lines(out), extractOpts{strip: t.strip, noClobber: keep, replaces: true, recursiveUnlink: t.has(0, "recursive-unlink")})
	default:
		st.opaque(c, "tar mode not modelled")
	}
}

// extractOpts says how an extractor places members.
type extractOpts struct {
	strip     int  // leading components dropped (tar --strip-components)
	junk      bool // directories dropped (unzip -j)
	noClobber bool // existing files are kept (tar -k, unzip -n)
	// replaces is set for tar, which replaces a file or symlink where the archive has a
	// directory; unzip refuses instead.
	replaces bool
	// recursiveUnlink: tar removes a directory tree where the archive has a file.
	recursiveUnlink bool
}

func (st *state) extractInto(c *call, base string, names []string, o extractOpts) {
	st.makeDirs(c, base)
	for _, name := range names {
		isDir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		if o.junk {
			if isDir {
				continue
			}
			name = filepath.Base(name)
		}
		if o.strip > 0 {
			parts := strings.Split(name, "/")
			if len(parts) <= o.strip {
				continue
			}
			name = strings.Join(parts[o.strip:], "/")
		}
		if name == "" || name == "." {
			continue
		}
		p := filepath.Join(base, name)
		st.makeDirs(c, filepath.Dir(p))
		fi, exists := st.lstat(p)
		if isDir {
			if exists && !fi.IsDir() && o.replaces {
				// tar removes a file or symlink that stands where the archive has a
				// directory: that is a deletion, and is captured.
				st.add(c, Effect{Kind: Remove, Path: p})
				exists = false
			}
			if !exists {
				st.add(c, Effect{Kind: Create, Path: p, Dir: true})
			}
			continue
		}
		if exists && fi.IsDir() {
			// A directory where the archive has a file: tar removes it only when it is
			// empty, or with --recursive-unlink; otherwise the member fails.
			if o.replaces && (o.recursiveUnlink || emptyDir(p)) {
				st.add(c, Effect{Kind: Remove, Path: p})
				st.add(c, Effect{Kind: Create, Path: p})
			}
			continue
		}
		if o.noClobber && exists {
			continue
		}
		st.writeTo(c, p)
	}
}

// makeDirs records the creation of each missing directory down to p, which an
// extractor makes as it goes. Undo removes them again.
func (st *state) makeDirs(c *call, p string) {
	var missing []string
	for q := p; q != "/" && q != "."; q = filepath.Dir(q) {
		if st.exists(q) {
			break
		}
		missing = append(missing, q)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		st.add(c, Effect{Kind: Create, Path: missing[i], Dir: true})
	}
}

func ruleUnzip(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	o := parseOpts(c.args, "dxP")
	if len(o.operands) == 0 {
		return
	}
	if o.has("L", "B") {
		// -L lowercases member names, and -B renames each file it replaces to NAME~,
		// overwriting any NAME~ already there.
		st.unresolved(c, "unzip -L or -B changes the names it writes")
		return
	}
	base := c.sc.wd
	if d, ok := o.val("d"); ok && d != "" {
		base = c.abs(d)
	}
	if !st.archiveReadable(c, c.abs(o.operands[0])) {
		return
	}
	out, ok := st.dry(c, []string{"unzip", "-Z1", c.abs(o.operands[0])})
	if !ok {
		return
	}
	st.extractInto(c, base, lines(out), extractOpts{junk: o.has("j"), noClobber: o.has("n")})
}

// archiveReadable reports whether an archive can be listed now as it will be when the
// command runs. Listing one that does not exist yet, or that an earlier part of the
// line creates or replaces, fails with no output, which would read as "extracts
// nothing" and let the extraction overwrite files unprotected.
func (st *state) archiveReadable(c *call, p string) bool {
	if _, made := st.virt.created[p]; made || !st.exists(p) {
		st.unresolved(c, "the archive does not exist yet, so what it would extract cannot be listed")
		return false
	}
	for _, e := range st.an.Effects {
		if e.Path == p && e.Kind == Write {
			st.unresolved(c, "an earlier part of the line rewrites the archive, so what it would extract cannot be listed")
			return false
		}
	}
	return true
}

// ---- rsync ----

var itemizeLine = regexp.MustCompile(`^([<>ch.*][a-zA-Z.?+]{10}) (.+)$`)

func ruleRsync(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	if hasAny(c.args, "-n", "--dry-run") {
		return
	}
	if hasAny(c.args, "--remove-source-files") || hasPrefixAny(c.args, "-e", "--rsh", "--rsync-path") {
		st.unresolved(c, "rsync option that changes the source or runs a remote command")
		return
	}
	// These write their file even under --dry-run, so the dry run that resolves the
	// targets would not be read-only.
	if hasPrefixAny(c.args, "--log-file", "--write-batch", "--only-write-batch") {
		st.unresolved(c, "rsync option that writes a file even in a dry run")
		return
	}
	o := parseOpts(c.args, "ehfBT", "rsh", "exclude", "include", "filter", "files-from", "backup-dir", "log-file", "port",
		"bwlimit", "partial-dir", "temp-dir", "link-dest", "compare-dest", "copy-dest", "timeout", "chmod", "chown", "exclude-from", "include-from")
	ops := o.operands
	if len(ops) < 2 {
		return
	}
	for _, op := range ops {
		if strings.Contains(op, ":") {
			st.unrec(c, "rsync to or from a remote host")
			return
		}
	}
	argv := append([]string{"rsync"}, c.args...)
	argv = append(argv, "--dry-run", "--itemize-changes")
	out, ok := st.dry(c, argv)
	if !ok {
		return
	}
	dest := c.abs(ops[len(ops)-1])
	// A destination that is a file (or, for a single file source, a name that does not
	// exist yet) is the file itself, not a directory to copy into.
	if fi, err := os.Lstat(dest); err == nil && !fi.IsDir() {
		for _, l := range lines(out) {
			if itemizeLine.MatchString(l) {
				st.add(c, Effect{Kind: Write, Path: dest})
				return
			}
		}
		return
	} else if err != nil && len(ops) == 2 && !strings.HasSuffix(ops[1], "/") {
		if sfi, serr := os.Lstat(c.abs(ops[0])); serr == nil && !sfi.IsDir() {
			st.add(c, Effect{Kind: Create, Path: dest})
			return
		}
	}
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "*deleting") {
			name := strings.TrimSpace(strings.TrimPrefix(l, "*deleting"))
			st.add(c, Effect{Kind: Remove, Path: filepath.Join(dest, strings.TrimSuffix(name, "/"))})
			continue
		}
		m := itemizeLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(m[2], " -> ", 2)[0])
		p := filepath.Join(dest, strings.TrimSuffix(name, "/"))
		if name == "./" || name == "." {
			continue
		}
		if strings.HasPrefix(m[1][2:], "++") {
			st.add(c, Effect{Kind: Create, Path: p})
		} else if m[1][0] == '>' || m[1][0] == '<' || m[1][0] == 'c' && !strings.HasSuffix(name, "/") {
			st.add(c, Effect{Kind: Write, Path: p})
		}
	}
}

// ---- rename ----

// renamedLine matches the dry-run output of the two tools called rename: util-linux prints
// "a renamed as b" and Debian's Perl rename prints "rename(a, b)".
var renamedLine = regexp.MustCompile("^(?:[`']?(.+?)[`']? (?:renamed as|->) [`']?(.+?)[`']?|rename\\((.+), (.+)\\))$")

func ruleRename(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	if hasAny(c.args, "-n", "--dry-run", "--no-act") {
		return
	}
	// Perl's rename reports what it would do on standard error, so both streams are read.
	// -n goes first: after the operands, Perl's rename takes it for a file name and does nothing.
	out, ok := st.dry(c, append([]string{"bash", "-c", `"$@" 2>&1`, "_", c.name, "-n"}, c.args...))
	if !ok {
		return
	}
	ls := lines(out)
	matched := 0
	for _, l := range ls {
		m := renamedLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		matched++
		a, b := m[1], m[2]
		if a == "" {
			a, b = m[3], m[4]
		}
		from, to := c.abs(a), c.abs(b)
		if st.exists(to) {
			st.add(c, Effect{Kind: Write, Path: to})
		} else {
			st.add(c, Effect{Kind: Create, Path: to})
		}
		st.add(c, Effect{Kind: Remove, Path: from, MovedTo: to})
	}
	if len(ls) > 0 && matched == 0 {
		st.unresolved(c, "rename's dry-run output was not understood")
	}
}

// ---- git ----

func gitSplit(args []string) (dir, sub string, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" && i+1 < len(args):
			i++
			dir = args[i]
		case a == "-c" && i+1 < len(args):
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return dir, a, args[i+1:]
		}
	}
	return dir, "", nil
}

var gitReadSubs = setOf(`status log diff show ls-files ls-tree blame rev-parse rev-list describe grep shortlog whatchanged
cat-file for-each-ref name-rev merge-base show-ref ls-remote count-objects diff-tree diff-files diff-index version help
verify-commit verify-tag fsck annotate range-diff cherry --version`)

func gitReadOnly(args []string) bool {
	_, sub, rest := gitSplit(args)
	if sub == "" || gitReadSubs[sub] {
		return true
	}
	o := parseOpts(rest, "")
	switch sub {
	case "branch":
		return len(o.operands) == 0 || o.has("l", "list", "a", "r", "v", "vv", "show-current", "contains")
	case "remote":
		f := firstOperand(rest)
		return f == "" || f == "show" || f == "get-url" || o.has("v")
	case "tag":
		return len(o.operands) == 0 || o.has("l", "list")
	case "config":
		return o.has("get", "get-all", "list", "l", "get-regexp", "get-urlmatch")
	case "stash":
		f := firstOperand(rest)
		return f == "list" || f == "show"
	case "reflog":
		f := firstOperand(rest)
		return f != "expire" && f != "delete"
	case "worktree":
		return firstOperand(rest) == "list"
	case "submodule":
		f := firstOperand(rest)
		return f == "" || f == "status" || f == "summary"
	}
	return false
}

func repoRoot(from string) string {
	for d := from; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if d == "/" || d == "." {
			return ""
		}
	}
}

// gitSafe names the -c options that keep a read-only git query from running
// anything configured in the repository.
var gitSafe = []string{"git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "diff.external=", "--no-pager"}

var gitLocalSafe = setOf(`add commit fetch init clone stash tag remote config notes worktree submodule branch switch push`)

func ruleGit(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	dirOpt, sub, rest := gitSplit(c.args)
	wd := c.sc.wd
	if dirOpt != "" {
		wd = c.abs(dirOpt)
	}
	root := repoRoot(wd)
	o := parseOpts(rest, "")
	switch sub {
	case "reset":
		if !o.has("hard") {
			return
		}
		if root == "" {
			return
		}
		st.add(c, Effect{Kind: Meta, Path: root, Hint: "git-head"})
		sc := c.sc.clone()
		sc.wd = wd
		cc := *c
		cc.sc = sc
		if out, ok := st.dry(&cc, append(append([]string{}, gitSafe...), "diff", "--name-only", "-z", "--no-ext-diff", "--no-textconv", "HEAD")); ok {
			for _, f := range bytes.Split(out, []byte{0}) {
				if len(f) == 0 {
					continue
				}
				if p := filepath.Join(root, string(f)); st.exists(p) {
					st.add(c, Effect{Kind: Write, Path: p})
				}
			}
		}
	case "clean":
		if o.has("n", "dry-run") || root == "" {
			return
		}
		argv := append([]string{}, gitSafe...)
		argv = append(argv, "clean", "-n")
		for _, a := range rest {
			switch {
			case a == "--":
			case strings.HasPrefix(a, "--"):
			case strings.HasPrefix(a, "-"):
				if fl := strings.Map(func(r rune) rune {
					if strings.ContainsRune("dxXq", r) {
						return r
					}
					return -1
				}, a); fl != "" {
					argv = append(argv, "-"+fl)
				}
			}
		}
		argv = append(argv, "--")
		argv = append(argv, o.operands...)
		sc := c.sc.clone()
		sc.wd = wd
		cc := *c
		cc.sc = sc
		out, ok := st.dry(&cc, argv)
		if !ok {
			return
		}
		for _, l := range lines(out) {
			if p, found := strings.CutPrefix(l, "Would remove "); found {
				st.add(c, Effect{Kind: Remove, Path: filepath.Join(wd, strings.TrimSuffix(p, "/"))})
			}
		}
	case "checkout", "restore", "switch":
		if root == "" || o.has("staged") && !o.has("worktree", "W") {
			return
		}
		if (sub == "checkout" && o.has("b", "B")) || (sub == "switch" && o.has("c", "C", "create", "force-create")) {
			st.gitNewBranch(c, root, firstOperand(rest), o.has("B", "C", "force-create"))
			return
		}
		var paths []string
		afterDash := false
		for _, a := range rest {
			if a == "--" {
				afterDash = true
				continue
			}
			if strings.HasPrefix(a, "-") && !afterDash {
				continue
			}
			if afterDash || sub == "restore" || a == "." || st.exists(filepath.Join(wd, a)) {
				paths = append(paths, a)
			}
		}
		if sub == "switch" || (sub == "checkout" && len(paths) == 0 && !o.has("f", "force")) {
			if !o.has("f", "force", "discard-changes") {
				return
			}
			paths = []string{"."}
		}
		if o.has("f", "force", "discard-changes") && len(paths) == 0 {
			paths = []string{"."}
		}
		if len(paths) == 0 {
			return
		}
		st.gitDirtyWrites(c, wd, paths)
	case "stash":
		switch f := firstOperand(rest); {
		case f == "list" || f == "show":
		case f == "drop" || f == "clear":
			st.unrec(c, "git stash "+f+" discards saved work")
		case f == "" || f == "push" || f == "save":
			if o.has("u", "a", "include-untracked", "all") {
				st.unrec(c, "git stash with untracked files removes files git does not track")
			} else if root != "" {
				st.gitDirtyWrites(c, wd, []string{"."})
				// The stash entry is a ref the working-tree capture does not cover.
				st.an.States = append(st.an.States, StateChange{Subject: "git:stash", Change: "git stash",
					Inverse: [][]string{{"git", "-C", root, "stash", "drop"}}, Source: c.src})
			}
		default:
			st.unrec(c, "git stash "+f+" rewrites the working tree from the stash")
		}
	case "rm":
		if o.has("cached") {
			return
		}
		for _, op := range o.operands {
			if p := filepath.Join(wd, op); st.exists(p) {
				st.add(c, Effect{Kind: Remove, Path: p})
			}
		}
	case "mv":
		sub := *c
		sub.args = rest
		ruleMv(st, &sub)
	case "push":
		if o.has("f", "force", "force-with-lease", "delete", "d", "mirror", "prune") {
			st.unrec(c, "git push rewrites or deletes remote history")
		}
	case "pull", "merge", "rebase", "cherry-pick", "revert", "am", "apply", "filter-branch", "gc", "prune", "update-ref", "replace", "bisect":
		st.unrec(c, "git "+sub+" rewrites history or the working tree and is not modelled")
	default:
		if gitLocalSafe[sub] {
			if sub == "branch" && o.has("d", "D", "delete", "m", "M", "move", "c", "C") {
				st.unrec(c, "git branch removes or renames a branch")
			}
			if sub == "tag" && o.has("d", "delete") {
				st.unrec(c, "git tag removes a tag")
			}
			if sub == "stash" && (firstOperand(rest) == "drop" || firstOperand(rest) == "clear") {
				st.unrec(c, "git stash drop discards saved work")
			}
			if sub == "remote" && (firstOperand(rest) == "remove" || firstOperand(rest) == "rm") {
				st.unrec(c, "git remote remove discards configuration")
			}
			return
		}
		st.opaque(c, "git "+sub+" is not modelled")
	}
}

// gitDirtyWrites adds a write for every tracked file under paths that differs from
// HEAD: those are the files a checkout, restore, or stash would overwrite.
func (st *state) gitDirtyWrites(c *call, wd string, paths []string) {
	sc := c.sc.clone()
	sc.wd = wd
	cc := *c
	cc.sc = sc
	argv := append(append([]string{}, gitSafe...), "diff", "--name-only", "-z", "--relative", "--no-ext-diff", "--no-textconv", "HEAD", "--")
	out, ok := st.dry(&cc, append(argv, paths...))
	if !ok {
		return
	}
	for _, f := range bytes.Split(out, []byte{0}) {
		if len(f) > 0 {
			if p := filepath.Join(wd, string(f)); st.exists(p) {
				st.add(c, Effect{Kind: Write, Path: p})
			}
		}
	}
}

// gitNewBranch records that a command creates a branch and switches to it. The undo
// switches back and deletes the branch, which is safe because it holds no commits of
// its own yet. Forcing onto an existing branch moves it, which this does not undo.
func (st *state) gitNewBranch(c *call, root, name string, force bool) {
	if name == "" {
		st.unresolved(c, "the branch name could not be determined")
		return
	}
	if force {
		st.unrec(c, "git resets an existing branch to a new starting point")
		return
	}
	prev, err := st.readOnly(c, []string{"git", "-C", root, "symbolic-ref", "--short", "-q", "HEAD"})
	from := strings.TrimSpace(string(prev))
	if err != nil || from == "" {
		out, err2 := st.readOnly(c, []string{"git", "-C", root, "rev-parse", "HEAD"})
		if err2 != nil || strings.TrimSpace(string(out)) == "" {
			st.unresolved(c, "could not read the current branch")
			return
		}
		from = strings.TrimSpace(string(out))
	}
	st.an.States = append(st.an.States, StateChange{Subject: "git:branch:" + name, Change: "git creates branch " + name,
		Inverse: [][]string{{"git", "-C", root, "checkout", "-q", from}, {"git", "-C", root, "branch", "-D", name}}, Source: c.src})
}
