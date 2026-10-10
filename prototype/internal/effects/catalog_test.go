package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	docPath    = "../../../docs/recoverability-analysis.md"
	beginMark  = "<!-- BEGIN GENERATED: rule-map -->"
	endMark    = "<!-- END GENERATED: rule-map -->"
	updateHint = "regenerate with `make -C prototype rulemap`"
)

func TestCatalogCoversEveryRule(t *testing.T) {
	for k := range rules {
		if ruleNotes[k] == "" {
			t.Errorf("rule %q has no entry in ruleNotes", k)
		}
	}
	for k := range ruleNotes {
		if rules[k] == nil {
			t.Errorf("ruleNotes describes %q, which has no rule", k)
		}
	}
}

// TestCatalogDocIsCurrent keeps the rule map in the reference document identical to
// the tables in the code. With UPDATE_DOCS=1 it rewrites the section instead.
func TestCatalogDocIsCurrent(t *testing.T) {
	path, _ := filepath.Abs(docPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	doc := string(raw)
	i, j := strings.Index(doc, beginMark), strings.Index(doc, endMark)
	if i < 0 || j < i {
		t.Fatalf("%s has no generated rule-map section", path)
	}
	want := "\n" + CatalogMarkdown() + "\n"
	got := doc[i+len(beginMark) : j]
	if got == want {
		return
	}
	if os.Getenv("UPDATE_DOCS") == "1" {
		if err := os.WriteFile(path, []byte(doc[:i+len(beginMark)]+want+doc[j:]), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("the rule map in %s is out of date; %s", path, updateHint)
}
