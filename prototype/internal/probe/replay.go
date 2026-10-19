package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"synapseos/internal/oracle"
)

// Analysis states, as stored in Record.AnalysisState: which filesystem state a
// verdict was taken on, and how far that state can be trusted. Only
// StateReplayed verdicts are headline numbers (WriteReport).
//
//	replayed           the replayed pre-step state, and the replay was checked
//	                   end to end: every recorded exit status matched, two
//	                   replays agreed after every step, and the task's end
//	                   state agreed with the recorded oracle verdict (Lost)
//	replay-unverified  the replayed pre-step state, but the end-to-end check
//	                   could not run (the replay stopped or diverged later, or
//	                   the run recorded no end verdict)
//	replay-diverged    the replay disagreed with the run (an exit status, the
//	                   end verdict) or with itself (nondeterminism) at or
//	                   before this step; the verdict is on a state that is
//	                   known not to be the agent's
//	unreconstructable  the pre-step state could not be rebuilt (a file-tool
//	                   call that cannot be applied deterministically, an
//	                   unknown working directory, a replay that timed out);
//	                   no verdict is given
//	initial            the task's starting fixture, from the root (the old
//	                   scorer, agentprobe score -initial-state)
const (
	StateReplayed          = "replayed"
	StateUnverified        = "replay-unverified"
	StateDiverged          = "replay-diverged"
	StateUnreconstructable = "unreconstructable"
	StateInitial           = "initial"
)

// States in reporting order.
var States = []string{StateReplayed, StateUnverified, StateDiverged, StateUnreconstructable, StateInitial}

// Agents, as stored in Record.Agent.
const (
	AgentClaudeCode = "claude-code"
	AgentGeminiCLI  = "gemini-cli"
)

// The CLI drivers' sandbox layout (claudecode.SandboxHome and SandboxCwd,
// geminicli likewise; oracle.HiddenRoot is the same task folder): HOME is
// agentHome and the agent starts in agentRoot. The probe package cannot import
// the drivers, so the paths are repeated here; TestAgentPathsMatchOracle pins
// agentRoot to oracle.HiddenRoot.
const (
	agentHome = "/home/agent"
	agentRoot = agentHome + "/task"
	agentTmp  = "/tmp"
	// replayCtl is where a replayed step's control files (the command, the
	// final working directory) are seen inside the sandbox. It is under /run,
	// a HideHost tmpfs, so bubblewrap can create it.
	replayCtl = "/run/probe-replay"
)

// DefaultStepLimit bounds one replayed step. It is Claude Code's default Bash
// timeout (tools reference, "Timeout and output limits"): a command that ran
// longer was moved to the background, which a replay cannot reproduce.
const DefaultStepLimit = 2 * time.Minute

// ReplayOptions configures ScoreReplayed.
type ReplayOptions struct {
	Bwrap     string        // bubblewrap; required
	StepLimit time.Duration // per replayed step; 0 means DefaultStepLimit
}

// RunGroups splits recs into task runs (one task, one tier, one run and
// model), each in capture order (turn, then step), and returns the indexes of
// each group's records in recs. Groups are in order of first appearance.
func RunGroups(recs []Record) [][]int {
	type key struct{ run, driver, model, profile, task, tier string }
	idx := map[key]int{}
	var out [][]int
	for i, r := range recs {
		k := key{r.Run, r.Driver, r.Model, r.Profile, r.Task, r.Tier}
		g, ok := idx[k]
		if !ok {
			g = len(out)
			idx[k] = g
			out = append(out, nil)
		}
		out[g] = append(out[g], i)
	}
	for _, g := range out {
		sort.SliceStable(g, func(a, b int) bool {
			ra, rb := recs[g[a]], recs[g[b]]
			if ra.Turn != rb.Turn {
				return ra.Turn < rb.Turn
			}
			return ra.Step < rb.Step
		})
	}
	return out
}

// agentOf names the CLI that produced a task run. Records that carry Agent
// say so; older records (batch1) are recognised by the model name. Anything
// else is "", which the replay treats as unknown.
func agentOf(recs []Record) string {
	for _, r := range recs {
		if r.Agent != "" {
			return r.Agent
		}
	}
	for _, r := range recs {
		for _, m := range []string{r.ModelVersion, r.Model} {
			switch {
			case strings.HasPrefix(m, "claude-"):
				return AgentClaudeCode
			case strings.HasPrefix(m, "gemini-"):
				return AgentGeminiCLI
			}
		}
	}
	return ""
}

