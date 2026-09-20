package effects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Runner executes an argv in a directory and returns its standard output. The
// analyser only ever hands it commands it has proven read-only. A non-zero exit
// status is not an error (grep finding nothing, find hitting a denied directory);
// failing to run, or running out of time, is.
type Runner func(ctx context.Context, wd string, argv []string) ([]byte, error)

// Analyzer analyses command lines against a working directory.
type Analyzer struct {
	WD  string
	Env []string
	// Run resolves run-time targets by executing read-only commands. Nil disables
	// resolution: anything that needs it stays unresolved, and so unrecoverable.
	Run Runner
	// MaxItems caps how many resolved targets one command may expand to.
	MaxItems int
	// MaxLoop caps loop iterations analysed one by one.
	MaxLoop int
	// MaxDepth caps nesting of substitutions, sh -c strings, and wrappers.
	MaxDepth int
}

func New(wd string) *Analyzer {
	return &Analyzer{WD: wd, Env: os.Environ(), MaxItems: 5000, MaxLoop: 300, MaxDepth: 8}
}

// unknownWD stands in for a working directory that cannot be determined (after a
// cd to a computed path). Any effect that depends on it becomes unresolved.
const unknownWD = "\x00unknown-wd"

var (
	errUnknownWD    = errors.New("working directory is unknown")
	errNotResolved  = errors.New("substitution is not provably read-only")
	errNoRunner     = errors.New("resolution is disabled")
	errTooManyItems = errors.New("too many targets")
)

// Analyze parses cmd and derives its effects. It never runs a mutating command.
func (a *Analyzer) Analyze(ctx context.Context, cmd string) *Analysis {
	return a.analyzeText(ctx, cmd, newScope(a.WD), 0, nil)
}

// overlay tracks what earlier parts of the same command line have created or
// removed, so later parts see the filesystem as it will be by then.
type overlay struct {
	created map[string]bool // path -> is a directory
	removed map[string]bool
	// movedFrom maps a path that a move created to the path the data came from. The
	// data exists nowhere else, so a later overwrite or removal of the destination
	// destroys it, and the move can no longer be undone by moving back.
	movedFrom map[string]string
}

func newOverlay() *overlay {
	return &overlay{created: map[string]bool{}, removed: map[string]bool{}, movedFrom: map[string]string{}}
}

func (o *overlay) removedUnder(p string) bool {
	for q := p; ; q = filepath.Dir(q) {
		if o.removed[q] {
			return true
		}
		if q == "/" || q == "." {
			return false
		}
	}
}

func (a *Analyzer) analyzeText(ctx context.Context, text string, sc *scope, depth int, virt *overlay) *Analysis {
	an := &Analysis{}
	if depth > a.MaxDepth {
		an.Issues = append(an.Issues, Issue{IssueOpaque, text, "nested too deeply to analyse"})
		return an
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		an.Issues = append(an.Issues, Issue{IssueOpaque, text, "does not parse as shell: " + err.Error()})
		return an
	}
	if virt == nil {
		virt = newOverlay()
	}
	st := &state{a: a, ctx: ctx, src: text, an: an, depth: depth, substs: map[*syntax.CmdSubst]*Analysis{}, virt: virt}
	st.stmts(f.Stmts, sc)
	return an
}

type scope struct {
	wd    string
	vars  map[string]string
	unset map[string]bool
}

func newScope(wd string) *scope {
	return &scope{wd: wd, vars: map[string]string{}, unset: map[string]bool{}}
}

func (s *scope) clone() *scope {
	n := &scope{wd: s.wd, vars: make(map[string]string, len(s.vars)), unset: make(map[string]bool, len(s.unset))}
	for k, v := range s.vars {
		n.vars[k] = v
	}
	for k := range s.unset {
		n.unset[k] = true
	}
	return n
}

