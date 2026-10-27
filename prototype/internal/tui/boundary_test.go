package tui

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// presentationMayReach is everything in this module the TUI may depend on,
// directly or through another package. The TUI is presentation: it receives
// the loop as an injected TaskRunner and its progress as loopevent events,
// and it must never run, classify, gate, or undo a command itself (D26, D48).
// Importing the executor, the analysis, the classifier, the gate, or undo
// would let it, so this list is an allowlist, not a denylist: a new
// dependency fails here until someone decides it belongs to presentation.
var presentationMayReach = map[string]bool{
	"synapseos/internal/loopevent": true,
	"synapseos/internal/termsafe":  true, // shows untrusted text safely; runs nothing
}

const module = "synapseos/"

func TestTUIReachesOnlyPresentationPackages(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	reached := map[string][]string{} // package -> the chain that reached it
	var walk func(pkg string, chain []string)
	walk = func(pkg string, chain []string) {
		for _, imp := range moduleImports(t, filepath.Join(root, strings.TrimPrefix(pkg, module))) {
			if _, seen := reached[imp]; seen {
				continue
			}
			next := append(append([]string{}, chain...), imp)
			reached[imp] = next
			walk(imp, next)
		}
	}
	walk(module+"internal/tui", []string{module + "internal/tui"})

	var bad []string
	for pkg, chain := range reached {
		if !presentationMayReach[pkg] {
			bad = append(bad, strings.Join(chain, " -> "))
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("the TUI reaches a non-presentation package: %s", b)
	}
	if len(reached) == 0 {
		t.Fatal("found no module imports at all; the walk is broken, not the TUI clean (it must reach loopevent)")
	}
}

// moduleImports returns the module-internal packages imported by the non-test
// Go files in dir.
func moduleImports(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	set := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", n, err)
		}
		for _, is := range f.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			if strings.HasPrefix(p, module) {
				set[p] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
