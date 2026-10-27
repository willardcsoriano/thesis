package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"synapseos/internal/ollama"
)

// Round-1 live findings atk1-08, atk1-09, atk2-01, atk2-03 and atk4-05
// (docs/cli-breaking.md): undo destroyed what the person had written since
// the command ran. Each test changes a path after the command, undoes, and
// checks that the undo happened and that the later version still exists.

// inTrash reports whether some file under home's synapse trash holds content.
func inTrash(t *testing.T, home, content string) bool {
	t.Helper()
	found := false
	filepath.WalkDir(filepath.Join(home, ".synapse", "trash"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); string(b) == content {
				found = true
			}
		}
		return nil
	})
	return found
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func undoYes(t *testing.T, home string) synapseResult {
	t.Helper()
	return execSynapse(t, synapseRun{args: []string{"undo"}, home: home, stdin: "y\n"})
}

func TestUndoOfACreationKeepsWhatWasWrittenSince(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	m := newFakeModel(t, runStep("touch todo.txt"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "make a todo list"}, env: m.env(), dir: dir, home: home}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(dir, "todo.txt"), "buy milk\n")
	r := undoYes(t, home)
	if !strings.Contains(r.stdout, "will not be lost") {
		t.Errorf("the undo plan did not say the current file is kept:\n%s", r.stdout)
	}
	if fileExists(filepath.Join(dir, "todo.txt")) {
		t.Errorf("todo.txt still there: the creation was not undone (exit %d) %s", r.code, r.stderr)
	}
	if !inTrash(t, home, "buy milk\n") {
		t.Errorf("the text written since the command is gone")
	}
}

func TestUndoOfAMoveKeepsANewerFileAtTheOriginalName(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "essay.txt"), "first draft\n")
	os.Mkdir(filepath.Join(dir, "archive"), 0o755)
	m := newFakeModel(t, runStep("mv essay.txt archive/"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "archive my essay"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(dir, "essay.txt"), "second draft\n")
	r := undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "essay.txt")); got != "first draft\n" {
		t.Errorf("essay.txt = %q after undo, want the moved original (exit %d) %s", got, r.code, r.stderr)
	}
	if !inTrash(t, home, "second draft\n") {
		t.Errorf("the newer essay.txt is gone")
	}
}

func TestUndoOfAnOverwriteKeepsAnEditMadeSince(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "original\n")
	m := newFakeModel(t, runStep("echo hello > a.txt"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "write hello into a.txt"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "my edit\n")
	r := undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "original\n" {
		t.Errorf("a.txt = %q after undo, want the original (exit %d) %s", got, r.code, r.stderr)
	}
	if !inTrash(t, home, "my edit\n") {
		t.Errorf("the edit made after the command is gone")
	}
}

func TestUndoOfADeletionIntoARecreatedFolderRestoresAndKeeps(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(dir, "docs"), 0o755)
	writeFile(t, filepath.Join(dir, "docs", "README.md"), "original readme\n")
	m := newFakeModel(t, runStep("rm -rf docs"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "delete docs"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	os.Mkdir(filepath.Join(dir, "docs"), 0o755)
	writeFile(t, filepath.Join(dir, "docs", "README.md"), "my new readme\n")
	r := undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "docs", "README.md")); got != "original readme\n" {
		t.Errorf("docs/README.md = %q after undo, want the original (exit %d) %s", got, r.code, r.stderr)
	}
	if !inTrash(t, home, "my new readme\n") {
		t.Errorf("the recreated README is gone")
	}
}

// A command that failed creates nothing, so there is nothing to undo; a file
// the person makes later under that name must survive undo.
func TestAFailedCreationIsNotJournaledAsACreation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir, home := t.TempDir(), t.TempDir()
	os.Chmod(dir, 0o555)
	// The exact command from finding atk4-05: it takes the analysis's
	// planned-capture path, which journaled the planned creation although
	// the command failed. (A bare `touch` takes the snapshot path, which
	// never journals a failed step, so it could not show the defect.)
	m := newFakeModel(t, runStep("touch new.txt && echo hello > new.txt"))
	execSynapse(t, synapseRun{args: []string{"run", "write hello into a new file called new.txt"}, env: m.env(), dir: dir, home: home, stdin: "y\n"})
	// Checked before any undo: an undo would consume a phantom entry and
	// hide it, which is how an earlier version of this test passed without
	// the fix (volkswagen audit, finding 3).
	for _, e := range journalEntries(t, home) {
		for _, c := range e.Created {
			if strings.HasSuffix(c, "new.txt") {
				t.Errorf("a creation that failed was journaled: %+v", e)
			}
		}
	}
	// And the person's own file made later under that name survives undo.
	os.Chmod(dir, 0o755)
	writeFile(t, filepath.Join(dir, "new.txt"), "mine\n")
	undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "new.txt")); got != "mine\n" && !inTrash(t, home, "mine\n") {
		t.Fatalf("new.txt = %q and not kept: undo destroyed a file the failed command never made", got)
	}
}

