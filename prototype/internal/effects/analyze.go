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
	// Compose controls whether transparent wrappers, find -exec/-execdir/-ok/-okdir,
	// xargs, eval, sh/bash/zsh/dash/ksh/ash -c strings, and for-loops are resolved
	// through. False routes each straight to opaque instead of analysing what they
	// run, with every other dispatch path (the per-command rule table, readOnlyForm,
	// &&/pipe/subshell/if/case traversal) left untouched. This isolates how much of
	// the analysis's result comes from composition versus the underlying per-command
	// rule table alone (docs/open-problems.md row 38, the adviser's own question).
	Compose bool
}

func New(wd string) *Analyzer {
	return &Analyzer{WD: wd, Env: os.Environ(), MaxItems: 5000, MaxLoop: 300, MaxDepth: 8, Compose: true}
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
	// removed maps a path an earlier part of the line removes or moves away to whether
	// that removal is certain to have happened by now. It is certain only across &&;
	// after a part that may have failed (see mayHaveFailed) it is a possibility, and the
	// path is looked up on disk again, so a later write to it is captured. A creation
	// needs no such care: it is recorded only for a path that did not exist, so if it
	// did not happen the path is still absent and there is nothing there to lose.
	removed map[string]bool
	// recreated holds the creations over a path an earlier part of the line removed.
	// The path existed before the line, so such a creation is doubted like the
	// removal: if the removal failed, what a later part writes there is the original.
	recreated map[string]bool
	// movedFrom maps a path that a move created to the path the data came from. The
	// data exists nowhere else, so a later overwrite or removal of the destination
	// destroys it, and the move can no longer be undone by moving back.
	movedFrom map[string]string
}

func newOverlay() *overlay {
	return &overlay{created: map[string]bool{}, removed: map[string]bool{}, recreated: map[string]bool{}, movedFrom: map[string]string{}}
}

func (o *overlay) clone() *overlay {
	n := newOverlay()
	for k, v := range o.created {
		n.created[k] = v
	}
	for k, v := range o.removed {
		n.removed[k] = v
	}
	for k, v := range o.recreated {
		n.recreated[k] = v
	}
	for k, v := range o.movedFrom {
		n.movedFrom[k] = v
	}
	return n
}

// doubt makes every removal so far, and every creation over a removed path, a
// possibility rather than a fact.
func (o *overlay) doubt() {
	for p := range o.removed {
		o.removed[p] = false
	}
	for p := range o.recreated {
		delete(o.created, p)
		delete(o.recreated, p)
	}
}

// wasRemoved reports whether an earlier part of the line removed p or a directory
// above it, certainly or possibly.
func (o *overlay) wasRemoved(p string) bool {
	for q := p; ; q = filepath.Dir(q) {
		if _, ok := o.removed[q]; ok {
			return true
		}
		if q == "/" || q == "." {
			return false
		}
	}
}

// removedUnder reports whether p, or a directory above it, is certainly gone.
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
	// cded records a cd since the start of the construct being analysed. If what
	// follows may run after that cd failed, the directory is unknown (mayHaveFailed).
	cded bool
}

func newScope(wd string) *scope {
	return &scope{wd: wd, vars: map[string]string{}, unset: map[string]bool{}}
}

// join sets s to what is known after one of alts ran, without knowing which: a
// variable or working directory on which they disagree is unknown afterwards, so a
// command that uses it asks.
func (s *scope) join(alts ...*scope) {
	first := alts[0]
	wd, cded := first.wd, false
	names := map[string]bool{}
	for _, a := range alts {
		if a.wd != wd {
			wd = unknownWD
		}
		cded = cded || a.cded
		for k := range a.vars {
			names[k] = true
		}
		for k := range a.unset {
			names[k] = true
		}
	}
	vars, unset := map[string]string{}, map[string]bool{}
	for k := range names {
		v, set := first.vars[k]
		gone := first.unset[k]
		for _, a := range alts[1:] {
			if w, ok := a.vars[k]; ok != set || w != v || a.unset[k] != gone {
				set, gone = false, true
				break
			}
		}
		if set {
			vars[k] = v
		}
		if gone {
			unset[k] = true
		}
	}
	s.wd, s.vars, s.unset, s.cded = wd, vars, unset, cded
}

