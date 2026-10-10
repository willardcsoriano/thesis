package effects

import (
	"context"
	"strings"
	"testing"
)

// The tests in this file check the analysis's guarantees rather than individual rules:
// that it only ever executes read-only commands, that its bounds hold, that switching
// composition off does what the ablation claims, and how the fail-closed baseline
// behaves. They are the ones docs/recoverability-analysis.md cites for each guarantee.

// seedCommands are the fuzz seeds: one per construct the analysis composes through,
// plus the resolvers' own shapes. `go test` runs every seed as an ordinary test case.
var seedCommands = []string{
	"rm a.txt", "rm $(ls *.log)", "ls | xargs rm", "find . -name '*.tmp' -delete",
	"find . -name '*.tmp' -exec rm {} +", "find . -type f -exec chmod 600 {} \\;",
	"for f in *.txt; do rm \"$f\"; done", "sh -c 'rm a.txt'", "eval 'rm a.txt'",
	"sudo find . -delete", "tar -xf a.tar", "unzip -o a.zip", "rsync -a --delete src/ backup/",
	"rename 's/a/b/' *.txt", "git clean -fd", "git reset --hard", "git checkout -b x",
	"apt install hello", "systemctl stop cron", "dpkg -r hello",
	"cat $(find . -name '*.go' -exec rm {} +)", "rm $(rm b.txt; echo a.txt)",
	"find . | xargs -I{} sh -c 'rm {}'", "ls $(touch x)", "echo $(cat a.txt > b.txt)",
	"rsync -a --log-file=r.log src/ dest/", "find . -fprint out -exec rm {} +",
}

// recorder is a Runner that executes nothing: it records each argv it is handed and
// returns empty output, which the analysis reads as "matched nothing".
func recorder(ran *[][]string) Runner {
	return func(_ context.Context, _ string, argv []string) ([]byte, error) {
		*ran = append(*ran, append([]string(nil), argv...))
		return nil, nil
	}
}

// readOnlyResolver reports whether argv is a command the analysis is allowed to run:
// either a shell string the analysis itself proves read-only, or one of the fixed
// dry-run adapters, each checked for the property that makes it read-only.
func readOnlyResolver(t *testing.T, wd string, argv []string) (bool, string) {
	t.Helper()
	switch argv[0] {
	case "bash":
		if len(argv) >= 3 && argv[1] == "-c" && argv[2] == `"$@" 2>&1` {
			// rename's adapter: the command runs with -n placed before its operands.
			ok := len(argv) >= 6 && (argv[4] == "rename" || argv[4] == "prename") && argv[5] == "-n"
			return ok, "rename adapter without a leading -n"
		}
		if len(argv) != 3 || argv[1] != "-c" {
			return false, "unexpected bash form"
		}
		var inner [][]string
		a := New(wd)
		a.Run = recorder(&inner)
		if res := a.Analyze(context.Background(), argv[2]); !res.ReadOnly() {
			return false, "shell text the analysis does not prove read-only: " + argv[2]
		}
		return true, ""
	case "find":
		for _, tok := range argv[1:] {
			if findActionTokens[tok] {
				return false, "find still carries action " + tok
			}
		}
		return true, ""
	case "rsync":
		if !hasAny(argv, "--dry-run") {
			return false, "rsync without --dry-run"
		}
		if hasPrefixAny(argv, "--log-file", "--write-batch", "--only-write-batch", "--remove-source-files") {
			return false, "rsync option that writes even in a dry run"
		}
		return true, ""
	case "tar":
		return len(argv) >= 3 && argv[1] == "-tf", "tar not in list mode"
	case "unzip":
		return len(argv) == 3 && argv[1] == "-Z1", "unzip not in list mode"
	case "git":
		_, sub, rest := gitSplit(argv[1:])
		switch sub {
		case "diff", "symbolic-ref", "rev-parse":
			return true, ""
		case "clean":
			return hasAny(rest, "-n"), "git clean without -n"
		}
		return false, "git subcommand " + sub
	case "apt-get":
		return len(argv) >= 2 && argv[1] == "-s", "apt-get without -s"
	case "apt-cache":
		return len(argv) >= 2 && argv[1] == "policy", "apt-cache form"
	case "dpkg-query":
		return len(argv) >= 2 && argv[1] == "-W", "dpkg-query form"
	case "dpkg-deb":
		return len(argv) >= 2 && argv[1] == "-f", "dpkg-deb form"
	case "systemctl":
		for _, a := range argv[1:] {
			if a == "is-active" || a == "is-enabled" {
				return true, ""
			}
		}
		return false, "systemctl form"
	}
	return false, "not a known resolver"
}

