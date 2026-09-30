package effects

import (
	"io"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// FailClosed is the baseline the effect analysis has to beat: a list flipped to
// fail closed. Every command in the line, including those inside substitutions,
// loops, and wrappers (sudo, find -exec, xargs, sh -c), must be a known read-only
// form, or the line asks. It resolves nothing, computes no targets, and plans no
// capture — it is what a careful allowlist plus a parser gets you, and no more.
func FailClosed(cmd string) (asks bool, reasons []string) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return true, []string{"does not parse"}
	}
	fc := &failClosed{}
	fc.file(f, cmd)
	return len(fc.reasons) > 0, fc.reasons
}

type failClosed struct{ reasons []string }

func (fc *failClosed) ask(why string) { fc.reasons = append(fc.reasons, why) }

func (fc *failClosed) file(f *syntax.File, src string) {
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.Redirect:
			switch x.Op {
			case syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.RdrInOut, syntax.RdrAll, syntax.AppAll:
				if x.Word != nil {
					if w := staticWord(x.Word); !strings.HasPrefix(w, "/dev/") {
						fc.ask("redirects output to a file")
					}
				}
			}
		case *syntax.CallExpr:
			if len(x.Args) > 0 {
				argv := make([]string, len(x.Args))
				for i, w := range x.Args {
					argv[i] = staticWord(w)
				}
				fc.leaf(argv, x.Args, src)
			}
		case *syntax.FuncDecl:
			fc.ask("defines a function")
		case *syntax.DeclClause, *syntax.LetClause:
		}
		return true
	})
}

// staticWord is a word's text with quoting removed and no expansion performed.
func staticWord(w *syntax.Word) string {
	cfg := &expand.Config{
		Env:       expand.ListEnviron(),
		CmdSubst:  func(io.Writer, *syntax.CmdSubst) error { return errNotResolved },
		ProcSubst: func(*syntax.ProcSubst) (string, error) { return "", errNotResolved },
		NoUnset:   true,
	}
	if f, err := expand.Fields(cfg, w); err == nil && len(f) == 1 {
		return f[0]
	}
	s, err := expand.Literal(cfg, w)
	if err != nil {
		var b strings.Builder
		_ = syntax.NewPrinter().Print(&b, w)
		return b.String()
	}
	return s
}

func (fc *failClosed) leaf(argv []string, words []*syntax.Word, src string) {
	name := argv[0]
	if strings.Contains(name, "/") {
		if !stdBinDirs[dirOf(name)] {
			fc.ask("runs " + name + " by path")
			return
		}
		name = baseOf(name)
	}
	args := argv[1:]
	switch name {
	case "cd", "export", "declare", "local", "readonly", "typeset", "pushd", "popd":
		return
	}
	if w, ok := wrappers[name]; ok {
		if inner := stripWrapper(args, w); len(inner) > 0 {
			fc.leaf(inner, nil, src)
		}
		return
	}
	switch name {
	case "find":
		fc.find(args)
		return
	case "xargs":
		inner := xargsInner(args)
		if len(inner) > 0 {
			fc.leaf(inner, nil, src)
		}
		return
	case "sh", "bash", "zsh", "dash", "ksh", "ash":
		for i, a := range args {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.HasSuffix(a, "c") && i+1 < len(args) {
				if bad, rs := FailClosed(args[i+1]); bad {
					fc.reasons = append(fc.reasons, rs...)
				}
				return
			}
		}
		fc.ask("runs a shell script or piped code")
		return
	}
	if readOnlyForm(name, args, true) {
		return
	}
	if _, ok := stateTools[name]; ok {
		if stateTools[name].ro(args) {
			return
		}
	}
	fc.ask(name + " is not a known read-only command")
}

func (fc *failClosed) find(args []string) {
	_, _, expr := splitFind(args)
	_, acts, files, err := rewriteFind(expr)
	if err != nil || len(files) > 0 {
		fc.ask("find writes files")
		return
	}
	for _, a := range acts {
		if a.kind == "delete" {
			fc.ask("find -delete")
			continue
		}
		if len(a.inner) == 0 {
			fc.ask("empty find -exec")
			continue
		}
		fc.leaf(a.inner, nil, "")
	}
}

func dirOf(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func baseOf(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

// stripWrapper and xargsInner return the command a wrapper runs, without
// resolving anything.
func stripWrapper(args []string, w wrapperSpec) []string {
	long := setOf(strings.Join(w.longWithArg, " "))
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
	i += w.positional
	if w.assigns {
		for i < len(args) && assignWord.MatchString(args[i]) {
			i++
		}
	}
	if i >= len(args) {
		return nil
	}
	return args[i:]
}

func xargsInner(args []string) []string {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--":
			return args[i+1:]
		case a == "-I" || a == "-d" || a == "-a" || a == "-n" || a == "-L" || a == "-P" || a == "-s" || a == "-E" || a == "-l":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return args[i:]
		}
		i++
	}
	return nil
}
