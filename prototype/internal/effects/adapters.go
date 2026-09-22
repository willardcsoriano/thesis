package effects

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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

// tarArgs finds the mode letter, archive file, -C directory, and remaining operands.
func tarArgs(args []string) (mode byte, file, dir string, rest []string) {
	setMode := func(ch byte) {
		if strings.IndexByte("cxtrudA", ch) >= 0 {
			mode = ch
		}
	}
	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idx := 1
		for j := 0; j < len(args[0]); j++ {
			switch ch := args[0][j]; ch {
			case 'f':
				if idx < len(args) {
					file = args[idx]
					idx++
				}
			case 'C':
				if idx < len(args) {
					dir = args[idx]
					idx++
				}
			default:
				setMode(ch)
			}
		}
		i = idx
	}
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			rest = append(rest, args[i+1:]...)
			return
		case a == "--file" || a == "-f":
			if i+1 < len(args) {
				i++
				file = args[i]
			}
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case a == "--directory" || a == "-C":
			if i+1 < len(args) {
				i++
				dir = args[i]
			}
		case strings.HasPrefix(a, "--directory="):
			dir = strings.TrimPrefix(a, "--directory=")
		case a == "--create":
			mode = 'c'
		case a == "--extract" || a == "--get":
			mode = 'x'
		case a == "--list":
			mode = 't'
		case a == "--append":
			mode = 'r'
		case a == "--update":
			mode = 'u'
		case a == "--diff" || a == "--compare":
			mode = 'd'
		case strings.HasPrefix(a, "--"):
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				switch ch := a[j]; ch {
				case 'f', 'C', 'T', 'X':
					// The value is what is left of this cluster, or the next argument.
					v := a[j+1:]
					if v == "" && i+1 < len(args) {
						i++
						v = args[i]
					}
					switch ch {
					case 'f':
						file = v
					case 'C':
						dir = v
					}
					j = len(a)
				default:
					setMode(ch)
				}
			}
		default:
			rest = append(rest, a)
		}
	}
	return
}

func stripComponents(args []string) int {
	for _, a := range args {
		if strings.HasPrefix(a, "--strip-components=") {
			n := 0
			for _, ch := range strings.TrimPrefix(a, "--strip-components=") {
				if ch < '0' || ch > '9' {
					return 0
				}
				n = n*10 + int(ch-'0')
			}
			return n
		}
	}
	return 0
}

func (st *state) extractInto(c *call, base string, names []string, strip int, junk, noClobber bool) {
	for _, name := range names {
		isDir := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		if junk {
			name = filepath.Base(name)
		}
		if strip > 0 {
			parts := strings.Split(name, "/")
			if len(parts) <= strip {
				continue
			}
			name = strings.Join(parts[strip:], "/")
		}
		if name == "" || name == "." {
			continue
		}
		p := filepath.Join(base, name)
		if isDir {
			if !st.exists(p) {
				st.add(c, Effect{Kind: Create, Path: p, Dir: true})
			}
			continue
		}
		if noClobber && st.exists(p) {
			continue
		}
		st.writeTo(c, p)
	}
}

func ruleTar(st *state, c *call) {
	if !st.needArgs(c) {
		return
	}
	mode, file, dir, rest := tarArgs(c.args)
	base := c.sc.wd
	if dir != "" {
		base = c.abs(dir)
	}
	switch mode {
	case 0, 't', 'd':
	case 'c', 'r', 'u', 'A':
		if file != "" && file != "-" {
			st.writeTo(c, c.abs(file))
		}
		if hasAny(c.args, "--remove-files") && file != "" {
			for _, r := range rest {
				if p := filepath.Join(base, r); st.exists(p) {
					st.add(c, Effect{Kind: Remove, Path: p}) // the archive is not the file: capture it
				}
			}
		}
	case 'x':
		if file == "" || file == "-" {
			st.unresolved(c, "the archive comes from standard input")
			return
		}
		out, ok := st.dry(c, append([]string{"tar", "-tf", c.abs(file)}, rest...))
		if !ok {
			return
		}
		st.extractInto(c, base, lines(out), stripComponents(c.args), false, hasAny(c.args, "-k", "--keep-old-files", "--skip-old-files"))
	default:
		st.opaque(c, "tar mode not modelled")
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
	base := c.sc.wd
	if d, ok := o.val("d"); ok && d != "" {
		base = c.abs(d)
	}
	out, ok := st.dry(c, []string{"unzip", "-Z1", c.abs(o.operands[0])})
	if !ok {
		return
	}
	st.extractInto(c, base, lines(out), 0, o.has("j"), o.has("n"))
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