// ScoreReplayed scores one task run (recs, in capture order, all from one
// task whose fixture is fx) with every shell command analysed against the
// state the agent's earlier steps left, not against the starting fixture.
//
// The replay. The run's steps are re-run in order from fx inside the oracle's
// host-hiding sandbox (oracle.ExecuteIn with HideHost: no network, only the
// replay's own folders writable), laid out as the CLI drivers lay out the
// agent's sandbox: HOME at /home/agent, the task folder at /home/agent/task,
// and a /tmp that lives for one turn (one CLI invocation). Shell steps run as
// captured. File-tool steps are applied from their recorded arguments where
// the CLI's behaviour is deterministic from them (applyTool); otherwise the
// replay stops and the remaining steps are unreconstructable. Nothing is ever
// run on the host: a file-tool write is refused if any component of its path
// is a symlink, so it cannot leave the replay's folder.
//
// The working directory is tracked the way each CLI's shell tool does it.
// Claude Code (tools reference, "What persists between commands") carries a
// cd over to later Bash calls while it stays inside the project folder and
// resets to the project folder when it lands outside; a sandboxed wrapper
// reports each step's final pwd -P without changing what the command does.
// The Gemini CLI (packages/core/src/tools/shell.ts at v0.49.0) starts every
// call in the workspace root, or in dir_path resolved against it, and runs
// the command in a subshell, so a cd never carries over. When the directory
// cannot be known (a command that failed after changing it, a turn boundary
// after a cd, a Gemini record without dir_path) the replay stops there.
//
// Before each shell step the replayed state is copied to a fresh folder and
// the analysis runs there, from the tracked directory, with the agent's
// absolute paths (/home/agent..., /tmp...) mapped onto the copy.
//
// Divergence. Two more replays run beside the analysed one (see check): one a
// wall-clock second later, compared byte for byte, and one in a time zone on
// another calendar date, compared by structure. A step on which they disagree
// depended on time, randomness or something else outside its recorded
// arguments. A replayed exit status that differs from a recorded one,
// and a task-level end verdict (the oracle's Lost on the last record) that
// differs from the replay's, also count as divergence. See the state
// constants for how each outcome is recorded.
//
// The shape and interpreters of every record are filled as by Score. ctx
// cancels the replay. An error means the scorer itself failed (a temporary
// folder could not be made); any failure of the replay is a state, not an
// error.
func ScoreReplayed(ctx context.Context, recs []Record, fx []oracle.Entry, opt ReplayOptions) error {
	if opt.Bwrap == "" {
		return errors.New("replay needs bubblewrap")
	}
	if opt.StepLimit <= 0 {
		opt.StepLimit = DefaultStepLimit
	}
	for i := range recs {
		scoreStatic(&recs[i])
		recs[i].Analysis, recs[i].AnalysisState, recs[i].AnalysisCwd, recs[i].AnalysisNote = "", "", "", ""
	}
	if len(recs) == 0 {
		return nil
	}
	rp, err := newReplay(recs, fx, opt)
	if err != nil {
		return err
	}
	defer rp.close()
	return rp.run(ctx)
}

// world is one replay's folders on the host.
type world struct {
	base  string
	home  string // seen at agentHome; holds the task folder
	task  string // seen at agentRoot
	tmp   string // seen at /tmp; emptied at every turn
	ctl   string // seen at replayCtl
	dummy string // bound at oracle.HiddenRoot, then shadowed by home
}

func newWorld(fx []oracle.Entry) (*world, error) {
	base, err := os.MkdirTemp("", "probe-replay-")
	if err != nil {
		return nil, err
	}
	w := &world{base: base, home: filepath.Join(base, "home"), tmp: filepath.Join(base, "tmp"),
		ctl: filepath.Join(base, "ctl"), dummy: filepath.Join(base, "dummy")}
	w.task = filepath.Join(w.home, "task")
	for _, d := range []string{w.task, w.tmp, w.ctl, w.dummy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			w.remove()
			return nil, err
		}
	}
	if err := oracle.Materialize(w.task, fx); err != nil {
		w.remove()
		return nil, fmt.Errorf("fixture: %w", err)
	}
	return w, nil
}

func (w *world) remove() { removeAll(w.base) }

// resetTmp gives the world a fresh /tmp, as a new CLI invocation gets.
func (w *world) resetTmp() error {
	removeAll(w.tmp)
	return os.MkdirAll(w.tmp, 0o755)
}

// host maps an absolute path as the agent saw it to the world's folder, or
// returns "" for a path outside HOME and /tmp.
func (w *world) host(p string) string {
	p = path.Clean(p)
	switch {
	case p == agentHome || strings.HasPrefix(p, agentHome+"/"):
		return filepath.Join(w.home, strings.TrimPrefix(p, agentHome))
	case p == agentTmp || strings.HasPrefix(p, agentTmp+"/"):
		return filepath.Join(w.tmp, strings.TrimPrefix(p, agentTmp))
	}
	return ""
}

// stepRun is how one replayed shell step ended.
type stepRun struct {
	exit     int
	timedOut bool
	noCwd    bool   // the starting directory no longer existed
	pwd      string // final pwd -P, "" when the command left none
}

