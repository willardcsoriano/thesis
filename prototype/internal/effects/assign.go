package effects

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// The analysis expands words with the values variables hold at that point of the
// line, so it must see every way a variable gets its value. Plain assignments, for
// loops, and while-read records it follows. Everything else that assigns by name
// (printf -v, declare and its kin, getopts, read, mapfile, arithmetic, ${v:=x},
// eval, wait -p, {fd} redirections, coproc) rebinds the name: to its value where
// that is known, otherwise to unknown. A word that reads an unknown variable cannot
// be expanded, so the command that has it asks (defect 14 in analyzer-defects.md).
//
// "Unknown" is not "unset": an unset variable expands to nothing and ${v:-x} to x,
// which is exactly what the shell does after `unset v`, but after `read v` either
// could be wrong. Some changes reach further than one name: a nameref (declare -n)
// can make any assignment change any variable, so after one every variable is
// unknown; and options that change how words expand or commands are found (shopt,
// set -f, GLOBIGNORE, PATH, hash -p) leave every later command unresolved.

// unknownKind is what is known about a variable the analysis cannot follow.
type unknownKind uint8

const (
	unknownInt unknownKind = iota + 1 // some integer: only arithmetic assigned it
	unknownAny                        // anything
)

// errUnknownVar is the error expanding a word that reads an unknown variable.
var errUnknownVar = errors.New("the value of a variable is not known")

// bashOwn are variables bash sets itself, so the environment's value (the
// analyser's own, for some) is not the one the line sees. Positional parameters are
// here too: the analysis does not follow set -- and shift.
var bashOwn = setOf(`_ SHLVL BASHPID BASH_COMMAND BASH_SUBSHELL SECONDS RANDOM SRANDOM EPOCHSECONDS EPOCHREALTIME
PPID OPTIND OPTARG PIPESTATUS BASH_REMATCH FUNCNAME HISTCMD BASH_ARGV BASH_ARGC BASH_ARGV0 BASH_LINENO BASH_SOURCE
COPROC COLUMNS LINES @ * # 0 1 2 3 4 5 6 7 8 9 ? ! -`)

// behaviourVars change how the shell expands words, finds commands, or starts, or
// which code a program loads. Assigning one leaves every later command unresolved.
var behaviourVars = setOf(`PATH GLOBIGNORE BASHOPTS SHELLOPTS POSIXLY_CORRECT BASH_ENV ENV EXECIGNORE BASH_COMPAT
LD_PRELOAD LD_LIBRARY_PATH LD_AUDIT BASH_LOADABLES_PATH BASH_XTRACEFD`)

// harmlessShopt are shopt options that do not change what a command line expands to,
// which commands it runs, or where: interactive behaviour, history, messages, and
// failglob, which only stops a command whose glob matched nothing.
var harmlessShopt = setOf(`checkhash checkjobs checkwinsize cmdhist complete_fullquote direxpand dirspell
extquote failglob force_fignore gnu_errfmt histappend histreedit histverify hostcomplete huponexit
inherit_errexit interactive_comments lithist login_shell mailwarn no_empty_cmd_completion noexpand_translation
progcomp progcomp_alias promptvars restricted_shell shift_verbose varredir_close nocasematch`)

func (s *scope) forget(k string) {
	delete(s.vars, k)
	delete(s.unset, k)
	s.unknown[k] = unknownAny
}

func (s *scope) forgetInt(k string) {
	delete(s.vars, k)
	delete(s.unset, k)
	if s.unknown[k] != unknownAny {
		s.unknown[k] = unknownInt
	}
}

// unknownName reports whether the value of k is not known.
func (s *scope) unknownName(k string) bool {
	if s.everything || s.unknown[k] != 0 || bashOwn[k] || isDigits(k) {
		return true
	}
	if s.envUnknown {
		_, in := s.vars[k]
		return !in && !s.unset[k] && k != "IFS" && k != "PWD" // bash sets these itself
	}
	return false
}

// lookup returns the value k has, as the analysis knows it, from env when the line
// has not touched it; ok is false when it is not known.
func (s *scope) lookup(k string, env []string) (v string, set, ok bool) {
	if s.unknownName(k) {
		return "", false, false
	}
	if v, in := s.vars[k]; in {
		return v, true, true
	}
	if s.unset[k] || k == "IFS" {
		return "", false, true
	}
	for _, kv := range env {
		if name, val, found := strings.Cut(kv, "="); found && name == k {
			return val, true, true
		}
	}
	return "", false, true
}