// A write through a symlink is undone in the file it reached, and the link
// survives (findings r2-atk7-01, r2-atk6-03).
func TestUndoThroughASymlinkRestoresTheTargetAndTheLink(t *testing.T) {
	for _, c := range []struct{ name, cmd string }{
		{"truncate through the link", "echo '' > notes.txt"},
		{"sed -i replaces the link", "sed -i 's/secret/public/' notes.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			os.Mkdir(filepath.Join(dir, "real"), 0o755)
			writeFile(t, filepath.Join(dir, "real", "secret.txt"), "secret data\n")
			if err := os.Symlink("real/secret.txt", filepath.Join(dir, "notes.txt")); err != nil {
				t.Fatal(err)
			}
			m := newFakeModel(t, runStep(c.cmd))
			if r := execSynapse(t, synapseRun{args: []string{"run", "change notes"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
				t.Fatalf("setup: %d %s", r.code, r.stderr)
			}
			r := undoYes(t, home)
			if got := readFile(t, filepath.Join(dir, "real", "secret.txt")); got != "secret data\n" {
				t.Errorf("target = %q after undo, want the original (exit %d) %s", got, r.code, r.stderr)
			}
			if text, err := os.Readlink(filepath.Join(dir, "notes.txt")); err != nil || text != "real/secret.txt" {
				t.Errorf("notes.txt is no longer the link (readlink %q, %v)", text, err)
			}
		})
	}
}

// A command aimed at synapse's own trash or history asks, and says plainly
// that undo cannot take it back (finding r2-atk5-05).
func TestCommandsOnSynapsesOwnStateAskAndWarn(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".synapse", "trash"), 0o755)
	writeFile(t, filepath.Join(home, ".synapse", "trash", "kept.txt"), "a kept copy\n")
	m := newFakeModel(t, runStep("rm -rf "+filepath.Join(home, ".synapse", "trash")+"/*"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "empty synapse's trash"}, env: m.env(), home: home})
	if r.code != exitDeclined {
		t.Fatalf("exit %d, want %d (asked, declined)", r.code, exitDeclined)
	}
	text := approvalFromJSON(t, r.stdout)
	if !strings.Contains(text, "earlier steps can no longer be undone") || !strings.Contains(text, "Undo cannot take this back") {
		t.Errorf("approval does not warn about synapse's own state: %q", text)
	}
	if strings.Contains(text, "can put things back") {
		t.Errorf("approval promises undo for a command that deletes undo's copies: %q", text)
	}
}

// Keeping a copy of a folder that contains synapse's trash does not copy the
// copy into itself (finding r2-atk5-06).
func TestDeletingSynapsesFolderDoesNotRecurse(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".synapse", "trash"), 0o755)
	writeFile(t, filepath.Join(home, ".synapse", "trash", "kept.txt"), "x")
	m := newFakeModel(t, runStep("rm -rf .synapse"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "delete the hidden synapse folder"}, env: m.env(), dir: home, home: home, stdin: "y\n"})
	// The copy must succeed and the approved command must run. Without the
	// guard the copy recursed until the path was too long; the copy then
	// failed, and the command did not run (capture_failed). Checking for the
	// error text alone proved nothing once errors were shortened.
	recs := readJSONLines(t, r.stdout)
	if end := recs[len(recs)-1]; end.Outcome != outcomeComplete {
		t.Fatalf("outcome %q, want complete; stderr:\n%.500s", end.Outcome, r.stderr)
	}
	if fileExists(filepath.Join(home, ".synapse", "trash", "kept.txt")) {
		t.Error("the approved rm -rf .synapse did not run")
	}
}

