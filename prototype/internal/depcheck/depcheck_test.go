package depcheck

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

const (
	prototypeRoot = "../.."
	manifestPath  = "../../../distro/manifest.tsv"
)

// shellScripts are the shell programs the image runs, scanned for the
// commands they call. The build scripts (check.sh, hoard.sh) are not here:
// they run on the machine that assembles the image, not in it.
var shellScripts = []string{"../../../distro/synapseos-session"}

// indirect are programs the code depends on that the scan cannot see, each
// with the file, relative to prototype/, that makes it one and a literal that
// file must still contain. When the literal goes, the entry is stale and the
// test fails, so this list cannot outlive the code it describes.
var indirect = []struct {
	program, file, literal, why string
}{
	{"pasta", "internal/probe/claudecode/network.go", `look("pasta")`,
		"looked up through a func parameter that is exec.LookPath outside tests; builds the isolated network"},
	{"flock", "internal/probe/claudecode/network.go", `"flock"`,
		"resolved through a table of {name, destination} pairs; holds the isolated namespace's lifeline"},
	{"true", "internal/oracle/oracle.go", `"true")`,
		"the program bubblewrap starts to prove namespaces work (also in effects/runner.go and cmd/recoverycheck)"},
	{"xclip", "go.mod", "github.com/atotto/clipboard",
		"the TUI's text input pastes on ctrl+v through atotto/clipboard, which runs xclip (or xsel, wl-paste)"},
}

// notDependencies are names the scan finds that are not programs the
// repository needs, with the reason. Keep it short: a name belongs here only
// when it is provably not something that has to be installed.
// The scan's rules keep it empty today: test data is not scanned beyond what
// a test starts, and the commands under analysis sit in strings, not argv.
var notDependencies = map[string]string{}

// builtins are bash builtins, which a shell script calls without any
// package. They are skipped only in shell scripts: a name Go code starts is
// always a program, true and kill included. (mvdan.cc/sh/v3/interp has the
// list, but importing it would pull golang.org/x/term into the module for one
// test.)
var builtins = map[string]bool{}

func init() {
	for _, b := range strings.Fields(`. : [ alias bg bind break builtin caller cd command compgen complete
		continue declare dirs disown echo enable eval exec exit export false fc fg getopts hash help
		history jobs kill let local logout mapfile popd printf pushd pwd read readarray readonly return
		set shift shopt source suspend test times trap true type typeset ulimit umask unalias unset wait`) {
		builtins[b] = true
	}
}

var programName = regexp.MustCompile(`^[a-z][a-z0-9._+-]*$`)

var binDirs = []string{"/bin/", "/usr/bin/", "/sbin/", "/usr/sbin/"}

type row struct{ kind, name, probe, required, purpose string }

