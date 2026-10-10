package effects

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ruleCase is one command, the verdict it must get on the standard fixture, and the
// effects that must (and must not) appear. TestRuleTable gives every entry in the rule
// table, the wrapper table, and the dispatch fallbacks at least one case of its own, so
// that `make ci` exercises each rule rather than relying on the corpus run to do it.
type ruleCase struct {
	cmd     string
	resolve bool
	want    Class
	effects []string // each must appear, as "<kind> <path relative to the fixture>"
	absent  []string // each must not appear
}

func runRuleCases(t *testing.T, cases []ruleCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			w := fixture(t)
			checkRuleCase(t, w, analyze(t, w, tc.cmd, tc.resolve), tc)
		})
	}
}

func checkRuleCase(t *testing.T, wd string, a *Analysis, tc ruleCase) {
	t.Helper()
	if v := a.Verdict(); v.Class != tc.want {
		t.Fatalf("class = %s, want %s\n effects: %v\n issues: %+v", v.Class, tc.want, rel(wd, a), a.Issues)
	}
	got := rel(wd, a)
	has := func(e string) bool {
		for _, g := range got {
			if g == e {
				return true
			}
		}
		return false
	}
	for _, e := range tc.effects {
		if !has(e) {
			t.Errorf("missing effect %q in %v", e, got)
		}
	}
	for _, e := range tc.absent {
		if has(e) {
			t.Errorf("unexpected effect %q in %v", e, got)
		}
	}
}