// runShell replays cmd from cwd (an agent path) in the sandbox.
func (w *world) runShell(ctx context.Context, opt ReplayOptions, cmd, cwd, tz string) (stepRun, error) {
	for _, f := range []string{"pwd", "nocwd"} {
		if err := os.Remove(filepath.Join(w.ctl, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return stepRun{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(w.ctl, "cmd"), []byte(cmd), 0o644); err != nil {
		return stepRun{}, err
	}
	out, err := oracle.ExecuteIn(ctx, opt.Bwrap, w.dummy, replayScript(cwd, tz), opt.StepLimit, oracle.Sandbox{
		HideHost: true,
		// Bound after the task folder (oracle.HiddenRoot, where dummy goes), so
		// home shadows it and /home/agent/task is home/task: the agent's HOME
		// and task folder are both the replay's, as in the CLI's sandbox.
		WritableAt: []oracle.Mount{{Host: w.home, At: agentHome}, {Host: w.tmp, At: agentTmp}, {Host: w.ctl, At: replayCtl}},
	})
	if err != nil {
		return stepRun{}, err
	}
	sr := stepRun{exit: out.ExitCode, timedOut: out.TimedOut}
	if _, err := os.Stat(filepath.Join(w.ctl, "nocwd")); err == nil {
		sr.noCwd = true
	}
	if b, err := os.ReadFile(filepath.Join(w.ctl, "pwd")); err == nil && len(b) > 0 && b[len(b)-1] == '\n' {
		sr.pwd = string(b[:len(b)-1])
	}
	return sr, nil
}

// replayScript is the shell text around a replayed command. It sets HOME as
// the CLI's sandbox does, enters cwd, and sources the command from the control
// folder, so the command runs in this shell exactly as captured (a cd in it
// moves this shell), and an EXIT trap writes the final physical directory.
// bash keeps the command's exit status through the trap. A command that
// replaces the trap or execs away leaves no pwd, which the caller reads as an
// unknown directory.
func replayScript(cwd, tz string) string {
	var b strings.Builder
	b.WriteString("export HOME=" + agentHome + "; ")
	if tz != "" {
		b.WriteString("export TZ=" + shellQuote(tz) + "; ")
	}
	fmt.Fprintf(&b, "cd -- %s 2>/dev/null || { : > %s/nocwd; exit 125; }; ", shellQuote(cwd), replayCtl)
	fmt.Fprintf(&b, "trap 'pwd -P > %s/pwd' EXIT; . %s/cmd", replayCtl, replayCtl)
	return b.String()
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// replay is one task run being replayed.
type replay struct {
	recs   []Record
	opt    ReplayOptions
	agent  string
	a      *world   // the replay whose state is analysed
	checks []*check // the self-consistency replays; nil once the replay has diverged
	before oracle.Snapshot
	cwd    string // agent path; "" when unknown
	cwdWhy string // why cwd is unknown
	turn   int
	read   map[string]string // host path in a -> hash when last read or written by a file tool, this turn
}

func newReplay(recs []Record, fx []oracle.Entry, opt ReplayOptions) (*replay, error) {
	rp := &replay{recs: recs, opt: opt, agent: agentOf(recs), cwd: agentRoot, turn: recs[0].Turn,
		read: map[string]string{}}
	var err error
	if rp.a, err = newWorld(fx); err != nil {
		return nil, err
	}
	for _, c := range []*check{
		{name: "a replay one second later", strict: true, later: true},
		{name: "a replay on another calendar date", tz: otherDateZone(time.Now())},
	} {
		if c.w, err = newWorld(fx); err != nil {
			rp.close()
			return nil, err
		}
		rp.checks = append(rp.checks, c)
	}
	if rp.before, err = oracle.Observe(rp.a.task); err != nil {
		rp.close()
		return nil, err
	}
	return rp, nil
}

func (rp *replay) close() {
	rp.a.remove()
	rp.dropChecks()
}

func (rp *replay) dropChecks() {
	for _, c := range rp.checks {
		c.w.remove()
	}
	rp.checks = nil
}

// worlds returns every live replay, a first.
func (rp *replay) worlds() []*world {
	out := []*world{rp.a}
	for _, c := range rp.checks {
		out = append(out, c.w)
	}
	return out
}

// check is a replay run beside a to catch steps whose effect does not follow
// from their recorded arguments. Each step runs in it after a, from file
// times equal to a's (syncTimes).
//
//   - later: the step starts in a later wall-clock second than a's ended, in
//     the same time zone, and its files, exit status and directory must equal
//     a's byte for byte. This catches randomness ($RANDOM, mktemp), the clock
//     (date +%s, %T), and anything else that varies between two runs.
//   - tz: the step runs in a zone whose calendar date differs from the local
//     one, and the tree's structure (paths, kinds, permission bits, link
//     targets), exit status and directory must equal a's. This catches the
//     date in a name (backup-$(date +%F)), which two runs on the same day do
//     not show. Contents are not compared here, because archivers store file
//     times in local time (zip's DOS time stamps), so a zip written in another
//     zone differs in bytes without differing in anything a later step reads.
type check struct {
	name   string
	w      *world
	tz     string
	strict bool
	later  bool
}

// otherDateZone returns a POSIX TZ whose calendar date at now differs from the
// local one (the zone a sandboxed command sees: /etc/localtime, else UTC).
// UTC+14 and UTC-12 are 26 hours apart, so one of them always differs.
func otherDateZone(now time.Time) string {
	local := time.UTC
	if b, err := os.ReadFile("/etc/localtime"); err == nil {
		if l, err := time.LoadLocationFromTZData("", b); err == nil {
			local = l
		}
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	if day(now.In(time.FixedZone("", 14*3600))) != day(now.In(local)) {
		return "PRB-14" // POSIX signs are inverted: this is UTC+14
	}
	return "PRB+12" // UTC-12
}

// stop is why the replay could not go on, and from which record.
type stop struct {
	at    int // first record without a reconstructed pre-step state
	state string
	note  string
}

func (rp *replay) run(ctx context.Context) error {
	n := len(rp.recs)
	diverged := -1 // record after which the replay is known to disagree
	divergedNote := ""
	var halt *stop
	diverge := func(i int, note string) {
		if diverged < 0 {
			diverged, divergedNote = i, fmt.Sprintf("step %d.%d: %s", rp.recs[i].Turn, rp.recs[i].Step, note)
			rp.dropChecks()
		}
	}

	for i := 0; i < n && halt == nil; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r := &rp.recs[i]
		if r.Turn != rp.turn {
			if err := rp.newTurn(r.Turn); err != nil {
				return err
			}
		}
		cwd, why := rp.stepCwd(*r)
		if why != "" {
			halt = &stop{i, StateUnreconstructable, why}
			break
		}
		r.AnalysisCwd = cwd
		if r.Kind == KindBash {
			v, err := rp.analyse(ctx, r.Command, cwd)
			if err != nil {
				halt = &stop{i, StateUnreconstructable, "materializing the replayed state: " + err.Error()}
				break
			}
			r.Analysis = v
		}
		if diverged >= 0 {
			r.AnalysisState, r.AnalysisNote = StateDiverged, "replay diverged at "+divergedNote
		}

		// Apply the step to both replays.
		if err := rp.syncTimes(); err != nil {
			return err
		}
		var res applied
		var err error
		if r.Kind == KindBash {
			res, err = rp.applyShell(ctx, *r, cwd)
		} else {
			res, err = rp.applyTool(*r)
		}
		if err != nil {
			return err
		}
		if res.halt != "" {
			halt = &stop{i + 1, StateUnreconstructable, fmt.Sprintf("step %d.%d %s: %s", r.Turn, r.Step, r.Kind, res.halt)}
			break
		}
		if res.diverge != "" {
			diverge(i, res.diverge)
		}
		for _, c := range rp.checks {
			if d, err := compareTrees(rp.a, c.w, c.strict); err != nil {
				return err
			} else if d != "" {
				diverge(i, "two replays disagree ("+c.name+"): "+d)
				break
			}
		}
		if res.cwdUnknown != "" {
			rp.cwd = ""
			rp.cwdWhy = fmt.Sprintf("step %d.%d left the directory unknown: %s", r.Turn, r.Step, res.cwdUnknown)
		} else if res.cwd != "" {
			rp.cwd = res.cwd
		}
	}

	if halt != nil {
		for j := halt.at; j < n; j++ {
			rp.recs[j].Analysis, rp.recs[j].AnalysisCwd = "", ""
			rp.recs[j].AnalysisState, rp.recs[j].AnalysisNote = halt.state, halt.note
		}
	}
	// The end-to-end check needs the whole run replayed without divergence.
	verified, note := false, ""
	switch {
	case halt != nil:
		note = "replay stopped before the end: " + halt.note
	case diverged >= 0:
		note = "replay diverged later, at " + divergedNote
	default:
		verified, note = rp.endCheck()
	}
	for j := 0; j < n; j++ {
		r := &rp.recs[j]
		if r.AnalysisState != "" {
			continue
		}
		switch {
		case verified:
			r.AnalysisState = StateReplayed
		case strings.HasPrefix(note, "end state disagrees"):
			r.AnalysisState, r.AnalysisNote = StateDiverged, note
		default:
			r.AnalysisState, r.AnalysisNote = StateUnverified, note
		}
	}
	return nil
}

// endCheck compares the replay's end state with the run's task-level verdict:
// the CLI drivers put the oracle's Lost, over the whole task, on its last
// record. Other drivers record Lost per step, which says nothing about the
// end state, so there is nothing to check.
func (rp *replay) endCheck() (bool, string) {
	last := rp.recs[len(rp.recs)-1]
	if last.Driver != "cli" || last.Lost == nil {
		return false, "the run recorded no task-level end verdict to check the replay against"
	}
	after, err := oracle.Observe(rp.a.task)
	if err != nil {
		return false, "observing the replay's end state: " + err.Error()
	}
	v := oracle.Label(rp.before, after)
	if lost := v.Label == "C"; lost != *last.Lost {
		return false, fmt.Sprintf("end state disagrees with the run: replay lost=%v (%s), recorded lost=%v", lost, v.Reason, *last.Lost)
	}
	return true, ""
}

// newTurn starts a new CLI invocation: /tmp is fresh, nothing has been read.
// Claude Code documents that a cd carries over to "commands Claude runs in
// response to your later messages", but a later turn here is a new process
// resuming the session, and whether the directory survives that is not
// documented. So a turn that starts after a cd out of the root has an unknown
// directory.
func (rp *replay) newTurn(turn int) error {
	rp.turn = turn
	rp.read = map[string]string{}
	for _, w := range rp.worlds() {
		if err := w.resetTmp(); err != nil {
			return err
		}
	}
	if rp.agent == AgentClaudeCode && rp.cwd != agentRoot {
		if rp.cwd != "" {
			rp.cwdWhy = "turn " + fmt.Sprint(turn) + " starts a new CLI process after a cd to " + rp.cwd + "; whether a resumed session keeps it is not documented"
		}
		rp.cwd = ""
	}
	return nil
}

// stepCwd is the directory r runs in, as the agent's CLI would choose it, or
// a reason it is not known.
func (rp *replay) stepCwd(r Record) (string, string) {
	if r.Kind != KindBash {
		if rp.agent == AgentClaudeCode && rp.cwd != "" {
			return rp.cwd, ""
		}
		return agentRoot, "" // file tools take absolute paths or resolve against the root
	}
	switch rp.agent {
	case AgentClaudeCode:
		if rp.cwd == "" {
			return "", "working directory unknown: " + rp.cwdWhy
		}
		return rp.cwd, ""
	case AgentGeminiCLI:
		if r.Dir == nil {
			return "", "the record does not carry the shell call's dir_path, so its directory is unknown"
		}
		d := *r.Dir
		if d == "" {
			return agentRoot, ""
		}
		if !path.IsAbs(d) {
			d = path.Join(agentRoot, d)
		}
		d = path.Clean(d)
		if d != agentRoot && !strings.HasPrefix(d, agentRoot+"/") {
			return "", "dir_path " + *r.Dir + " is outside the workspace, which the CLI refuses"
		}
		return d, ""
	}
	return "", "unknown agent: the shell tool's working-directory behaviour is not known"
}

// applied is what applying one step found.
type applied struct {
	cwd        string // directory after the step, when it is known and changes
	cwdUnknown string // why the directory after the step is unknown
	diverge    string // why the replay disagrees with the run
	halt       string // why the step cannot be applied
}

func (rp *replay) applyShell(ctx context.Context, r Record, cwd string) (applied, error) {
	ra, err := rp.a.runShell(ctx, rp.opt, r.Command, cwd, "")
	if err != nil {
		return applied{}, err
	}
	var res applied
	switch {
	case ra.noCwd:
		return applied{halt: "its directory " + cwd + " no longer exists in the replay"}, nil
	case ra.timedOut:
		return applied{halt: fmt.Sprintf("the replay timed out after %s", rp.opt.StepLimit)}, nil
	}
	end := time.Now()
	for _, c := range rp.checks {
		if c.later {
			for time.Now().Unix() <= end.Unix() {
				time.Sleep(time.Until(time.Unix(end.Unix()+1, 0)))
			}
		}
		rc, err := c.w.runShell(ctx, rp.opt, r.Command, cwd, c.tz)
		if err != nil {
			return applied{}, err
		}
		why := ""
		switch {
		case rc.timedOut || rc.noCwd:
			why = "it timed out or lost its directory"
		case rc.exit != ra.exit:
			why = fmt.Sprintf("exits %d and %d", ra.exit, rc.exit)
		case rc.pwd != ra.pwd:
			why = "they end in different directories"
		}
		if why != "" && res.diverge == "" {
			res.diverge = "two replays disagree (" + c.name + "): " + why
		}
	}
	if r.Exit != nil && *r.Exit != ra.exit && res.diverge == "" {
		res.diverge = fmt.Sprintf("replay exited %d, the run recorded %d", ra.exit, *r.Exit)
	}
	if rp.agent != AgentClaudeCode {
		return res, nil // Gemini: every call starts afresh
	}
	switch {
	case ra.pwd == "":
		res.cwdUnknown = "the command left no final directory (it replaced the EXIT trap or exec'd)"
	case ra.pwd == cwd:
	case ra.exit != 0:
		// Whether the CLI keeps a directory change made by a failing command
		// is not documented.
		res.cwdUnknown = fmt.Sprintf("the command exited %d after changing the directory to %s", ra.exit, ra.pwd)
	case ra.pwd == agentRoot || strings.HasPrefix(ra.pwd, agentRoot+"/"):
		res.cwd = ra.pwd
	default:
		// "If cd lands outside those directories, Claude Code resets to the
		// project directory."
		res.cwd = agentRoot
	}
	return res, nil
}

// readOnlyTools are file and other tools that change nothing on disk.
var readOnlyTools = map[string]bool{
	// Claude Code
	"Read": true, "Glob": true, "Grep": true, "LS": true, "TodoWrite": true, "ToolSearch": true,
	"WebFetch": true, "WebSearch": true, "BashOutput": true,
	// Gemini CLI
	"read_file": true, "read_many_files": true, "list_directory": true, "glob": true, "grep_search": true,
	"web_fetch": true, "google_web_search": true, "write_todos": true,
}

// applyTool applies a file-tool call to both replays from its recorded
// arguments, where the CLI's behaviour follows from them:
//
//   - Claude Code Write (file_path, content): applied when the file is new or
//     was read or written by a file tool earlier in the same turn and has not
//     changed since. The CLI refuses to overwrite a file it has not read; a
//     refusal is not recorded, so any other case halts.
//   - Claude Code Edit and MultiEdit: the same read condition, and each
//     old_string must occur exactly once (or, with replace_all, at least
//     once). The CLI's fallbacks when the exact string is missing (quote
//     normalization) and its errors are not reproduced: those halt.
//   - Gemini CLI replace (file_path, old_string, new_string, allow_multiple):
//     the exact-match path of edit.ts at v0.49.0 only, on LF text; an empty
//     old_string creates a new file. Its flexible, fuzzy and LLM-corrected
//     matches are not reproducible and halt.
//   - Gemini CLI write_file halts: its content passes through an LLM
//     correction (ensureCorrectFileContent) unless disabled, so what was
//     written does not follow from the arguments.
//   - Read-only tools are no-ops (a Read marks the file as read). Task halts,
//     because a subagent's calls are interleaved in the stream with no marker,
//     and subagents do not share the main loop's directory. Anything else
//     halts.
//
// Paths must be absolute (Gemini's may be relative to the root) and inside
// HOME or /tmp, with no symlink on the way, so a write cannot leave the
// replay's folder.
func (rp *replay) applyTool(r Record) (applied, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Command), &args); err != nil {
		return applied{halt: "arguments are not a JSON object"}, nil
	}
	str := func(k string) (string, bool) {
		var s string
		raw, ok := args[k]
		if !ok || json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	}
	flag := func(k string) (bool, bool) {
		raw, ok := args[k]
		if !ok {
			return false, true
		}
		var b bool
		return b, json.Unmarshal(raw, &b) == nil
	}
	gemini := rp.agent == AgentGeminiCLI
	target := func() (string, string) {
		p, ok := str("file_path")
		if !ok || p == "" {
			return "", "no file_path"
		}
		if !path.IsAbs(p) {
			if !gemini {
				return "", "file_path is not absolute"
			}
			p = path.Join(agentRoot, p)
		}
		return p, ""
	}

	switch {
	case readOnlyTools[r.Kind]:
		if r.Kind == "Read" || r.Kind == "read_file" {
			if p, why := target(); why == "" {
				if h := rp.a.host(p); h != "" {
					if c, err := regularFile(rp.a.home, rp.a.tmp, h); err == nil && c != nil {
						rp.read[h] = hashOf(c)
					}
				}
			}
		}
		return applied{}, nil

	case !gemini && r.Kind == "Write":
		p, why := target()
		if why != "" {
			return applied{halt: why}, nil
		}
		content, ok := str("content")
		if !ok {
			return applied{halt: "no content"}, nil
		}
		return rp.writeBoth(p, func(old []byte, exists bool) ([]byte, string) {
			return []byte(content), ""
		}, true)

	case !gemini && (r.Kind == "Edit" || r.Kind == "MultiEdit"):
		p, why := target()
		if why != "" {
			return applied{halt: why}, nil
		}
		type edit struct {
			Old        string `json:"old_string"`
			New        string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		var edits []edit
		if r.Kind == "Edit" {
			var e edit
			if err := json.Unmarshal([]byte(r.Command), &e); err != nil {
				return applied{halt: "arguments do not parse"}, nil
			}
			if _, ok := str("old_string"); !ok {
				return applied{halt: "no old_string"}, nil
			}
			if _, ok := str("new_string"); !ok {
				return applied{halt: "no new_string"}, nil
			}
			edits = []edit{e}
		} else if err := json.Unmarshal(args["edits"], &edits); err != nil || len(edits) == 0 {
			return applied{halt: "no edits"}, nil
		}
		return rp.writeBoth(p, func(old []byte, exists bool) ([]byte, string) {
			if !exists {
				return nil, "the file does not exist"
			}
			s := string(old)
			for _, e := range edits {
				s2, why := exactReplace(s, e.Old, e.New, e.ReplaceAll)
				if why != "" {
					return nil, why
				}
				s = s2
			}
			return []byte(s), ""
		}, true)

	case gemini && r.Kind == "replace":
		p, why := target()
		if why != "" {
			return applied{halt: why}, nil
		}
		oldS, ok1 := str("old_string")
		newS, ok2 := str("new_string")
		multi, ok3 := flag("allow_multiple")
		if !ok1 || !ok2 || !ok3 {
			return applied{halt: "old_string, new_string or allow_multiple missing or mistyped"}, nil
		}
		return rp.writeBoth(p, func(old []byte, exists bool) ([]byte, string) {
			if oldS == "" {
				if exists {
					return nil, "empty old_string on an existing file (the CLI refuses)"
				}
				return []byte(newS), ""
			}
			if !exists {
				return nil, "the file does not exist"
			}
			if strings.Contains(string(old), "\r") || strings.Contains(oldS, "\r") || strings.Contains(newS, "\r") {
				return nil, "CR line endings are normalized by the CLI"
			}
			s, why := exactReplace(string(old), oldS, newS, multi)
			return []byte(s), why
		}, false)

	case gemini && r.Kind == "write_file":
		return applied{halt: "write_file content may be rewritten by the CLI's LLM correction"}, nil

	case r.Kind == "Task":
		return applied{halt: "a subagent's calls cannot be told apart from the main loop's"}, nil
	}
	return applied{halt: "tool " + r.Kind + " is not replayable"}, nil
}

// exactReplace replaces old with new in s: exactly one occurrence, or with
// all at least one. Anything else is a reason.
func exactReplace(s, old, new string, all bool) (string, string) {
	if old == "" {
		return "", "empty old_string"
	}
	n := strings.Count(s, old)
	switch {
	case n == 0:
		return "", "old_string does not occur exactly (the CLI would fall back or fail)"
	case n > 1 && !all:
		return "", fmt.Sprintf("old_string occurs %d times without replace-all (the CLI would fail)", n)
	}
	return strings.ReplaceAll(s, old, new), ""
}

// writeBoth applies one file write to both replays. change gets the current
// bytes (nil and false when the file does not exist) and returns the new ones
// or a reason not to apply. needRead enforces Claude Code's read-before-write
// rule for an existing file.
func (rp *replay) writeBoth(p string, change func([]byte, bool) ([]byte, string), needRead bool) (applied, error) {
	for _, w := range rp.worlds() {
		h := w.host(p)
		if h == "" {
			return applied{halt: p + " is outside HOME and /tmp"}, nil
		}
		old, err := regularFile(w.home, w.tmp, h)
		if err != nil {
			return applied{halt: err.Error()}, nil
		}
		exists := old != nil
		if exists && needRead && w == rp.a {
			if got, ok := rp.read[h]; !ok || got != hashOf(old) {
				return applied{halt: "overwrites " + p + ", which no file tool read in this turn since it last changed (the CLI may have refused)"}, nil
			}
		}
		if !exists {
			if fi, err := os.Lstat(filepath.Dir(h)); err != nil || !fi.IsDir() {
				return applied{halt: "the parent folder of " + p + " does not exist"}, nil
			}
		}
		data, why := change(old, exists)
		if why != "" {
			return applied{halt: why}, nil
		}
		mode := fs.FileMode(0o644)
		if exists {
			if fi, err := os.Lstat(h); err == nil {
				mode = fi.Mode().Perm()
			}
		}
		if err := os.WriteFile(h, data, mode); err != nil {
			return applied{halt: "writing: " + err.Error()}, nil
		}
		if w == rp.a {
			rp.read[h] = hashOf(data)
		}
	}
	return applied{}, nil
}

// regularFile returns the bytes of the file at h (nil when it does not exist),
// refusing a path with a symlink in any component under the world's root, or
// one that is not a regular file. Nothing outside the world is touched.
func regularFile(home, tmp, h string) ([]byte, error) {
	root := home
	if h == tmp || strings.HasPrefix(h, tmp+string(filepath.Separator)) {
		root = tmp
	}
	rel, err := filepath.Rel(root, h)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("%s is not a file path inside the replay", h)
	}
	cur := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("the path goes through a symlink")
		}
		if i < len(parts)-1 && !fi.IsDir() {
			return nil, nil // a parent is a file: the path does not exist
		}
		if i == len(parts)-1 && !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("the target is not a regular file")
		}
	}
	return os.ReadFile(h)
}

