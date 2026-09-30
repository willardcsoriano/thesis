// Command recoverycheck proves, by sandboxed execute-and-diff, that the capture plan
// computed by internal/effects restores the prior state.
//
// For each case it builds a fresh fixture, analyses the command, applies the plan's
// captures with the real internal/undo package, runs the command inside a bubblewrap
// sandbox where only the fixture is writable, applies undo, and compares the tree
// with the one it snapshotted first. A command whose verdict is Unrecoverable is
// never executed. If bubblewrap does not work here, nothing is executed at all.
//
// Two undo variants are scored per case, because they answer different questions:
//
//	V1  what production does today: a Recoverable command is undone by the
//	    directory-diff mechanism only; a command with a capture plan is undone by
//	    its captures only (backupBeforeIrreversible's path).
//	V2  the plan's captures plus the directory-diff undo together, which is what a
//	    complete recovery of "the prior state" needs, since a capture never removes
//	    what the command created.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
	"synapseos/internal/oracle"
	"synapseos/internal/recoverycheck"
	"synapseos/internal/undo"
)

// Case is one command and the fixture state it runs against.
type Case struct {
	ID    string
	Setup string // bash, run in the fixture before anything else
	Cmd   string
	Home  bool // HOME points at the fixture
	From  string
	// Fx is a complete fixture written by the corpus generator. When set it replaces
	// the base fixture, and HOME is the fixture's home directory, as it was for the oracle.
	Fx []oracle.Entry
}

// homeFor is the HOME the command runs with: the fixture root for the older
// corpora, its home subdirectory for a generated one.
func homeFor(c Case, wd string) string {
	if c.Fx != nil {
		return filepath.Join(wd, "home")
	}
	return wd
}

type trial struct {
	ok         bool
	diffs      []string
	undoErrs   []string
	capBytes   int64
	capEntries int
	fixBytes   int64
	planSecs   float64
	cmdSecs    float64
	undoSecs   float64
	exit       int
	class      string
	mechs      string
	skipped    string
}

func baseFixture(wd string) error {
	files := map[string]string{
		"a.txt": "a\n", "b.txt": "b\n", "notes.txt": "notes\n", "app.conf": "k=v\n",
		"report.docx": "doc\n", "data.csv": "x,1\ny,2\n", "big.log": "log\n",
		"src/main.go": "package main\n", "logs/x.log": "x\n", "logs/y.log": "y\n",
		"build/out.o": "o\n", "backup/old.txt": "old\n",
	}
	for p, body := range files {
		full := filepath.Join(wd, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return os.MkdirAll(filepath.Join(wd, "tmp"), 0o755)
}

func mkFixture(setup string, fx []oracle.Entry) (string, error) {
	wd, err := os.MkdirTemp("", "rc-fix-")
	if err != nil {
		return "", err
	}
	if fx != nil {
		if err := oracle.Materialize(wd, fx); err != nil {
			return wd, err
		}
		if err := os.MkdirAll(filepath.Join(wd, "home"), 0o755); err != nil {
			return wd, err
		}
	} else if err := baseFixture(wd); err != nil {
		return wd, err
	}
	if setup != "" {
		c := exec.Command("bash", "-c", setup)
		c.Dir = wd
		if out, err := c.CombinedOutput(); err != nil {
			return wd, fmt.Errorf("setup failed: %v: %s", err, out)
		}
	}
	return wd, nil
}

func cleanup(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	_ = os.RemoveAll(dir)
}

func bwrapWorks() (string, bool) {
	p, err := exec.LookPath("bwrap")
	if err != nil {
		return "", false
	}
	return p, exec.Command(p, "--ro-bind", "/", "/", "--unshare-all", "--die-with-parent", "true").Run() == nil
}

func sandboxRun(bw, wd, cmd, home string) (int, time.Duration, error) {
	args := []string{"--ro-bind", "/", "/", "--bind", wd, wd, "--dev", "/dev", "--proc", "/proc",
		"--unshare-all", "--die-with-parent", "--chdir", wd}
	if home != "" {
		args = append(args, "--setenv", "HOME", home)
	}
	// The same privilege shim the oracle uses, so a sudo prefix runs as the fixture's owner.
	args = append(args, "--", "bash", "-c", oracle.SudoShim+cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bw, args...)
	start := time.Now()
	err := c.Run()
	d := time.Since(start)
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), d, nil
	}
	return 0, d, err
}