func TestRuleTable(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// rmdir removes only an empty directory; on a non-empty one it fails and changes nothing.
		{"rmdir tmp", false, RecoverableWithCapture, []string{"remove tmp"}, nil},
		{"rmdir logs", false, Recoverable, nil, []string{"remove logs"}},
		// rm without -r leaves a directory alone; rm of a missing file does nothing.
		{"rm logs", false, Recoverable, nil, []string{"remove logs"}},
		{"rm -f missing.txt", false, Recoverable, nil, nil},
		{"rm -rf /home", false, Unrecoverable, nil, nil},
		// install: -d makes directories; otherwise it copies like cp.
		{"install -d newdir", false, Recoverable, []string{"create newdir"}, nil},
		{"install -m 644 a.txt b.txt", false, RecoverableWithCapture, []string{"write b.txt"}, nil},
		{"install a.txt backup/", false, Recoverable, []string{"create backup/a.txt"}, nil},
		// touch creates what is missing; on an existing file it changes only times, which are not captured.
		{"touch new.txt", false, Recoverable, []string{"create new.txt"}, nil},
		{"touch a.txt", false, Recoverable, nil, []string{"write a.txt"}},
		{"touch -c new.txt", false, Recoverable, nil, []string{"create new.txt"}},
		// chown and chgrp are metadata changes, recursive with -R.
		{"chown -R nobody src", false, RecoverableWithCapture, []string{"metadata src", "metadata src/main.go"}, nil},
		{"chgrp users a.txt", false, RecoverableWithCapture, []string{"metadata a.txt"}, nil},
		// cp: recursive copy into a new name creates; -n never overwrites.
		{"cp -r src newsrc", false, Recoverable, []string{"create newsrc/main.go"}, nil},
		{"cp -n a.txt b.txt", false, Recoverable, nil, []string{"write b.txt"}},
		{"cp src newsrc", false, Recoverable, nil, []string{"create newsrc"}}, // a directory without -r is not copied
		// mv -n onto an existing file does nothing.
		{"mv -n a.txt b.txt", false, Recoverable, nil, []string{"write b.txt"}},
		{"mv a.txt b.txt logs", false, Recoverable, []string{"create logs/a.txt", "create logs/b.txt"}, nil},
		// ln -f replacing a file captures the file it replaces.
		{"ln -sf a.txt b.txt", false, RecoverableWithCapture, []string{"remove b.txt"}, nil},
		// awk: in place edits write; a program that writes or runs commands is opaque.
		{"awk -i inplace '{print toupper($0)}' a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{`awk '{system("rm a.txt")}' data.csv`, false, Unrecoverable, nil, nil},
		{`awk '{print > "out.txt"}' data.csv`, false, Unrecoverable, nil, nil},
		{"awk -f prog.awk data.csv", false, Unrecoverable, nil, nil},
		// sed: a script that writes files is opaque; an in-place backup suffix makes a file.
		{"sed 'w out.txt' a.txt", false, Unrecoverable, nil, nil},
		{"sed -i.bak 's/a/b/' a.txt", false, RecoverableWithCapture, []string{"write a.txt", "create a.txt.bak"}, nil},
		// tee writes each file operand, new or existing; - and devices are not files.
		{"echo x | tee out.txt", false, Recoverable, []string{"create out.txt"}, nil},
		{"echo x | tee -a a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"echo x | tee /dev/null -", false, Recoverable, nil, nil},
		// shred destroys on purpose: no capture can undo the intent, so it always asks.
		{"shred -u a.txt", false, Unrecoverable, nil, nil},
		{"shred missing.txt", false, Recoverable, nil, nil},
		// uniq's second operand is an output file.
		{"uniq data.csv out.csv", false, Recoverable, []string{"create out.csv"}, nil},
		{"uniq data.csv a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		// wget names its output from the URL, or -O; -O - is standard output.
		{"wget https://example.com/files/f.zip", false, Recoverable, []string{"create f.zip"}, nil},
		{"wget -O a.txt https://example.com/x", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"wget -O - https://example.com/x", false, Recoverable, nil, nil},
		{"wget -P logs https://example.com/", false, Recoverable, []string{"create logs/index.html"}, nil},
		// wget never replaces an existing file: it saves NAME.1, so undo must not delete NAME.
		{"wget https://example.com/a.txt", false, Recoverable, []string{"create a.txt.1"}, []string{"create a.txt"}},
		{"wget -nc https://example.com/a.txt", false, Recoverable, nil, []string{"create a.txt.1"}},
		{"wget -N https://example.com/a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"wget -r https://example.com/", false, Unrecoverable, nil, nil},
		{"wget -o wget.log https://example.com/f.zip", false, Recoverable, []string{"create wget.log", "create f.zip"}, nil},
		// curl writes only with an output option; -O names the file from the URL.
		{"curl -o page.html https://example.com", false, Recoverable, []string{"create page.html"}, nil},
		{"curl -O https://example.com/a.txt", false, RecoverableWithCapture, []string{"write a.txt"}, nil},
		{"curl -K cfg https://example.com", false, Unrecoverable, nil, nil},
		// dd writes its of= file; a raw device is unrecoverable.
		{"dd if=a.txt of=b.txt", false, RecoverableWithCapture, []string{"write b.txt"}, nil},
		// compression: decompression writes the plain file and removes the archive.
		{"gzip -k big.log", false, Recoverable, []string{"create big.log.gz"}, []string{"remove big.log"}},
		{"gzip -c big.log > big.gz", false, Recoverable, []string{"create big.gz"}, nil},
		// tar: creating writes the archive; --remove-files captures what it removes.
		{"tar -cf a.tar a.txt --remove-files", false, RecoverableWithCapture, []string{"create a.tar", "remove a.txt"}, nil},
		{"tar -tf whatever.tar", false, Recoverable, nil, nil},
		{"tar -xf - < a.tar", false, Unrecoverable, nil, nil},
		// truncate creates unless -c.
		{"truncate -c -s 0 new.txt", false, Recoverable, nil, []string{"create new.txt"}},
		// sort -o and redirects onto an existing file write it.
		{"sort data.csv > sorted.csv", false, Recoverable, []string{"create sorted.csv"}, nil},
		{"ls > /dev/null 2>&1", false, Recoverable, nil, nil},
		// rsync options whose own output is a file, even in a dry run, cannot be resolved by one.
		{"rsync -a --log-file=r.log src/ dest/", true, Unrecoverable, nil, nil},
		{"rsync -a src/ host:dest/", true, Unrecoverable, nil, nil},
		{"rsync -n -a src/ dest/", true, Recoverable, nil, nil},
		// git: read-only subcommands and local-safe ones pass; history rewrites ask.
		{"git log --oneline", false, Recoverable, nil, nil},
		{"git rebase main", false, Unrecoverable, nil, nil},
		{"git push --force", false, Unrecoverable, nil, nil},
		{"git frobnicate", false, Unrecoverable, nil, nil},
	})
}

func TestStateToolsAndNameTables(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// state tools: read-only forms pass, everything else asks.
		{"pip list", false, Recoverable, nil, nil},
		{"pip install requests", false, Unrecoverable, nil, nil},
		{"crontab -l", false, Recoverable, nil, nil},
		{"crontab -r", false, Unrecoverable, nil, nil},
		{"ip addr", false, Recoverable, nil, nil},
		{"ip link set eth0 down", false, Unrecoverable, nil, nil},
		{"ufw disable", false, Unrecoverable, nil, nil},
		{"docker ps", false, Recoverable, nil, nil},
		{"docker rm web", false, Unrecoverable, nil, nil},
		{"sysctl -w vm.swappiness=10", false, Unrecoverable, nil, nil},
		{"journalctl --vacuum-time=1d", false, Unrecoverable, nil, nil},
		// named unrecoverable effects
		{"mkfs.ext4 /dev/sdb1", false, Unrecoverable, nil, nil},
		{"reboot", false, Unrecoverable, nil, nil},
		{"userdel guest", false, Unrecoverable, nil, nil},
		// interpreters are opaque, and code piped into one is unrecoverable
		{"cat a.txt | python3", false, Unrecoverable, nil, nil},
		{"perl -e 'unlink q(a.txt)'", false, Unrecoverable, nil, nil},
		// read-only forms with special cases
		{"date", false, Recoverable, nil, nil},
		{"date -s 2020-01-01", false, Unrecoverable, nil, nil},
		{"hostname newname", false, Unrecoverable, nil, nil},
		{"sort -o out.txt data.csv", false, Recoverable, []string{"create out.txt"}, nil},
		{"unzip -l a.zip", false, Recoverable, nil, nil},
		{"docker image ls", false, Recoverable, nil, nil},
		{"docker image rm web", false, Unrecoverable, nil, nil},
		// a .deb that cannot be read leaves the install unresolved
		{"dpkg -i missing.deb", true, Unrecoverable, nil, nil},
		// find with arguments that cannot be expanded: harmless if it has no action, asks if it has one
		{"find \"$UNSET_VARIABLE_XYZ\" -name x", false, Recoverable, nil, nil},
		{"find \"$UNSET_VARIABLE_XYZ\" -delete", false, Unrecoverable, nil, nil},
		{"base64 -o out a.txt", false, Unrecoverable, nil, nil},
	})
}

