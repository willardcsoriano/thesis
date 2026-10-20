package effects

import "testing"

// Defect 15: a wrapper's own options were read past the command it runs, or not at
// all: `command rm -v a.txt` was taken for `command -v`, a lookup, so the removal was
// never analysed; env -C and sudo -D ran the command in another directory than the
// one its paths were placed in; env -S hid the command in a string; and the
// variables env sets were not the ones the command it runs was analysed with.
func TestWrapperOptionsAndEnvironment(t *testing.T) {
	wd := assignFixture(t)
	runRuleCasesIn(t, wd, []ruleCase{
		{`command rm -v a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`command -p rm -v a.txt`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`command -v rm`, true, Recoverable, nil, nil},
		{`command -pV rm`, true, Recoverable, nil, nil},
		{`f=a.txt; command printf -v f keep/k.txt; rm "$f"`, true, Unrecoverable, nil, nil},
		{`env -C keep rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		{`env -Ckeep rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		{`env --chdir=keep rm k.txt`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, nil},
		{`env -C nothing-here rm a.txt`, true, Recoverable, nil, []string{"remove a.txt"}},
		{`env -S 'rm a.txt'`, true, Unrecoverable, nil, nil},
		{`f=a.txt; env f=keep/k.txt bash -c 'rm "$f"'`, true, RecoverableWithCapture, []string{"remove keep/k.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; env -u f bash -c 'rm "${f:-b.txt}"'`, true, RecoverableWithCapture, []string{"remove b.txt"}, []string{"remove a.txt"}},
		{`f=a.txt; env -i bash -c 'rm "${f:-b.txt}"'`, true, Unrecoverable, nil, nil},
		{`env -i f=a.txt bash -c 'rm "$f"'`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`env -i bash -c 'rm a.txt'`, true, RecoverableWithCapture, []string{"remove a.txt"}, nil},
		{`env PATH=. rm a.txt`, true, Unrecoverable, nil, nil},
		{`HOME=$PWD/keep; sudo bash -c 'rm "$HOME/k.txt"'`, true, Unrecoverable, nil, nil},
	})
}
