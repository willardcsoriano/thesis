package effects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mirrorFixture is the standard fixture with mirror/ holding copies of some files, as in
// the agent command study's "delete what has no copy in the mirror" task.
func mirrorFixture(t *testing.T) string {
	t.Helper()
	wd := fixture(t)
	for _, p := range []string{"a.txt", "src/main.go", "logs/x.log"} {
		full := filepath.Join(wd, "mirror", p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("copy\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wd
}

// The agent command study's line (open-problems row 45).
const mirrorLoop = `find . -path ./mirror -prune -o -type f -print0 | while IFS= read -r -d '' f; do [ -e "mirror/${f#./}" ] || { rm -v -- "$f"; }; done`

func TestWhileReadLoops(t *testing.T) {
	wd := mirrorFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		// The loop variable is bound to each NUL-delimited record find gives. The test in
		// the body is not evaluated: the rm may run for any file, so every file outside
		// the pruned mirror is captured, and nothing inside it.
		{mirrorLoop, true, RecoverableWithCapture,
			[]string{"remove b.txt", "remove notes.txt", "remove logs/y.log", "remove src/main.go"},
			[]string{"remove mirror/a.txt", "remove mirror/src/main.go", "remove mirror/logs/x.log", "remove mirror"}},
		// Newline-delimited, with and without -r and IFS=.
		{`find . -name '*.tmp' | while read f; do rm "$f"; done`, true, RecoverableWithCapture,
			[]string{"remove one.tmp", "remove sub/two.tmp"}, []string{"remove a.txt"}},
		{`find logs -type f | while IFS= read -r f; do rm -- "$f"; done`, true, RecoverableWithCapture,
			[]string{"remove logs/x.log", "remove logs/y.log"}, nil},
		{`find . -name '*.tmp' | while read -r f; do rm "${f#./}"; done`, true, RecoverableWithCapture,
			[]string{"remove one.tmp", "remove sub/two.tmp"}, nil},
		// ls lists the directory; rm without -r leaves the directories alone.
		{`ls | while read f; do rm "$f"; done`, true, RecoverableWithCapture,
			[]string{"remove a.txt", "remove b.txt", "remove data.csv"}, []string{"remove logs", "remove mirror"}},
		{`printf 'a.txt\nb.txt\n' | while read f; do rm "$f"; done`, true, RecoverableWithCapture,
			[]string{"remove a.txt", "remove b.txt"}, []string{"remove notes.txt"}},
		{`printf 'a.txt\n' | while read; do rm "$REPLY"; done`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// read strips IFS whitespace from both ends, unless IFS is empty.
		{`printf '  a.txt  \n' | while read f; do rm "$f"; done`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`printf '  a.txt  \n' | while IFS= read -r f; do rm "$f"; done`, true, Recoverable, nil, []string{"remove a.txt"}},
		// A final record without its newline is analysed too, although read fails on it.
		{`printf 'a.txt\nb.txt' | while read f; do rm "$f"; done`, true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		// An empty producer runs the body no times.
		{`find . -name '*.nothing' | while read f; do rm "$f"; done`, true, Recoverable, nil, nil},
		// Creations the producer cannot see: ls has listed the directory in full before
		// the first record, and mv's targets are outside find's starting point.
		{`ls *.tmp | while read f; do cp "$f" "$f.bak"; done`, true, Recoverable, []string{"create one.tmp.bak"}, nil},
		{`find logs -type f | while read f; do cp "$f" "tmp/$(basename "$f")"; done`, true, Recoverable, []string{"create tmp/x.log"}, nil},
		// A body that creates files where find is still searching could feed it records
		// it did not give when resolved, so the loop asks.
		{`find . -name '*.txt' | while read f; do cp "$f" "$f.bak"; done`, true, Unrecoverable, nil, nil},
		// Stays fail-closed: a producer that cannot be resolved, or is not read-only, or
		// whose output read would alter, or a read form not modelled.
		{`curl -s https://example.com/list | while read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`sh gen.sh | while read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`find . -name '*.tmp' -delete -print | while read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`find . -name '*.tmp' | while read f; do rm "$f"; done`, false, Unrecoverable, nil, nil}, // resolution disabled
		{`find . -name '*.tmp' -print0 | while read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a\\.txt\n' | while read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt b.txt\n' | while read x y; do rm "$x"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt\n' | while read -a arr; do rm "$arr"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt\n' | while IFS=, read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt\n' | while read f; do rm "$f"; done < list.txt`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt\n' | while read f; do rm "$f" "$g"; done`, true, Unrecoverable, nil, nil},
		{`printf 'a.txt\n' | until read f; do rm "$f"; done`, true, Unrecoverable, nil, nil},
		// When the records cannot be known, a body that only reads still passes, as it
		// did before loops were resolved (round 7's N257 under the resolution ablation).
		{`find . -type d | while read -r d; do printf '%s\n' "$d"; done`, false, Recoverable, nil, nil},
		{`curl -s https://example.com/list | while read f; do echo "$f"; done`, true, Recoverable, nil, nil},
		// Not in a pipeline, the loop binds nothing it can see, as before.
		{`while read f; do rm "$f"; done < list.txt`, true, Unrecoverable, nil, nil},
	})
}

func TestMirrorLoopCapturePlan(t *testing.T) {
	wd := mirrorFixture(t)
	a := analyze(t, wd, mirrorLoop, true)
	plan := map[string]bool{}
	for _, c := range a.Plan().Captures {
		plan[strings.TrimPrefix(c.Path, wd+"/")] = true
	}
	for _, p := range []string{"b.txt", "notes.txt", "logs/y.log", "build/out.o", "one.tmp", "sub/two.tmp"} {
		if !plan[p] {
			t.Errorf("%s has no copy in mirror/ and is not captured: plan %v", p, plan)
		}
	}
	for p := range plan {
		if strings.HasPrefix(p, "mirror") {
			t.Errorf("the pruned mirror is captured: %s", p)
		}
	}
}

func TestWhileReadNeedsComposition(t *testing.T) {
	wd := mirrorFixture(t)
	a := New(wd)
	a.Run = DefaultRunner(defaultTestTimeout)
	a.Compose = false
	if v := a.Analyze(context.Background(), mirrorLoop).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("with composition disabled the loop must ask, got %s", v.Class)
	}
}

func TestWhileReadLoopCap(t *testing.T) {
	wd := fixture(t)
	a := New(wd)
	a.Run = DefaultRunner(defaultTestTimeout)
	a.MaxLoop = 1
	if v := a.Analyze(context.Background(), `printf 'a.txt\nb.txt\n' | while read f; do rm "$f"; done`).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("a loop over more records than the cap must ask, got %s", v.Class)
	}
}