// changeShell records that the shell no longer expands or finds commands the way the
// analysis models; every later command is then unresolved.
func (s *scope) changeShell(why string) {
	if s.changed == "" {
		s.changed = why
	}
}

// scopeEnv is the environment words are expanded against. Reading a variable whose
// value is not known records its name in hit, and the expansion is then refused.
type scopeEnv struct {
	expand.Environ
	sc  *scope
	hit *string
}

func (e scopeEnv) Get(name string) expand.Variable {
	if e.sc.unknownName(name) {
		if *e.hit == "" {
			*e.hit = name
		}
		return expand.Variable{}
	}
	return e.Environ.Get(name)
}

func (e scopeEnv) Each(f func(string, expand.Variable) bool) {
	if e.sc.everything || len(e.sc.unknown) > 0 {
		if *e.hit == "" {
			*e.hit = "*" // ${!prefix*} lists names, and some are not known
		}
	}
	e.Environ.Each(f)
}

func unknownVarError(name string) error {
	if name == "*" {
		return fmt.Errorf("%w: the names a prefix expansion lists", errUnknownVar)
	}
	return fmt.Errorf("%w: $%s (set in a way the analysis cannot follow)", errUnknownVar, name)
}

// ---- assignments made while a statement is expanded ----

var assignOps = map[syntax.BinAritOperator]bool{
	syntax.Assgn: true, syntax.AddAssgn: true, syntax.SubAssgn: true, syntax.MulAssgn: true,
	syntax.QuoAssgn: true, syntax.RemAssgn: true, syntax.AndAssgn: true, syntax.OrAssgn: true,
	syntax.XorAssgn: true, syntax.ShlAssgn: true, syntax.ShrAssgn: true, syntax.AndBoolAssgn: true,
	syntax.OrBoolAssgn: true, syntax.XorBoolAssgn: true, syntax.PowAssgn: true,
}

var leadingName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

// preAssign handles the assignments a statement makes while it is expanded or
// evaluated, before the statement is analysed: arithmetic (in $(( )), (( )), let,
// for (( )), subscripts, substring offsets, and [[ -eq ]] operands) and ${v:=x},
// ${v=x}. Each assigned name becomes unknown, beforehand, so that even a later word
// of the same command does not use the old value. Arithmetic also evaluates the
// values of the variables it names as expressions, so naming one whose value is not
// known, or is itself an assignment, may assign anything, and then every variable
// is unknown. Nested statements and substitutions are left to their own turn: those
// in a substitution run in a subshell.
func (st *state) preAssign(root *syntax.Stmt, sc *scope) {
	var arith func(n syntax.Node)
	arith = func(n syntax.Node) {
		if n == nil {
			return
		}
		syntax.Walk(n, func(x syntax.Node) bool {
			switch y := x.(type) {
			case *syntax.CmdSubst, *syntax.ProcSubst:
				sc.everything = true // its output is evaluated as an expression
				return false
			case *syntax.BinaryArithm:
				if assignOps[y.Op] {
					st.arithTarget(y.X, sc)
				}
			case *syntax.UnaryArithm:
				if y.Op == syntax.Inc || y.Op == syntax.Dec {
					st.arithTarget(y.X, sc)
				}
			case *syntax.ParamExp:
				if y.Param != nil && !y.Excl && !y.Length && y.Index == nil && y.Slice == nil && y.Repl == nil && y.Exp == nil && y.Names == 0 {
					st.arithName(y.Param.Value, sc)
				} else {
					sc.everything = true
				}
				return false
			case *syntax.Lit:
				// Parsed arithmetic has its operators outside the words; a [[ -eq ]]
				// operand is a word, so an assignment in it is in its text.
				if strings.ContainsRune(y.Value, '=') || strings.Contains(y.Value, "++") || strings.Contains(y.Value, "--") {
					sc.everything = true
				}
				for _, name := range identifiers(y.Value) {
					st.arithName(name, sc)
				}
			}
			return true
		})
	}
	syntax.Walk(root, func(x syntax.Node) bool {
		switch y := x.(type) {
		case *syntax.Stmt:
			return y == root
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return false
		case *syntax.ArithmExp:
			arith(y.X)
			return false
		case *syntax.ArithmCmd:
			arith(y.X)
			return false
		case *syntax.LetClause:
			for _, e := range y.Exprs {
				arith(e)
			}
			return false
		case *syntax.CStyleLoop:
			arith(y.Init)
			arith(y.Cond)
			arith(y.Post)
			return false
		case *syntax.ParamExp:
			if y.Exp != nil && (y.Exp.Op == syntax.AssignUnset || y.Exp.Op == syntax.AssignUnsetOrNull) && y.Param != nil {
				sc.forget(y.Param.Value)
			}
			if y.Index != nil {
				arith(y.Index)
			}
			if y.Slice != nil {
				arith(y.Slice.Offset)
				arith(y.Slice.Length)
			}
		case *syntax.Assign:
			if y.Index != nil {
				arith(y.Index)
			}
		case *syntax.BinaryTest:
			switch y.Op {
			case syntax.TsEql, syntax.TsNeq, syntax.TsLeq, syntax.TsGeq, syntax.TsLss, syntax.TsGtr:
				arith(y.X)
				arith(y.Y)
			}
		}
		return true
	})
}