// fresh runs f with the record of cd commands cleared and then restores it. A failure
// point inside f concerns only the cd commands in f: a cd before f either succeeded,
// since f runs only after it, or was already doubted at an earlier failure point.
func (s *scope) fresh(f func()) {
	before := s.cded
	s.cded = false
	f()
	s.cded = s.cded || before
}

func (s *scope) clone() *scope {
	n := &scope{wd: s.wd, cded: s.cded, vars: make(map[string]string, len(s.vars)), unset: make(map[string]bool, len(s.unset))}
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

// mayHaveFailed is applied where what follows can run although something before it
// failed: after ; or a newline, on the right of ||, after a negated command, between
// pipeline stages, in an else branch, between loop passes. The analysis otherwise
// reads the line as if every part succeeded. A removal before this point may not have
// happened, so it becomes a possibility (overlay.doubt), and a cd may have failed and
// left the shell where it was, so the working directory is unknown and later commands
// that depend on it ask.
func (st *state) mayHaveFailed(sc *scope) {
	st.virt.doubt()
	if sc.cded {
		sc.wd = unknownWD
	}
}

func (st *state) stmts(list []*syntax.Stmt, sc *scope) {
	sc.fresh(func() {
		for i, s := range list {
			if i > 0 && !guarded(list[i-1]) {
				st.mayHaveFailed(sc)
			}
			st.stmt(s, sc, nil)
		}
	})
}

// guarded reports whether what follows s runs only if s succeeded, as after
// `cd dir || exit`.
func guarded(s *syntax.Stmt) bool {
	b, ok := s.Cmd.(*syntax.BinaryCmd)
	return ok && !s.Negated && !s.Background && b.Op == syntax.OrStmt && exits(b.Y)
}

// exits reports whether s ends the shell, as `exit` or a block ending in it does.
// `return` is not counted: outside a function it fails and the shell carries on.
func exits(s *syntax.Stmt) bool {
	if s.Negated || s.Background {
		return false
	}
	switch x := s.Cmd.(type) {
	case *syntax.CallExpr:
		return len(x.Args) > 0 && x.Args[0].Lit() == "exit"
	case *syntax.Block:
		return len(x.Stmts) > 0 && exits(x.Stmts[len(x.Stmts)-1])
	}
	return false
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
			// Every stage runs in a subshell, at the same time as the others, and any
			// of them may fail.
			stages := flattenPipe(x)
			starts := make([]int, len(stages)+1)
			for i, stage := range stages {
				if i > 0 {
					st.virt.doubt()
				}
				starts[i] = len(st.an.Effects)
				st.stmt(stage, sc.clone(), &pipeCtx{stages: stages, idx: i})
			}
			starts[len(stages)] = len(st.an.Effects)
			st.racingMoves(starts)
			st.virt.doubt()
		case syntax.OrStmt:
			sc.fresh(func() {
				st.stmt(x.X, sc, nil)
				if exits(x.Y) {
					return // what follows runs only if X succeeded
				}
				y := sc.clone()
				st.mayHaveFailed(y) // Y runs only if X failed
				st.stmt(x.Y, y, nil)
				sc.join(sc.clone(), y)
				st.virt.doubt() // Y's removals happened only if X failed
			})
		default: // &&
			// Each part runs only if every part before it succeeded, so it is analysed
			// from the state they leave; what follows the chain may come after any
			// of them stopped it, so it gets the join of the states after each part.
			parts := append(andChain(x.X), andChain(x.Y)...)
			run := sc.clone()
			st.stmt(parts[0], run, nil)
			alts := []*scope{run.clone()}
			for _, p := range parts[1:] {
				st.stmt(p, run, nil)
				alts = append(alts, run.clone())
			}
			sc.join(alts...)
		}
	case *syntax.Subshell:
		st.stmts(x.Stmts, sc.clone())
	case *syntax.Block:
		st.stmts(x.Stmts, sc)
	case *syntax.IfClause:
		st.ifClause(x, sc)
		st.virt.doubt()
	case *syntax.WhileClause:
		// `producer | while read v; do ...; done` binds v to each record the producer
		// gives, when the producer can be resolved (whileread.go).
		if !x.Until && st.a.Compose && pc != nil && pc.idx > 0 && !readsStdinElsewhere(s) && st.whileRead(x, sc, pc, st.text(s)) {
			break
		}
		st.stmts(x.Cond, sc)
		st.loopBody(x.Do, sc, nil)
	case *syntax.ForClause:
		if !st.a.Compose {
			st.issue(IssueOpaque, st.text(s), "composition disabled (ablation): loop body not expanded")
			return
		}
		st.forLoop(x, sc)
	case *syntax.CaseClause:
		st.collectNode(x.Word, sc)
		alts := []*scope{sc.clone()} // no pattern matched
		for _, it := range x.Items {
			for _, p := range it.Patterns {
				st.collectNode(p, sc)
			}
			b := sc.clone()
			st.stmts(it.Stmts, b)
			alts = append(alts, b)
		}
		sc.join(alts...)
		st.virt.doubt()
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
	if s.Negated {
		st.mayHaveFailed(sc) // `! A && B` runs B when A failed
	}
}

