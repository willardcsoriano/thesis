package effects

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ---- transparent wrappers: sudo, env, nohup, timeout ... ----

type wrapperSpec struct {
	withArg     string // short options that take a value
	longWithArg []string
	positional  int  // positional arguments before the command (timeout DURATION)
	assigns     bool // skips leading VAR=value words
}

var wrappers = map[string]wrapperSpec{
	"sudo":     {withArg: "ugCDhprtTU", longWithArg: []string{"user", "group", "chdir", "host", "prompt", "role", "type"}, assigns: true},
	"doas":     {withArg: "uC"},
	"env":      {withArg: "uSC", assigns: true},
	"nohup":    {},
	"nice":     {withArg: "n"},
	"ionice":   {withArg: "cnp"},
	"timeout":  {withArg: "sk", positional: 1},
	"stdbuf":   {withArg: "ioe"},
	"setsid":   {},
	"command":  {},
	"builtin":  {},
	"exec":     {withArg: "a"},
	"unbuffer": {},
}

var assignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func (st *state) wrapper(c *call, w wrapperSpec) {
	if c.aerr != nil {
		st.unresolved(c, "arguments could not be determined: "+c.aerr.Error())
		return
	}
	if c.name == "command" && hasAny(c.args, "-v", "-V") {
		return
	}
	long := setOf(strings.Join(w.longWithArg, " "))
	args := c.args
	i := 0
scan:
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			i++
			break scan
		case strings.HasPrefix(a, "--"):
			if !strings.Contains(a, "=") && long[a[2:]] {
				i++
			}
		case len(a) > 1 && a[0] == '-':
			cl := a[1:]
			for j := 0; j < len(cl); j++ {
				if strings.ContainsRune(w.withArg, rune(cl[j])) {
					if j == len(cl)-1 {
						i++
					}
					break
				}
			}
		default:
			break scan
		}
		i++
	}
	for k := 0; k < w.positional && i < len(args); k++ {
		i++
	}
	if w.assigns {
		for i < len(args) && assignWord.MatchString(args[i]) {
			i++
		}
	}
	if i >= len(args) {
		if c.name == "sudo" && hasAny(args, "-i", "-s") {
			st.opaque(c, "opens an interactive root shell")
		}
		return
	}
	inner := c.sub(args[i:])
	if c.name == "sudo" || c.name == "doas" || c.priv {
		inner.priv = true
	}
	st.dispatch(inner)
}

// ---- sh -c ----

func (st *state) shell(c *call) {
	if !st.needArgs(c) {
		return
	}
	args := c.args
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i = len(args)
		case a == "-o" || a == "+o":
			i++
		case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "c"):
			if i+1 < len(args) {
				an := st.a.analyzeText(st.ctx, args[i+1], c.sc.clone(), st.depth+1, st.virt)
				st.an.merge(an)
				return
			}
			return
		case strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+"):
		default:
			if strings.HasPrefix(a, "/dev/fd/") {
				st.unrec(c, "executes code from a process substitution")
				return
			}
			st.opaque(c, "runs the script file "+a+", which is not modelled")
			return
		}
	}
	if c.pc != nil && c.pc.idx > 0 {
		st.unrec(c, "executes code piped in from another command")
		return
	}
	st.opaque(c, "starts a shell whose input is not visible")
}

// ---- find ----

var findActionTokens = setOf("-delete -exec -execdir -ok -okdir -fprint -fprint0 -fprintf -fls")

type findAct struct {
	kind  string // delete | exec | execdir
	inner []string
	plus  bool
}

func splitFind(args []string) (pre, paths, expr []string) {
	i := 0
	for i < len(args) && (args[i] == "-H" || args[i] == "-L" || args[i] == "-P" || strings.HasPrefix(args[i], "-D") || strings.HasPrefix(args[i], "-O")) {
		pre = append(pre, args[i])
		i++
	}
	for i < len(args) && !isFindExprStart(args[i]) {
		paths = append(paths, args[i])
		i++
	}
	return pre, paths, args[i:]
}

func isFindExprStart(a string) bool {
	return strings.HasPrefix(a, "-") || a == "(" || a == ")" || a == "!" || a == ","
}

