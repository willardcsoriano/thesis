package effects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/testenv"
)

// Defect 16 (found by the review of PR #109): bash evaluates array subscripts, and
// every value arithmetic reads, as expressions in which a subscript may hold a
// command substitution, run when the expression is evaluated:
//
//	printf -v 'x[$(rm a.txt)]' 1
//	v='x[$(rm a.txt)]'; (( v ))
//
// both delete a.txt. The analysis saw only that a variable changed, so it made the
// variables unknown and called the line recoverable with no effects. Some of the same
// paths assign by name where the analysis did not look (let with a quoted word, a
// subscripted read target, [[ -v ]] and unset subscripts, ${!r:=x}), so it captured
// the old value's file while bash removed another. Now arithmetic that evaluates a
// value the analysis does not know, or one that may hold code, makes the line opaque,
// and each of those assignments is followed.

// subscriptFixture is assignFixture with a file whose line is code in arithmetic.
func subscriptFixture(t *testing.T) string {
	t.Helper()
	wd := assignFixture(t)
	if err := os.WriteFile(filepath.Join(wd, "code.txt"), []byte("x[$(rm a.txt)]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return wd
}

// soundAgainstBash runs cmd under bash in a sandboxed copy of the fixture and checks
// the analysis against what was lost there: the line must ask, or capture every path
// bash lost. It also requires that bash lost something, so each case is evidence of a
// real hole and not of a line that does nothing.
func soundAgainstBash(t *testing.T, cmd string) {
	t.Helper()
	bwrap := oracle.Bwrap()
	if bwrap == "" {
		testenv.Unavailable(t, "bubblewrap is not usable here")
	}
	run := subscriptFixture(t)
	before, err := oracle.Observe(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oracle.Execute(context.Background(), bwrap, run, cmd, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	after, err := oracle.Observe(run)
	if err != nil {
		t.Fatal(err)
	}
	truth := oracle.Label(before, after)
	if truth.Label != "C" {
		t.Fatalf("bash lost nothing running %q, so the case shows no hole: %s", cmd, truth.Reason)
	}
	wd := subscriptFixture(t)
	a := analyze(t, wd, cmd, true)
	v := a.Verdict()
	if v.Class == Unrecoverable {
		return
	}
	captured := map[string]bool{}
	for _, e := range a.Effects {
		if e.Kind == Remove || e.Kind == Write || e.Kind == Meta {
			captured[strings.TrimPrefix(e.Path, wd+"/")] = true
		}
	}
	for _, p := range truth.Changed {
		if !captured[p] {
			t.Errorf("class %s, but bash lost %s, which the analysis does not capture\n effects: %v\n issues: %+v", v.Class, p, rel(wd, a), a.Issues)
		}
	}
}

// Code in a subscript or an arithmetic value runs; the line must ask.
func TestSubscriptCodeAsks(t *testing.T) {
	const code = `x[$(rm a.txt)]`
	for _, cmd := range []string{
		// The review's lines.
		`printf -v '` + code + `' 1`,
		`read '` + code + `' < list.txt`,
		`declare '` + code + `=1'`,
		`[[ -v '` + code + `' ]]`,
		`x=(1); unset '` + code + `'`,
		`v='` + code + `'; (( v ))`,
		`v='` + code + `'; [[ $v -eq 0 ]]`,
		`declare -i n; n='` + code + `'`,
		`read v < code.txt; (( v ))`,
		// Their neighbours.
		`declare -a '` + code + `=1'`,
		`x=(1); unset -v '` + code + `'`,
		`test -v '` + code + `'`,
		`[ -v '` + code + `' ]`,
		`v='` + code + `'; [[ v -eq 0 ]]`,
		`[[ 1 -eq '` + code + `' ]]`,
		`v='` + code + `'; [[ 0+$v -eq 0 ]]`,
		`let '` + code + `'`,
		`v='` + code + `'; let v`,
		`v='` + code + `'; (( v + 1 ))`,
		`v='` + code + `'; w=v; (( w ))`,
		`v='` + code + `'; (( ${v} ))`,
		`v='` + code + `'; (( ${v:-0} ))`,
		`v='` + code + `'; (( ${#v} > 0 && v ))`,
		`v='` + code + `'; x=(1 2); (( x[v] ))`,
		`v='` + code + `'; x=(1 2); (( ${x[v]} ))`,
		`v='` + code + `'; x=(1 2); x[v]=3`,
		`v='` + code + `'; x=([v]=1)`,
		`v='` + code + `'; for (( i = v; 0; )); do :; done`,
		`v='` + code + `'; declare -i n=v`,
		`v='` + code + `'; declare -i n; n+=v`,
		`declare -i n; read n < code.txt`,
		`n=$(cat code.txt); (( n ))`,
		`(( $(cat code.txt) ))`,
		`[[ $(cat code.txt) -eq 0 ]]`,
		`declare -n r='` + code + `'; r=1`,
		`declare -n r='` + code + `'; : "$r"`,
		`v='` + code + `'; : "${!v}"`,
		`v='` + code + `'; : "${!v:=1}"`,
		`read -r v < code.txt; [[ $v -gt 0 ]]`,
		// A substitution the analysis would resolve by running it.
		`v='` + code + `'; echo "$( (( v )); echo hi)"`,
		`v='` + code + `'; if (( v )); then :; fi`,
		`v='` + code + `'; (( v )) | cat`,
	} {
		t.Run(cmd, func(t *testing.T) { soundAgainstBash(t, cmd) })
	}
}

// Assignments by name the analysis did not follow: bash removes another file than
// the one captured.
func TestSubscriptAssignmentsAreFollowed(t *testing.T) {
	for _, cmd := range []string{
		`f=a.txt; let 'f=1'; rm "$f"`,
		`f=a.txt; let "f=1"; rm "$f"`,
		`f=a.txt; let 'f = 3' n=2; rm "$f"`,
		`f=a.txt; read 'f[0]' < list.txt; rm "$f"`,
		`f=a.txt; read -a 'f' < list.txt; rm "$f"`,
		`f=a.txt; [[ -v 'x[f=1]' ]]; rm "$f"`,
		`f=a.txt; test -v 'x[f=1]'; rm "$f"`,
		`f=a.txt; x=(1); unset 'x[f=1]'; rm "$f"`,
		`f=a.txt; printf -v 'x[f=3]' 1; rm "$f"`,
		`f=a.txt; [[ 1 -eq 'f=3' ]]; rm "$f"`,
		`f=a.txt; v='f=1'; [[ $v -eq 1 ]]; rm "$f"`,
		`f=; r=f; : "${!r:=keep/k.txt}"; rm "$f"`,
		`unset f; r=f; : "${!r=keep/k.txt}"; rm "$f"`,
	} {
		t.Run(cmd, func(t *testing.T) { soundAgainstBash(t, cmd) })
	}
}

// The safe neighbours keep resolving: none of these may start asking.
func TestSafeSubscriptsStayPrecise(t *testing.T) {
	wd := subscriptFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		{`f=a.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`printf -v 'x[0]' 1; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`x=(1); unset 'x[0]'; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`read 'x[1]' < list.txt; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`declare 'x[2]=1'; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`[[ -v f ]] && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`[[ -v 'x[0]' ]] && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`test -v f && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`(( n = 5 )); rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`i=3; (( i > 2 )) && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`i=3; [[ $i -gt 2 ]] && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`i=3; [[ i -gt 2 ]] && rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`for (( i = 0; i < 2; i++ )); do :; done; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`let i=1+2; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`f=a.txt; declare -i n; n=5; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`f=a.txt; declare -i n=2+3; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`r=f; f=a.txt; rm "${!r}"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
	})
}
