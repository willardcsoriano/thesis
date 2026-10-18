package shape

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"ls -la", "plain"},
		{"rm a.txt", "plain"},
		{"echo hi > out.txt", "redirect"},
		{"echo hi >> out.txt", "redirect"},
		{"cat a.txt | sort", "compound"},
		{"mkdir x && cd x", "compound"},
		{"find . -name '*.log' -delete", "runtime"},
		{"rm $(ls *.tmp)", "runtime"},
		{"for f in *.txt; do mv $f $f.bak; done", "runtime"},
		{"sh -c 'rm a.txt'", "hidden"},
		{"/usr/bin/python3 -c pass", "hidden"},
		{"find . -name '*.sh' | sh", "hidden"}, // hidden outranks runtime
		// Only a call's own first word counts: the sh that xargs launches is an
		// argument, so the line is runtime. Execve capture is what sees it run.
		{"find . | xargs sh -c 'rm $0'", "runtime"},
		{"if then fi ((", "plain"}, // does not parse
	}
	for _, c := range cases {
		if got := Of(c.cmd); got != c.want {
			t.Errorf("Of(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

// TestCorporaShapes proves the move kept behaviour: every stored shape in the
// corpora the round 6m and round 7 results rest on is what Of returns now.
//
// The one exception is partition E (external). corpusgen assigns those items the
// shape "external" by its partition rule before the classifier's result is used,
// so the stored field is not Of's output. Those rows are checked to carry exactly
// "external", and Of's value for them is only required to be one of its own five
// shapes; the test logs how they fall.
func TestCorporaShapes(t *testing.T) {
	valid := map[string]bool{"hidden": true, "runtime": true, "compound": true, "redirect": true, "plain": true}
	for _, name := range []string{"corpus7.jsonl", "corpus6m.jsonl"} {
		p := filepath.Join("..", "..", "pilot", name)
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		n, ext := 0, map[string]int{}
		for sc.Scan() {
			var it struct {
				ID, Partition, Shape, Command string
			}
			if err := json.Unmarshal(sc.Bytes(), &it); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			n++
			got := Of(it.Command)
			if it.Partition == "E" {
				if it.Shape != "external" {
					t.Errorf("%s %s: external item stored with shape %q", name, it.ID, it.Shape)
				}
				if !valid[got] {
					t.Errorf("%s %s: Of returned %q", name, it.ID, got)
				}
				ext[got]++
				continue
			}
			if got != it.Shape {
				t.Errorf("%s %s: Of(%q) = %q, stored %q", name, it.ID, it.Command, got, it.Shape)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n == 0 {
			t.Fatalf("%s: no items", name)
		}
		t.Logf("%s: %d items; external items by Of: %v", name, n, ext)
	}
}