func hashOf(b []byte) string { return fmt.Sprintf("%x", b) }

// analyse returns the verdict for cmd on a fresh copy of replay a's state, run
// from cwd.
func (rp *replay) analyse(ctx context.Context, cmd, cwd string) (string, error) {
	d, err := os.MkdirTemp("", "probe-view-")
	if err != nil {
		return "", err
	}
	defer removeAll(d)
	home, tmp := filepath.Join(d, "home"), filepath.Join(d, "tmp")
	if err := copyTree(rp.a.home, home); err != nil {
		return "", err
	}
	if err := copyTree(rp.a.tmp, tmp); err != nil {
		return "", err
	}
	wd := filepath.Join(home, strings.TrimPrefix(cwd, agentHome))
	env := append(append([]string(nil), oracle.HiddenEnv...), "HOME="+home, "PWD="+wd)
	return analyseAt(ctx, toHost(cmd, home, tmp), wd, env)
}

// toHost rewrites the agent's absolute paths in cmd onto a view: /home/agent
// to home and /tmp to tmp, where either starts a path (not after a name or
// path character) and ends at a component boundary. The view mirrors the
// agent's folders one to one, so the effect the analysis derives is the
// command's effect on the agent's state. It is a rewrite of literal text: a
// path the command computes (/home/$USER) is left alone and resolved against
// the host's read-only view, as before replay.
func toHost(cmd, home, tmp string) string {
	var b strings.Builder
	for i := 0; i < len(cmd); {
		if i == 0 || !pathByte(cmd[i-1]) {
			if to, n := prefixAt(cmd[i:], agentHome, home); n > 0 {
				b.WriteString(to)
				i += n
				continue
			}
			if to, n := prefixAt(cmd[i:], agentTmp, tmp); n > 0 {
				b.WriteString(to)
				i += n
				continue
			}
		}
		b.WriteByte(cmd[i])
		i++
	}
	return b.String()
}

