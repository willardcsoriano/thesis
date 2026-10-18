// Package shape classifies a command line by the syntactic form that decides how
// hard its effects are to see from the string alone. It is the classifier the
// corpora were built with (cmd/corpusgen), moved here so the agent command study
// (internal/probe) scores agent commands with the very same copy.
//
// The shapes, in precedence order:
//
//   - hidden: some call runs a program that runs other code (a shell, eval, an
//     interpreter, env, nohup, time, command, busybox), so the string does not
//     show the effect.
//   - runtime: the targets are only known when it runs (find, xargs, command or
//     process substitution, a for or while loop).
//   - compound: more than one statement or more than one call.
//   - redirect: a single call whose output is redirected to a file (>, >>, >|).
//   - plain: anything else, including a line that does not parse.
//
// The corpora also store a sixth shape, "external", for commands whose effects
// lie outside the filesystem. That is a partition rule applied by corpusgen
// before the classifier, not something Of returns.
package shape

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Of returns the shape of cmd: "hidden", "runtime", "compound", "redirect" or
// "plain".
func Of(cmd string) string {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return "plain"
	}
	hidden, runtime, redirect := false, false, false
	stmts, calls := len(f.Stmts), 0
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.CallExpr:
			calls++
			if len(x.Args) > 0 {
				switch path.Base(Lit(x.Args[0])) {
				case "sh", "bash", "zsh", "dash", "eval", "python", "python3", "perl", "ruby", "awk", "node", "env", "nohup", "time", "command", "busybox":
					hidden = true
				case "find", "xargs":
					runtime = true
				}
			}
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ForClause, *syntax.WhileClause:
			runtime = true
		case *syntax.Redirect:
			if x.Op == syntax.RdrOut || x.Op == syntax.AppOut || x.Op == syntax.RdrClob {
				redirect = true
			}
		}
		return true
	})
	switch {
	case hidden:
		return "hidden"
	case runtime:
		return "runtime"
	case stmts > 1 || calls > 1:
		return "compound"
	case redirect:
		return "redirect"
	}
	return "plain"
}

// Lit returns the literal text of w: unquoted literals, single-quoted text, and
// the literal parts of double-quoted text. Expansions contribute nothing.
func Lit(w *syntax.Word) string {
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				if l, ok := q.(*syntax.Lit); ok {
					b.WriteString(l.Value)
				}
			}
		}
	}
	return b.String()
}