// racingMoves handles the stages of a pipeline running at the same time: an earlier
// stage may overwrite or remove the destination of a later stage's move after the move,
// destroying the moved data, which the stage-by-stage walk (where destroyMoved sees
// only what comes later) does not see. Such a move is demoted to a captured removal of
// its source, as destroyMoved does in the other order. starts[i] is the index of stage
// i's first effect, and starts[len] the end.
func (st *state) racingMoves(starts []int) {
	for j := 1; j+1 < len(starts); j++ {
		for _, mv := range st.an.Effects[starts[j]:starts[j+1]] {
			if mv.Kind != Remove || mv.MovedTo == "" {
				continue
			}
			for _, e := range st.an.Effects[starts[0]:starts[j]] {
				if (e.Kind == Write || e.Kind == Remove) && e.Hint == "" &&
					(e.Path == mv.MovedTo || strings.HasPrefix(mv.MovedTo, e.Path+"/")) {
					st.demoteMove(mv.Path, mv.MovedTo)
					break
				}
			}
		}
	}
}

// readsStdinElsewhere reports whether s redirects its standard input, so that a loop
// in a pipeline does not read the pipe.
func readsStdinElsewhere(s *syntax.Stmt) bool {
	for _, r := range s.Redirs {
		switch r.Op {
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
			return true
		}
	}
	return false
}