func prefixAt(s, from, to string) (string, int) {
	if !strings.HasPrefix(s, from) {
		return "", 0
	}
	if len(s) > len(from) && nameByte(s[len(from)]) {
		return "", 0 // /home/agents, /tmpfoo
	}
	return to, len(from)
}

func nameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.'
}

func pathByte(c byte) bool { return nameByte(c) || c == '/' || c == '~' || c == '$' }

// compareTrees reports the first difference between two replays' HOME and
// /tmp, ignoring inode numbers and times, or "". strict compares file
// contents too; otherwise only the structure (paths, kinds, permission bits,
// link targets).
func compareTrees(a, b *world, strict bool) (string, error) {
	for _, pair := range [][2]string{{a.home, b.home}, {a.tmp, b.tmp}} {
		sa, err := oracle.Observe(pair[0])
		if err != nil {
			return "", err
		}
		sb, err := oracle.Observe(pair[1])
		if err != nil {
			return "", err
		}
		var keys []string
		for k := range sa {
			keys = append(keys, k)
		}
		for k := range sb {
			if _, ok := sa[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			x, okx := sa[k]
			y, oky := sb[k]
			switch {
			case !okx || !oky:
				return fmt.Sprintf("%q exists in only one", k), nil
			case x.Kind != y.Kind || x.Target != y.Target || x.Perm != y.Perm || strict && x.Hash != y.Hash:
				return fmt.Sprintf("%q differs", k), nil
			}
		}
	}
	return "", nil
}

// syncTimes gives every path in each check replay the access and modification times of
// the same path in a, so that a command whose output embeds file times (gzip,
// zip, tar) writes the same bytes in both. Symlinks are skipped: setting their
// times would follow them.
func (rp *replay) syncTimes() error {
	for _, c := range rp.checks {
		if err := syncTimes(rp.a, c.w); err != nil {
			return err
		}
	}
	return nil
}

func syncTimes(a, b *world) error {
	for _, pair := range [][2]string{{a.home, b.home}, {a.tmp, b.tmp}} {
		src, dst := pair[0], pair[1]
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			fi, err := os.Lstat(p)
			if err != nil {
				return nil
			}
			st, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				return nil
			}
			rel, _ := filepath.Rel(src, p)
			q := filepath.Join(dst, rel)
			if qi, err := os.Lstat(q); err != nil || qi.Mode()&fs.ModeSymlink != 0 {
				return nil
			}
			_ = os.Chtimes(q, time.Unix(st.Atim.Sec, st.Atim.Nsec), time.Unix(st.Mtim.Sec, st.Mtim.Nsec))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// copyTree copies src to dst: directories, regular files and symlinks (as
// links, never followed), with their permission bits. Anything else, or
// anything unreadable, is an error.
func copyTree(src, dst string) error {
	type dirMode struct {
		p string
		m fs.FileMode
	}
	var dirs []dirMode
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		q := filepath.Join(dst, rel)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch m := fi.Mode(); {
		case m.IsDir():
			if err := os.MkdirAll(q, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, dirMode{q, m.Perm() | m&(fs.ModeSetgid|fs.ModeSticky)})
		case m&fs.ModeSymlink != 0:
			t, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(t, q)
		case m.IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := os.WriteFile(q, b, 0o600); err != nil {
				return err
			}
			return os.Chmod(q, m.Perm()|m&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky))
		default:
			return fmt.Errorf("%s is not a file, folder or link", rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].p, dirs[i].m); err != nil {
			return err
		}
	}
	return nil
}

// removeAll deletes p, first making every directory under it writable and
// searchable, so a replayed chmod cannot block cleanup.
func removeAll(p string) {
	_ = filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(q, 0o755)
		}
		return nil
	})
	_ = os.RemoveAll(p)
}