// arithTarget makes the variable an arithmetic assignment writes an unknown integer.
func (st *state) arithTarget(x syntax.ArithmExpr, sc *scope) {
	w, ok := x.(*syntax.Word)
	if !ok {
		sc.everything = true
		return
	}
	var b strings.Builder
	_ = syntax.NewPrinter().Print(&b, w)
	name := leadingName.FindString(b.String())
	if name == "" {
		sc.everything = true
		return
	}
	if sc.unknown[name] == 0 && !sc.everything {
		sc.forgetInt(name)
	}
	if strings.ContainsRune(b.String(), '[') {
		sc.forget(name) // an element of an array: the whole array is no longer known
	}
}

// arithName handles arithmetic reading a variable: its value is evaluated as an
// expression in turn, which may assign.
func (st *state) arithName(name string, sc *scope) {
	if !st.arithSafe(name, sc, 0) {
		sc.everything = true
	}
}

// arithSafe reports whether evaluating name in arithmetic cannot assign: its value
// is known to be an integer, or empty, or an expression without assignments whose
// names are safe in turn.
func (st *state) arithSafe(name string, sc *scope, depth int) bool {
	if depth > 10 {
		return false
	}
	if sc.unknown[name] == unknownInt {
		return true
	}
	if isDigits(name) {
		return true
	}
	v, _, ok := sc.lookup(name, st.a.Env)
	if !ok {
		return false
	}
	v = strings.TrimSpace(v)
	if v == "" || isInteger(v) {
		return true
	}
	if strings.ContainsAny(v, "=$`[") || strings.Contains(v, "++") || strings.Contains(v, "--") {
		return false
	}
	for _, n := range identifiers(v) {
		if !st.arithSafe(n, sc, depth+1) {
			return false
		}
	}
	return true
}

var identToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func identifiers(s string) []string { return identToken.FindAllString(s, -1) }

func isInteger(s string) bool {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	return isDigits(s)
}

// readsUnknown returns the first unknown variable the code in n reads, or "": a
// substitution or producer that reads one cannot be resolved, since the resolver
// would run it with some other value. Arithmetic names count as reads.
func readsUnknown(n syntax.Node, sc *scope) string {
	hit := ""
	syntax.Walk(n, func(x syntax.Node) bool {
		if hit != "" {
			return false
		}
		switch y := x.(type) {
		case *syntax.ParamExp:
			if y.Param != nil && sc.unknownName(y.Param.Value) {
				hit = y.Param.Value
			}
			if y.Excl && (sc.everything || len(sc.unknown) > 0) {
				hit = "!" + y.Param.Value
			}
		case *syntax.ArithmExp, *syntax.ArithmCmd, *syntax.LetClause, *syntax.CStyleLoop:
			syntax.Walk(y, func(z syntax.Node) bool {
				if l, ok := z.(*syntax.Lit); ok && hit == "" {
					for _, name := range identifiers(l.Value) {
						if sc.unknownName(name) {
							hit = name
						}
					}
				}
				return true
			})
		}
		return true
	})
	if hit == "" && sc.everything {
		hit = "*"
	}
	return hit
}

// ---- builtins that assign by name ----

