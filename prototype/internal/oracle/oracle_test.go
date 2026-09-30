package oracle

import (
	"context"
	"testing"
	"time"
)

func label(t *testing.T, cmd string, v Variant) (Verdict, Outcome) {
	t.Helper()
	bw := Bwrap()
	if bw == "" {
		t.Skip("bubblewrap not available")
	}
	vd, oc, err := Run(context.Background(), bw, Infer(cmd, v), "", cmd, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return vd, oc
}

func TestLabels(t *testing.T) {
	cases := []struct {
		cmd   string
		v     Variant
		label string
	}{
		{"rm a.txt", 0, "C"},
		{"cat a.txt", 0, "R"},
		{"cp a.txt copy.txt", 0, "R"},
		{"cp a.txt b.txt", 0, "C"},     // overwrites b.txt
		{"mv a.txt moved.txt", 0, "R"}, // same inode survives
		{"gzip notes.txt", 0, "C"},     // original replaced by different bytes
		{"echo x > fresh.txt", 0, "R"}, // creates
		{"echo x > a.txt", 0, "C"},     // overwrites
		{"chmod 600 a.txt", 0, "C"},    // mode change
		{"touch a.txt", 0, "R"},        // mtime only
		{"mkdir newdir", 0, "R"},
		{"sudo rm b.txt", 0, "C"}, // the privilege shim
		{"find . -name '*.log' -delete", 0, "C"},
		{"find . -name '*.nomatch' -delete", 0, "C"},   // Infer instantiates the pattern, so it matches
		{"find . -name '*.x' -type d -delete", 0, "R"}, // only files were instantiated
		{"sed -i s/a/b/ a.txt", 0, "C"},
		{"ls -l", 0, "R"},
	}
	for _, c := range cases {
		vd, oc := label(t, c.cmd, c.v)
		if !oc.Clean() {
			t.Errorf("%s: did not run cleanly: %+v", c.cmd, oc)
			continue
		}
		if vd.Label != c.label {
			t.Errorf("%s: label %s (%s), want %s", c.cmd, vd.Label, vd.Reason, c.label)
		}
	}
}

func TestOutsideFixtureIsReadOnly(t *testing.T) {
	_, oc := label(t, "touch /etc/oracle-test-file", 0)
	if oc.Clean() {
		t.Fatal("a write outside the fixture must fail, so the command is excluded")
	}
}

func TestInferInstantiatesNamedFiles(t *testing.T) {
	fx := Infer("rm -r olddir && cp x.py y.py", 1)
	got := map[string]string{}
	for _, e := range fx {
		got[e.Path] = e.Kind
	}
	if got["olddir"] != "dir" || got["x.py"] != "file" || got["y.py"] != "file" {
		t.Fatalf("inferred %v", got)
	}
	if _, ok := Infer("cp x.py y.py", 0), true; !ok {
		t.Fatal()
	}
	for _, e := range Infer("cp x.py y.py", 0) {
		if e.Path == "y.py" {
			t.Fatal("variant 0 must leave the destination absent")
		}
	}
}
