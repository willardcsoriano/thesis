package effects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The tests in this file come from the second mutation-testing sample (seed 20261011):
// each covers a place where a mechanical change to the code went unnoticed by every
// other test and changed what the analysis reports. The expected values come from
// what bash and the tools do, checked by running them, not from what the code did.

// prepared returns the standard fixture with extra files written into it.
func prepared(t *testing.T, files map[string]string) string {
	t.Helper()
	wd := fixture(t)
	for p, body := range files {
		full := filepath.Join(wd, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wd
}

func TestMutationGapsShellAndRules(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// bash: `>&WORD` with a file name sends both streams to that file.
		{"echo x >& a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		// a file removed earlier on the line no longer exists, so touch makes a new one
		{"rm a.txt && touch a.txt", false, RecoverableWithCapture, []string{"remove a.txt", "create a.txt"}, nil},
		// an arithmetic for loop runs its body
		{"for ((i=0;i<2;i++)); do rm a.txt; done", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// xxd's second operand is an output file it overwrites
		{"xxd a.txt b.txt", false, Unrecoverable, nil, nil},
		// wget --spider checks that the page exists and saves nothing
		{"wget --spider https://example.com/files/f.zip", false, Recoverable, nil, []string{"create f.zip"}},
		// sed -f reads its script from a file, which may hold w or e commands
		{"sed -f prog.sed a.txt", false, Unrecoverable, nil, nil},
		// curl -D and -o inside a cluster of short options still write their file
		{"curl -sD a.txt https://example.com", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"curl -sLo a.txt https://example.com", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		// mkdir -p creates only what is missing; mkdir of an existing directory fails
		{"mkdir -p logs/new/deeper", false, Recoverable, []string{"create logs/new", "create logs/new/deeper"}, []string{"create logs"}},
		{"mkdir logs", false, Recoverable, nil, []string{"create logs"}},
		// mv --backup and -S name the copy of the file being replaced (GNU mv, checked)
		{"mv --backup a.txt b.txt", false, RecoverableWithCapture, []string{"write b.txt", "create b.txt~"}, nil},
		{"mv -b -S .bak a.txt b.txt", false, RecoverableWithCapture, []string{"create b.txt.bak"}, []string{"create b.txt~"}},
		// timeout -v is its verbose flag, not the end of its options
		{"timeout -v 5 rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// a script file whose name ends in c is still a script file, not -c
		{"bash .bashrc", false, Unrecoverable, nil, nil},
		// bash -s reads the script from its input: code piped into it
		{"cat a.txt | bash -s", false, Unrecoverable, nil, nil},
		// an -c string with nothing destructive in it is just that
		{"sh -c 'touch new.txt'", false, Recoverable, []string{"create new.txt"}, nil},
		// doas, like sudo, runs as root, so its targets are never resolved as the user
		{"doas find . -name '*.tmp' -delete", true, Unrecoverable, nil, nil},
		// -exec runs in find's starting directory, so a relative path in it is relative to that
		// xargs -n 2 runs one mv per pair
		{"printf 'a.txt c.txt b.txt d.txt' | xargs -n 2 mv", true, Recoverable, []string{"create c.txt", "create d.txt"}, nil},
		// git -c takes a value: the subcommand comes after it
		{"git -c core.pager=cat log", false, Recoverable, nil, nil},
		// service with no action is a form the analysis does not model
		{"service nginx", false, Unrecoverable, nil, nil},
	})
}

func TestMutationGapsPreparedFiles(t *testing.T) {
	wd := prepared(t, map[string]string{
		"1": "one\n", "backup.tar": "old\n", "-file.txt": "m\n", "x.tar": "old\n",
		"c.txt": "c\n", "c.txt.~1~": "1\n", "c.txt.~2~": "2\n",
	})
	runRuleCasesIn(t, wd, []ruleCase{
		// bash: `> 1` writes a file named 1; only `>&1` duplicates a descriptor
		{"echo x > 1", false, RecoverableWithCapture, []string{"write 1"}, nil},
		// after --, a name starting with - is a member, not an option (GNU tar, checked)
		{"tar -cf backup.tar -- -file.txt", false, RecoverableWithCapture, []string{"write backup.tar"}, nil},
		// -fNAME takes the rest of the cluster as the archive (GNU tar, checked)
		{"tar -cfx.tar a.txt", false, RecoverableWithCapture, []string{"write x.tar"}, []string{"write a.txt"}},
		// numbered backups continue from the highest existing number (GNU cp, checked)
		{"cp --backup=numbered a.txt c.txt", false, RecoverableWithCapture, []string{"write c.txt", "create c.txt.~3~"}, []string{"write c.txt.~1~"}},
	})
}

func TestMutationGapsTarDirectoryAndRewrittenArchive(t *testing.T) {
	wd := prepared(t, map[string]string{"out/a.txt": "old\n"})
	if out, err := execIn(wd, "tar", "-cf", "arch.tar", "a.txt"); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	if out, err := execIn(wd, "tar", "-cf", "other.tar", "b.txt"); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	runRuleCasesIn(t, wd, []ruleCase{
		// -C DIR extracts into DIR, overwriting what is there
		{"tar -xf arch.tar -C out", true, RecoverableWithCapture, []string{"write out/a.txt"}, nil},
		// an archive an earlier part of the line overwrites cannot be listed beforehand
		{"cp other.tar arch.tar && tar -xf arch.tar", true, Unrecoverable, nil, nil},
	})
	// A resolver that fails to run leaves the members unknown, so it asks.
	a := New(wd)
	a.Run = func(context.Context, string, []string) ([]byte, error) { return nil, errors.New("timed out") }
	if v := a.Analyze(context.Background(), "tar -xf arch.tar").Verdict(); v.Class != Unrecoverable {
		t.Fatalf("a failed dry run must ask, got %s", v.Class)
	}
}

func TestMutationGapsUnsetVariable(t *testing.T) {
	// Regression: unset was read-only and left the variable at its environment value,
	// so `unset HOME; rm -rf "$HOME/x"` was analysed as removing the home directory's x
	// and captured that, while bash removes /x. A variable the line unsets or reads into
	// is now unknown, and the command using it asks.
	wd := prepared(t, map[string]string{"x/f": "f\n"})
	for _, cmd := range []string{
		`unset HOME; rm -rf "$HOME/x"`,
		`unset -v HOME && rm -rf "$HOME/x"`,
		`read HOME < a.txt; rm -rf "$HOME/x"`,
	} {
		a := New(wd)
		a.Env = []string{"HOME=" + wd}
		res := a.Analyze(context.Background(), cmd)
		if v := res.Verdict(); v.Class != Unrecoverable {
			t.Errorf("%s: class = %s, effects %v", cmd, v.Class, rel(wd, res))
		}
		for _, e := range res.Effects {
			if e.Path == filepath.Join(wd, "x") {
				t.Errorf("%s: captured %s, which bash does not touch", cmd, e.Path)
			}
		}
	}
	// unset -f removes a function, not a variable
	a := New(wd)
	a.Env = []string{"HOME=" + wd}
	if v := a.Analyze(context.Background(), `unset -f HOME; rm -rf "$HOME/x"`).Verdict(); v.Class != RecoverableWithCapture {
		t.Errorf("unset -f: class = %s", v.Class)
	}
}

func TestMutationGapsCaptureAndPlan(t *testing.T) {
	wd := fixture(t)
	// Writing into a named pipe hands the bytes to whatever reads it: there is no file
	// pre-image to capture.
	if err := syscall.Mkfifo(filepath.Join(wd, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if v := analyze(t, wd, "echo x > pipe", false).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("writing into a pipe: %s", v.Class)
	}
	// A directory hardlinked as a whole covers a removal under it, whichever comes first.
	p := analyze(t, wd, "rm logs/x.log && rm -r logs", false).Plan()
	trash := 0
	for _, c := range p.Captures {
		if c.Mechanism == Trash {
			trash++
		}
	}
	if trash != 1 {
		t.Fatalf("want one trash capture for logs, got %+v", p.Captures)
	}
}

func TestMutationGapsFindRootOutsideWorkingDirectory(t *testing.T) {
	wd := fixture(t)
	other := prepared(t, nil)
	// The copies match the same pattern, so find can visit what it has just made.
	cmd := "find " + other + ` -name '*.txt' -exec cp {} {}.txt \;`
	if v := analyze(t, wd, cmd, true).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("find feeding itself under an absolute root: %s", v.Class)
	}
}

func TestMutationGapsGit(t *testing.T) {
	wd := prepared(t, map[string]string{".gitignore": "*.o\n"})
	for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "i"}} {
		if out, err := execIn(wd, "git", c...); err != nil {
			t.Fatalf("git %v: %v %s", c, err, out)
		}
	}
	files := map[string]string{"a.txt": "edited\n", "junk.txt": "j\n", "newdir/n.txt": "n\n", "build/extra.o": "o\n"}
	for p, body := range files {
		os.MkdirAll(filepath.Join(wd, filepath.Dir(p)), 0o755)
		if err := os.WriteFile(filepath.Join(wd, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runRuleCasesIn(t, wd, []ruleCase{
		// -n only lists
		{"git clean -n -d", true, Recoverable, nil, []string{"remove junk.txt"}},
		// -d takes untracked directories, -x ignored files too (git, checked)
		{"git clean -fdx", true, RecoverableWithCapture, []string{"remove junk.txt", "remove newdir", "remove build"}, nil}, // git lists the wholly ignored directory, not its file
		{"git clean -fd", true, RecoverableWithCapture, []string{"remove junk.txt", "remove newdir"}, []string{"remove build", "remove build/extra.o"}},
		// switch -c makes a branch, undone by switching back and deleting it
		{"git switch -c feature", true, RecoverableWithCapture, nil, nil},
	})
	// reset --hard overwrites the edited tracked file, and nothing else
	a := analyze(t, wd, "git reset --hard", true)
	for _, e := range a.Effects {
		if e.Kind == Write && e.Path != filepath.Join(wd, "a.txt") {
			t.Errorf("reset --hard writes only edited tracked files, got a write of %s", e.Path)
		}
	}
	s := analyze(t, wd, "git switch -c feature", true)
	if len(s.States) != 1 || s.States[0].Subject != "git:branch:feature" {
		t.Fatalf("switch -c states = %+v", s.States)
	}
}

func TestMutationGapsPackages(t *testing.T) {
	// The installed version is no longer offered by any repository, only a newer one is,
	// so a removal cannot be put back.
	const policyOnlyNewer = `old:
  Installed: 1.0
  Candidate: 2.0
  Version table:
     2.0 500
        500 http://deb.debian.org/debian trixie/main amd64 Packages
 *** 1.0 100
        100 /var/lib/dpkg/status
`
	a := analyzeWith("apt-get remove old", map[string]string{
		"apt-get -s remove old": "Remv old [1.0]\n",
		"apt-cache policy old":  policyOnlyNewer,
	})
	if v := a.Verdict(); v.Class != Unrecoverable {
		t.Fatalf("removing a package that cannot be reinstalled: %s, inverses %v", v.Class, inverses(a))
	}
	// The confirmation text names what apt will do.
	for act, sim := range map[string]string{"remove": "Remv", "purge": "Purg"} {
		want := act + " hello"
		r := analyzeWith("apt-get "+act+" hello", map[string]string{
			"apt-get -s " + act + " hello":          sim + " hello [2.10-3]\n",
			"apt-cache policy hello":                policyHello,
			"dpkg-query -W -f=${Conffiles}\n hello": "",
		})
		if len(r.States) != 1 || r.States[0].Change != want {
			t.Errorf("apt-get %s: states %+v, issues %+v", act, r.States, r.Issues)
		}
	}
	// dpkg --auto-deconfigure lets dpkg deconfigure other packages, which is not modelled.
	deb := map[string]string{
		"dpkg-deb -f /x.deb Package Version": "Package: hello\nVersion: 2.10-3\n",
	}
	if v := analyzeWith("dpkg -i x.deb", deb).Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("plain dpkg -i: %s", v.Class)
	}
	if v := analyzeWith("dpkg --auto-deconfigure -i x.deb", deb).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("dpkg --auto-deconfigure: %s", v.Class)
	}
}

func TestLaterCommandOnWhatTheLineMovedOrChanged(t *testing.T) {
	// Regression (found by the chapter 3 review, 2026-10-12): find listed the tree as it
	// is now, so after `mv a.txt logs/` it missed logs/a.txt and kept the move as
	// "undo moves it back", although find deletes it. A tree the line changes cannot be
	// listed beforehand, so find asks.
	runRuleCases(t, []ruleCase{
		{"mv a.txt logs/ && find logs -name '*.txt' -delete", true, Unrecoverable, nil, nil},
		{"echo x > logs/new.txt && find logs -delete", true, Unrecoverable, nil, nil},
		// an unchanged tree still resolves
		{"find . -maxdepth 1 -name a.txt -delete", true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
	})

	// Regression: removing a directory destroyed what the line had moved into it, while
	// the move stayed "undo moves it back" and the directory's capture, taken before the
	// line runs, does not hold the moved file. The move is now a captured removal.
	wd := fixture(t)
	a := analyze(t, wd, "mv a.txt logs/ && rm -r logs", false)
	if v := a.Verdict(); v.Class != RecoverableWithCapture {
		t.Fatalf("class = %v, want recoverable-with-capture", v.Class)
	}
	captured := false
	for _, c := range a.Plan().Captures {
		if c.Path == filepath.Join(wd, "a.txt") {
			captured = true
		}
	}
	if !captured {
		t.Fatalf("a.txt, moved into the removed directory, is not captured: %+v", a.Plan().Captures)
	}
}

func TestEarlierPartMayHaveFailed(t *testing.T) {
	// Regression (found by the chapter 3 review, 2026-10-12): the analysis read a line
	// as if every part succeeded. After ;, ||, a pipe, or a branch, a later part can
	// run although an earlier one failed, so a removal may not have happened, a cd may
	// have left the shell where it was, and a branch's assignment may not have run.
	runRuleCases(t, []ruleCase{
		// the move may fail, leaving a.txt for the redirect to overwrite
		{"mv a.txt tmp/ || echo x > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"mv a.txt tmp/; echo x > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"rm a.txt | echo x > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		// after &&, the move did happen, and the redirect makes a new file
		{"mv a.txt tmp/ && echo x > a.txt", false, Recoverable, nil, []string{"write a.txt"}},
		// rm runs only where cd failed, or after a cd that may have failed
		{"cd logs || rm a.txt", false, Unrecoverable, nil, nil},
		{"cd logs; rm *", false, Unrecoverable, nil, nil},
		{"(cd logs; rm y.log); ls", false, Unrecoverable, nil, nil},
		// the guarded form is certain
		{"cd logs || exit 1; rm x.log", false, RecoverableWithCapture, []string{"remove logs/x.log"}, nil},
		{"cd logs && rm a.txt && cd .. && rm b.txt", false, RecoverableWithCapture, []string{"remove b.txt"}, nil},
		// a branch or loop's assignments and cd outlive it, and may not have run
		{"x=a.txt; if test -f q; then x=b.txt; fi; rm $x", false, Unrecoverable, nil, nil},
		{"x=a.txt; if true; then x=b.txt; else x=b.txt; fi; rm $x", false, RecoverableWithCapture, []string{"remove b.txt"}, nil},
		{"if true; then cd logs; fi; rm *", false, Unrecoverable, nil, nil},
		{"for d in logs; do cd $d; done; rm x.log", false, Unrecoverable, nil, nil},
		{"x=a.txt; for f in b.txt; do x=$f; done; rm $x", false, RecoverableWithCapture, []string{"remove b.txt"}, []string{"remove a.txt"}},
		{"x=a.txt; while false; do x=b.txt; done; rm $x", false, Unrecoverable, nil, nil},
		// a path removed and made again: if the removal failed, the last write
		// overwrites the original (found by the round 10 review)
		{"mv a.txt b.txt && echo n > a.txt; echo x > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"rm a.txt && touch a.txt; echo x > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"rm a.txt && echo n > a.txt && echo x > a.txt", false, RecoverableWithCapture, nil, []string{"write a.txt"}},
		// a branch's removal may not have happened
		{"if test -f q; then rm a.txt; fi && echo y > a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
	})
}
