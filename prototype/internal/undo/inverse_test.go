package undo

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// stubInverseRunner replaces the runner for one test and restores it afterwards.
func stubInverseRunner(t *testing.T, fn func(Inverse) error) {
	t.Helper()
	orig := inverseRunner
	inverseRunner = fn
	t.Cleanup(func() { inverseRunner = orig })
}

func TestInverseString(t *testing.T) {
	cases := []struct {
		inv  Inverse
		want string
	}{
		{Inverse{Argv: []string{"apt-get", "purge", "-y", "hello"}}, "apt-get purge -y hello"},
		{Inverse{Argv: []string{"apt-get", "purge", "-y", "hello"}, Sudo: true}, "sudo apt-get purge -y hello"},
	}
	for _, c := range cases {
		if got := c.inv.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestInverseStringDoesNotMutateArgv(t *testing.T) {
	inv := Inverse{Argv: make([]string, 2, 8), Sudo: true}
	inv.Argv[0], inv.Argv[1] = "systemctl", "stop"
	_ = inv.String()
	if !reflect.DeepEqual(inv.Argv, []string{"systemctl", "stop"}) {
		t.Errorf("Argv mutated: %v", inv.Argv)
	}
}

func TestApplyRunsInversesInStoredOrder(t *testing.T) {
	var ran []string
	stubInverseRunner(t, func(inv Inverse) error {
		ran = append(ran, inv.String())
		return nil
	})
	e := Entry{Dir: t.TempDir(), Inverses: []Inverse{
		{Argv: []string{"systemctl", "stop", "foo"}, Sudo: true},
		{Argv: []string{"apt-get", "purge", "-y", "foo"}, Sudo: true},
		{Argv: []string{"apt-get", "autoremove", "-y"}},
	}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	want := []string{
		"sudo systemctl stop foo",
		"sudo apt-get purge -y foo",
		"apt-get autoremove -y",
	}
	if !reflect.DeepEqual(ran, want) {
		t.Errorf("ran = %v, want %v", ran, want)
	}
}

func TestApplyFailingInverseReportsManualCommandAndContinues(t *testing.T) {
	boom := errors.New("exit status 100")
	var ran []string
	stubInverseRunner(t, func(inv Inverse) error {
		ran = append(ran, inv.String())
		if inv.Argv[0] == "apt-get" {
			return boom
		}
		return nil
	})
	e := Entry{Dir: t.TempDir(), Inverses: []Inverse{
		{Argv: []string{"apt-get", "purge", "-y", "hello"}, Sudo: true},
		{Argv: []string{"systemctl", "stop", "hello"}},
	}}
	errs := Apply(e)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
	msg := errs[0].Error()
	if !strings.Contains(msg, "sudo apt-get purge -y hello") {
		t.Errorf("error %q does not name the manual command with its sudo prefix", msg)
	}
	if !errors.Is(errs[0], boom) {
		t.Errorf("error %q does not wrap the runner's error", msg)
	}
	// A failure must not strand the inverses after it.
	if len(ran) != 2 || ran[1] != "systemctl stop hello" {
		t.Errorf("ran = %v, want the second inverse to run after the first failed", ran)
	}
}

func TestApplyFailingInverseWithoutSudoOmitsPrefix(t *testing.T) {
	stubInverseRunner(t, func(Inverse) error { return errors.New("nope") })
	errs := Apply(Entry{Dir: t.TempDir(), Inverses: []Inverse{{Argv: []string{"systemctl", "stop", "x"}}}})
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if strings.Contains(errs[0].Error(), "sudo") {
		t.Errorf("error %q mentions sudo for a non-sudo inverse", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "run it yourself: systemctl stop x") {
		t.Errorf("error %q lacks the manual command", errs[0])
	}
}

func TestRunInverseEmptyArgvIsAnError(t *testing.T) {
	if err := runInverse(Inverse{}); err == nil {
		t.Fatal("runInverse of an empty command returned nil")
	}
	// And Apply surfaces that through the real runner.
	if errs := Apply(Entry{Dir: t.TempDir(), Inverses: []Inverse{{}}}); len(errs) != 1 {
		t.Fatalf("errs = %v, want one", errs)
	}
}

func TestRunInverseRunsRealCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	if err := runInverse(Inverse{Argv: []string{"touch", marker}}); err != nil {
		t.Fatalf("runInverse: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("command did not run: %v", err)
	}
	if err := runInverse(Inverse{Argv: []string{"false"}}); err == nil {
		t.Error("a failing command must return an error")
	}
}

// The inverse install keeps configuration restored from trash, so trash must be
// back in place before any inverse runs.
func TestApplyRestoresTrashBeforeRunningInverse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	work := t.TempDir()
	conf := filepath.Join(work, "app.conf")
	if err := os.WriteFile(conf, []byte("settings"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, errs := TrashPreserve([]string{conf})
	if len(errs) != 0 || len(items) != 1 {
		t.Fatalf("TrashPreserve: %+v %v", items, errs)
	}
	if err := os.Remove(conf); err != nil {
		t.Fatal(err)
	}

	var sawAtRun string
	stubInverseRunner(t, func(Inverse) error {
		b, err := os.ReadFile(conf)
		if err != nil {
			return err
		}
		sawAtRun = string(b)
		return nil
	})
	e := Entry{Dir: work, Trashed: items, Inverses: []Inverse{{Argv: []string{"apt-get", "install", "-y", "app"}}}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v (inverse ran before trash was restored?)", errs)
	}
	if sawAtRun != "settings" {
		t.Errorf("file seen by the inverse = %q, want %q", sawAtRun, "settings")
	}
}

// Content restore comes after: the inverse must see the changed bytes, and Apply
// must leave the backed-up bytes behind.
func TestApplyRunsInverseBeforeContentRestore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups, errs := BackupContent([]string{target})
	if len(errs) != 0 || len(backups) != 1 {
		t.Fatalf("BackupContent: %+v %v", backups, errs)
	}
	if err := os.WriteFile(target, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	var sawAtRun string
	stubInverseRunner(t, func(Inverse) error {
		b, err := os.ReadFile(target)
		sawAtRun = string(b)
		return err
	})
	e := Entry{Dir: filepath.Dir(target), ContentBackups: backups, Inverses: []Inverse{{Argv: []string{"true"}}}}
	if errs := Apply(e); len(errs) != 0 {
		t.Fatalf("Apply: %v", errs)
	}
	if sawAtRun != "changed" {
		t.Errorf("inverse saw %q, want the pre-restore content %q", sawAtRun, "changed")
	}
	if b, _ := os.ReadFile(target); string(b) != "original" {
		t.Errorf("final content = %q, want %q", b, "original")
	}
}

func TestEntryWithOnlyInversesIsNotNoop(t *testing.T) {
	if (Entry{}).IsNoop() != true {
		t.Fatal("zero Entry must be a no-op")
	}
	e := Entry{Inverses: []Inverse{{Argv: []string{"apt-get", "purge", "-y", "hello"}}}}
	if e.IsNoop() {
		t.Error("an entry that has only Inverses must not be a no-op, or it would never be journaled")
	}
}

func TestJournalRoundTripPreservesInverses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undo.log")
	want := Entry{
		Command: "sudo apt-get install -y hello",
		Dir:     "/work",
		Inverses: []Inverse{
			{Argv: []string{"apt-get", "purge", "-y", "hello"}, Sudo: true},
			{Argv: []string{"systemctl", "disable", "hello"}},
		},
	}
	other := Entry{Command: "ls", Dir: "/work", Created: []string{"x"}}
	if err := AppendJournal(path, other); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(path, want); err != nil {
		t.Fatal(err)
	}

	peek, ok, err := PeekLastJournal(path)
	if err != nil || !ok {
		t.Fatalf("Peek: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(peek.Inverses, want.Inverses) {
		t.Errorf("peeked Inverses = %+v, want %+v", peek.Inverses, want.Inverses)
	}

	popped, ok, err := PopLastJournal(path)
	if err != nil || !ok {
		t.Fatalf("Pop: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(popped.Inverses, want.Inverses) || popped.Command != want.Command {
		t.Errorf("popped = %+v, want Inverses %+v", popped, want.Inverses)
	}
	// The earlier entry, which has no inverses, must survive with none.
	rest, ok, err := PeekLastJournal(path)
	if err != nil || !ok || rest.Command != "ls" || len(rest.Inverses) != 0 {
		t.Errorf("remaining entry = %+v ok=%v err=%v", rest, ok, err)
	}
}

func TestJournalOmitsEmptyInverses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undo.log")
	if err := AppendJournal(path, Entry{Command: "ls", Created: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "inverses") {
		t.Errorf("journal line %q should not carry an empty inverses field", b)
	}
}