// When the copy undo depends on cannot be saved, the command does not run.
func TestACommandWhoseCopyFailsDoesNotRun(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir, home := t.TempDir(), t.TempDir()
	backups := filepath.Join(home, ".synapse", "content-backups")
	os.MkdirAll(backups, 0o755)
	os.Chmod(backups, 0o500)
	t.Cleanup(func() { os.Chmod(backups, 0o755) })
	writeFile(t, filepath.Join(dir, "a.txt"), "original\n")
	m := newFakeModel(t, runStep("echo replaced > a.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "replace a.txt"}, env: m.env(), dir: dir, home: home, stdin: "y\n"})
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "original\n" {
		t.Fatalf("a.txt = %q: the command ran without its copy", got)
	}
	recs := readJSONLines(t, r.stdout)
	if end := recs[len(recs)-1]; end.Outcome != outcomeCaptureFailed || r.code != exitFailed {
		t.Errorf("end %+v exit %d, want %s and %d", end, r.code, outcomeCaptureFailed, exitFailed)
	}
}

// A file that appears while the person is deciding changes what the command
// destroys; the approval is checked again and asked again (finding r2-atk8-04).
func TestAChangeWhileDecidingIsAskedAgain(t *testing.T) {
	t.Setenv("SYNAPSE_ANALYSIS", "strict")
	dir := t.TempDir()
	chdir(t, dir)
	server := scriptedOllamaServer(t, []string{"shred -u alpha2.txt"})
	defer server.Close()
	calls := 0
	confirm := func(string) bool {
		calls++
		if calls == 1 {
			writeFile(t, filepath.Join(dir, "alpha2.txt"), "precious\n") // appears at the prompt
			return true
		}
		return false
	}
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "shred alpha2", confirm, &out, &errOut, filepath.Join(t.TempDir(), "undo.log"), withoutAnswer())
	if calls != 2 {
		t.Fatalf("asked %d time(s), want 2: the change at the prompt was not noticed; out:\n%s", calls, out.String())
	}
	if code != exitDeclined || readFile(t, filepath.Join(dir, "alpha2.txt")) != "precious\n" {
		t.Fatalf("exit %d, alpha2.txt = %q; want declined and intact", code, readFile(t, filepath.Join(dir, "alpha2.txt")))
	}
	if !strings.Contains(out.String(), "changed while you were deciding") {
		t.Errorf("no notice that the folder changed:\n%s", out.String())
	}
}

// With an undo history that cannot be written, nothing that changes files
// runs unasked, and the person is told why (finding r2-atk8-08).
func TestUnwritableJournalMakesChangesAsk(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	dir, home := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(home, ".synapse"), 0o755)
	jp := filepath.Join(home, ".synapse", "undo.log")
	writeFile(t, jp, "")
	os.Chmod(jp, 0o444)
	m := newFakeModel(t, runStep("touch made.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "make a file"}, env: m.env(), dir: dir, home: home})
	if r.code != exitDeclined || fileExists(filepath.Join(dir, "made.txt")) {
		t.Fatalf("exit %d, made.txt exists=%v: a change ran unasked without undo; stderr:\n%s", r.code, fileExists(filepath.Join(dir, "made.txt")), r.stderr)
	}
	if !strings.Contains(r.stderr, "Undo is not available right now") {
		t.Errorf("not told that undo is unavailable:\n%s", r.stderr)
	}
	// Reading still runs.
	m2 := newFakeModel(t, runStep("ls"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "list files"}, env: m2.env(), dir: dir, home: home}); r.code != exitOK {
		t.Errorf("a read-only command was blocked: exit %d", r.code)
	}
}

// A file added inside a folder the command made is set aside, so the undo
// removes the folder and finishes (finding r2-atk6-02).
func TestUndoOfACreatedFolderWithAddedFilesFinishes(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	m := newFakeModel(t, runStep("mkdir out && cp a.txt out/"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "copy a into a new folder"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(dir, "out", "mine.txt"), "added later\n")
	r := undoYes(t, home)
	if r.code != exitOK || fileExists(filepath.Join(dir, "out")) {
		t.Fatalf("undo exit %d, out/ exists=%v; want the folder removed; stderr:\n%s", r.code, fileExists(filepath.Join(dir, "out")), r.stderr)
	}
	if !inTrash(t, home, "added later\n") {
		t.Error("the file added later is gone")
	}
}

// A saved copy that has gone missing stops the undo before anything moves,
// and the entry stays so it can be retried (finding r2-atk6-05).
func TestUndoWithAMissingCopyChangesNothing(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "original\n")
	m := newFakeModel(t, runStep("echo replaced > a.txt"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "replace a"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	for _, e := range journalEntries(t, home) {
		for _, cb := range e.ContentBackups {
			os.Remove(cb.BackupPath)
		}
	}
	r := undoYes(t, home)
	if r.code != exitFailed || !strings.Contains(r.stderr, "is missing") {
		t.Fatalf("undo exit %d; want %d and a missing-copy error; stderr:\n%s", r.code, exitFailed, r.stderr)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "replaced\n" {
		t.Errorf("a.txt = %q: the failed undo moved the current file", got)
	}
	if len(journalEntries(t, home)) != 1 {
		t.Error("the entry was lost, so the undo cannot be retried")
	}
}

// Undoing a git reset stashes the person's edits only, before anything is
// set aside, and claims a stash only when there were edits (r2-atk6-01).
func TestUndoOfAGitResetStashesOnlyRealEdits(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited=%v", edit), func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			git := func(args ...string) string {
				c := exec.Command("git", args...)
				c.Dir = dir
				b, err := c.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, b)
				}
				return string(b)
			}
			git("init", "-q")
			writeFile(t, filepath.Join(dir, "f.txt"), "one\n")
			git("add", ".")
			git("commit", "-qm", "one")
			writeFile(t, filepath.Join(dir, "f.txt"), "two\n")
			git("commit", "-qam", "two")
			m := newFakeModel(t, runStep("git reset --hard HEAD~1"))
			if r := execSynapse(t, synapseRun{args: []string{"run", "go back one commit"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
				t.Fatalf("setup: %d %s", r.code, r.stderr)
			}
			if edit {
				writeFile(t, filepath.Join(dir, "f.txt"), "my edit\n")
			}
			r := undoYes(t, home)
			if got := readFile(t, filepath.Join(dir, "f.txt")); got != "two\n" {
				t.Errorf("f.txt = %q after undo, want the commit's content (exit %d) %s", got, r.code, r.stderr)
			}
			claims := strings.Contains(r.stdout, "saved as git stash")
			if claims != edit {
				t.Fatalf("stash claimed=%v with edited=%v:\n%s", claims, edit, r.stdout)
			}
			if edit && !strings.Contains(git("stash", "show", "-p"), "my edit") {
				t.Error("the stash does not hold the edit")
			}
		})
	}
}