// andChain flattens a && b && c, which parses as (a && b) && c, into its parts.
func andChain(s *syntax.Stmt) []*syntax.Stmt {
	b, ok := s.Cmd.(*syntax.BinaryCmd)
	if !ok || b.Op != syntax.AndStmt || s.Negated || s.Background || len(s.Redirs) > 0 {
		return []*syntax.Stmt{s}
	}
	return append(andChain(b.X), andChain(b.Y)...)
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

// ifClause analyses each branch from the state its condition leaves, and joins the
// branches afterwards, since which one ran is not known. A branch's assignments and
// cd commands outlive it, as they do in the shell.
func (st *state) ifClause(x *syntax.IfClause, sc *scope) {
	if len(x.Cond) == 0 { // a plain else, which runs whenever it is reached
		st.stmts(x.Then, sc)
		return
	}
	sc.fresh(func() {
		st.stmts(x.Cond, sc)
		then := sc.clone()
		st.stmts(x.Then, then)
		other := sc.clone()
		st.mayHaveFailed(other) // the condition failed
		if x.Else != nil {
			st.ifClause(x.Else, other)
		}
		sc.join(then, other)
	})
}

// loopBody analyses a body that runs zero or more times, each pass starting from what
// the one before left. One pass is first analysed on a scratch copy to see what it
// changes; the body is then analysed from the join of that and the state before the
// loop, and the state after the loop is the join of the two. prep, if set, adjusts the
// state each pass starts from.
func (st *state) loopBody(body []*syntax.Stmt, sc *scope, prep func(*scope)) {
	if prep == nil {
		prep = func(*scope) {}
	}
	once := sc.clone()
	once.cded = false // only a cd inside the body can have failed in an earlier pass
	prep(once)
	scratch := st.child(st.depth)
	scratch.virt = st.virt.clone()
	scratch.stmts(body, once)
	pass := sc.clone()
	pass.join(sc, once)
	pass.cded = once.cded
	prep(pass)
	st.mayHaveFailed(pass) // an earlier pass may have failed part-way
	st.stmts(body, pass)
	sc.join(sc.clone(), pass)
	st.virt.doubt()
}

func (st *state) forLoop(x *syntax.ForClause, sc *scope) {
	wi, ok := x.Loop.(*syntax.WordIter)
	if !ok {
		st.collectNode(x.Loop, sc)
		st.loopBody(x.Do, sc, nil)
		return
	}
	for _, w := range wi.Items {
		st.collectNode(w, sc)
	}
	name := wi.Name.Value
	unbound := func() {
		st.loopBody(x.Do, sc, func(s *scope) { s.drop(name) })
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
	// The passes share the shell's state, as they do in bash: each starts from what the
	// one before left, and the loop variable keeps the last item afterwards.
	sc.fresh(func() {
		for i, it := range items {
			if i > 0 {
				st.mayHaveFailed(sc) // the pass before may have failed part-way
			}
			sc.set(name, it)
			st.stmts(x.Do, sc)
		}
	})
	if len(items) > 0 {
		st.virt.doubt()
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
	return expand.Fields(st.cfg(sc), unescapeGlobs(words)...)
}

// unescapeGlobs works around the shell library treating a backslash-escaped glob
// character as a glob: in bash, \*.log is the literal name "*.log", but the library
// expands it against the directory, so `find -name \*.log` is analysed as
// `find -name a.log` and finds nothing. Each escaped * ? [ is rewritten as a quoted
// character, which the library does treat literally.
func unescapeGlobs(words []*syntax.Word) []*syntax.Word {
	out := make([]*syntax.Word, len(words))
	for i, w := range words {
		out[i] = w
		var parts []syntax.WordPart
		changed := false
		for _, p := range w.Parts {
			lit, ok := p.(*syntax.Lit)
			if !ok || !strings.ContainsAny(lit.Value, "\\") {
				parts = append(parts, p)
				continue
			}
			var buf strings.Builder
			flush := func() {
				if buf.Len() > 0 {
					parts = append(parts, &syntax.Lit{Value: buf.String()})
					buf.Reset()
				}
			}
			v := lit.Value
			for k := 0; k < len(v); k++ {
				if v[k] == '\\' && k+1 < len(v) && strings.IndexByte("*?[", v[k+1]) >= 0 {
					flush()
					parts = append(parts, &syntax.SglQuoted{Value: string(v[k+1])})
					k++
					changed = true
					continue
				}
				if v[k] == '\\' && k+1 < len(v) {
					buf.WriteByte(v[k])
					k++
				}
				buf.WriteByte(v[k])
			}
			flush()
		}
		if changed {
			out[i] = &syntax.Word{Parts: parts}
		}
	}
	return out
}

// nondeterministic matches text whose output differs from one run to the next, so
// resolving it once and running it later would act on different names. Network
// clients are here too: a resolver runs without the network, so what they fetch is
// empty then and whatever the server sends when the line runs.
var nondeterministic = regexp.MustCompile(`/dev/u?random|\$RANDOM|\$\$|\bshuf\b|\bmktemp\b|\buuidgen\b|\bdate\b|\bopenssl\s+rand\b|\$SRANDOM|\bpgrep\b|\bps\b|\bpidof\b|` +
	`\b(curl|wget|ssh|scp|sftp|ftp|nc|ncat|socat|telnet|aria2c|dig|nslookup|whois)\b`)

// readsWhatTheLineWrites reports whether text mentions a file an earlier part of the
// same line writes or creates: what it would read now is not what it will read then.
func (st *state) readsWhatTheLineWrites(text string) bool {
	for _, e := range st.an.Effects {
		if e.Kind == Write || e.Kind == Create {
			if b := filepath.Base(e.Path); b != "" && b != "." && strings.Contains(text, b) {
				return true
			}
		}
	}
	return false
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
	if nondeterministic.MatchString(body.String()) || st.readsWhatTheLineWrites(body.String()) {
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
	if e.Kind == Remove && e.MovedTo == "" && e.Hint == "" && (e.Path == st.a.WD || strings.HasPrefix(st.a.WD, e.Path+"/")) {
		// The directory the command runs in cannot be moved aside to a holding
		// directory while the command is running in it.
		st.issue(IssueUnrecoverable, c.src, "removes the directory it runs in, which cannot be captured beforehand")
		return
	}
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
		if st.virt.wasRemoved(e.Path) {
			st.virt.recreated[e.Path] = true
		}
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
	// Removing a directory also destroys whatever an earlier part of the line moved
	// into it, which the directory's own capture, taken before the line runs, does not
	// hold: demote each such move to a captured removal of its source.
	if e.Kind == Remove {
		for q, src := range st.virt.movedFrom {
			if strings.HasPrefix(q, e.Path+"/") {
				st.demoteMove(src, q)
			}
		}
	}
	for q := e.Path; q != "/" && q != "."; q = filepath.Dir(q) {
		src, moved := st.virt.movedFrom[q]
		if !moved {
			continue
		}
		if q == e.Path {
			st.demoteMove(src, q)
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

// demoteMove turns the earlier move of src to dst into a plain removal of src, which
// the plan captures, because moving back would no longer return the data.
func (st *state) demoteMove(src, dst string) {
	for i := range st.an.Effects {
		if x := &st.an.Effects[i]; x.Kind == Remove && x.Path == src && x.MovedTo == dst {
			x.MovedTo = ""
		}
	}
	delete(st.virt.movedFrom, dst)
}

// lineChangesUnder reports whether an earlier part of the line created, moved into,
// removed, or wrote anything at or under one of roots, so that listing the tree now
// would not show what a later command will find there.
func (st *state) lineChangesUnder(roots []string) bool {
	under := func(p string) bool {
		for _, r := range roots {
			if p == r || strings.HasPrefix(p, r+"/") {
				return true
			}
		}
		return false
	}
	for p := range st.virt.created {
		if under(p) {
			return true
		}
	}
	for p := range st.virt.removed {
		if under(p) {
			return true
		}
	}
	for p := range st.virt.movedFrom {
		if under(p) {
			return true
		}
	}
	for _, e := range st.an.Effects {
		if e.Kind == Write && under(e.Path) {
			return true
		}
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
	if name == "unset" || name == "read" || name == "mapfile" || name == "readarray" {
		st.forgetVars(c)
		return
	}
	if w, ok := wrappers[name]; ok {
		if !st.a.Compose {
			st.opaque(c, "composition disabled (ablation): wrapper not resolved through")
			return
		}
		st.wrapper(c, w)
		return
	}
	switch name {
	case "find":
		if !st.a.Compose {
			st.opaque(c, "composition disabled (ablation): find not resolved through")
			return
		}
		st.find(c)
		return
	case "xargs":
		if !st.a.Compose {
			st.opaque(c, "composition disabled (ablation): xargs not resolved through")
			return
		}
		st.xargs(c)
		return
	case "sh", "bash", "zsh", "dash", "ksh", "ash":
		if !st.a.Compose {
			st.opaque(c, "composition disabled (ablation): shell -c string not resolved through")
			return
		}
		st.shell(c)
		return
	case "eval":
		if !st.a.Compose {
			st.opaque(c, "composition disabled (ablation): eval not resolved through")
			return
		}
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
	if c.sc.wd == unknownWD {
		// Every rule checks whether its targets exist before recording an effect, and
		// nothing exists under a directory that is unknown, so a rule run here would
		// report "no effect" and the line would pass silently. It is unresolved instead,
		// even for an absolute path the rule could have placed: that costs a question.
		st.unresolved(c, errUnknownWD.Error())
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

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// forgetVars handles the builtins that unset or assign variables without an
// assignment word: unset, read, mapfile, readarray. Each variable they name is marked
// unset in the scope, so a later expansion of it fails and the command using it asks.
// Otherwise the variable keeps the value it had in the environment, which is not what
// bash expands: after `unset HOME`, "$HOME/x" is /x, not the home directory's x. Every
// argument shaped like a name is dropped, option values included, which can only make
// the analysis ask more. Names computed at run time cannot be known, so they ask.
func (st *state) forgetVars(c *call) {
	if c.aerr != nil {
		st.unresolved(c, "the variables "+c.name+" changes could not be determined")
		return
	}
	if c.name == "unset" && hasAny(c.args, "-f") {
		return // functions, which the analysis does not track
	}
	switch c.name {
	case "read":
		c.sc.drop("REPLY")
	case "mapfile", "readarray":
		c.sc.drop("MAPFILE")
	}
	for _, a := range c.args {
		if identifier.MatchString(a) {
			c.sc.drop(a)
		}
	}
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
		c.sc.wd, c.sc.cded = p, true
		return
	}
	// The directory does not exist, so cd fails and the shell stays where it was.
}
