package effects

import (
	"os"
	"path/filepath"
	"testing"
)

// Defect 14: the analysis follows a variable through plain assignments, for loops, and
// the while-read records it resolves, and expands later words with the value it
// holds. Any other way a variable changes, the analysis did not see, so it expanded
// the old value, captured that, and the command acted on the new one uncaptured:
//
//	find . -name '*.tmp' | while read f; do printf -v f keep/k.txt; rm "$f"; done
//
// captured the .tmp files and deleted keep/k.txt. Every builtin that assigns by name
// now rebinds the name: to its value where that is known, otherwise to unknown, and
// a word that reads an unknown variable cannot be expanded, so the command asks.

// assignFixture is the standard fixture with the files these lines reach for.
func assignFixture(t *testing.T) string {
	t.Helper()
	wd := fixture(t)
	for p, body := range map[string]string{
		"keep/k.txt": "keep\n", "list.txt": "keep/k.txt\n", "1": "one\n", "3": "three\n",
		"A.TXT": "upper\n", "b": "b\n", "set.sh": "f=keep/k.txt\n", ".hidden": "h\n",
	} {
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

const tmpLoop = `find . -name '*.tmp' | while read f; do `

func TestAssigningBuiltinsRebind(t *testing.T) {
	wd := assignFixture(t)
	ask := func(cmd string) ruleCase { return ruleCase{cmd, true, Unrecoverable, nil, nil} }
	runRuleCasesIn(t, wd, []ruleCase{
		// The audit's lines.
		ask(tmpLoop + `printf -v f keep/k.txt; rm "$f"; done`),
		ask(tmpLoop + `declare -n r=f; r=keep/k.txt; rm "$f"; done`),
		ask(`f=a.txt; printf -v f keep/k.txt; rm "$f"`),
		// printf -v in every spelling, and through a wrapper.
		ask(`f=a.txt; printf -vf keep/k.txt; rm "$f"`),
		ask(`f=a.txt; printf -v f -- keep/k.txt; rm "$f"`),
		ask(`f=a.txt; builtin printf -v f keep/k.txt; rm "$f"`),
		// Namerefs reach whatever they name, so after one nothing is known.
		ask(`f=a.txt; declare -n r=f; r=keep/k.txt; rm "$f"`),
		ask(`f=a.txt; typeset -n r=f; r=keep/k.txt; rm "$f"`),
		ask(`f=a.txt; declare -n r; r=f; r=keep/k.txt; rm "$f"`),
		ask(`f=a.txt; n=f; declare -n r=$n; r=keep/k.txt; rm "$f"`),
		ask(`f=a.txt; declare -n r=f; read r < list.txt; rm "$f"`),
		ask(`f=a.txt; declare -n r=f; HOME=/; rm "$f"`),
		// declare and its kin with an argument that cannot be known: it may be an
		// option, or name any variable.
		ask(`f=a.txt; read n < list.txt; declare "$n=x"; rm "$f"`),
		ask(`f=a.txt; read o < list.txt; export $o f=keep/k.txt; rm "$f"`),
		// Attributes that change what a later assignment stores.
		ask(`f=a.txt; declare -u f; f=a.txt; rm "$f"`),
		ask(`declare -l f=A.TXT; rm "$f"`),
		ask(`f=a.txt; declare -i f; f=1+2; rm "$f"`),
		ask(`f=a.txt; readonly f; f=keep/k.txt; rm "$f"`),
		// getopts, read, mapfile in every form.
		ask(`set -- -b; f=a.txt; getopts ab f; rm "$f"`),
		ask(`f=a.txt; read f < list.txt; rm "${f:-a.txt}"`),
		ask(`f=a.txt; IFS= read -r f < list.txt; rm "$f"`),
		ask(`f=a.txt; read -ra f < list.txt; rm "$f"`),
		ask(`f=a.txt; read -r -d '' f < list.txt; rm "$f"`),
		ask(`f=a.txt; mapfile -t f < list.txt; rm "$f"`),
		ask(`f=a.txt; readarray -t f < list.txt; rm "$f"`),
		ask(`mapfile -C 'rm keep/k.txt; :' -c 1 arr < list.txt`),
		// Arithmetic assigns wherever it is evaluated.
		ask(`f=a.txt; let f=1; rm "$f"`),
		ask(`f=a.txt; (( f = 1 )); rm "$f"`),
		ask(`f=a.txt; (( f++ )); rm "$f"`),
		ask(`f=a.txt; : $(( f = 3 )); rm "$f"`),
		ask(`f=a.txt; echo "${x[f=3]}"; rm "$f"`),
		ask(`f=a.txt; s=abc; echo "${s:f=1}"; rm "$f"`),
		ask(`f=a.txt; for (( f = 1; f < 2; f++ )); do :; done; rm "$f"`),
		ask(`f=a.txt; [[ f=3 -eq 3 ]]; rm "$f"`),
		// A variable whose value is an expression assigns when it is evaluated.
		ask(`f=a.txt; v='f=1'; (( v )); rm "$f"`),
		ask(`f=a.txt; read v < list.txt; (( v )); rm "$f"`),
		// ${f:=x} and ${f=x} assign.
		ask(`f=; : "${f:=keep/k.txt}"; rm "$f"`),
		ask(`unset f; echo "${f=keep/k.txt}" >/dev/null; rm "$f"`),
		// source runs a file in the current shell.
		ask(`f=a.txt; . ./set.sh; rm "$f"`),
		ask(`f=a.txt; source ./set.sh; rm "$f"`),
		// Other builtins that assign by name.
		ask(`f=a.txt; sleep 0 & wait -p f; rm "$f"`),
		ask(`f=a.txt; exec {f}>/dev/null; rm "$f"`),
		// A function may assign anything; a function named like a command is not it.
		ask(`g() { f=keep/k.txt; }; f=a.txt; g; rm "$f"`),
		ask(`rm() { command rm keep/k.txt; }; rm a.txt`),
		// The variables bash sets itself are not the environment's.
		ask(`ls keep/k.txt >/dev/null; rm "$_"`),
		// A prefix assignment the analysis cannot know.
		ask(`f=a.txt; read v < list.txt; f=$v bash -c 'rm "$f"'`),
		// Options that change how words expand or commands are found.
		ask(`shopt -s dotglob; rm -r *`),
		ask(`GLOBIGNORE=x; rm -r *`),
		ask(`shopt -s lastpipe; f=a.txt; cat list.txt | read f; rm "$f"`),
		ask(`set -f; rm *.txt`),
		ask(`PATH=.:$PATH; rm a.txt`),
		ask(`hash -p ./set.sh rm; rm a.txt`),
		ask(`trap 'rm keep/k.txt' EXIT; true`),
		ask(`HOME=$PWD/keep; cd; rm k.txt`),
		ask(`CDPATH=keep; cd .hidden 2>/dev/null; cd logs; rm x.log`),
		// A variable a branch may have set is not known after it.
		ask(`if [ -e nothing ]; then f=a.txt; fi; rm "${f:-keep/k.txt}"`),
	})
}

// The same builtins with values the analysis can know keep the analysis precise.
func TestKnownAssignmentsStayPrecise(t *testing.T) {
	wd := assignFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		{`f=a.txt; declare f=keep/k.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; export f=keep/k.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; readonly f=keep/k.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		// Arguments the parser does not see as assignments, but whose text is known.
		{`f=a.txt; n=f; declare "$n=keep/k.txt"; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; n=f; export "$n=keep/k.txt"; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; v=keep/k.txt; n=f; declare -- "$n"=$v; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; f+=.bak; rm "$f"`, true, Recoverable, nil, []string{"remove a.txt"}},
		{`f=a; f+=.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		// eval runs in the current shell, and a prefix assignment is what the command
		// it prefixes sees, for that command only.
		{`f=a.txt; eval 'f=keep/k.txt'; rm "$f"`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; f=keep/k.txt eval 'rm "$f"'`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; f=keep/k.txt bash -c 'rm "$f"'`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; f=keep/k.txt env | head -1; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, []string{"remove keep/k.txt"}},
		{`f=a.txt; (f=keep/k.txt); rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, []string{"remove keep/k.txt"}},
		{`f=a.txt; f=keep/k.txt true; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, []string{"remove keep/k.txt"}},
		{`f=a.txt; echo keep/k.txt | read f; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`i=0; (( i++ )); rm "$i"`, true, Unrecoverable, nil, nil},
		{`f=a.txt; (( n = 1 )); rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`set -e; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`set -euo pipefail; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`trap - EXIT; rm a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`cd keep && rm k.txt; cd "$OLDPWD" && rm a.txt`, true, Unrecoverable, nil, nil},
		{`cd keep && rm "$OLDPWD/a.txt"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`HOME=$PWD/keep; cd && rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		{`CDPATH=; cd keep && rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		{`cd ./keep && rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		// The loop is a pipeline stage, a subshell: f outside it is untouched.
		{`printf '%s\n' a.txt | while read -r f; do rm "$f"; done; rm "${f:-b.txt}"`, true, RecoverableWithCapture, []string{"remove a.txt", "remove b.txt"}, nil},
		{`f=keep/k.txt; unset f; rm "${f:-a.txt}"`, true, RecoverableWithCapture, []string{"remove a.txt"}, []string{"remove keep/k.txt"}},
		{`for f in a.txt; do :; done; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`declare -r f=a.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`declare -x f=a.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`declare -a f; f=a.txt; rm "$f"`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{tmpLoop + `rm "$f"; done`, true, RecoverableWithCapture, []string{"remove one.tmp", "remove sub/two.tmp"}, nil},
	})
}
