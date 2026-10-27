// Command comparedecisions compares two listpilot decision files row by row,
// keyed by (id, system), and reports every row whose decision differs: whether
// the system asked (prompted) or the class it gave. It is how an analyzer fix
// is checked against the frozen round-7 decisions (docs/analyzer-defects.md).
//
//	comparedecisions FROZEN.jsonl RERUN.jsonl
//
// It exits 1 when the files do not hold the same rows, because then the
// comparison itself is invalid, and 2 on a usage or read error. Differing
// decisions are reported, not treated as an error: deciding whether a change
// is acceptable is the reader's job.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

type key struct{ ID, System string }

type row struct {
	ID       string  `json:"id"`
	System   string  `json:"system"`
	Prompted *bool   `json:"prompted"`
	Class    *string `json:"class"`
}

type report struct {
	Rows           int
	Missing, Extra []key
	Changed        []key
}

func load(r io.Reader) (map[key]row, error) {
	out := map[key]row{}
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 16<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var x row
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		k := key{x.ID, x.System}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("line %d: duplicate row %v", n, k)
		}
		out[k] = x
	}
	return out, sc.Err()
}

func same(a, b *bool) bool    { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func sameS(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func compare(a, b map[key]row) report {
	var r report
	r.Rows = len(a)
	for k, x := range a {
		y, ok := b[k]
		if !ok {
			r.Missing = append(r.Missing, k)
			continue
		}
		if !same(x.Prompted, y.Prompted) || !sameS(x.Class, y.Class) {
			r.Changed = append(r.Changed, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			r.Extra = append(r.Extra, k)
		}
	}
	for _, s := range [][]key{r.Missing, r.Extra, r.Changed} {
		sort.Slice(s, func(i, j int) bool { return s[i].ID+s[i].System < s[j].ID+s[j].System })
	}
	return r
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: comparedecisions FROZEN.jsonl RERUN.jsonl")
		os.Exit(2)
	}
	var m [2]map[key]row
	for i, p := range os.Args[1:] {
		f, err := os.Open(p)
		if err == nil {
			m[i], err = load(f)
			f.Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			os.Exit(2)
		}
	}
	r := compare(m[0], m[1])
	fmt.Printf("rows %d, missing %d, extra %d, decisions changed %d\n", r.Rows, len(r.Missing), len(r.Extra), len(r.Changed))
	for _, k := range r.Changed {
		a, b := m[0][k], m[1][k]
		fmt.Printf("changed %s %s: prompted %v -> %v, class %v -> %v\n", k.ID, k.System, deref(a.Prompted), deref(b.Prompted), derefS(a.Class), derefS(b.Class))
	}
	for _, k := range r.Missing {
		fmt.Printf("missing %s %s\n", k.ID, k.System)
	}
	for _, k := range r.Extra {
		fmt.Printf("extra %s %s\n", k.ID, k.System)
	}
	if len(r.Missing) > 0 || len(r.Extra) > 0 {
		os.Exit(1)
	}
}

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func derefS(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