// FuzzResolversAreReadOnly checks the guarantee everything else rests on: whatever
// the command line, every command the analysis hands its runner is read-only.
// Run longer with: go test ./internal/effects -run '^$' -fuzz FuzzResolversAreReadOnly -fuzztime 60s
func FuzzResolversAreReadOnly(f *testing.F) {
	for _, s := range seedCommands {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		wd := fixture(t)
		var ran [][]string
		a := New(wd)
		a.Run = recorder(&ran)
		res := a.Analyze(context.Background(), cmd)
		for _, argv := range ran {
			if ok, why := readOnlyResolver(t, wd, argv); !ok {
				t.Fatalf("%q handed the runner a command that is not read-only (%s): %q", cmd, why, argv)
			}
		}
		// The verdict follows the issues: anything unresolved, opaque, or unrecoverable asks.
		if len(res.Issues) > 0 && res.Verdict().Class != Unrecoverable {
			t.Fatalf("%q has issues %+v but verdict %s", cmd, res.Issues, res.Verdict().Class)
		}
	})
}

func TestBoundsFailClosed(t *testing.T) {
	wd := fixture(t)
	type bound struct {
		name string
		set  func(*Analyzer)
		cmd  string
	}
	for _, b := range []bound{
		{"loop iterations", func(a *Analyzer) { a.MaxLoop = 1 }, "for f in a.txt b.txt; do rm $f; done"},
		{"nesting depth", func(a *Analyzer) { a.MaxDepth = 0 }, "sh -c 'rm a.txt'"},
		{"resolved targets", func(a *Analyzer) { a.MaxItems = 1 }, "find . -name '*.tmp' -delete"},
		{"recursive walk", func(a *Analyzer) { a.MaxItems = 1 }, "chmod -R 600 src"},
	} {
		t.Run(b.name, func(t *testing.T) {
			a := New(wd)
			a.Run = DefaultRunner(defaultTestTimeout)
			b.set(a)
			if v := a.Analyze(context.Background(), b.cmd).Verdict(); v.Class != Unrecoverable {
				t.Fatalf("over the bound, %q should ask; got %s %v", b.cmd, v.Class, v.Reasons)
			}
			// and under the default bounds the same command is resolved
			d := New(wd)
			d.Run = DefaultRunner(defaultTestTimeout)
			if v := d.Analyze(context.Background(), b.cmd).Verdict(); v.Class != RecoverableWithCapture {
				t.Fatalf("under the default bounds %q should resolve; got %s %v", b.cmd, v.Class, v.Reasons)
			}
		})
	}
}

// TestCompositionAblation pins what Compose=false means in the ablation (§3.1(h)): each
// composition construct becomes opaque, and the rule table underneath is unchanged.
func TestCompositionAblation(t *testing.T) {
	wd := fixture(t)
	for _, cmd := range []string{
		"find . -name '*.tmp' -delete", "find . -name '*.tmp' -exec rm {} +", "ls *.tmp | xargs rm",
		"for f in *.tmp; do rm $f; done", "sudo rm a.txt", "nohup rm a.txt", "sh -c 'rm a.txt'", "eval 'rm a.txt'",
	} {
		a := New(wd)
		a.Run = DefaultRunner(defaultTestTimeout)
		on := a.Analyze(context.Background(), cmd)
		a.Compose = false
		off := a.Analyze(context.Background(), cmd)
		if on.Verdict().Class != RecoverableWithCapture || len(on.Plan().Captures) == 0 {
			t.Errorf("composition on, %q should be captured: %s", cmd, on.Verdict().Class)
		}
		if off.Verdict().Class != Unrecoverable || len(off.Effects) != 0 {
			t.Errorf("composition off, %q should ask with nothing captured: %s %v", cmd, off.Verdict().Class, rel(wd, off))
		}
		found := false
		for _, is := range off.Issues {
			found = found || strings.Contains(is.Reason, "composition disabled")
		}
		if !found {
			t.Errorf("composition off, %q: no ablation issue in %+v", cmd, off.Issues)
		}
	}
	// What composition does not touch keeps its rule-table answer.
	for cmd, want := range map[string]Class{"rm a.txt": RecoverableWithCapture, "ls -la": Recoverable, "rm a.txt && ls | wc -l": RecoverableWithCapture} {
		a := New(wd)
		a.Compose = false
		if v := a.Analyze(context.Background(), cmd).Verdict(); v.Class != want {
			t.Errorf("composition off, plain %q: %s, want %s", cmd, v.Class, want)
		}
	}
}

func TestFailClosedBaseline(t *testing.T) {
	for cmd, wantAsk := range map[string]bool{
		"ls -la": false, "cat a.txt | sort | uniq -c": false, "timeout 5 ls": false, "sudo ls": false,
		"find . -name '*.go' -exec grep -l main {} +": false, "ls | xargs grep x": false,
		"sh -c 'ls'": false, "echo x > /dev/null": false, "apt list": false, "git log": false,
		"rm a.txt": true, "find . -delete": true, "find . -fprint out": true, "ls | xargs rm": true,
		"bash script.sh": true, "echo x > f": true, "f() { :; }": true, "apt install x": true,
		"/opt/tool/run": true, "ls $(rm a)": true, "sh -c 'rm a'": true, "it's": true,
		"sudo rm a.txt": true, "unknown-tool": true,
	} {
		if ask, why := FailClosed(cmd); ask != wantAsk {
			t.Errorf("FailClosed(%q) = %v (%v), want %v", cmd, ask, why, wantAsk)
		}
	}
}
