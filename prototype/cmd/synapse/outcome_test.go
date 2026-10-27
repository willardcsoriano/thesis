package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Every outcome the loop can record is named in docs/cli.md, which is what a
// script reading `run --json` has to go on. The list is read from outcome.go,
// so an outcome added later is covered without anyone updating this test.
func TestEveryOutcomeIsDocumented(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "outcome.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if !strings.HasPrefix(n.Name, "outcome") || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					v, _ := strconv.Unquote(lit.Value)
					values = append(values, v)
				}
			}
		}
	}
	if len(values) < 10 {
		t.Fatalf("found only %d outcomes (%v); the source scan is broken", len(values), values)
	}
	doc, err := os.ReadFile("../../../docs/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if !strings.Contains(string(doc), "`"+v+"`") {
			t.Errorf("outcome %q is not named in docs/cli.md", v)
		}
	}
}