// Undo says when the step it is about to undo was done in another folder,
// and a step with nothing to restore is reported and dropped, not "complete"
// (findings r2-atk5-03, r2-atk5-08).
func TestUndoPlanSaysWhereAndWhetherAnythingCanComeBack(t *testing.T) {
	dir, elsewhere, home := t.TempDir(), t.TempDir(), t.TempDir()
	m := newFakeModel(t, runStep("touch made.txt"))
	execSynapse(t, synapseRun{args: []string{"run", "make a file"}, env: m.env(), dir: dir, home: home})
	r := execSynapse(t, synapseRun{args: []string{"undo"}, dir: elsewhere, home: home})
	if !strings.Contains(r.stdout, "different folder") || !strings.Contains(r.stdout, "ago") && !strings.Contains(r.stdout, "just now") {
		t.Errorf("the plan does not say where or when the step was done:\n%s", r.stdout)
	}
}

// A command that failed before doing anything leaves no undo entry, so undo
// does not report a phantom "move back" as complete (finding r3-atk9-03).
func TestAFailedMoveLeavesNoPhantomUndo(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	writeFile(t, filepath.Join(dir, "b.txt"), "b")
	writeFile(t, filepath.Join(dir, "dest"), "i am a file, not a folder")
	m := newFakeModel(t, runStep("mv a.txt b.txt dest"))
	execSynapse(t, synapseRun{args: []string{"run", "move them into dest"}, env: m.env(), dir: dir, home: home, stdin: "y\n"})
	for _, e := range journalEntries(t, home) {
		if e.Restorable() {
			t.Errorf("a failed mv left an undo entry: %+v", e)
		}
	}
	r := undoYes(t, home)
	if strings.Contains(r.stdout, "Undo complete") {
		t.Errorf("undo reported success for a command that did nothing:\n%s", r.stdout)
	}
}

// `sort f > f` empties f; the confirmation says so before, and the run says
// so after (finding r3-atk9-04).
func TestSelfTruncationIsWarnedBeforeAndAfter(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "data.txt"), "b\na\n")
	m := newFakeModel(t, runStep("sort data.txt > data.txt"))
	r := execSynapse(t, synapseRun{args: []string{"run", "--json", "sort data.txt in place"}, env: m.env(), dir: dir, home: home, stdin: "y\n"})
	if text := approvalFromJSON(t, r.stdout); !strings.Contains(text, "will end up empty") {
		t.Errorf("the approval does not warn that data.txt will be emptied: %q", text)
	}
	if !strings.Contains(r.stdout, "is now empty") {
		t.Errorf("the run does not say data.txt is now empty:\n%s", r.stdout)
	}
	undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "data.txt")); got != "b\na\n" {
		t.Errorf("undo left data.txt = %q", got)
	}
}

