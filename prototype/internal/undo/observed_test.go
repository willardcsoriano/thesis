package undo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// KeepObserved follows a path through later moves before deciding a step did
// not happen. The analysis turns a chained move into a capture today, so this
// entry is built by hand; it is the shape Entry allows, and judging each move
// by its own destination dropped the first move and left undo with the file
// at the middle name.
func TestKeepObservedFollowsLaterMoves(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"c.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := Entry{Dir: dir,
		Created: []string{"made.txt", "gone.txt"},
		Moves: []Move{
			{OldPath: "a.txt", NewPath: "b.txt"},
			{OldPath: "b.txt", NewPath: "c.txt"},
			{OldPath: "made.txt", NewPath: "n.txt"}, // made, moved, then moved on to d.txt
			{OldPath: "n.txt", NewPath: "d.txt"},
			{OldPath: "p.txt", NewPath: "q.txt"}, // never happened: q.txt is not there
		}}
	got := e.KeepObserved()
	if want := e.Moves[:4]; !reflect.DeepEqual(got.Moves, want) {
		t.Errorf("moves = %+v, want %+v", got.Moves, want)
	}
	if !reflect.DeepEqual(got.Created, []string{"made.txt"}) {
		t.Errorf("created = %v, want only made.txt (gone.txt exists nowhere)", got.Created)
	}
}

// A cycle of moves whose paths are all gone ends rather than looping.
func TestKeepObservedEndsOnACycle(t *testing.T) {
	e := Entry{Dir: t.TempDir(), Moves: []Move{{OldPath: "a", NewPath: "b"}, {OldPath: "b", NewPath: "a"}, {OldPath: "a", NewPath: "b"}}}
	if got := e.KeepObserved(); len(got.Moves) != 0 {
		t.Errorf("moves = %+v, want none: nothing exists", got.Moves)
	}
}