func (s *scope) set(k, v string) { s.vars[k] = v; delete(s.unset, k) }
func (s *scope) drop(k string)   { delete(s.vars, k); s.unset[k] = true }

type pipeCtx struct {
	stages []*syntax.Stmt
	idx    int
}

type state struct {
	a      *Analyzer
	ctx    context.Context
	src    string
	an     *Analysis
	depth  int
	substs map[*syntax.CmdSubst]*Analysis
	virt   *overlay
}

func (st *state) child(depth int) *state {
	return &state{a: st.a, ctx: st.ctx, src: st.src, an: &Analysis{}, depth: depth, substs: st.substs, virt: st.virt}
}

func (st *state) text(n syntax.Node) string {
	s, e := int(n.Pos().Offset()), int(n.End().Offset())
	if s < 0 || e > len(st.src) || s > e {
		return ""
	}
	return st.src[s:e]
}

func (st *state) issue(k IssueKind, src, why string) {
	st.an.Issues = append(st.an.Issues, Issue{Kind: k, Source: src, Reason: why})
}

func (st *state) stmts(list []*syntax.Stmt, sc *scope) {
	for _, s := range list {
		st.stmt(s, sc, nil)
	}
}

func (st *state) stmt(s *syntax.Stmt, sc *scope, pc *pipeCtx) {
	for _, r := range s.Redirs {
		st.redirect(r, sc)
	}
	switch x := s.Cmd.(type) {
	case nil:
	case *syntax.CallExpr:
		st.call(x, sc, pc)
	case *syntax.BinaryCmd:
		switch x.Op {
		case syntax.Pipe, syntax.PipeAll:
			stages := flattenPipe(x)
			for i, stage := range stages {
				st.stmt(stage, sc.clone(), &pipeCtx{stages: stages, idx: i})
			}
		default:
			st.stmt(x.X, sc, nil)
			st.stmt(x.Y, sc, nil)
		}
	case *syntax.Subshell:
		st.stmts(x.Stmts, sc.clone())
	case *syntax.Block:
		st.stmts(x.Stmts, sc)
	case *syntax.IfClause:
		st.ifClause(x, sc)
	case *syntax.WhileClause:
		st.stmts(x.Cond, sc.clone())
		st.stmts(x.Do, sc.clone())
	case *syntax.ForClause:
		st.forLoop(x, sc)
	case *syntax.CaseClause:
		st.collectNode(x.Word, sc)
		for _, it := range x.Items {
			for _, p := range it.Patterns {
				st.collectNode(p, sc)
			}
			st.stmts(it.Stmts, sc.clone())
		}
	case *syntax.TimeClause:
		if x.Stmt != nil {
			st.stmt(x.Stmt, sc, pc)
		}
	case *syntax.CoprocClause:
		st.stmt(x.Stmt, sc, nil)
	case *syntax.DeclClause:
		st.declClause(x, sc)
	case *syntax.TestClause, *syntax.ArithmCmd, *syntax.LetClause:
		st.collectNode(x, sc)
	case *syntax.FuncDecl:
		// A definition runs nothing. A later call to it is an unknown command.
	default:
		st.issue(IssueOpaque, st.text(s), fmt.Sprintf("unsupported construct %T", x))
	}
}

func flattenPipe(b *syntax.BinaryCmd) []*syntax.Stmt {
	var out []*syntax.Stmt
	var walk func(s *syntax.Stmt)
	walk = func(s *syntax.Stmt) {
		if bc, ok := s.Cmd.(*syntax.BinaryCmd); ok && (bc.Op == syntax.Pipe || bc.Op == syntax.PipeAll) && len(s.Redirs) == 0 {
			walk(bc.X)
			walk(bc.Y)
			return
		}
		out = append(out, s)
	}
	walk(b.X)
	walk(b.Y)
	return out
}

func (st *state) ifClause(x *syntax.IfClause, sc *scope) {
	st.stmts(x.Cond, sc.clone())
	st.stmts(x.Then, sc.clone())
	if x.Else != nil {
		st.ifClause(x.Else, sc)
	}
}