// sandboxSelfTest proves the sandbox blocks a write outside the fixture and permits one inside.
func sandboxSelfTest(bw string) error {
	wd, err := os.MkdirTemp("", "rc-self-")
	if err != nil {
		return err
	}
	defer cleanup(wd)
	outside := filepath.Join(os.TempDir(), fmt.Sprintf("rc-escape-%d", os.Getpid()))
	defer os.Remove(outside)
	if _, _, err := sandboxRun(bw, wd, "echo x > "+outside+"; echo y > inside.txt", ""); err != nil {
		return err
	}
	if _, err := os.Stat(outside); err == nil {
		return fmt.Errorf("sandbox let a command write outside the fixture: %s", outside)
	}
	if _, err := os.Stat(filepath.Join(wd, "inside.txt")); err != nil {
		return fmt.Errorf("sandbox did not let a command write inside the fixture")
	}
	return nil
}

func envWithHome(home string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HOME=") {
			out = append(out, kv)
		}
	}
	return append(out, "HOME="+home)
}

// capture applies the plan with the real undo package, as backupBeforeIrreversible does.
func capture(plan effects.Plan, wd string) (undo.Entry, int, []error) {
	e := undo.Entry{Timestamp: time.Now(), Dir: wd}
	var trash, content, meta []string
	var errs []error
	for _, c := range plan.Captures {
		switch c.Mechanism {
		case effects.Trash:
			trash = append(trash, c.Path)
		case effects.Content:
			content = append(content, c.Path)
		case effects.Metadata:
			meta = append(meta, c.Path)
		case effects.GitHead:
			sha, err := undo.CaptureGitHead(c.Path)
			if err != nil {
				errs = append(errs, err)
			} else {
				e.GitReset = sha
			}
		}
	}
	if len(trash) > 0 {
		var es []error
		e.Trashed, es = undo.TrashPreserve(trash)
		errs = append(errs, es...)
	}
	if len(content) > 0 {
		var es []error
		e.ContentBackups, es = undo.BackupContent(content)
		errs = append(errs, es...)
	}
	if len(meta) > 0 {
		var es []error
		mb, es2 := undo.BackupMetadata(meta)
		errs = append(errs, es2...)
		_ = es
		seen := map[string]bool{}
		for _, m := range mb {
			if !seen[m.Path] {
				seen[m.Path] = true
				e.MetadataBackups = append(e.MetadataBackups, m)
			}
		}
	}
	n := len(e.Trashed) + len(e.ContentBackups) + len(e.MetadataBackups)
	if e.GitReset != "" {
		n++
	}
	return e, n, errs
}

// allocated is the bytes newly allocated under dir: regular files nothing else links to.
func allocated(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
			return nil
		}
		n += fi.Size()
		return nil
	})
	return n
}

