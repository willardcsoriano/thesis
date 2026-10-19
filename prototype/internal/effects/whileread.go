package effects

import (
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// A `while read` loop at the end of a pipeline runs its body once per record the
// pipeline before it produces:
//
//	find . -type f -print0 | while IFS= read -r -d '' f; do rm -- "$f"; done
//
// When that producer is one the analysis can already resolve (proven read-only, then
// run, exactly as for xargs), the records are split as read would split them, and the
// body is analysed once per record with the variable bound, as a for loop is. Anything
// about the read the analysis does not model exactly leaves the loop as before: the
// variable is unbound and the body asks.

// readLoop is a `while read` condition whose records the analysis can reproduce.
type readLoop struct {
	name  string // the variable read into; REPLY when none is named
	delim byte   // the record delimiter: '\n' by default, 0 for -d ''
	raw   bool   // -r: backslashes are kept as they are
	trim  string // the IFS whitespace read strips from both ends of the record
}

// readLoopSpec recognises a loop condition that is exactly one `read`, with at most an
// IFS assignment before it, the options -r, -s, -d, and at most one variable. Every
// other form (-a, -n, -u, a timeout, several variables, an IFS with characters other
// than whitespace) returns false.
func (st *state) readLoopSpec(cond []*syntax.Stmt, sc *scope) (readLoop, bool) {
	rl := readLoop{delim: '\n'}
	if len(cond) != 1 {
		return rl, false
	}
	s := cond[0]
	ce, ok := s.Cmd.(*syntax.CallExpr)
	if !ok || s.Negated || s.Background || len(s.Redirs) > 0 || len(ce.Args) == 0 || ce.Args[0].Lit() != "read" {
		return rl, false
	}
	ifs := " \t\n" // bash does not take IFS from the environment
	if v, ok := sc.vars["IFS"]; ok {
		ifs = v
	}
	for _, as := range ce.Assigns {
		if as.Name == nil || as.Name.Value != "IFS" || as.Append || as.Array != nil || as.Index != nil {
			return rl, false
		}
		ifs = ""
		if as.Value != nil {
			v, err := st.literal(as.Value, sc)
			if err != nil {
				return rl, false
			}
			ifs = v
		}
	}
	var names []string
	for i := 1; i < len(ce.Args); i++ {
		a, err := st.literal(ce.Args[i], sc)
		if err != nil {
			return rl, false
		}
		if len(names) > 0 || a == "" || a[0] != '-' || a == "-" {
			names = append(names, a)
			continue
		}
		if a == "--" {
			for _, w := range ce.Args[i+1:] {
				n, err := st.literal(w, sc)
				if err != nil {
					return rl, false
				}
				names = append(names, n)
			}
			break
		}
	cluster:
		for j := 1; j < len(a); j++ {
			switch a[j] {
			case 'r':
				rl.raw = true
			case 's':
			case 'd':
				v := a[j+1:]
				if v == "" {
					if i+1 >= len(ce.Args) {
						return rl, false
					}
					i++
					if v, err = st.literal(ce.Args[i], sc); err != nil {
						return rl, false
					}
				}
				rl.delim = 0
				if v != "" {
					rl.delim = v[0]
				}
				break cluster
			default:
				return rl, false
			}
		}
	}
	switch len(names) {
	case 0:
		rl.name = "REPLY" // read with no name keeps the record whole
		return rl, true
	case 1:
		if !identifier.MatchString(names[0]) {
			return rl, false
		}
		rl.name = names[0]
	default:
		return rl, false
	}
	if strings.Trim(ifs, " \t\n") != "" {
		return rl, false // a non-whitespace IFS character changes how one variable is assigned
	}
	rl.trim = ifs
	return rl, true
}

// records splits a producer's output into the values read assigns, one per pass. It
// refuses (false) output that read would alter in ways not modelled here: NUL bytes,
// which bash drops from a record unless NUL is the delimiter, and backslashes without
// -r. A final record without its delimiter makes read fail, so the body does not run
// for it; it is analysed anyway, which can only record more.
func records(data []byte, rl readLoop) ([]string, bool) {
	if rl.delim != 0 && bytes.IndexByte(data, 0) >= 0 {
		return nil, false
	}
	if !rl.raw && bytes.IndexByte(data, '\\') >= 0 {
		return nil, false
	}
	if len(data) == 0 {
		return nil, true
	}
	parts := bytes.Split(data, []byte{rl.delim})
	if len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.Trim(string(p), rl.trim))
	}
	return out, true
}