// shellState applies what a builtin does to the shell's own state, before the
// command is otherwise dispatched. It reports false when it has recorded an issue
// and nothing more is to be done.
func (st *state) shellState(c *call) bool {
	sc := c.sc
	switch c.name {
	case "printf":
		if c.aerr != nil {
			w0, ok := constWord(c.words, 0)
			if !ok || strings.HasPrefix(w0, "-") {
				sc.everything = true // a -v the analysis cannot see may name anything
			}
			return true
		}
		if name, ok := printfTarget(c.args); ok {
			forgetTarget(sc, name)
		}
	case "declare", "typeset", "local", "export", "readonly":
		if c.aerr != nil {
			sc.everything = true
			return true
		}
		var items []declItem
		for _, a := range c.args {
			items = append(items, declItem{raw: a, known: true})
		}
		st.declare(c.name, items, sc)
	case "getopts":
		sc.forget("OPTARG")
		sc.forgetInt("OPTIND")
		if c.aerr != nil {
			if n, ok := constWord(c.words, 1); ok && identifier.MatchString(n) {
				sc.forget(n)
			} else {
				sc.everything = true
			}
			return true
		}
		if len(c.args) > 1 {
			forgetTarget(sc, c.args[1])
		}
	case "let":
		sc.everything = true // its arguments are expressions, assigning anything
	case "wait":
		if c.aerr != nil {
			sc.everything = true
			return true
		}
		for i, a := range c.args {
			if strings.HasPrefix(a, "-") && strings.ContainsRune(a, 'p') && i+1 < len(c.args) {
				forgetTarget(sc, c.args[i+1])
			}
		}
	case "mapfile", "readarray":
		for _, a := range c.args {
			if strings.HasPrefix(a, "-") && strings.ContainsRune(a, 'C') {
				st.opaque(c, c.name+" -C runs a callback, whose effects are not modelled")
				return false
			}
		}
	case "set":
		if c.aerr != nil {
			sc.changeShell("set with arguments that could not be determined")
			return true
		}
		if why := setChangesShell(c.args); why != "" {
			sc.changeShell(why)
		}
	case "shopt":
		if c.aerr != nil {
			sc.changeShell("shopt with arguments that could not be determined")
			return true
		}
		if why := shoptChangesShell(c.args); why != "" {
			sc.changeShell(why)
		}
	case "hash":
		if c.aerr != nil || hasCluster(c.args, 'p') {
			sc.changeShell("hash -p changes which program a command name runs")
		}
	case "trap":
		if c.aerr != nil || trapSetsCode(c.args) {
			st.opaque(c, "trap sets code to run later; its effects are not modelled")
			return false
		}
	}
	return true
}

// constWord returns words[i] when it has no expansion in it.
func constWord(words []*syntax.Word, i int) (string, bool) {
	if i >= len(words) {
		return "", false
	}
	w := words[i]
	constant := true
	syntax.Walk(w, func(x syntax.Node) bool {
		switch x.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst, *syntax.ExtGlob, *syntax.BraceExp:
			constant = false
		}
		return constant
	})
	if !constant {
		return "", false
	}
	s, err := expand.Literal(&expand.Config{Env: expand.ListEnviron()}, w)
	return s, err == nil
}

// printfTarget returns the variable printf -v assigns, if any.
func printfTarget(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return "", false
		case a == "-v":
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		case strings.HasPrefix(a, "-v"):
			return a[2:], true
		default:
			return "", false
		}
	}
	return "", false
}

// forgetTarget makes the variable a builtin assigns unknown. A name with a subscript
// assigns an element; a subscript is arithmetic, which may assign anything else.
func forgetTarget(sc *scope, target string) {
	name := leadingName.FindString(target)
	if name == "" {
		return // not a valid name: the builtin fails and assigns nothing
	}
	sc.forget(name)
	if rest := target[len(name):]; rest != "" {
		if !strings.HasPrefix(rest, "[") {
			return
		}
		if idx := strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]"); !isDigits(idx) {
			sc.everything = true
		}
	}
}

func hasCluster(args []string, opt rune) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if len(a) > 1 && a[0] == '-' && strings.ContainsRune(a[1:], opt) {
			return true
		}
	}
	return false
}

// setChangesShell returns why set's options change how words expand, commands are
// found, or cd resolves, or "" when they do not.
func setChangesShell(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || a == "-" || len(a) < 2 || (a[0] != '-' && a[0] != '+') {
			return "" // what follows sets the positional parameters
		}
		if a == "-o" || a == "+o" {
			if i+1 >= len(args) {
				return ""
			}
			i++
			switch args[i] {
			case "noglob", "braceexpand", "keyword", "physical", "posix", "onecmd", "history":
				return "set " + a + " " + args[i] + " changes how the shell expands or runs the line"
			}
			continue
		}
		if strings.ContainsAny(a[1:], "fBkPt") {
			return "set " + a + " changes how the shell expands or runs the line"
		}
	}
	return ""
}