// rewriteFind replaces each action that would change something with a -printf that
// prints which action fired and on what, so the same expression resolves its own
// targets. Actions that only print become -true. It returns the argv tail, the
// actions in order, and files that -fprint-style actions would write.
func rewriteFind(expr []string) (out []string, acts []findAct, files []string, err error) {
	for j := 0; j < len(expr); j++ {
		t := expr[j]
		switch t {
		case "-delete":
			out = append(out, "-printf", fmt.Sprintf("%d\\t%%p\\0", len(acts)))
			acts = append(acts, findAct{kind: "delete"})
		case "-exec", "-execdir", "-ok", "-okdir":
			k := j + 1
			for k < len(expr) && expr[k] != ";" && expr[k] != "+" {
				k++
			}
			if k >= len(expr) || k == j+1 {
				return nil, nil, nil, fmt.Errorf("unterminated %s", t)
			}
			kind := "exec"
			if t == "-execdir" || t == "-okdir" {
				kind = "execdir"
			}
			out = append(out, "-printf", fmt.Sprintf("%d\\t%%p\\0", len(acts)))
			acts = append(acts, findAct{kind: kind, inner: expr[j+1 : k], plus: expr[k] == "+"})
			j = k
		case "-fprint", "-fprint0", "-fls":
			if j+1 >= len(expr) {
				return nil, nil, nil, fmt.Errorf("%s needs a file", t)
			}
			files = append(files, expr[j+1])
			out = append(out, "-true")
			j++
		case "-fprintf":
			if j+2 >= len(expr) {
				return nil, nil, nil, fmt.Errorf("-fprintf needs a file and a format")
			}
			files = append(files, expr[j+1])
			out = append(out, "-true")
			j += 2
		case "-print", "-print0", "-ls":
			out = append(out, "-true")
		case "-printf":
			out = append(out, "-true")
			j++
		default:
			out = append(out, t)
		}
	}
	return out, acts, files, nil
}

func (st *state) findLiterallyReadOnly(c *call) bool {
	for _, w := range c.words {
		if l := w.Lit(); l != "" && findActionTokens[l] {
			return false
		}
	}
	return true
}

func (st *state) find(c *call) {
	if c.aerr != nil {
		if !st.findLiterallyReadOnly(c) {
			st.unresolved(c, "find arguments could not be determined: "+c.aerr.Error())
		}
		return
	}
	pre, paths, expr := splitFind(c.args)
	rewritten, acts, files, err := rewriteFind(expr)
	if err != nil {
		st.unresolved(c, "could not parse the find expression: "+err.Error())
		return
	}
	for _, f := range files {
		st.writeTo(c, c.abs(f))
	}
	needRun := false
	for _, a := range acts {
		if a.kind == "delete" || len(a.inner) == 0 || !readOnlyForm(a.inner[0], a.inner[1:], true) {
			needRun = true
		}
	}
	if !needRun {
		return
	}
	if st.a.Run == nil {
		st.unresolved(c, "find's targets need resolving and resolution is disabled")
		return
	}
	argv := append(append(append([]string{"find"}, pre...), paths...), rewritten...)
	for _, t := range argv[1:] {
		if findActionTokens[t] {
			st.unresolved(c, "refusing to resolve a find that still contains action "+t)
			return
		}
	}
	if c.sc.wd == unknownWD {
		st.unresolved(c, errUnknownWD.Error())
		return
	}
	if c.priv {
		st.unresolved(c, "runs with elevated privilege, so its targets cannot be resolved as the current user")
		return
	}
	out, err := st.a.Run(st.ctx, c.sc.wd, argv)
	if err != nil {
		st.unresolved(c, "could not resolve find's targets: "+err.Error())
		return
	}
	matches := make([][]string, len(acts))
	total := 0
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		idx, path, ok := strings.Cut(string(rec), "\t")
		var n int
		if _, e := fmt.Sscanf(idx, "%d", &n); !ok || e != nil || n < 0 || n >= len(acts) {
			continue
		}
		if total++; total > st.a.MaxItems {
			st.unresolved(c, errTooManyItems.Error())
			return
		}
		matches[n] = append(matches[n], path)
	}
	before := len(st.an.Effects)
	for n, act := range acts {
		ms := matches[n]
		switch act.kind {
		case "delete":
			for _, m := range ms {
				st.add(c, Effect{Kind: Remove, Path: c.abs(m)})
			}
		default:
			st.findExec(c, act, ms)
		}
	}
	// An action that creates files inside the tree find is searching feeds itself: find
	// visits what it has just made, so the matches it resolved beforehand are not the
	// matches it will act on.
	roots := paths
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, e := range st.an.Effects[before:] {
		if e.Kind != Create {
			continue
		}
		for _, r := range roots {
			if root := c.abs(r); e.Path == root || strings.HasPrefix(e.Path, root+"/") {
				st.unresolved(c, "creates files inside the tree it is searching, so the set of matches changes while it runs")
				return
			}
		}
	}
}

