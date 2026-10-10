package effects

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The resolvers are the riskiest part of the analysis: they are where it runs other
// commands, and where all three defects found on 2026-10-09 were. These tests cover
// their option handling and their fail-closed exits.

func TestXargsOptions(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"printf 'a.txt b.txt' | xargs -r rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs --no-run-if-empty --verbose rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -I % rm %", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -i rm {}", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs --replace=X rm X", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt,b.txt' | xargs -d , rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt,b.txt' | xargs --delimiter=, rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -d '\\n' rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs --max-args=1 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs -P 4 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs -- rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		// clusters of short flags, each letter read in turn
		{"printf 'a.txt\\0b.txt\\0' | xargs -0r rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\0b.txt\\0' | xargs -0n1 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\0b.txt\\0' | xargs -0I% rm %", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -rI {} rm {}", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -ri rm {}", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -ri% rm %", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -rL1 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs -rn 1 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt,b.txt' | xargs -rd, rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt,b.txt' | xargs -rd , rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{"printf 'a.txt b.txt' | xargs -rP4 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		// GNU xargs without -0 cuts each line at its first NUL: only the first name survives
		{"printf 'a.txt\\0b.txt\\0' | xargs rm", true, RecoverableWithCapture, []string{"remove a.txt"}, []string{"remove b.txt"}},
		// a read-only command run by xargs needs no resolution
		{"ls | xargs echo", false, Recoverable, nil, nil},
		// fail-closed exits
		{"rm b.txt | xargs rm", true, Unrecoverable, nil, nil}, // the producer is not read-only
		{"ls | xargs rm", false, Unrecoverable, nil, nil},      // no resolver
		{"cd -; ls | xargs rm", true, Unrecoverable, nil, nil}, // unknown directory
		{"ls | xargs rm $UNSET_VARIABLE_XYZ", true, Unrecoverable, nil, nil},
		{"xargs -a nothing-here.txt rm", true, Unrecoverable, nil, nil},
	})
}

func TestXargsArgumentFileAndBounds(t *testing.T) {
	wd := fixture(t)
	if err := os.WriteFile(filepath.Join(wd, "list.txt"), []byte("a.txt\nb.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"xargs -a list.txt rm", "xargs --arg-file list.txt rm", "xargs -ra list.txt rm"} {
		a := analyze(t, wd, cmd, true)
		if got := rel(wd, a); len(got) != 2 || a.Verdict().Class != RecoverableWithCapture {
			t.Errorf("%s: %v %+v", cmd, got, a.Issues)
		}
	}
	an := New(wd)
	an.Run = DefaultRunner(defaultTestTimeout)
	an.MaxItems = 1
	if v := an.Analyze(context.Background(), "printf 'a.txt b.txt' | xargs rm").Verdict(); v.Class != Unrecoverable {
		t.Errorf("over the item bound: %s", v.Class)
	}
	failing := New(wd)
	failing.Run = func(context.Context, string, []string) ([]byte, error) { return nil, errors.New("timed out") }
	if v := failing.Analyze(context.Background(), "ls | xargs rm").Verdict(); v.Class != Unrecoverable {
		t.Errorf("a resolver that fails: %s", v.Class)
	}
}

func TestRsyncResolvesThroughItsDryRun(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
	wd := fixture(t)
	// backup/ starts with old.txt; give it a stale x.log for logs/ to overwrite.
	if err := os.WriteFile(filepath.Join(wd, "backup/x.log"), []byte("stale, longer than the source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRuleCasesIn(t, wd, []ruleCase{
		{"rsync -a src/ backup/", true, Recoverable, []string{"create backup/main.go"}, []string{"remove backup/old.txt"}},
		{"rsync -a --delete src/ backup/", true, RecoverableWithCapture, []string{"create backup/main.go", "remove backup/old.txt"}, nil},
		{"rsync -a src backup/", true, Recoverable, []string{"create backup/src", "create backup/src/main.go"}, nil},
		{"rsync -a logs/ backup/", true, RecoverableWithCapture, []string{"write backup/x.log", "create backup/y.log"}, nil},
		{"rsync -a src", true, Recoverable, nil, nil},
		{"rsync -a -e ssh src/ dest/", true, Unrecoverable, nil, nil},
		{"rsync -a --remove-source-files src/ dest/", true, Unrecoverable, nil, nil},
		{"rsync -a src/ backup/", false, Unrecoverable, nil, nil}, // no resolver
		{"rsync -a $UNSET_VARIABLE_XYZ backup/", true, Unrecoverable, nil, nil},
		{"rename 's/a/b/' $UNSET_VARIABLE_XYZ", true, Unrecoverable, nil, nil},
	})
}

func TestGitWorkingTreeCommands(t *testing.T) {
	wd := fixture(t)
	for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "i"}} {
		if out, err := execIn(wd, "git", c...); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(wd, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRuleCasesIn(t, wd, []ruleCase{
		// commands that put tracked files back to HEAD overwrite the edit, so it is captured
		{"git checkout -- a.txt", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"git checkout a.txt", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"git restore a.txt", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"git checkout -f", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"git switch --discard-changes main", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"git -C . reset --hard", true, RecoverableWithCapture, []string{"write a.txt"}, nil},
		// ones that leave the working tree alone
		{"git restore --staged a.txt", true, Recoverable, nil, nil},
		{"git switch main", true, Recoverable, nil, nil},
		{"git checkout main", true, Recoverable, nil, nil},
		{"git reset --soft HEAD", true, Recoverable, nil, nil},
		{"git add .", true, Recoverable, nil, nil},
		{"git rm --cached a.txt", true, Recoverable, nil, nil},
		// file operations through git
		{"git rm b.txt", true, RecoverableWithCapture, []string{"remove b.txt"}, nil},
		{"git mv a.txt c.txt", true, Recoverable, []string{"create c.txt"}, nil},
		{"git clean --force -d -- logs", true, Recoverable, nil, nil}, // nothing untracked there
		// discarding saved work or history asks
		{"git stash drop", true, Unrecoverable, nil, nil},
		{"git stash -u", true, Unrecoverable, nil, nil},
		{"git stash pop", true, Unrecoverable, nil, nil},
		{"git branch -D feature", true, Unrecoverable, nil, nil},
		{"git tag -d v1", true, Unrecoverable, nil, nil},
		{"git remote remove origin", true, Unrecoverable, nil, nil},
		// fail-closed exits
		{"git clean -fd", false, Unrecoverable, nil, nil}, // no resolver
		{"git clean $UNSET_VARIABLE_XYZ", true, Unrecoverable, nil, nil},
	})
	// Outside a repository, checkout has no tracked files to overwrite.
	outside := fixture(t)
	if v := analyze(t, outside, "git checkout -- a.txt", true).Verdict(); v.Class != Recoverable {
		t.Errorf("outside a repository: %s", v.Class)
	}
}

func TestAptUnmodelledForms(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"apt", false, Unrecoverable, nil, nil},
		{"apt frobnicate", false, Unrecoverable, nil, nil},
		{"apt install $UNSET_VARIABLE_XYZ", false, Unrecoverable, nil, nil},
		{"apt-get update", false, Recoverable, nil, nil},
	})
}

// runRuleCasesIn is runRuleCases against one prepared directory instead of a fresh fixture.
func runRuleCasesIn(t *testing.T, wd string, cases []ruleCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			a := analyze(t, wd, tc.cmd, tc.resolve)
			checkRuleCase(t, wd, a, tc)
		})
	}
}