func runTrial(bw string, c Case, variant int, origHome string) *trial {
	t := &trial{}
	wd, err := mkFixture(c.Setup, c.Fx)
	defer cleanup(wd)
	if err != nil {
		t.skipped = "fixture: " + err.Error()
		return t
	}
	jh, _ := os.MkdirTemp("", "rc-home-")
	defer cleanup(jh)

	homeForAnalysis := origHome
	sandboxHome := ""
	if c.Home || c.Fx != nil {
		homeForAnalysis = homeFor(c, wd)
		sandboxHome = homeForAnalysis
	}
	analysisStart := time.Now()
	an := effects.New(wd)
	an.Env = envWithHome(homeForAnalysis)
	an.Run = effects.DefaultRunner(10 * time.Second)
	res := an.Analyze(context.Background(), c.Cmd)
	v := res.Verdict()
	plan := res.Plan()
	analysisSecs := time.Since(analysisStart).Seconds()
	t.class = v.Class.String()
	var ms []string
	seen := map[effects.Mechanism]bool{}
	for _, cp := range plan.Captures {
		if !seen[cp.Mechanism] {
			seen[cp.Mechanism] = true
			ms = append(ms, string(cp.Mechanism))
		}
	}
	sort.Strings(ms)
	t.mechs = strings.Join(ms, "+")
	if v.Class == effects.Unrecoverable {
		reason := ""
		if len(v.Reasons) > 0 {
			reason = ": " + v.Reasons[0]
		}
		t.skipped = "unrecoverable, not executed" + reason
		return t
	}
	if bw == "" {
		t.skipped = "no working sandbox, not executed"
		return t
	}

	want, err := recoverycheck.Snapshot(wd)
	if err != nil {
		t.skipped = "snapshot: " + err.Error()
		return t
	}
	t.fixBytes = recoverycheck.Bytes(want)

	os.Setenv("HOME", jh)
	defer os.Setenv("HOME", origHome)
	captureStart := time.Now()
	var entry undo.Entry
	var n int
	var errs []error
	if variant == 3 {
		gd := gate.Decision{Analysis: res, Verdict: v, Plan: plan, Confident: len(res.Issues) == 0}
		entry, errs = gd.Capture(wd, c.Cmd)
		n = len(entry.ContentBackups) + len(entry.Trashed) + len(entry.MetadataBackups)
	} else {
		entry, n, errs = capture(plan, wd)
	}
	t.planSecs = analysisSecs + time.Since(captureStart).Seconds()
	t.capEntries = n
	t.capBytes = allocated(jh)
	for _, e := range errs {
		t.undoErrs = append(t.undoErrs, "capture: "+e.Error())
	}
	before, _ := undo.Snapshot(wd)
	beforeIDs, _ := undo.SnapshotIDs(wd)

	exit, d, err := sandboxRun(bw, wd, c.Cmd, sandboxHome)
	t.exit, t.cmdSecs = exit, d.Seconds()
	if err != nil {
		t.skipped = "sandbox run failed: " + err.Error()
		return t
	}

	u0 := time.Now()
	// V3 undoes from the gate's entry alone: what the analysis says was created,
	// moved, overwritten, removed, or re-moded, with no directory diff.
	useDiff := variant != 3 && (variant == 2 || v.Class == effects.Recoverable)
	useCaptures := variant == 3 || variant == 2 || v.Class != effects.Recoverable
	if !useCaptures {
		entry = undo.Entry{Timestamp: time.Now(), Dir: wd}
	}
	if useDiff {
		after, _ := undo.Snapshot(wd)
		afterIDs, _ := undo.SnapshotIDs(wd)
		de := undo.BuildEntryIDs(wd, c.Cmd, before, after, beforeIDs, afterIDs)
		entry.Moves, entry.Created, entry.Unhandled = de.Moves, de.Created, de.Unhandled
	}
	for _, e := range undo.Apply(entry) {
		t.undoErrs = append(t.undoErrs, "undo: "+e.Error())
	}
	t.undoSecs = time.Since(u0).Seconds()

	got, err := recoverycheck.Snapshot(wd)
	if err != nil {
		t.skipped = "snapshot after undo: " + err.Error()
		return t
	}
	t.diffs = recoverycheck.Diff(want, got)
	t.ok = len(t.diffs) == 0
	return t
}

// ---- cases ----

const gitSetup = "git init -q && git config user.email t@t && git config user.name t && echo v1 > tracked.txt && " +
	"git add tracked.txt && git commit -qm one && echo v2 >> tracked.txt && git commit -qam two && echo dirty >> tracked.txt"