// A step that ran but could not be recorded for undo does not end the task
// as complete (volkswagen audit, finding 10).
func TestAStepThatCouldNotBeJournaledIsNotComplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	chdir(t, dir)
	locked := t.TempDir()
	os.Chmod(locked, 0o555)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	server := scriptedOllamaServer(t, []string{"touch made.txt", "DONE"})
	defer server.Close()
	var outcome string
	var out, errOut bytes.Buffer
	code := runLoop(context.Background(), ollama.New(server.URL), "m", "make a file", neverConfirm(t), &out, &errOut,
		filepath.Join(locked, "undo.log"), withoutAnswer(), withOutcome(func(o string) { outcome = o }))
	if !fileExists(filepath.Join(dir, "made.txt")) {
		t.Fatal("setup: the step did not run")
	}
	if code != exitNotUndoable || outcome != outcomeNotUndoable {
		t.Fatalf("exit %d outcome %q, want %d %q; out:\n%s", code, outcome, exitNotUndoable, outcomeNotUndoable, out.String())
	}
}

// An undo that cannot clear a path changes nothing and keeps its journal
// entry, so the same undo succeeds once the cause is fixed. The ship review
// of 2026-10-27 found the entry removed while nothing was undone: an
// overwrite's undo clears the current version first, and a read-only folder
// makes that clear fail.
func TestAnUndoThatCannotClearAPathChangesNothingAndCanBeRetried(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only folder this test relies on")
	}
	dir, home := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "original\n")
	m := newFakeModel(t, runStep("echo hello > a.txt"))
	if r := execSynapse(t, synapseRun{args: []string{"run", "write hello into a.txt"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
		t.Fatalf("setup: %d %s", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(dir, "a.txt"), "my edit\n")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	r := undoYes(t, home)
	if r.code == exitOK {
		t.Errorf("an undo that could not clear a.txt exited 0:\n%s%s", r.stdout, r.stderr)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "my edit\n" {
		t.Errorf("a.txt = %q after the failed undo, want the edit untouched", got)
	}

	os.Chmod(dir, 0o755)
	r = undoYes(t, home)
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "original\n" {
		t.Errorf("a.txt = %q after the retried undo, want the original: the entry was lost (exit %d)\n%s%s", got, r.code, r.stdout, r.stderr)
	}
	if !inTrash(t, home, "my edit\n") {
		t.Error("the edit made after the command is gone")
	}
}

// A file moved twice in one command comes back to where it started, and a
// file made and then moved is removed, not left at its first name. Undo used
// to judge each move by its own destination, which a later move had emptied,
// and reported success with the file at the middle name (ship review,
// 2026-10-27).
func TestUndoFollowsAChainOfMoves(t *testing.T) {
	cases := []struct {
		name, cmd string
		setup     map[string]string
		want      map[string]string // path -> content after undo; "" means absent
	}{
		{"moved twice", "mv a.txt b.txt && mv b.txt c.txt", map[string]string{"a.txt": "mine\n"},
			map[string]string{"a.txt": "mine\n", "b.txt": "", "c.txt": ""}},
		{"made, then moved", "touch x.txt && mv x.txt y.txt", nil,
			map[string]string{"x.txt": "", "y.txt": ""}},
		{"moved, then moved back", "mv a.txt b.txt && mv b.txt a.txt", map[string]string{"a.txt": "mine\n"},
			map[string]string{"a.txt": "mine\n", "b.txt": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, home := t.TempDir(), t.TempDir()
			for p, body := range c.setup {
				writeFile(t, filepath.Join(dir, p), body)
			}
			m := newFakeModel(t, runStep(c.cmd))
			if r := execSynapse(t, synapseRun{args: []string{"run", "do it"}, env: m.env(), dir: dir, home: home, stdin: "y\n"}); r.code != exitOK {
				t.Fatalf("setup: %d %s", r.code, r.stderr)
			}
			r := undoYes(t, home)
			for p, want := range c.want {
				full := filepath.Join(dir, p)
				switch {
				case want == "" && fileExists(full):
					t.Errorf("%s is still there after undo (exit %d)\n%s%s", p, r.code, r.stdout, r.stderr)
				case want != "" && readFile(t, full) != want:
					t.Errorf("%s = %q after undo, want %q (exit %d)\n%s%s", p, readFile(t, full), want, r.code, r.stdout, r.stderr)
				}
			}
		})
	}
}