// shoptChangesShell returns why shop's settings change how the line expands or runs.
func shoptChangesShell(args []string) string {
	setting := false
	var names []string
	for _, a := range args {
		switch {
		case a == "--":
		case strings.HasPrefix(a, "-"):
			if strings.ContainsAny(a, "su") {
				setting = true
			}
		default:
			names = append(names, a)
		}
	}
	if !setting {
		return ""
	}
	if len(names) == 0 {
		return "shopt with no option names changes all of them"
	}
	for _, n := range names {
		if !harmlessShopt[n] {
			return "shopt " + n + " changes how the shell expands or runs the line"
		}
	}
	return ""
}

// trapSetsCode reports whether trap's arguments set code to run on a signal or at
// exit, as opposed to listing, printing, ignoring (”) or resetting (-) traps.
func trapSetsCode(args []string) bool {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") && args[i] != "-" {
		if args[i] == "--" {
			i++
			break
		}
		if strings.ContainsAny(args[i], "lp") {
			return false
		}
		i++
	}
	rest := args[i:]
	switch {
	case len(rest) == 0:
		return false
	case rest[0] == "" || rest[0] == "-":
		return false
	case len(rest) == 1:
		return false // a lone signal resets it
	}
	return true
}

// declItem is one argument of declare and its kin: raw text when it is a word, or a
// parsed assignment.
type declItem struct {
	raw   string
	known bool // raw (or the assignment's value) could be determined
	as    *syntax.Assign
}

// declare applies declare, typeset, local, export, readonly, or nameref to the scope.
// Options apply to every name; a value is followed exactly when it is known and no
// attribute transforms it.
func (st *state) declare(variant string, items []declItem, sc *scope) {
	flags := map[rune]bool{}
	switch variant {
	case "readonly":
		flags['r'] = true
	case "nameref":
		flags['n'] = true
	}
	type target struct {
		name     string
		value    string
		hasValue bool
		known    bool
		append   bool
		whole    bool // the whole value is replaced by something not followed (an array)
	}
	var targets []target
	opts := true
	for _, it := range items {
		if it.as != nil && it.as.Name != nil {
			as := it.as
			t := target{name: as.Name.Value, append: as.Append}
			switch {
			case as.Array != nil || as.Index != nil:
				t.whole = true
			case as.Value != nil:
				t.hasValue = true
				v, err := st.literal(as.Value, sc)
				t.value, t.known = v, err == nil
			}
			targets = append(targets, t)
			opts = false
			continue
		}
		if !it.known {
			sc.everything = true // an argument that may be an option or name anything
			return
		}
		a := it.raw
		if opts && a == "--" {
			opts = false
			continue
		}
		if opts && len(a) > 1 && (a[0] == '-' || a[0] == '+') {
			for _, r := range a[1:] {
				if a[0] == '-' || r == 'i' || r == 'l' || r == 'u' || r == 'c' {
					flags[r] = true
				}
			}
			continue
		}
		opts = false
		name, value, hasValue := strings.Cut(a, "=")
		t := target{hasValue: hasValue, value: value, known: true}
		if strings.HasSuffix(name, "+") && hasValue {
			name, t.append = strings.TrimSuffix(name, "+"), true
		}
		base := leadingName.FindString(name)
		switch {
		case base == "":
			continue // not a valid name: an error, nothing assigned
		case base != name:
			if !strings.HasPrefix(name[len(base):], "[") {
				continue
			}
			forgetTarget(sc, name)
			continue
		}
		t.name = name
		targets = append(targets, t)
	}
	if flags['f'] || flags['F'] {
		return // the names are functions
	}
	if flags['n'] && variant != "export" {
		sc.everything = true // a nameref may make any assignment change any variable
		return
	}
	transforms := flags['l'] || flags['u'] || flags['c']
	for _, t := range targets {
		switch {
		case variant == "local":
			// Outside a function local fails and assigns nothing, and the analysis
			// never analyses a function's body, so which happened is not known.
			if t.hasValue || t.whole {
				sc.forget(t.name)
			}
			continue
		case flags['i']:
			sc.fixed[t.name] = 'i'
			if t.hasValue || t.whole {
				st.assignInt(sc, t.name, t.value, t.known && !t.whole && !t.append)
			}
		case transforms:
			sc.fixed[t.name] = 'c'
			if t.hasValue || t.whole {
				sc.forget(t.name)
			}
		case t.whole || t.hasValue && !t.known:
			sc.forget(t.name)
		case t.hasValue:
			st.assign(sc, t.name, t.value, t.append)
		}
		if flags['r'] {
			sc.fixed[t.name] = 'c' // a later assignment fails and leaves it, or stops the line
		}
		if (flags['a'] || flags['A']) && !t.hasValue && !t.whole {
			if _, in := sc.vars[t.name]; !in && !sc.unset[t.name] {
				sc.forget(t.name)
			}
		}
	}
}