var curated = []Case{
	{ID: "S01-rm-tree", Cmd: "rm -r src build"},
	{ID: "S02-rm-glob", Cmd: "rm *.txt"},
	{ID: "S03-mv-onto-existing", Cmd: "mv a.txt b.txt"},
	{ID: "S04-mv-dir", Cmd: "mv src srcmoved"},
	{ID: "S05-mv-dir-into-dir", Cmd: "mv logs build"},
	{ID: "S06-cp-r-over-tree", Setup: "mkdir -p dst/src && echo old > dst/src/main.go && echo keep > dst/keep.txt", Cmd: "cp -r src dst"},
	{ID: "S07-sed-i-multi", Cmd: "sed -i 's/a/b/' a.txt b.txt notes.txt"},
	{ID: "S08-chmod-R", Cmd: "chmod -R 000 src"},
	{ID: "S09-chmod-single", Cmd: "chmod 600 a.txt"},
	{ID: "S10-truncate", Cmd: "truncate -s 0 big.log"},
	{ID: "S11-redirect-truncate", Cmd: "echo new > notes.txt"},
	{ID: "S12-redirect-append", Cmd: "echo more >> notes.txt"},
	{ID: "S13-tee", Cmd: "echo t | tee notes.txt"},
	{ID: "S14-sort-o", Cmd: "sort -o data.csv data.csv"},
	{ID: "S15-gzip", Cmd: "gzip big.log"},
	{ID: "S16-gunzip", Setup: "gzip big.log", Cmd: "gunzip big.log.gz"},
	{ID: "S17-tar-x-over-existing", Setup: "tar -czf arch.tgz a.txt src && echo modified > a.txt && echo m > src/main.go", Cmd: "tar -xzf arch.tgz"},
	{ID: "S18-tar-create", Cmd: "tar -czf out.tgz src"},
	{ID: "S19-find-delete", Cmd: "find logs -name '*.log' -delete"},
	{ID: "S20-find-exec-rm", Setup: "mkdir -p sub && touch one.tmp sub/two.tmp", Cmd: "find . -name '*.tmp' -exec rm {} \\;"},
	{ID: "S21-find-exec-chmod", Setup: "mkdir -p sub && touch one.tmp sub/two.tmp", Cmd: "find . -name '*.tmp' -exec chmod 600 {} +"},
	{ID: "S22-xargs-rm", Cmd: "ls *.log | xargs rm"},
	{ID: "S23-for-loop-rm", Cmd: "for f in logs/*; do rm \"$f\"; done"},
	{ID: "S24-git-reset-hard", Setup: gitSetup, Cmd: "git reset --hard HEAD~1"},
	{ID: "S25-git-clean-fd", Setup: "git init -q && git config user.email t@t && git config user.name t && echo v > t.txt && git add t.txt && git commit -qm x && echo u > untracked.txt && mkdir -p ud && echo u > ud/f.txt", Cmd: "git clean -fd"},
	{ID: "S26-create-then-delete", Cmd: "echo tmp > scratch.txt && rm scratch.txt"},
	{ID: "S27-rename-collision", Setup: "echo c > x_a.txt", Cmd: "for f in *.txt; do mv \"$f\" \"x_$f\"; done"},
	{ID: "S28-spaces", Setup: "echo s > 'my file.txt'", Cmd: "rm 'my file.txt'"},
	{ID: "S29-newline-name", Setup: "echo s > \"$(printf 'nl\\nfile')\"", Cmd: "rm nl*"},
	{ID: "S30-rm-symlink", Setup: "ln -s a.txt lnk", Cmd: "rm lnk"},
	{ID: "S31-rm-dangling-symlink", Setup: "ln -s nothere dang", Cmd: "rm dang"},
	{ID: "S32-hidden-file", Setup: "echo h > .hidden", Cmd: "rm .hidden"},
	{ID: "S33-empty-dir", Setup: "mkdir emptyd", Cmd: "rmdir emptyd"},
	{ID: "S34-large-remove", Setup: "head -c 10485760 /dev/zero > huge.bin", Cmd: "rm huge.bin"},
	{ID: "S36-rename-new-name", Cmd: "mv notes.txt renamed.txt"},
	{ID: "S37-mv-into-subdir", Cmd: "mv a.txt logs/"},
	{ID: "S35-large-overwrite", Setup: "head -c 10485760 /dev/zero > huge.bin", Cmd: "echo x > huge.bin"},
}

type corpusRow struct {
	ID        string         `json:"id"`
	Partition string         `json:"partition"`
	Label     string         `json:"label"`
	Command   string         `json:"command"`
	CmdFx     string         `json:"cmd_fx"`
	Setup     string         `json:"setup"`
	Home      bool           `json:"home"`
	Fx        []oracle.Entry `json:"fx"`
}