func (st *state) forLoop(x *syntax.ForClause, sc *scope) {
	wi, ok := x.Loop.(*syntax.WordIter)
	if !ok {
		body := sc.clone()
		st.collectNode(x.Loop, sc)
		st.stmts(x.Do, body)
		return
	}
	for _, w := range wi.Items {
		st.collectNode(w, sc)
	}
	name := wi.Name.Value
	unbound := func() {
		body := sc.clone()
		body.drop(name)
		st.stmts(x.Do, body)
	}
	if !wi.InPos.IsValid() {
		unbound()
		return
	}
	items, err := st.expand(wi.Items, sc)
	if err != nil {
		unbound()
		return
	}
	if len(items) > st.a.MaxLoop {
		unbound()
		st.issue(IssueUnresolved, st.text(x), fmt.Sprintf("loop over %d items is too many to analyse one by one", len(items)))
		return
	}
	for _, it := range items {
		body := sc.clone()
		body.set(name, it)
		st.stmts(x.Do, body)
	}
}

func (st *state) declClause(x *syntax.DeclClause, sc *scope) {
	st.collectNode(x, sc)
	if v := x.Variant.Value; v != "export" && v != "declare" && v != "local" && v != "readonly" && v != "typeset" {
		return
	}
	for _, as := range x.Args {
		if as.Name == nil {
			continue
		}
		st.bind(as, sc)
	}
}

func (st *state) bind(as *syntax.Assign, sc *scope) {
	name := as.Name.Value
	if as.Value == nil || as.Array != nil {
		if as.Array != nil {
			sc.drop(name)
		}
		return
	}
	v, err := st.literal(as.Value, sc)
	if err != nil {
		sc.drop(name)
		return
	}
	if as.Append {
		v = sc.vars[name] + v
	}
	sc.set(name, v)
}

// collectNode analyses every command or process substitution inside n. Their
// effects happen when the enclosing word is expanded, whether or not the
// enclosing command is one this package understands.
func (st *state) collectNode(n syntax.Node, sc *scope) {
	if n == nil {
		return
	}
	syntax.Walk(n, func(x syntax.Node) bool {
		switch s := x.(type) {
		case *syntax.CmdSubst:
			c := st.child(st.depth + 1)
			if c.depth > st.a.MaxDepth {
				st.issue(IssueOpaque, st.text(s), "substitution nested too deeply")
				return false
			}
			c.stmts(s.Stmts, sc.clone())
			st.substs[s] = c.an
			st.an.merge(c.an)
			return false
		case *syntax.ProcSubst:
			c := st.child(st.depth + 1)
			c.stmts(s.Stmts, sc.clone())
			st.an.merge(c.an)
			return false
		}
		return true
	})
}

func (st *state) redirect(r *syntax.Redirect, sc *scope) {
	if r.Word != nil {
		st.collectNode(r.Word, sc)
	}
	if r.Hdoc != nil {
		st.collectNode(r.Hdoc, sc)
	}
	switch r.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.RdrInOut, syntax.RdrAll, syntax.AppAll, syntax.DplOut:
	default:
		return
	}
	if r.Word == nil {
		return
	}
	src := st.text(r)
	f, err := st.expand([]*syntax.Word{r.Word}, sc)
	if err != nil || len(f) != 1 {
		st.issue(IssueUnresolved, src, "redirect target could not be determined")
		return
	}
	target := f[0]
	if r.Op == syntax.DplOut && (target == "-" || isDigits(target)) {
		return
	}
	p := absIn(sc, target)
	switch {
	case strings.HasPrefix(p, "/dev/"):
		if !deviceOK(p) {
			st.issue(IssueUnrecoverable, src, "writes to a raw device "+p)
		}
		return
	case strings.HasPrefix(p, unknownWD):
		st.issue(IssueUnresolved, src, errUnknownWD.Error())
		return
	}
	kind := Create
	if st.exists(p) {
		kind = Write
	}
	st.add(&call{src: src}, Effect{Kind: kind, Path: p})
}