// whileRead analyses a `while read` loop that is a later stage of a pipeline. It
// reports false when the condition is not a read it models, leaving the loop to the
// general rule.
func (st *state) whileRead(x *syntax.WhileClause, sc *scope, pc *pipeCtx, src string) bool {
	rl, ok := st.readLoopSpec(x.Cond, sc)
	if !ok {
		return false
	}
	c := &call{src: src, sc: sc, pc: pc}
	unbound := func() {
		st.loopBody(x.Do, sc, func(s *scope) { s.drop(rl.name) })
	}
	// When the records cannot be known, the loop is analysed as before, with the
	// variable unbound: a body that uses it for anything but reading asks, and a body
	// that only reads (printf "$f") passes, as it should.
	data, producer, why := st.resolveProducer(c, "the loop", "the loop's")
	if why != "" {
		unbound()
		return true
	}
	items, ok := records(data, rl)
	if !ok || len(items) > st.a.MaxLoop {
		unbound()
		return true
	}
	before := len(st.an.Effects)
	// The passes share the subshell's state: each starts from what the one before left.
	sc.fresh(func() {
		for i, it := range items {
			if i > 0 {
				st.mayHaveFailed(sc) // the pass before may have failed part-way
			}
			sc.set(rl.name, it)
			st.stmts(x.Do, sc)
		}
	})
	sc.drop(rl.name) // the read that ends the loop empties the variable
	if len(items) > 0 {
		st.virt.doubt()
	}
	if why := st.feedsOnItself(producer, st.an.Effects[before:], pc, sc); why != "" {
		st.unresolved(c, why)
	}
	return true
}

// feedsOnItself reports why the producer, which runs at the same time as the loop, may
// give records it did not give when it was resolved, because the body creates files it
// could still list or writes a file it reads; or "" when it cannot. A producer that
// lists nothing (printf, echo), or reads only named files, or lists one directory in
// full before it prints (ls without -R), cannot see the body's new files; find sees
// them only under its starting points.
func (st *state) feedsOnItself(producer string, body []Effect, pc *pipeCtx, sc *scope) string {
	var made []string
	for _, e := range body {
		if e.Kind == Create {
			made = append(made, e.Path)
		}
	}
	if st.readsWhatTheLineWrites(producer) {
		return "the loop writes a file the command feeding it reads"
	}
	if len(made) == 0 {
		return ""
	}
	const why = "the loop creates files where the command feeding it may still be listing"
	for _, stage := range pc.stages[:pc.idx] {
		ce, ok := stage.Cmd.(*syntax.CallExpr)
		if !ok || len(ce.Args) == 0 {
			return why
		}
		argv, err := st.expand(ce.Args, sc)
		if err != nil || len(argv) == 0 {
			return why
		}
		name, args := argv[0], argv[1:]
		switch {
		case namedInputOnly[name]:
			if name == "grep" && hasPrefixAny(args, "-r", "-R", "--recursive", "-d", "--directories") {
				return why
			}
			if (name == "grep" || name == "egrep" || name == "fgrep") && grepClusterRecurses(args) {
				return why
			}
		case name == "ls":
			o := parseOpts(args, "")
			if o.has("R", "recursive", "U", "f") || hasPrefixAny(args, "--sort") || len(o.operands) > 1 {
				return why
			}
		case name == "find":
			pre, roots, _ := splitFind(args)
			if hasAny(pre, "-L") {
				return why
			}
			if len(roots) == 0 {
				roots = []string{"."}
			}
			for _, r := range roots {
				root := absIn(sc, r)
				for _, p := range made {
					if p == root || strings.HasPrefix(p, root+"/") {
						return why
					}
				}
			}
		default:
			return why
		}
	}
	return ""
}

// namedInputOnly are producers that read only the files they are given (or standard
// input), so files the loop creates elsewhere cannot reach them.
var namedInputOnly = setOf(`printf echo cat head tail sort uniq cut tr sed awk gawk mawk nawk grep egrep fgrep
basename dirname realpath readlink jq wc comm paste nl tac rev fold seq`)

func grepClusterRecurses(args []string) bool {
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], "rR") {
			return true
		}
	}
	return false
}
