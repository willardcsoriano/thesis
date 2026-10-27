package main

import (
	"strings"
	"testing"
)

func mustLoad(t *testing.T, s string) map[key]row {
	t.Helper()
	m, err := load(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Known answers, including the ones where the instrument must say "different".
func TestCompareKnownAnswers(t *testing.T) {
	base := `{"id":"N1","system":"L0","prompted":true,"class":null}
{"id":"N1","system":"ALG-strict","prompted":false,"class":"recoverable"}
{"id":"N2","system":"ALG-strict","prompted":true,"class":"unrecoverable"}
`
	cases := []struct {
		name                    string
		other                   string
		changed, missing, extra int
	}{
		{"identical", base, 0, 0, 0},
		{"same rows, other order", `{"id":"N2","system":"ALG-strict","prompted":true,"class":"unrecoverable"}
{"id":"N1","system":"ALG-strict","prompted":false,"class":"recoverable"}
{"id":"N1","system":"L0","prompted":true,"class":null}
`, 0, 0, 0},
		{"prompted flips", strings.Replace(base, `"ALG-strict","prompted":false`, `"ALG-strict","prompted":true`, 1), 1, 0, 0},
		{"class changes", strings.Replace(base, `"unrecoverable"`, `"recoverable-with-capture"`, 1), 1, 0, 0},
		{"null class becomes a value", strings.Replace(base, `"class":null`, `"class":"recoverable"`, 1), 1, 0, 0},
		{"same id, other system is a different row", strings.Replace(base, `"N2","system":"ALG-strict"`, `"N2","system":"ALG-capture"`, 1), 0, 1, 1},
		{"a row missing", strings.SplitAfterN(base, "\n", 2)[1], 0, 1, 0},
		{"a reason change alone is not a decision change", strings.Replace(base, `"class":"recoverable"}`, `"class":"recoverable","reason":"new words"}`, 1), 0, 0, 0},
	}
	a := mustLoad(t, base)
	for _, c := range cases {
		r := compare(a, mustLoad(t, c.other))
		if len(r.Changed) != c.changed || len(r.Missing) != c.missing || len(r.Extra) != c.extra {
			t.Errorf("%s: changed %d missing %d extra %d, want %d %d %d", c.name, len(r.Changed), len(r.Missing), len(r.Extra), c.changed, c.missing, c.extra)
		}
	}
}

func TestLoadRefusesDuplicatesAndBadLines(t *testing.T) {
	if _, err := load(strings.NewReader(`{"id":"N1","system":"L0"}` + "\n" + `{"id":"N1","system":"L0"}` + "\n")); err == nil {
		t.Error("a duplicate row was accepted")
	}
	if _, err := load(strings.NewReader("{not json\n")); err == nil {
		t.Error("a malformed line was accepted")
	}
}