func readManifest(t *testing.T) []row {
	t.Helper()
	f, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []row
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) != 5 {
			t.Fatalf("manifest line %d has %d tab-separated columns, want 5 (kind name probe required purpose)", n, len(cols))
		}
		if n == 1 {
			if strings.Join(cols, " ") != "kind name probe required purpose" {
				t.Fatalf("manifest header is %q", sc.Text())
			}
			continue
		}
		rows = append(rows, row{cols[0], cols[1], cols[2], cols[3], cols[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestManifestRows checks the schema check.sh and hoard.sh parse, and that
// every row says which part of the system needs it, so the image's package
// set can be read off the runtime rows.
func TestManifestRows(t *testing.T) {
	kinds := map[string]bool{"apt": true, "toolchain": true, "gomod": true, "model": true, "binary": true, "pip": true}
	seen := map[string]bool{}
	for _, r := range readManifest(t) {
		if !kinds[r.kind] {
			t.Errorf("%s: unknown kind %q", r.name, r.kind)
		}
		if r.name == "" || r.probe == "" {
			t.Errorf("row %+v: name and probe must be set (probe - when there is no command)", r)
		}
		if r.required != "yes" && r.required != "no" {
			t.Errorf("%s: required is %q, want yes or no", r.name, r.required)
		}
		role, _, ok := strings.Cut(r.purpose, ": ")
		if !ok || (role != "runtime" && role != "study" && role != "build") {
			t.Errorf("%s: purpose %q must start with runtime:, study: or build:", r.name, r.purpose)
		}
		if r.required == "yes" && role != "runtime" {
			t.Errorf("%s: required yes is for runtime essentials only, but its role is %s", r.name, role)
		}
		key := r.kind + "\t" + r.name + "\t" + r.probe
		if seen[key] {
			t.Errorf("duplicate row %s %s %s", r.kind, r.name, r.probe)
		}
		seen[key] = true
	}
}

// TestEveryProgramIsInTheManifest fails when Go code starts or looks up a
// program that no manifest row probes for.
func TestEveryProgramIsInTheManifest(t *testing.T) {
	probes := map[string]bool{}
	for _, r := range readManifest(t) {
		probes[r.probe] = true
	}
	refs := scan(t)
	for _, in := range indirect {
		b, err := os.ReadFile(filepath.Join(prototypeRoot, in.file))
		if err != nil {
			t.Errorf("indirect %s: %v", in.program, err)
			continue
		}
		if !strings.Contains(string(b), in.literal) {
			t.Errorf("indirect %s is stale: %s no longer contains %s; remove the entry or update it", in.program, in.file, in.literal)
			continue
		}
		refs[in.program] = append(refs[in.program], in.file+" ("+in.why+")")
	}
	for _, p := range shellScripts {
		for name, at := range shellCommands(t, p) {
			refs[name] = append(refs[name], at...)
		}
	}
	var names []string
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Logf("programs referenced: %s", strings.Join(names, " "))
	for _, n := range names {
		if probes[n] {
			continue
		}
		if _, ok := notDependencies[n]; ok {
			continue
		}
		t.Errorf("%s is run by the code but no row of distro/manifest.tsv probes for it; add it (see distro/README.md, Adding a dependency):\n\t%s",
			n, strings.Join(refs[n], "\n\t"))
	}
	for n := range notDependencies {
		if _, ok := refs[n]; !ok {
			t.Errorf("allowlisted %s is no longer referenced; remove it from notDependencies", n)
		}
	}
}

// scan returns every program name the Go sources start or look up, with
// where.
func scan(t *testing.T) map[string][]string {
	t.Helper()
	refs := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(prototypeRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == "bin" || (strings.HasPrefix(n, ".") && n != "." && n != "..") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(prototypeRoot, p)
		test := strings.HasSuffix(p, "_test.go")
		add := func(name string, at token.Pos) {
			for _, d := range binDirs {
				if rest, ok := strings.CutPrefix(name, d); ok && !strings.Contains(rest, "/") {
					name = rest
				}
			}
			if !programName.MatchString(name) {
				return
			}
			pos := fset.Position(at)
			refs[name] = append(refs[name], rel+":"+strconv.Itoa(pos.Line))
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if i := execArg(x); i >= 0 && i < len(x.Args) {
					if s, ok := literal(x.Args[i]); ok {
						add(s, x.Pos())
					}
				}
				if test {
					// Test data is full of commands under analysis; only
					// what a test itself starts or looks up counts.
					return true
				}
				if executorCall(x, f.Name.Name) && len(x.Args) > 0 {
					if s, ok := leadingLiteral(x.Args[len(x.Args)-1]); ok {
						add(strings.Fields(s + " ")[0], x.Pos())
					}
				}
				for _, a := range x.Args {
					if cl, ok := a.(*ast.CompositeLit); ok && isStringSlice(cl.Type) {
						if s, ok := firstLiteral(cl); ok {
							add(s, cl.Pos())
						}
					}
				}
			case *ast.CompositeLit:
				if test {
					return true
				}
				if at, ok := x.Type.(*ast.ArrayType); ok && isStringSlice(at.Elt) {
					for _, e := range x.Elts {
						if inner, ok := e.(*ast.CompositeLit); ok {
							if s, ok := firstLiteral(inner); ok {
								add(s, inner.Pos())
							}
						}
					}
				}
			case *ast.BasicLit:
				if test || x.Kind != token.STRING {
					return true
				}
				s, _ := strconv.Unquote(x.Value)
				for _, d := range binDirs {
					if strings.HasPrefix(s, d) && !strings.ContainsAny(s, " \t\n") {
						add(s, x.Pos())
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return refs
}

// execArg is the index of the program argument when call is exec.Command,
// exec.CommandContext or exec.LookPath, else -1.
func execArg(call *ast.CallExpr) int {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return -1
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "exec" {
		return -1
	}
	switch sel.Sel.Name {
	case "Command", "LookPath":
		return 0
	case "CommandContext":
		return 1
	}
	return -1
}

// executorCall reports whether call is executor.Run or executor.RunIn, whose
// last argument is a shell command line.
func executorCall(call *ast.CallExpr, pkg string) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id, ok := fn.X.(*ast.Ident)
		return ok && id.Name == "executor" && (fn.Sel.Name == "Run" || fn.Sel.Name == "RunIn")
	case *ast.Ident:
		return pkg == "executor" && (fn.Name == "Run" || fn.Name == "RunIn")
	}
	return false
}

func isStringSlice(e ast.Expr) bool {
	at, ok := e.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	id, ok := at.Elt.(*ast.Ident)
	return ok && id.Name == "string"
}

func firstLiteral(cl *ast.CompositeLit) (string, bool) {
	if len(cl.Elts) == 0 {
		return "", false
	}
	return literal(cl.Elts[0])
}

func literal(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	return s, err == nil
}

// leadingLiteral is the literal at the left of a string concatenation, or
// the literal itself.
func leadingLiteral(e ast.Expr) (string, bool) {
	for {
		b, ok := e.(*ast.BinaryExpr)
		if !ok || b.Op != token.ADD {
			return literal(e)
		}
		e = b.X
	}
}

// shellCommands returns the commands a shell script calls by a literal name,
// with where. The word after exec or command is the command.
func shellCommands(t *testing.T, path string) map[string][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	file, err := syntax.NewParser().Parse(f, path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	syntax.Walk(file, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		for _, w := range call.Args {
			name := w.Lit()
			if name == "exec" || name == "command" || strings.HasPrefix(name, "-") {
				continue
			}
			if name != "" && !builtins[name] {
				if base := filepath.Base(name); programName.MatchString(base) {
					out[base] = append(out[base], filepath.Base(path)+":"+strconv.Itoa(int(w.Pos().Line())))
				}
			}
			break
		}
		return true
	})
	return out
}

// TestGoModulesListed fails when go.mod requires a module directly that no
// gomod row names. Indirect requirements are hoarded with them (go mod
// download all) and are not listed one by one.
func TestGoModulesListed(t *testing.T) {
	listed := map[string]bool{}
	for _, r := range readManifest(t) {
		if r.kind == "gomod" {
			listed[r.name] = true
		}
	}
	b, err := os.ReadFile(filepath.Join(prototypeRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	direct := 0
	inBlock := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
			continue
		case line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !inBlock:
			continue
		}
		if line == "" || strings.Contains(line, "// indirect") {
			continue
		}
		mod := strings.Fields(line)[0]
		direct++
		if !listed[mod] {
			t.Errorf("go.mod requires %s directly but distro/manifest.tsv has no gomod row for it", mod)
		}
	}
	if direct == 0 {
		t.Fatal("found no direct requirement in go.mod; the parser is out of step with the file")
	}
}
