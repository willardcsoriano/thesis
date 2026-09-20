package oracle

import (
	"hash/fnv"
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// BaseFixture is the directory every corpus command starts from. It is the same
// tree the earlier pilot rounds used, so results stay comparable.
func BaseFixture() []Entry {
	files := [][2]string{
		{"a.txt", "a\n"}, {"b.txt", "b\n"}, {"notes.txt", "notes\n"}, {"app.conf", "k=v\n"},
		{"report.docx", "doc\n"}, {"data.csv", "x,1\ny,2\n"}, {"big.log", "log\n"},
		{"src/main.go", "package main\n"}, {"logs/x.log", "x\n"}, {"logs/y.log", "y\n"},
		{"build/out.o", "o\n"}, {"backup/old.txt", "old\n"},
	}
	fx := make([]Entry, 0, len(files)+1)
	for _, f := range files {
		fx = append(fx, Entry{Path: f[0], Kind: "file", Data: f[1]})
	}
	return append(fx, Entry{Path: "tmp", Kind: "dir"})
}

var (
	pathish   = regexp.MustCompile(`^[A-Za-z0-9_.@+,%-][A-Za-z0-9_./@+,%*?\[\]-]*$`)
	pureNum   = regexp.MustCompile(`^[0-9.+-]+$`)
	globChars = strings.NewReplacer("*", "", "?", "", "[", "", "]", "")
)

// variant selects which pre-existence choice Infer makes for the paths a command
// creates (its last operand and its redirect targets): 0 leaves them absent, 1
// creates them. A command is tried with both, so overwrites are represented.
type Variant int

// Infer builds a fixture for cmd: the base tree, plus every relative file-like
// word the command names, so that it has something to act on. Words are
// instantiated, not interpreted: a name with a trailing slash, a name others are
// nested under, or the target of -C / find / cd is a directory, and a glob
// becomes a few matching files. What the command then does to them is observed,
// never assumed. Absolute paths and paths with parent references are left alone,
// since they escape the fixture.
func Infer(cmd string, v Variant) []Entry {
	fx := BaseFixture()
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(cmd), "")
	if err != nil {
		return fx
	}
	have := map[string]int{}
	for i, e := range fx {
		have[e.Path] = i
	}
	dirs := map[string]bool{}
	files := []string{}
	patterns := []string{}

	addName := func(p string, dir bool) {
		p = path.Clean(strings.TrimPrefix(p, "./"))
		if p == "." || p == "" || strings.HasPrefix(p, "..") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
			return
		}
		if dir {
			dirs[p] = true
		} else {
			files = append(files, p)
		}
	}

	syntax.Walk(f, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		words := make([]string, len(call.Args))
		for i, w := range call.Args {
			words[i] = literal(w)
		}
		cmdName := path.Base(words[0])
		last := -1
		for i := len(words) - 1; i >= 1; i-- {
			if words[i] != "" && !strings.HasPrefix(words[i], "-") {
				last = i
				break
			}
		}
		recursive := false
		for _, w := range words[1:] {
			if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.ContainsAny(w, "rR") {
				recursive = true
			}
		}
		dirCmd := map[string]bool{"find": true, "du": true, "tree": true, "ls": true, "cd": true, "rmdir": true, "mkdir": true, "pushd": true}
		for i := 1; i < len(words); i++ {
			w := words[i]
			if w == "" {
				continue
			}
			if strings.HasPrefix(w, "-") {
				if k := strings.IndexByte(w, '='); strings.HasPrefix(w, "--") && k > 0 {
					w = w[k+1:]
				} else {
					continue
				}
			}
			prev := ""
			if i > 1 {
				prev = words[i-1]
			}
			switch prev {
			case "-name", "-iname", "-path", "-ipath", "-wholename", "-regex":
				patterns = append(patterns, w)
				continue
			case "-exec", "-execdir", "-ok", "-user", "-group", "-perm", "-size", "-mtime", "-atime", "-ctime", "-newer",
				"-type", "-maxdepth", "-mindepth", "-n", "-e", "-s", "-c", "-m", "-p", "-u", "-g", "-k", "-t":
				continue
			}
			if pureNum.MatchString(w) || !pathish.MatchString(w) {
				continue
			}
			isCreate := i == last && (cmdName == "cp" || cmdName == "mv" || cmdName == "ln" || cmdName == "touch" || cmdName == "mkdir" || cmdName == "tar" && prev == "-f")
			if isCreate && v == 0 && !strings.HasSuffix(w, "/") {
				continue
			}
			isDir := strings.HasSuffix(w, "/") || dirCmd[cmdName] || prev == "-C" || prev == "-d" ||
				(recursive && !strings.Contains(path.Base(w), ".") && !strings.ContainsAny(w, "*?["))
			if strings.ContainsAny(w, "*?[") {
				patterns = append(patterns, w)
				continue
			}
			addName(w, isDir)
		}
		return true
	})

	// Redirect targets: present or absent by variant, so both create and overwrite occur.
	syntax.Walk(f, func(n syntax.Node) bool {
		r, ok := n.(*syntax.Redirect)
		if !ok || r.Word == nil {
			return true
		}
		if w := literal(r.Word); w != "" && v == 1 && pathish.MatchString(w) {
			addName(w, false)
		}
		return true
	})

	for _, p := range patterns {
		instantiate(p, addName)
	}
	// A name that others are nested under is a directory.
	all := append([]string(nil), files...)
	for _, p := range files {
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	for d := range dirs {
		for p := path.Dir(d); p != "." && p != "/"; p = path.Dir(p) {
			dirs[p] = true
		}
	}
	for _, d := range sortedKeys(dirs) {
		if _, ok := have[d]; !ok {
			have[d] = len(fx)
			fx = append(fx, Entry{Path: d, Kind: "dir"})
		}
	}
	for _, p := range all {
		if dirs[p] {
			continue
		}
		if _, ok := have[p]; !ok {
			have[p] = len(fx)
			fx = append(fx, Entry{Path: p, Kind: "file", Data: content(p)})
		}
	}
	// Recursive commands need something inside each directory they name.
	for _, d := range sortedKeys(dirs) {
		if i := have[d]; i < len(fx) && fx[i].Kind == "dir" {
			inner := path.Join(d, "item.txt")
			if _, ok := have[inner]; !ok {
				have[inner] = len(fx)
				fx = append(fx, Entry{Path: inner, Kind: "file", Data: content(inner)})
			}
		}
	}
	return fx
}

// instantiate turns a glob or name pattern into a couple of matching paths.
func instantiate(p string, add func(string, bool)) {
	p = strings.TrimPrefix(p, "./")
	if strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return
	}
	dir, base := path.Split(p)
	dir = globChars.Replace(dir)
	dir = strings.Trim(dir, "/")
	if strings.ContainsAny(base, "*?[") {
		for _, stem := range []string{"alpha", "beta"} {
			name := strings.NewReplacer("*", stem, "?", "z").Replace(base)
			name = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(name, "a")
			add(path.Join(dir, name), false)
		}
		return
	}
	if base != "" && pathish.MatchString(base) {
		add(path.Join(dir, base), false)
	}
}

// literal returns the word's text when it is made only of literals and quotes.
func literal(w *syntax.Word) string {
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, ok := q.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(l.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}

// content gives a file plausible text, varied by name so that files differ.
func content(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return "hello world\nerror: sample " + name + "\nfoo bar baz\n" + string(rune('a'+h.Sum32()%26)) + "\n"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// shallow first, so parents precede children
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (strings.Count(out[j], "/") < strings.Count(out[j-1], "/") ||
			strings.Count(out[j], "/") == strings.Count(out[j-1], "/") && out[j] < out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