func deviceOK(p string) bool {
	switch p {
	case "/dev/null", "/dev/stdout", "/dev/stderr", "/dev/stdin", "/dev/tty", "/dev/zero":
		return true
	}
	return strings.HasPrefix(p, "/dev/fd/") || strings.HasPrefix(p, "/dev/pts/")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func absIn(sc *scope, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if sc.wd == unknownWD {
		return unknownWD + "/" + p
	}
	return filepath.Clean(filepath.Join(sc.wd, p))
}

// ---- expansion ----

func (st *state) env(sc *scope) []string {
	out := make([]string, 0, len(st.a.Env)+len(sc.vars))
	for _, kv := range st.a.Env {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		k := kv[:i]
		if _, ok := sc.vars[k]; ok || sc.unset[k] || k == "PWD" {
			continue
		}
		out = append(out, kv)
	}
	if sc.wd != unknownWD {
		out = append(out, "PWD="+sc.wd)
	}
	for k, v := range sc.vars {
		out = append(out, k+"="+v)
	}
	return out
}

func (st *state) cfg(sc *scope) *expand.Config {
	return &expand.Config{
		Env:     expand.ListEnviron(st.env(sc)...),
		NoUnset: true,
		ReadDir2: func(p string) ([]fs.DirEntry, error) {
			if sc.wd == unknownWD {
				return nil, errUnknownWD
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(sc.wd, p)
			}
			return os.ReadDir(p)
		},
		CmdSubst:  func(w io.Writer, cs *syntax.CmdSubst) error { return st.runSubst(w, cs, sc) },
		ProcSubst: func(*syntax.ProcSubst) (string, error) { return "/dev/fd/63", nil },
	}
}

func (st *state) expand(words []*syntax.Word, sc *scope) ([]string, error) {
	return expand.Fields(st.cfg(sc), words...)
}

func (st *state) literal(w *syntax.Word, sc *scope) (string, error) {
	return expand.Literal(st.cfg(sc), w)
}

// runSubst resolves a command substitution by running it, but only if the
// analysis of its body found no effects and nothing unresolved.
func (st *state) runSubst(w io.Writer, cs *syntax.CmdSubst, sc *scope) error {
	info := st.substs[cs]
	if info == nil || !info.ReadOnly() {
		return errNotResolved
	}
	if st.a.Run == nil {
		return errNoRunner
	}
	if sc.wd == unknownWD {
		return errUnknownWD
	}
	var body bytes.Buffer
	if err := syntax.NewPrinter().Print(&body, &syntax.File{Stmts: cs.Stmts}); err != nil {
		return err
	}
	if hasPrivilege(body.String()) {
		return errNotResolved
	}
	out, err := st.a.Run(st.ctx, sc.wd, []string{"bash", "-c", varPrefix(sc) + body.String()})
	if err != nil {
		return err
	}
	_, err = w.Write(bytes.TrimRight(out, "\n"))
	return err
}

func varPrefix(sc *scope) string {
	var b strings.Builder
	for k, v := range sc.vars {
		b.WriteString("export " + k + "=" + shQuote(v) + "; ")
	}
	return b.String()
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ---- commands ----

func (st *state) call(c *syntax.CallExpr, sc *scope, pc *pipeCtx) {
	st.collectNode(c, sc)
	if len(c.Args) == 0 {
		for _, as := range c.Assigns {
			st.bind(as, sc)
		}
		return
	}
	src := st.text(c)
	name, err := st.expand(c.Args[:1], sc)
	if err != nil || len(name) != 1 {
		st.issue(IssueOpaque, src, "the command name is computed at run time")
		return
	}
	args, aerr := st.expand(c.Args[1:], sc)
	st.dispatch(&call{name: name[0], args: args, aerr: aerr, src: src, sc: sc, pc: pc, words: c.Args[1:]})
}

// call is one command about to be interpreted.
type call struct {
	name  string
	args  []string
	aerr  error // set when the arguments could not be expanded
	src   string
	sc    *scope
	pc    *pipeCtx
	words []*syntax.Word
	// priv is set when the command runs under sudo, doas, or similar. Resolving its
	// targets as the current user could miss what root can reach and read as "nothing
	// found", so such commands are never resolved.
	priv bool
}

func (c *call) abs(p string) string { return absIn(c.sc, p) }

func (st *state) add(c *call, e Effect) {
	if strings.HasPrefix(e.Path, unknownWD) {
		st.issue(IssueUnresolved, c.src, errUnknownWD.Error())
		return
	}
	e.Source = c.src
	if e.Hint == "" && (e.Kind == Write || e.Kind == Remove) && st.destroyMoved(c, &e) {
		return
	}
	if e.Hint == "" && (e.Kind == Write || e.Kind == Remove || e.Kind == Meta) {
		if _, made := st.virt.created[e.Path]; made {
			// The same command line created this path, so there is no earlier
			// content to lose and nothing to capture.
			if e.Kind == Remove {
				st.unmake(e.Path)
			}
			return
		}
	}
	switch e.Kind {
	case Create:
		delete(st.virt.removed, e.Path)
		st.virt.created[e.Path] = e.Dir
	case Remove:
		st.unmake(e.Path)
		st.virt.removed[e.Path] = true
		if e.MovedTo != "" {
			st.virt.movedFrom[e.MovedTo] = e.Path
		}
	}
	st.an.Effects = append(st.an.Effects, e)
}

// destroyMoved handles an effect that overwrites or removes data an earlier part of
// this line moved into place. Moving it back would no longer return the right data,
// so the earlier move is demoted to a plain removal, which is captured before the
// command runs; and an effect on something inside a moved directory is applied to
// the original location, which is where the data is at capture time. It reports
// whether the effect was fully handled.
func (st *state) destroyMoved(c *call, e *Effect) bool {
	for q := e.Path; q != "/" && q != "."; q = filepath.Dir(q) {
		src, moved := st.virt.movedFrom[q]
		if !moved {
			continue
		}
		if q == e.Path {
			for i := range st.an.Effects {
				if x := &st.an.Effects[i]; x.Kind == Remove && x.Path == src && x.MovedTo == q {
					x.MovedTo = ""
				}
			}
			delete(st.virt.movedFrom, q)
			return false // still apply the effect: the destination is one the line made
		}
		rel, _ := filepath.Rel(q, e.Path)
		orig := *e
		orig.Path = filepath.Join(src, rel)
		if _, err := os.Lstat(orig.Path); err != nil {
			return true // nothing was there to lose
		}
		st.add(c, orig)
		return true
	}
	return false
}

func (st *state) unmake(p string) {
	delete(st.virt.created, p)
	for q := range st.virt.created {
		if strings.HasPrefix(q, p+"/") {
			delete(st.virt.created, q)
		}
	}
}

type fakeInfo struct {
	name string
	dir  bool
}

func (f fakeInfo) Name() string { return f.name }
func (f fakeInfo) Size() int64  { return 0 }
func (f fakeInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.dir }
func (f fakeInfo) Sys() any           { return nil }

// lstat is os.Lstat as the filesystem will look once the earlier parts of the
// command line have run.
func (st *state) lstat(p string) (fs.FileInfo, bool) {
	if dir, ok := st.virt.created[p]; ok {
		return fakeInfo{name: filepath.Base(p), dir: dir}, true
	}
	if st.virt.removedUnder(p) {
		return nil, false
	}
	fi, err := os.Lstat(p)
	return fi, err == nil
}

func (st *state) exists(p string) bool { _, ok := st.lstat(p); return ok }

// isDir follows symlinks for real paths and consults the overlay for virtual ones.
func (st *state) isDir(p string) bool {
	if dir, ok := st.virt.created[p]; ok {
		return dir
	}
	if st.virt.removedUnder(p) {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

var privilegeWord = regexp.MustCompile(`(^|[\s;&|(])(sudo|doas|su|pkexec)(\s|$)`)

func hasPrivilege(text string) bool { return privilegeWord.MatchString(text) }

func (st *state) unresolved(c *call, why string) { st.issue(IssueUnresolved, c.src, why) }
func (st *state) opaque(c *call, why string)     { st.issue(IssueOpaque, c.src, why) }
func (st *state) unrec(c *call, why string)      { st.issue(IssueUnrecoverable, c.src, why) }

func (c *call) sub(argv []string) *call {
	n := *c
	n.name, n.args, n.aerr, n.words = argv[0], argv[1:], nil, nil
	return &n
}

var stdBinDirs = map[string]bool{"/bin": true, "/usr/bin": true, "/sbin": true, "/usr/sbin": true, "/usr/local/bin": true}

func (st *state) dispatch(c *call) {
	if strings.Contains(c.name, "/") {
		if !stdBinDirs[filepath.Dir(c.name)] {
			st.opaque(c, "runs "+c.name+" by path, which is not modelled")
			return
		}
		c.name = filepath.Base(c.name)
	}
	name := c.name
	if c.sc.wd != unknownWD && name == "cd" {
		st.cd(c)
		return
	}
	if name == "cd" || name == "pushd" || name == "popd" {
		c.sc.wd = unknownWD
		return
	}
	if name == "export" || name == "declare" || name == "local" || name == "readonly" || name == "typeset" {
		return // handled as a DeclClause when it parses as one; harmless otherwise
	}
	if w, ok := wrappers[name]; ok {
		st.wrapper(c, w)
		return
	}
	switch name {
	case "find":
		st.find(c)
		return
	case "xargs":
		st.xargs(c)
		return
	case "sh", "bash", "zsh", "dash", "ksh", "ash":
		st.shell(c)
		return
	case "eval":
		// eval runs its arguments, joined by spaces, as shell: analyse that text.
		if !st.needArgs(c) {
			return
		}
		an := st.a.analyzeText(st.ctx, strings.Join(c.args, " "), c.sc.clone(), st.depth+1, st.virt)
		st.an.merge(an)
		return
	}
	if readOnlyForm(name, c.args, c.aerr == nil) {
		return
	}
	if r, ok := rules[name]; ok {
		r(st, c)
		return
	}
	if t, ok := stateTools[name]; ok {
		st.stateTool(c, t)
		return
	}
	if why, ok := unrecoverableNames[name]; ok || strings.HasPrefix(name, "mkfs") || strings.HasPrefix(name, "mkfs.") {
		if !ok {
			why = "creates a filesystem"
		}
		st.unrec(c, why)
		return
	}
	if interpreters[name] {
		if c.pc != nil && c.pc.idx > 0 && len(c.args) == 0 {
			st.unrec(c, "executes code piped in from another command")
			return
		}
		st.opaque(c, name+" runs arbitrary code; its effects are not modelled")
		return
	}
	st.opaque(c, "unknown command "+name)
}

func (st *state) cd(c *call) {
	if c.aerr != nil {
		c.sc.wd = unknownWD
		return
	}
	var ops []string
	for _, a := range c.args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			ops = append(ops, a)
		}
	}
	target := ""
	switch {
	case len(ops) == 0:
		target = os.Getenv("HOME")
	case ops[0] == "-":
		c.sc.wd = unknownWD
		return
	default:
		target = ops[0]
	}
	p := c.abs(target)
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		c.sc.wd = p
		return
	}
	// The directory does not exist, so cd fails and the shell stays where it was.
}