func loadCorpus(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r corpusRow
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		generated := r.Fx != nil
		if (!generated && r.Partition != "B") || (r.Label != "R" && r.Label != "C") {
			continue
		}
		cmd := r.Command
		if r.CmdFx != "" {
			cmd = r.CmdFx
		}
		out = append(out, Case{ID: r.ID, Setup: r.Setup, Cmd: cmd, Home: r.Home, From: "corpus2", Fx: r.Fx})
	}
	return out, sc.Err()
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func main() {
	corpus := "pilot/corpus2.jsonl"
	if len(os.Args) > 1 {
		corpus = os.Args[1]
	}
	origHome := os.Getenv("HOME")
	bw, ok := bwrapWorks()
	if !ok {
		bw = ""
		fmt.Println("NOTE: bubblewrap is not usable here; no command will be executed.")
	}
	if bw != "" {
		if err := sandboxSelfTest(bw); err != nil {
			fmt.Println("NOTE: sandbox self-test failed, nothing will be executed:", err)
			bw = ""
		} else {
			fmt.Println("sandbox self-test: a write outside the fixture is blocked, a write inside is allowed")
		}
	}
	cases, err := loadCorpus(corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
	for i := range curated {
		curated[i].From = "curated"
	}
	all := append(cases, curated...)
	if os.Getenv("RC_NO_CURATED") != "" {
		all = cases // score only the corpus, not the hand-written cases
	}

	type row struct {
		c          Case
		t1, t2, t3 *trial
	}
	var rows []row
	for _, c := range all {
		t1 := runTrial(bw, c, 1, origHome)
		var t2, t3 *trial
		if t1.skipped == "" {
			t2 = runTrial(bw, c, 2, origHome)
			t3 = runTrial(bw, c, 3, origHome)
		} else {
			t2, t3 = t1, t1
		}
		rows = append(rows, row{c, t1, t2, t3})
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "case\tclass\tplan\tV1\tV2\tV3\tcaptured B\tentries\tfixture B\tplan+capture s\tcommand s\texit")
	yn := func(t *trial) string {
		if t.skipped != "" {
			return "skip"
		}
		if t.ok {
			return "yes"
		}
		return "NO"
	}
	for _, r := range rows {
		t := r.t1
		if t.skipped != "" {
			fmt.Fprintf(tw, "%s\t%s\t%s\tskip\tskip\tskip\t-\t-\t-\t-\t-\t%s\n", r.c.ID, t.class, t.mechs, t.skipped)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%.3f\t%.3f\t%d\n", r.c.ID, t.class, t.mechs, yn(r.t1), yn(r.t2), yn(r.t3),
			t.capBytes, t.capEntries, t.fixBytes, t.planSecs, t.cmdSecs, t.exit)
	}
	tw.Flush()

	var exec1, ok1, ok2, ok3 int
	var ratios []float64
	var maxRatio float64
	var maxID string
	skipped := 0
	for _, r := range rows {
		if r.t1.skipped != "" {
			skipped++
			continue
		}
		exec1++
		if r.t1.ok {
			ok1++
		}
		if r.t2.ok {
			ok2++
		}
		if r.t3.ok {
			ok3++
		}
		if r.t1.class == effects.RecoverableWithCapture.String() && r.t1.fixBytes > 0 {
			ratio := float64(r.t1.capBytes) / float64(r.t1.fixBytes)
			ratios = append(ratios, ratio)
			if ratio > maxRatio {
				maxRatio, maxID = ratio, r.c.ID
			}
		}
	}
	fmt.Println()
	fmt.Println("SUMMARY")
	fmt.Printf("  cases: %d total, %d executed, %d skipped (unrecoverable verdict, or no sandbox)\n", len(rows), exec1, skipped)
	if exec1 > 0 {
		fmt.Printf("  recovered exactly, V1 (production-equivalent undo): %d/%d = %.0f%%\n", ok1, exec1, 100*float64(ok1)/float64(exec1))
		fmt.Printf("  recovered exactly, V2 (captures + directory-diff):  %d/%d = %.0f%%\n", ok2, exec1, 100*float64(ok2)/float64(exec1))
		fmt.Printf("  recovered exactly, V3 (internal/gate entry alone):  %d/%d = %.0f%%\n", ok3, exec1, 100*float64(ok3)/float64(exec1))
	}
	if len(ratios) > 0 {
		fmt.Printf("  storage overhead vs a full copy of the fixture, over %d capture cases: median %.4f%%, max %.4f%% (%s)\n",
			len(ratios), 100*median(ratios), 100*maxRatio, maxID)
	}
	fmt.Println("  modification times are not compared; inside .git only HEAD and refs/ are.")

	fmt.Println()
	fmt.Println("FAILURES (V1, V2 or V3 not exact), with the reproduction")
	nfail := 0
	for _, r := range rows {
		if r.t1.skipped != "" || (r.t1.ok && r.t2.ok && r.t3.ok) {
			continue
		}
		nfail++
		fmt.Printf("\n%s  class=%s plan=%s\n  setup:   %s\n  command: %s\n", r.c.ID, r.t1.class, r.t1.mechs, r.c.Setup, r.c.Cmd)
		for name, t := range map[string]*trial{"V1": r.t1, "V2": r.t2, "V3": r.t3} {
			if t.ok {
				fmt.Printf("  %s: exact\n", name)
				continue
			}
			fmt.Printf("  %s differences: %s\n", name, strings.Join(t.diffs, "; "))
			if len(t.undoErrs) > 0 {
				fmt.Printf("  %s errors: %s\n", name, strings.Join(t.undoErrs, "; "))
			}
		}
	}
	if nfail == 0 {
		fmt.Println("  none")
	}

	fmt.Println()
	fmt.Println("SKIPPED")
	for _, r := range rows {
		if r.t1.skipped != "" {
			fmt.Printf("  %s: %s\n", r.c.ID, r.t1.skipped)
		}
	}
}