func (st *state) findExec(c *call, act findAct, ms []string) {
	if len(ms) == 0 || len(act.inner) == 0 {
		return
	}
	if act.plus {
		var argv []string
		for _, tok := range act.inner {
			if tok == "{}" {
				argv = append(argv, ms...)
			} else {
				argv = append(argv, tok)
			}
		}
		st.dispatch(c.sub(argv))
		return
	}
	for _, m := range ms {
		p, sc := m, c.sc
		if act.kind == "execdir" {
			abs := c.abs(m)
			p, sc = "./"+filepath.Base(abs), c.sc.clone()
			sc.wd = filepath.Dir(abs)
		}
		argv := make([]string, len(act.inner))
		for i, tok := range act.inner {
			argv[i] = strings.ReplaceAll(tok, "{}", p)
		}
		sub := c.sub(argv)
		sub.sc = sc
		st.dispatch(sub)
	}
}

// ---- xargs ----

func (st *state) xargs(c *call) {
	if !st.needArgs(c) {
		return
	}
	args := c.args
	var (
		null    bool
		repl    string
		delim   string
		argFile string
		noRun   bool // -r: do nothing on empty input
		chunk   int  // -n: arguments per invocation
		lineCap int  // -L: input lines per invocation
		i       int
	)
scan:
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			i++
			break scan
		case a == "-0" || a == "--null":
			null = true
		case a == "-r" || a == "--no-run-if-empty":
			noRun = true
		case a == "-I" && i+1 < len(args):
			i++
			repl = args[i]
		case strings.HasPrefix(a, "-I") && len(a) > 2:
			repl = a[2:]
		case a == "-i" || a == "--replace":
			repl = "{}"
		case strings.HasPrefix(a, "--replace="):
			repl = strings.TrimPrefix(a, "--replace=")
		case a == "-d" || a == "--delimiter":
			i++
			if i < len(args) {
				delim = args[i]
			}
		case strings.HasPrefix(a, "--delimiter="):
			delim = strings.TrimPrefix(a, "--delimiter=")
		case a == "-a" || a == "--arg-file":
			i++
			if i < len(args) {
				argFile = args[i]
			}
		case a == "-n" || a == "--max-args":
			i++
			if i < len(args) {
				chunk, _ = strconv.Atoi(args[i])
			}
		case strings.HasPrefix(a, "--max-args="):
			chunk, _ = strconv.Atoi(strings.TrimPrefix(a, "--max-args="))
		case a == "-L":
			i++
			if i < len(args) {
				lineCap, _ = strconv.Atoi(args[i])
			}
		case a == "-P" || a == "-s" || a == "-E" || a == "--max-procs" || a == "--max-chars" || a == "--eof" || a == "--max-lines":
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// A cluster of short flags such as -0i or -0n1: read them one by one.
			cl := a[1:]
		flags:
			for k := 0; k < len(cl); k++ {
				rest := cl[k+1:]
				switch cl[k] {
				case '0':
					null = true
				case 'r':
					noRun = true
				case 'i':
					repl = "{}"
					if rest != "" {
						repl = rest
					}
					break flags
				case 'I':
					if rest != "" {
						repl = rest
					} else if i+1 < len(args) {
						i++
						repl = args[i]
					}
					break flags
				case 'n', 'L':
					v := rest
					if v == "" && i+1 < len(args) {
						i++
						v = args[i]
					}
					n, _ := strconv.Atoi(v)
					if cl[k] == 'n' {
						chunk = n
					} else {
						lineCap = n
					}
					break flags
				case 'd', 'a', 'P', 's', 'E':
					v := rest
					if v == "" && i+1 < len(args) {
						i++
						v = args[i]
					}
					switch cl[k] {
					case 'd':
						delim = v
					case 'a':
						argFile = v
					}
					break flags
				}
			}
		default:
			break scan
		}
		i++
	}
	cmd := args[i:]
	if len(cmd) == 0 || readOnlyForm(cmd[0], cmd[1:], true) {
		return
	}
	var data []byte
	switch {
	case argFile != "":
		b, err := os.ReadFile(c.abs(argFile))
		if err != nil {
			st.unresolved(c, "cannot read xargs' argument file")
			return
		}
		data = b
	case c.pc != nil && c.pc.idx > 0:
		producer := st.src[c.pc.stages[0].Pos().Offset():c.pc.stages[c.pc.idx-1].End().Offset()]
		pa := st.a.analyzeText(st.ctx, producer, c.sc.clone(), st.depth+1, newOverlay())
		if !pa.ReadOnly() {
			st.unresolved(c, "the command feeding xargs is not provably read-only, so its output cannot be resolved")
			return
		}
		if st.a.Run == nil {
			st.unresolved(c, "xargs' input needs resolving and resolution is disabled")
			return
		}
		if c.priv || hasPrivilege(producer) {
			st.unresolved(c, "runs with elevated privilege, so its input cannot be resolved as the current user")
			return
		}
		if c.sc.wd == unknownWD {
			st.unresolved(c, errUnknownWD.Error())
			return
		}
		if nondeterministic.MatchString(producer) || st.readsWhatTheLineWrites(producer) {
			st.unresolved(c, "the command feeding xargs gives different output when the line runs than it does now")
			return
		}
		out, err := st.a.Run(st.ctx, c.sc.wd, []string{"bash", "-c", varPrefix(c.sc) + producer})
		if err != nil {
			st.unresolved(c, "could not resolve xargs' input: "+err.Error())
			return
		}
		data = out
	default:
		st.unresolved(c, "xargs reads input from somewhere the analysis cannot see")
		return
	}
	if !null && bytes.IndexByte(data, 0) >= 0 {
		// GNU xargs without -0 cuts each line at its first NUL, so only the text before
		// it is used; a -print0 list piped in without -0 therefore yields one item.
		var keep [][]byte
		for _, ln := range bytes.Split(data, []byte{'\n'}) {
			if k := bytes.IndexByte(ln, 0); k >= 0 {
				ln = ln[:k]
			}
			keep = append(keep, ln)
		}
		data = bytes.Join(keep, []byte{'\n'})
	}
	items := splitItems(data, null, delim, repl != "")
	if len(items) > st.a.MaxItems {
		st.unresolved(c, errTooManyItems.Error())
		return
	}
	if len(items) == 0 {
		// GNU xargs runs the command once with no arguments unless told not to, or
		// unless it was asked to substitute each item into it.
		if !noRun && repl == "" {
			st.dispatch(c.sub(append([]string{}, cmd...)))
		}
		return
	}
	if repl != "" {
		for _, it := range items {
			argv := make([]string, len(cmd))
			for k, tok := range cmd {
				argv[k] = strings.ReplaceAll(tok, repl, it)
			}
			st.dispatch(c.sub(argv))
		}
		return
	}
	if lineCap > 0 && chunk == 0 {
		// -L counts input lines; the input has been split into items, so run once per
		// line's worth of items.
		lc := 0
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(ln) != "" {
				lc++
			}
		}
		if lc > 0 {
			chunk = ((len(items) + lc - 1) / lc) * lineCap
		}
	}
	if chunk <= 0 || chunk >= len(items) {
		st.dispatch(c.sub(append(append([]string{}, cmd...), items...)))
		return
	}
	for k := 0; k < len(items); k += chunk {
		end := min(k+chunk, len(items))
		st.dispatch(c.sub(append(append([]string{}, cmd...), items[k:end]...)))
	}
}

func splitItems(data []byte, null bool, delim string, lines bool) []string {
	var parts []string
	switch {
	case null:
		for _, p := range bytes.Split(data, []byte{0}) {
			parts = append(parts, string(p))
		}
	case delim != "":
		d := delim
		if d == `\n` {
			d = "\n"
		}
		parts = strings.Split(string(data), d)
	case lines:
		parts = strings.Split(string(data), "\n")
	default:
		return shellFields(string(data))
	}
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// shellFields splits on blanks the way xargs does, honouring quotes and backslashes.
func shellFields(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	var quote rune
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, in = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}
