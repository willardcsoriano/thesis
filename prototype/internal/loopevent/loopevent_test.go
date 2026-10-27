package loopevent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// declaredKinds reads the Kind constants from this package's source, so the
// test below covers a kind added later without anyone updating a bound.
func declaredKinds(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "loopevent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		inKind := false
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); ok {
				inKind = id.Name == "Kind"
			} else if vs.Type != nil || len(vs.Values) > 0 {
				inKind = false
			}
			if inKind {
				for _, n := range vs.Names {
					names = append(names, n.Name)
				}
			}
		}
	}
	return names
}

// Every Kind the package declares has a wire name, and no two share one: a
// Kind added without a name would reach `run --json` consumers as "unknown".
func TestEveryKindHasAUniqueWireName(t *testing.T) {
	names := declaredKinds(t)
	if len(names) < 8 {
		t.Fatalf("found only %d Kind constants (%v); the source scan is broken", len(names), names)
	}
	seen := map[string]Kind{}
	for k := Kind(0); int(k) < len(names); k++ {
		n := k.String()
		if n == "unknown" {
			t.Errorf("Kind %s has no wire name", names[k])
		}
		if prev, dup := seen[n]; dup {
			t.Errorf("Kinds %s and %s share the wire name %q", names[prev], names[k], n)
		}
		seen[n] = k
	}
	if Kind(-1).String() != "unknown" || Kind(len(names)).String() != "unknown" {
		t.Error("an undeclared Kind did not report unknown")
	}
}
