package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"synapseos/internal/telemetry"
	"synapseos/internal/undo"
)

// runUndo reverses the most recently journaled reversible command. It shows
// what the undo will do and asks for confirmation before touching anything
// — undoing is itself a consequential filesystem action, so it goes through
// the same explicit-confirmation pattern as an irreversible command rather
// than running silently. The journal entry is only removed (PopLastJournal)
// after confirmation; declining leaves it in place so a later `synapse undo`
// can still act on it.
//
// runUndo is the `synapse undo` subcommand's testable core: peek the most
// recent journal entry, show what applying it would do, and — if
// confirmFn approves — pop it off the journal and apply it. Mirrors
// runLoop's design (I/O and the confirmation prompt taken as parameters,
// an exit code returned rather than os.Exit called directly) so it can be
// tested against a scratch journal file and canned confirmation answers
// instead of the real terminal and the real ~/.synapse/undo.log.
//
// runUndo takes an optional telemetry logger because M7 folded the former
// M8's undo telemetry into this path. A nil logger is a no-op, so ordinary
// non-study use passes nil and records nothing.
func runUndo(confirmFn func(string) bool, out, errOut io.Writer, journalPath string, tel *telemetry.Logger, taskID string) int {
	entry, ok, err := undo.PeekLastJournal(journalPath)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n", err)
		return 1
	}
	if n, _ := undo.UnreadableJournalLines(journalPath); n > 0 {
		if !ok {
			// Not "nothing to undo": something was recorded and cannot be read.
			fmt.Fprintf(errOut, "error: the undo journal %s has %d unreadable line(s) and no readable entry, so nothing can be undone.\n", journalPath, n)
			return 1
		}
		fmt.Fprintf(errOut, "warning: %d unreadable line(s) in the undo journal %s were skipped; the newest readable entry is shown.\n", n, journalPath)
	}
	if !ok {
		fmt.Fprintln(out, "Nothing to undo.")
		return 0
	}

	d := displaySafe
	// A step that only recorded changes it could not save has nothing to put
	// back; "Undo complete" for it was false (finding r2-atk5-08). It is
	// dropped from the history so the step before it can be undone.
	if !entry.Restorable() {
		fmt.Fprintf(out, "Nothing from the last step (%s) can be put back: the changes it made could not be saved before it ran.\n", d(entry.Command))
		if err := undo.PopLastJournalIf(journalPath, entry); err == nil {
			fmt.Fprintln(out, "I've removed it from the undo history. Run synapse undo again to undo the step before it.")
		}
		return 1
	}
	fmt.Fprintf(out, "Undoing: %s\n  (ran in %s, %s)\n", d(entry.Command), d(entry.Dir), ago(entry.Timestamp))
	// The newest step may belong to another folder: a request that changed
	// nothing records nothing, so undo reaches further back than the person
	// expects (finding r2-atk5-03).
	if cwd, err := os.Getwd(); err == nil && entry.Dir != "" && cwd != entry.Dir {
		fmt.Fprintf(out, "  Note: this step was done in a different folder from the one you are in now.\n")
	}
	for _, m := range entry.Moves {
		fmt.Fprintf(out, "  move back: %s -> %s\n", d(m.NewPath), d(m.OldPath))
	}
	for _, c := range entry.Created {
		fmt.Fprintf(out, "  remove: %s\n", d(c))
	}
	for _, cb := range entry.ContentBackups {
		fmt.Fprintf(out, "  restore content: %s\n", d(cb.Path))
	}
	for _, item := range entry.Trashed {
		fmt.Fprintf(out, "  restore from trash: %s\n", d(item.OriginalPath))
	}
	for _, mb := range entry.MetadataBackups {
		fmt.Fprintf(out, "  restore permissions: %s\n", d(mb.Path))
	}
	if entry.GitReset != "" {
		fmt.Fprintf(out, "  reset git HEAD back to: %s\n", entry.GitReset)
	}
	if len(entry.Unhandled) > 0 {
		fmt.Fprintf(out, "  note: %d change(s) from this command could not be safely reconstructed and are not part of this undo: %v\n", len(entry.Unhandled), entry.Unhandled)
	}
	// Said before asking, in plain words: what is there now is never thrown
	// away by an undo, so a person who changed a file since can still say yes.
	if aside := undo.WillSetAside(entry); len(aside) > 0 {
		fmt.Fprintln(out, "  What is there now will not be lost: these are moved to synapse's trash (~/.synapse/trash) first, in case you still need them:")
		for _, p := range aside {
			fmt.Fprintf(out, "    %s\n", d(p))
		}
	}
	if entry.GitReset != "" {
		fmt.Fprintln(out, "  Any changes you have not committed are saved first (see git stash list).")
	}

	if !confirmFn("apply this undo?") {
		fmt.Fprintln(out, "Cancelled.")
		// A declined undo is a finding, not a non-event: it says the
		// participant reached for recovery and then chose not to take it.
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoDeclined, "")
		return exitDeclined
	}

	// Removed only if it is still the entry shown: another synapse may have
	// added or undone something while the person was deciding, and applying
	// the shown entry after a different one was removed would lose that one.
	if err := undo.PopLastJournalIf(journalPath, entry); err != nil {
		if errors.Is(err, undo.ErrJournalChanged) {
			fmt.Fprintln(errOut, "error: the undo history changed while you were deciding (another synapse was running), so nothing was undone. Run synapse undo again to see the newest entry.")
		} else {
			fmt.Fprintf(errOut, "error: %v\n", err)
		}
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoFailed, err.Error())
		return 1
	}
	res := undo.ApplySafely(entry)
	if res.Aborted {
		// Nothing changed, so the entry goes back: it can still be undone.
		if err := undo.AppendJournal(journalPath, entry); err != nil {
			fmt.Fprintf(errOut, "error: could not put the entry back in the undo journal: %v\n", err)
		}
	}
	for _, k := range res.Kept {
		if k.Cleared {
			fmt.Fprintf(out, "kept: what was at %s is now at %s\n", d(k.Path), d(k.TrashPath))
		} else {
			fmt.Fprintf(out, "kept: a copy of %s is at %s\n", d(k.Path), d(k.TrashPath))
		}
	}
	if res.GitStash != "" {
		fmt.Fprintf(out, "kept: your uncommitted changes are saved as git stash %s (git stash list shows it; git stash apply brings them back)\n", res.GitStash)
	}
	if errs := res.Errs; len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(errOut, "error: %v\n", e)
		}
		// "The participant tried to recover and could not" is precisely the
		// observation the study needs; logging only successes would bias the
		// record toward the system working.
		tel.UndoInvoked(taskID, entry.Command, telemetry.UndoFailed, errs[0].Error())
		return 1
	}
	fmt.Fprintln(out, "Undo complete.")
	tel.UndoInvoked(taskID, entry.Command, telemetry.UndoApplied, "")
	return 0
}

// ago says how long ago t was, in words a person reads at a glance.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minute(s) ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hour(s) ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d day(s) ago", int(d.Hours()/24))
}