// assign sets k to v (appended to its value when app), as the shell would, given the
// attributes the line gave k.
func (st *state) assign(sc *scope, k, v string, app bool) {
	if behaviourVars[k] {
		sc.changeShell("assigning " + k + " changes how the shell expands or finds commands")
	}
	if app {
		old, _, ok := sc.lookup(k, st.a.Env)
		if !ok {
			sc.forget(k)
			return
		}
		v = old + v
	}
	switch sc.fixed[k] {
	case 'i':
		st.assignInt(sc, k, v, !app)
		return
	case 'c':
		sc.forget(k)
		return
	}
	sc.set(k, v)
}

// assignInt assigns to a variable with the integer attribute: the value is
// evaluated as arithmetic, which may assign anything unless it is a plain integer.
func (st *state) assignInt(sc *scope, k, v string, known bool) {
	if known && isInteger(strings.TrimSpace(v)) {
		sc.set(k, strings.TrimSpace(v))
		return
	}
	if known && !strings.ContainsAny(v, "=$`[") && !strings.Contains(v, "++") && !strings.Contains(v, "--") {
		safe := true
		for _, n := range identifiers(v) {
			safe = safe && st.arithSafe(n, sc, 0)
		}
		if safe {
			sc.forgetInt(k)
			return
		}
	}
	sc.everything = true
}

// prefixAssigns binds the assignments before a command (f=x cmd) for that command
// only, as the shell does, and returns what restores the scope afterwards. A
// variable the command itself changed (read, printf -v) is unknown afterwards.
func (st *state) prefixAssigns(assigns []*syntax.Assign, sc *scope, c *call) func() {
	type saved struct {
		val          string
		inVars, gone bool
		kind         unknownKind
	}
	old := map[string]saved{}
	for _, as := range assigns {
		if as.Name == nil {
			continue
		}
		k := as.Name.Value
		if _, done := old[k]; !done {
			v, in := sc.vars[k]
			old[k] = saved{v, in, sc.unset[k], sc.unknown[k]}
		}
		if behaviourVars[k] {
			st.unresolved(c, "runs with "+k+" changed, which changes how it is found or what it loads")
		}
	}
	for _, as := range assigns {
		if as.Name != nil && !behaviourVars[as.Name.Value] {
			st.bind(as, sc)
		}
	}
	bound := map[string]saved{}
	for k := range old {
		v, in := sc.vars[k]
		bound[k] = saved{v, in, sc.unset[k], sc.unknown[k]}
	}
	return func() {
		for k, o := range old {
			v, in := sc.vars[k]
			if now := (saved{v, in, sc.unset[k], sc.unknown[k]}); now != bound[k] {
				sc.forget(k)
				continue
			}
			delete(sc.vars, k)
			delete(sc.unset, k)
			delete(sc.unknown, k)
			if o.inVars {
				sc.vars[k] = o.val
			}
			if o.gone {
				sc.unset[k] = true
			}
			if o.kind != 0 {
				sc.unknown[k] = o.kind
			}
		}
	}
}

// envHazards applies what the environment makes the shell do before the line runs:
// functions it exports, a startup file, options it imports.
func envHazards(env []string, sc *scope) {
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "BASH_FUNC_"):
			name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(k, "BASH_FUNC_"), "%%"), "()")
			sc.funcs[name] = true
		case (k == "BASH_ENV" || k == "ENV") && v != "":
			sc.changeShell(k + " is set, so the shell runs that file before the line")
		case k == "BASHOPTS" || k == "SHELLOPTS" || k == "POSIXLY_CORRECT" || k == "GLOBIGNORE" && v != "":
			sc.changeShell(k + " is in the environment, which changes how the shell expands the line")
		}
	}
}
