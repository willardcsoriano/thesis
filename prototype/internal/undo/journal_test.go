package undo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func entry(cmd string) Entry { return Entry{Timestamp: time.Unix(1, 0).UTC(), Command: cmd} }

func TestPopLastJournalIfRefusesAChangedJournal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "undo.log")
	for _, c := range []string{"one", "two"} {
		if err := AppendJournal(p, entry(c)); err != nil {
			t.Fatal(err)
		}
	}
	shown, _, _ := PeekLastJournal(p)
	if err := AppendJournal(p, entry("three")); err != nil { // another process, meanwhile
		t.Fatal(err)
	}
	if err := PopLastJournalIf(p, shown); !errors.Is(err, ErrJournalChanged) {
		t.Fatalf("PopLastJournalIf after a concurrent append = %v, want ErrJournalChanged", err)
	}
	if last, _, _ := PeekLastJournal(p); last.Command != "three" {
		t.Fatalf("the refused pop removed something: newest is %q", last.Command)
	}
	if err := PopLastJournalIf(p, entry("three")); err != nil {
		t.Fatalf("PopLastJournalIf on the current newest: %v", err)
	}
}

func TestUnreadableLinesAreSkippedAndKeptVerbatim(t *testing.T) {
	p := filepath.Join(t.TempDir(), "undo.log")
	if err := AppendJournal(p, entry("old")); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("{\"torn by a crash\n")
	f.Close()
	if err := AppendJournal(p, entry("new")); err != nil {
		t.Fatal(err)
	}
	if n, _ := UnreadableJournalLines(p); n != 1 {
		t.Fatalf("UnreadableJournalLines = %d, want 1", n)
	}
	e, ok, err := PopLastJournal(p)
	if err != nil || !ok || e.Command != "new" {
		t.Fatalf("PopLastJournal = %q %v %v", e.Command, ok, err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "{\"torn by a crash\n") {
		t.Fatalf("the unreadable line was not kept: %q", b)
	}
	if e, _, _ := PeekLastJournal(p); e.Command != "old" {
		t.Fatalf("newest readable after pop = %q, want old", e.Command)
	}
}

func TestJournalLinesLongerThanAMegabyteAreRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "undo.log")
	big := entry("big")
	for i := 0; i < 40000; i++ {
		big.Created = append(big.Created, fmt.Sprintf("/some/long/path/number/%08d.txt", i))
	}
	if err := AppendJournal(p, big); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Size() < 1<<20 {
		t.Fatalf("setup: journal only %d bytes", fi.Size())
	}
	e, ok, err := PeekLastJournal(p)
	if err != nil || !ok || len(e.Created) != 40000 {
		t.Fatalf("PeekLastJournal: ok=%v err=%v created=%d", ok, err, len(e.Created))
	}
}

func TestConcurrentAppendsAndPopsLoseNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "undo.log")
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := AppendJournal(p, entry(fmt.Sprint(i))); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	popped := make(chan string, n)
	for i := 0; i < n/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e, ok, err := PopLastJournal(p); err != nil || !ok {
				t.Errorf("pop: ok=%v err=%v", ok, err)
			} else {
				popped <- e.Command
			}
		}()
	}
	wg.Wait()
	close(popped)
	seen := map[string]bool{}
	for c := range popped {
		if seen[c] {
			t.Fatalf("entry %s popped twice", c)
		}
		seen[c] = true
	}
	lines, err := readJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.entry == nil || seen[l.entry.Command] {
			t.Fatalf("journal holds a bad or already-popped line %q", l.raw)
		}
		seen[l.entry.Command] = true
	}
	if len(seen) != n {
		t.Fatalf("%d of %d entries accounted for", len(seen), n)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".undo-journal-*")); len(leftovers) > 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

// An append after a line torn by a crash starts on a new line, so the new
// entry is readable (finding atk4-09).
func TestAppendAfterATornLineStartsANewLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "undo.log")
	if err := os.WriteFile(p, []byte(`{"timestamp":"2026-10-25T00:5`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJournal(p, entry("after the crash")); err != nil {
		t.Fatal(err)
	}
	e, ok, err := PeekLastJournal(p)
	if err != nil || !ok || e.Command != "after the crash" {
		t.Fatalf("newest entry = %q ok=%v err=%v: the append was glued onto the torn line", e.Command, ok, err)
	}
	if n, _ := UnreadableJournalLines(p); n != 1 {
		t.Fatalf("UnreadableJournalLines = %d, want just the torn one", n)
	}
}
