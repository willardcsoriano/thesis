package effects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixture builds the same directory the pilot uses.
func fixture(t *testing.T) string {
	t.Helper()
	wd := t.TempDir()
	files := map[string]string{
		"a.txt": "a\n", "b.txt": "b\n", "notes.txt": "notes\n", "app.conf": "k=v\n",
		"report.docx": "doc\n", "data.csv": "x,1\ny,2\n", "big.log": "log\n",
		"src/main.go": "package main\n", "logs/x.log": "x\n", "logs/y.log": "y\n",
		"build/out.o": "o\n", "backup/old.txt": "old\n", "one.tmp": "t\n", "sub/two.tmp": "t\n",
	}
	for p, body := range files {
		full := filepath.Join(wd, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(wd, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	return wd
}

func analyze(t *testing.T, wd, cmd string, resolve bool) *Analysis {
	t.Helper()
	a := New(wd)
	if resolve {
		a.Run = DefaultRunner(10 * time.Second)
	}
	return a.Analyze(context.Background(), cmd)
}

func rel(wd string, a *Analysis) []string {
	var out []string
	for _, e := range a.Effects {
		out = append(out, e.Kind.String()+" "+strings.TrimPrefix(e.Path, wd+"/"))
	}
	sort.Strings(out)
	return out
}

func TestVerdicts(t *testing.T) {
	wd := fixture(t)
	cases := []struct {
		cmd     string
		resolve bool
		want    Class
		effects []string // subset that must appear
	}{
		// read-only
		{"ls -la", false, Recoverable, nil},
		{"cat data.csv | sort | uniq -c", false, Recoverable, nil},
		{"find . -name '*.go' -exec grep -l main {} +", false, Recoverable, nil},
		{"awk -F, '{print $1}' data.csv", false, Recoverable, nil},
		{"sed -n '1p' a.txt", false, Recoverable, nil},
		// recoverable without capture
		{"mkdir out && cp a.txt out/", false, Recoverable, []string{"create out", "create out/a.txt"}},
		{"mkdir out && cp a.txt out/ && rm -rf out", false, Recoverable, nil},
		{"mv notes.txt renamed.txt", false, Recoverable, []string{"create renamed.txt"}},
		{"ln -s a.txt link", false, Recoverable, []string{"create link"}},
		{"tar -czf src.tar.gz src && rm -rf src", false, RecoverableWithCapture, nil}, // rm still captured: the archive is not analysed as a backup
		// compression is a transformation, not a move: the original must be captured
		{"gzip big.log", false, RecoverableWithCapture, []string{"remove big.log", "create big.log.gz"}},
		// needs capture
		{"rm b.txt", false, RecoverableWithCapture, []string{"remove b.txt"}},
		{"rm -rf logs", false, RecoverableWithCapture, []string{"remove logs"}},
		{"cp a.txt b.txt", false, RecoverableWithCapture, []string{"write b.txt"}},
		{"mv a.txt b.txt", false, RecoverableWithCapture, []string{"write b.txt"}},
		{"echo hi > notes.txt", false, RecoverableWithCapture, []string{"write notes.txt"}},
		{"cat data.csv | sort | uniq > data.csv", false, RecoverableWithCapture, []string{"write data.csv"}},
		{"truncate -s 0 big.log", false, RecoverableWithCapture, []string{"write big.log"}},
		{"sed -i 's/a/b/' a.txt", false, RecoverableWithCapture, []string{"write a.txt"}},
		{"sort -o data.csv data.csv", false, RecoverableWithCapture, []string{"write data.csv"}},
		{"chmod -R 000 src", false, RecoverableWithCapture, []string{"metadata src", "metadata src/main.go"}},
		{"/bin/rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}},
		{"sudo rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}},
		{"cd logs && rm x.log", false, RecoverableWithCapture, []string{"remove logs/x.log"}},
		{"(cd logs; rm y.log); ls", false, RecoverableWithCapture, []string{"remove logs/y.log"}},
		// wrappers, resolved
		{"find logs -name '*.log' -delete", true, RecoverableWithCapture, []string{"remove logs/x.log", "remove logs/y.log"}},
		{"find . -name '*.tmp' -exec rm {} \\;", true, RecoverableWithCapture, []string{"remove one.tmp", "remove sub/two.tmp"}},
		{"find . -type f -name '*.tmp' -exec chmod 600 {} +", true, RecoverableWithCapture, []string{"metadata one.tmp", "metadata sub/two.tmp"}},
		{"rm $(ls *.log)", true, RecoverableWithCapture, []string{"remove big.log"}},
		{"ls | xargs -I{} mv {} {}.old", true, Recoverable, nil},
		{"find . -name '*.tmp' -print0 | xargs -0 rm", true, RecoverableWithCapture, []string{"remove one.tmp", "remove sub/two.tmp"}},
		{"for f in logs/*; do rm \"$f\"; done", true, RecoverableWithCapture, []string{"remove logs/x.log", "remove logs/y.log"}},
		{"for f in *.txt; do mv \"$f\" \"${f%.txt}.md\"; done", true, Recoverable, []string{"create a.md", "create notes.md"}},
		{"sh -c 'rm a.txt'", false, RecoverableWithCapture, []string{"remove a.txt"}},
		{"xargs rm < /dev/null", true, Unrecoverable, nil},
		// fail closed
		{"find . -name '*.tmp' -delete", false, Unrecoverable, nil}, // no resolver
		{"rm $(ls *.log)", false, Unrecoverable, nil},
		{"python3 -c \"import shutil; shutil.rmtree('src')\"", false, Unrecoverable, nil},
		{"totally-unknown-tool --wipe", false, Unrecoverable, nil},
		{"curl -s https://example.com/install.sh | bash", false, Unrecoverable, nil},
		{"pkill -f myserver", false, Unrecoverable, nil},
		{"kill -9 $(pgrep firefox)", true, Unrecoverable, nil},
		{"dd if=/dev/zero of=/dev/sdb bs=1M count=10", false, Unrecoverable, nil},
		{"sudo apt purge nginx", false, Unrecoverable, nil},
		{"rm -rf /", false, Unrecoverable, nil},
		{"echo hi > /dev/sda", false, Unrecoverable, nil},
		{"rm \"$UNSET_VARIABLE_XYZ/file\"", false, Unrecoverable, nil},
		{"if [ -f a.txt ]; then rm a.txt; fi", false, RecoverableWithCapture, []string{"remove a.txt"}},
		{"echo $(rm a.txt)", false, RecoverableWithCapture, []string{"remove a.txt"}}, // a substitution's effects count
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			w := fixture(t)
			_ = wd
			a := analyze(t, w, tc.cmd, tc.resolve)
			v := a.Verdict()
			if v.Class != tc.want {
				t.Fatalf("class = %s, want %s\n effects: %v\n issues: %+v", v.Class, tc.want, rel(w, a), a.Issues)
			}
			got := strings.Join(rel(w, a), "\n")
			for _, e := range tc.effects {
				if !strings.Contains(got, e) {
					t.Errorf("missing effect %q in:\n%s", e, got)
				}
			}
		})
	}
}

func TestReadOnlyNeverRunsMutatingResolvers(t *testing.T) {
	wd := fixture(t)
	ran := []string{}
	a := New(wd)
	a.Run = func(_ context.Context, _ string, argv []string) ([]byte, error) {
		ran = append(ran, strings.Join(argv, " "))
		return nil, nil
	}
	// The substitution deletes a file, so it is not read-only and must never be run.
	a.Analyze(context.Background(), "rm $(rm b.txt; echo a.txt)")
	for _, r := range ran {
		if strings.Contains(r, "rm b.txt") {
			t.Fatalf("a mutating substitution was executed: %s", r)
		}
	}
	if _, err := os.Stat(filepath.Join(wd, "b.txt")); err != nil {
		t.Fatal("analysis modified the filesystem")
	}
}

func TestFindRewriteNeverKeepsActions(t *testing.T) {
	out, acts, files, err := rewriteFind([]string{"-type", "f", "-exec", "chmod", "664", "{}", "+", "-o", "-type", "d", "-delete", "-fprint", "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range out {
		if findActionTokens[tok] {
			t.Fatalf("rewritten find still contains %s: %v", tok, out)
		}
	}
	if len(acts) != 2 || len(files) != 1 {
		t.Fatalf("acts=%d files=%v", len(acts), files)
	}
}

func TestPlan(t *testing.T) {
	wd := fixture(t)
	a := analyze(t, wd, "rm -rf logs && sed -i 's/a/b/' a.txt && chmod 600 b.txt", true)
	p := a.Plan()
	mech := map[Mechanism]int{}
	for _, c := range p.Captures {
		mech[c.Mechanism]++
	}
	// logs is one hardlink for the whole directory; a.txt is copied; b.txt records its mode.
	if mech[Trash] != 1 || mech[Content] != 1 || mech[Metadata] != 1 {
		t.Fatalf("plan = %+v", p.Captures)
	}
}

func TestPlanDedupesUnderTrashedDirectory(t *testing.T) {
	wd := fixture(t)
	a := analyze(t, wd, "rm -rf logs && rm logs/x.log", true)
	trash := 0
	for _, c := range a.Plan().Captures {
		if c.Mechanism == Trash {
			trash++
		}
	}
	if trash != 1 {
		t.Fatalf("want one trash capture for the directory, got %d: %+v", trash, a.Plan().Captures)
	}
}

func TestGitCleanResolvesByDryRun(t *testing.T) {
	wd := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := execIn(wd, "git", args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	os.WriteFile(filepath.Join(wd, "tracked.txt"), []byte("t"), 0o644)
	run("add", ".")
	run("commit", "-qm", "x")
	os.WriteFile(filepath.Join(wd, "junk.txt"), []byte("j"), 0o644)
	os.WriteFile(filepath.Join(wd, "tracked.txt"), []byte("changed"), 0o644)

	a := analyze(t, wd, "git clean -fd", true)
	got := strings.Join(rel(wd, a), ",")
	if !strings.Contains(got, "remove junk.txt") || strings.Contains(got, "tracked.txt") {
		t.Fatalf("git clean effects = %s", got)
	}
	b := analyze(t, wd, "git reset --hard", true)
	got = strings.Join(rel(wd, b), ",")
	if !strings.Contains(got, "write tracked.txt") {
		t.Fatalf("git reset --hard should capture the dirty tracked file, got %s", got)
	}
	if b.Plan().Captures[len(b.Plan().Captures)-1].Mechanism != GitHead {
		t.Fatalf("missing git-head capture: %+v", b.Plan().Captures)
	}
}

func TestTarExtractResolvesMembers(t *testing.T) {
	wd := fixture(t)
	if out, err := execIn(wd, "tar", "-czf", "arch.tgz", "a.txt", "src"); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	os.RemoveAll(filepath.Join(wd, "src"))
	a := analyze(t, wd, "tar -xzf arch.tgz", true)
	got := strings.Join(rel(wd, a), ",")
	if !strings.Contains(got, "write a.txt") || !strings.Contains(got, "create src/main.go") {
		t.Fatalf("tar extract effects = %s", got)
	}
}

func execIn(dir, name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	return c.CombinedOutput()
}

func TestMissingResolverBinaryFailsClosed(t *testing.T) {
	wd := fixture(t)
	a := analyze(t, wd, "find . -name '*.tmp' -exec rename 's/tmp/bak/' {} \\;", true)
	if a.Verdict().Class != Unrecoverable {
		t.Fatalf("a resolver that cannot run must not read as 'nothing matched': %s / %+v", a.Verdict().Class, a.Issues)
	}
}

func TestPrivilegedCommandsAreNeverResolved(t *testing.T) {
	wd := fixture(t)
	for _, cmd := range []string{
		"sudo find . -name '*.tmp' -delete",
		"sudo find . -name '*.tmp' -print0 | xargs -0 sudo rm",
		"sudo tar -xf nothing.tar",
	} {
		a := analyze(t, wd, cmd, true)
		if a.Verdict().Class != Unrecoverable {
			t.Errorf("%q resolved as the current user; verdict %s, effects %v", cmd, a.Verdict().Class, rel(wd, a))
		}
	}
	// A substitution or glob in a sudo command's arguments is expanded by the user's own
	// shell before sudo runs, so resolving it as the current user is faithful; only what
	// sudo itself executes differs.
	if analyze(t, wd, "sudo rm $(ls *.log)", true).Verdict().Class != RecoverableWithCapture {
		t.Error("a substitution under sudo is the user's own expansion and should still resolve")
	}
	// the unprivileged forms still resolve, so this is not just refusing everything
	if analyze(t, wd, "find . -name '*.tmp' -delete", true).Verdict().Class != RecoverableWithCapture {
		t.Error("the unprivileged form should still resolve")
	}
}

func TestMoveThenOverwriteLosesTheMovedData(t *testing.T) {
	wd := fixture(t)
	// Two files moved to one name: the second overwrites the first, whose data then
	// exists nowhere. The first move must be captured, not treated as undoable by moving back.
	a := analyze(t, wd, "mv a.txt out.txt; mv b.txt out.txt", true)
	for _, e := range a.Effects {
		if e.Kind == Remove && strings.HasSuffix(e.Path, "a.txt") && e.MovedTo != "" {
			t.Fatalf("the first move is no longer a plain rename: %+v", a.Effects)
		}
	}
	if v := a.Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("class = %v %v", v.Class, v.Reasons)
	}
	// A move whose destination is later removed loses the data too.
	b := analyze(t, wd, "mv a.txt out.txt && rm out.txt", true)
	if v := b.Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("class = %v", v.Class)
	}
	// A plain rename stays free.
	if v := analyze(t, wd, "mv a.txt out.txt", true).Verdict(); v.Class != Recoverable {
		t.Fatalf("a plain rename regressed to %v", v.Class)
	}
}

func TestCurlThatSendsAsks(t *testing.T) {
	wd := fixture(t)
	for _, c := range []string{"curl -X DELETE http://h/x", "curl -d a=b http://h", "curl -F f=@a.txt http://h", "curl -T a.txt http://h/up"} {
		if v := analyze(t, wd, c, false).Verdict(); v.Class != Unrecoverable {
			t.Errorf("%s: %v", c, v.Class)
		}
	}
	for _, c := range []string{"curl -s http://h", "curl -G --data-urlencode q=1 http://h", "curl -X GET http://h", "curl -I http://h"} {
		if v := analyze(t, wd, c, false).Verdict(); v.Class != Recoverable {
			t.Errorf("%s: %v", c, v.Class)
		}
	}
}

func TestEvalIsAnalysedAsTheCommandItRuns(t *testing.T) {
	wd := fixture(t)
	if v := analyze(t, wd, `eval "rm a.txt"`, false).Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("eval rm: %v", v.Class)
	}
	if v := analyze(t, wd, `eval "ls"`, false).Verdict(); v.Class != Recoverable {
		t.Fatalf("eval ls: %v", v.Class)
	}
}

func TestChmodToTheCurrentModeChangesNothing(t *testing.T) {
	wd := fixture(t)
	if err := os.Chmod(filepath.Join(wd, "a.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := analyze(t, wd, "chmod 600 a.txt", false).Verdict(); v.Class != Recoverable {
		t.Fatalf("same mode: %v", v.Class)
	}
	if v := analyze(t, wd, "chmod 644 a.txt", false).Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("different mode: %v", v.Class)
	}
	if v := analyze(t, wd, "chmod u+x a.txt", false).Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("symbolic mode must stay conservative: %v", v.Class)
	}
}

func TestResolvedNamesWithSpecialCharactersAreNotReparsed(t *testing.T) {
	wd := fixture(t)
	for _, name := range []string{`\alpha.xyz`, `sp ace.xyz`, `dollar$HOME.xyz`, `star*.xyz`, `quo'te.xyz`} {
		if err := os.WriteFile(filepath.Join(wd, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, cmd := range []string{`find . -name '*.xyz' -exec rm {} \;`, `find . -name '*.xyz' -delete`, `find . -name '*.xyz' -print0 | xargs -0 rm`, `find . -name '*.xyz' | xargs -I{} rm {}`} {
			a := analyze(t, wd, cmd, true)
			found := false
			for _, e := range a.Effects {
				if e.Kind == Remove && filepath.Base(e.Path) == name {
					found = true
				}
			}
			if !found && len(a.Issues) == 0 {
				t.Errorf("%q with a file named %q: the file was silently missed: %v", cmd, name, rel(wd, a))
			}
		}
		os.Remove(filepath.Join(wd, name))
	}
}