func TestWrappersAndShellStructure(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// transparent wrappers are stripped, with their own options skipped
		{"timeout 5 rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"env FOO=1 rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"nice -n 5 rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"nohup rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"sudo -u root -- rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"command -v rm", false, Recoverable, nil, nil},
		{"sudo -i", false, Unrecoverable, nil, nil},
		// shells: -c strings are analysed; scripts and piped code are not visible
		{"bash -c 'rm a.txt'", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"bash script.sh", false, Unrecoverable, nil, nil},
		{"cat a.txt | sh", false, Unrecoverable, nil, nil},
		{"bash", false, Unrecoverable, nil, nil},
		// a program run by a path outside the standard directories is not modelled
		{"./cleanup.sh", false, Unrecoverable, nil, nil},
		// a function defined on the line is an unknown command when called
		{"f() { rm a.txt; }; f", false, Unrecoverable, nil, nil},
		// variables bound earlier on the line are followed
		{"D=logs; rm $D/x.log", false, RecoverableWithCapture, []string{"remove logs/x.log"}, nil},
		{"export D=logs; rm \"$D/x.log\"", false, RecoverableWithCapture, []string{"remove logs/x.log"}, nil},
		{"declare -a A=(x); rm $A", false, Unrecoverable, nil, nil},
		{"D=$(cat nothing-here); rm $D", false, Unrecoverable, nil, nil},
		// a resolved substitution sees the variables bound before it
		{"D=logs; rm $(ls -d $D/*.log)", true, RecoverableWithCapture, []string{"remove logs/x.log", "remove logs/y.log"}, nil},
		// while loops bind nothing they can see, so a read variable is unresolved
		{"while read f; do rm \"$f\"; done < list.txt", false, Unrecoverable, nil, nil},
		// case arms are all analysed, since any may run
		{"case x in x) rm a.txt;; y) rm b.txt;; esac", false, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		// both branches of || count
		{"false || rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// cd to an unknown place makes later targets unknown; cd to a missing one stays put
		// Regression: a rule under an unknown directory found no targets and reported no effect,
		// so the line ran unprotected. It must ask.
		{"cd - && rm a.txt", false, Unrecoverable, nil, nil},
		{"cd \"$UNSET_VARIABLE_XYZ\" && rm -rf logs", false, Unrecoverable, nil, nil},
		{"pushd logs && chmod 000 x.log", false, Unrecoverable, nil, nil},
		{"cd - && sudo rm a.txt", false, Unrecoverable, nil, nil},
		{"cd - && ls", false, Recoverable, nil, nil},
		{"cd nowhere; rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"cd $(mktemp -d) && rm a.txt", true, Unrecoverable, nil, nil},
		// a computed command name is opaque
		{"$(echo rm) a.txt", false, Unrecoverable, nil, nil},
		// time and process substitution are walked
		{"time rm a.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{"diff <(rm a.txt) b.txt", false, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// xargs reading from a file, and xargs whose input cannot be seen
		{"xargs -a list.txt rm", true, Unrecoverable, nil, nil},
		{"xargs rm", true, Unrecoverable, nil, nil},
		{"ls *.tmp | xargs -n 1 rm", true, RecoverableWithCapture, []string{"remove one.tmp"}, nil},
		{"printf 'a.txt\\nb.txt\\n' | xargs -L 1 rm", true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		// find writing a list file through -fprint creates it
		{"find . -name '*.tmp' -fprint found.txt", false, Recoverable, []string{"create found.txt"}, nil},
	})
}

func TestUnzipResolvesMembers(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("unzip is not installed")
	}
	wd := fixture(t)
	f, err := os.Create(filepath.Join(wd, "arch.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"a.txt", "fresh/c.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("z"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a := analyze(t, wd, "unzip -o arch.zip", true)
	got := strings.Join(rel(wd, a), ",")
	if !strings.Contains(got, "write a.txt") || !strings.Contains(got, "create fresh/c.txt") {
		t.Fatalf("unzip effects = %s issues %+v", got, a.Issues)
	}
	if a.Verdict().Class != RecoverableWithCapture {
		t.Fatalf("class = %s", a.Verdict().Class)
	}
	// -n never overwrites, and -j drops the directories.
	b := analyze(t, wd, "unzip -n -j arch.zip", true)
	got = strings.Join(rel(wd, b), ",")
	if strings.Contains(got, "a.txt") || !strings.Contains(got, "create c.txt") {
		t.Fatalf("unzip -n -j effects = %s", got)
	}
	// Without a resolver the members are unknown, so it asks.
	if v := analyze(t, wd, "unzip -o arch.zip", false).Verdict(); v.Class != Unrecoverable {
		t.Fatalf("unresolved unzip: %s", v.Class)
	}
}

// TestMutationFoundGaps holds the cases mutation testing showed were untested: in each,
// breaking the code went unnoticed. Expected values come from what the tools do.
func TestMutationFoundGaps(t *testing.T) {
	runRuleCases(t, []ruleCase{
		// a single file moved into a folder lands inside it
		{"mv a.txt logs", false, Recoverable, []string{"create logs/a.txt"}, []string{"write logs"}},
		// Regression: an archive the line downloads or creates cannot be listed beforehand.
		// Listing it failed with no output, read as "extracts nothing", and the extraction
		// ran unprotected. It must ask.
		{"tar cf arch.tar a.txt src && tar xf arch.tar", true, Unrecoverable, nil, nil},
		{"curl -o x.tar https://example.com/x.tar && tar xf x.tar", true, Unrecoverable, nil, nil},
		{"wget https://example.com/x.zip && unzip -o x.zip", true, Unrecoverable, nil, nil},
		{"tar xf missing.tar", true, Unrecoverable, nil, nil},
		// curl's attached and long method forms send too
		{"curl -XDELETE http://h/x", false, Unrecoverable, nil, nil},
		{"curl --request=DELETE http://h/x", false, Unrecoverable, nil, nil},
		{"curl -XGET http://h/x", false, Recoverable, nil, nil},
		{"curl --request=HEAD http://h/x", false, Recoverable, nil, nil},
		// a script read from a process substitution is code the analysis cannot see
		{"bash <(echo rm a.txt)", false, Unrecoverable, nil, nil},
		// a sed backup suffix with a directory or pattern in it puts the backup somewhere else
		{"sed -i'old/*' 's/a/b/' a.txt", false, Unrecoverable, nil, nil},
		// date -s with the value attached still sets the clock
		{"date -s2020-01-01", false, Unrecoverable, nil, nil},
		// read-only forms of pacman and sysctl
		{"pacman -Qi bash", false, Recoverable, nil, nil},
		{"sysctl vm.swappiness", false, Recoverable, nil, nil},
		// -b inside a cluster still makes a backup of the file being replaced
		{"cp -bv a.txt b.txt", false, RecoverableWithCapture, []string{"write b.txt", "create b.txt~"}, nil},
	})
}

func TestTarOldStyleAndStripComponents(t *testing.T) {
	wd := fixture(t)
	if out, err := execIn(wd, "tar", "-czf", "arch.tgz", "a.txt", "src"); err != nil {
		t.Fatalf("tar: %v %s", err, out)
	}
	runRuleCasesIn(t, wd, []ruleCase{
		{"tar xzf arch.tgz", true, RecoverableWithCapture, []string{"write a.txt", "write src/main.go"}, nil},
		// --strip-components=1 drops the first path component: src/main.go lands as main.go,
		// and a.txt, which has only one, is not extracted at all.
		{"tar -xzf arch.tgz --strip-components=1", true, Recoverable, []string{"create main.go"}, []string{"write a.txt", "write src/main.go"}},
	})
}

func TestPlanForAPlainMoveCapturesNothing(t *testing.T) {
	wd := fixture(t)
	a := analyze(t, wd, "mv a.txt renamed.txt", false)
	if p := a.Plan(); len(p.Captures) != 0 {
		t.Fatalf("a rename is undone by moving back and needs no capture: %+v", p.Captures)
	}
}

func TestEscapedGlobMatchesOnlyTheLiteralName(t *testing.T) {
	wd := fixture(t)
	for _, n := range []string{"*.txt", "*x.txt"} {
		if err := os.WriteFile(filepath.Join(wd, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runRuleCasesIn(t, wd, []ruleCase{
		{`rm \*.txt`, false, RecoverableWithCapture, []string{"remove *.txt"}, []string{"remove *x.txt", "remove a.txt"}},
	})
}

func TestSubstitutionNestedTooDeeplyAsks(t *testing.T) {
	wd := fixture(t)
	a := New(wd)
	a.MaxDepth = 0
	if v := a.Analyze(context.Background(), "echo $(rm a.txt)").Verdict(); v.Class != Unrecoverable {
		t.Fatalf("a substitution past the depth bound must ask, got %s", v.Class)
	}
}
