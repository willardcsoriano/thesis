package geminicli

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
)

// ErrFatal marks a failure every later task would repeat (no valid login,
// quota exhausted, a configuration the CLI rejects, output that cannot be
// written). The caller stops the run instead of producing empty records for
// the rest of the suite. It is claudecode.ErrFatal, so both drivers' shared
// code (claudecode.Exec) reports it the same way.
var ErrFatal = claudecode.ErrFatal

// Runner runs tasks through the CLI, one sandbox per task and tier.
type Runner struct {
	Sandbox     Sandbox
	Auth        Auth   // the only credential; the API key is placed in the child's environment only
	Model       string // passed to --model; empty leaves the CLI's default
	MaxTurns    int    // model.maxSessionTurns per invocation; 0 means no limit
	Debug       bool   // pass --debug so shell results carry exit codes
	TurnTimeout time.Duration
	RunID       string
	Out         string    // runs/<run-id>; raw streams go in Out/raw
	Log         io.Writer // progress and warnings; never receives a credential
}

// Turn is one planned CLI invocation of a task.
type Turn struct {
	N      int
	Prompt string
	Argv   []string // host command line
	Raw    string   // where the stream is saved
}

// Plan lays out every turn of a task on one tier: the first prompt, then
// each follow-up resuming the same session. It runs nothing, so a dry run
// can print exactly what would be executed. The run's output folder is
// masked inside the sandbox when it lies outside the hidden directories.
func (r Runner) Plan(t probe.Task, tier string, w claudecode.Workdir, sessionID string) ([]Turn, error) {
	if err := probe.CheckID("task", t.ID); err != nil {
		return nil, err
	}
	prompt, ok := t.Prompts[tier]
	if !ok || prompt == "" {
		return nil, fmt.Errorf("task %s has no %s prompt", t.ID, tier)
	}
	sb := r.Sandbox
	if m := claudecode.MaskFor(r.Out); m != "" {
		sb.Mask = append(slices.Clone(sb.Mask), m)
	}
	prompts := append([]string{prompt}, t.FollowUps...)
	turns := make([]Turn, len(prompts))
	for i, p := range prompts {
		args, err := GeminiArgs(p, sessionID, i, r.Model, r.Debug)
		if err != nil {
			return nil, fmt.Errorf("task %s turn %d: %w", t.ID, i, err)
		}
		stem := filepath.Join(r.Out, "raw", fmt.Sprintf("%s-%s-t%d", t.ID, tier, i))
		trace := ""
		if sb.Strace != "" {
			trace = stem + ".strace"
		}
		inner := append(sb.Command(), args...)
		turns[i] = Turn{N: i, Prompt: p, Raw: stem + ".jsonl", Argv: sb.Argv(w, trace, inner)}
	}
	return turns, nil
}

// RunTask runs one task on one tier and returns its records, with the
// oracle's whole-task verdict on the last one. The CLI acts on its own for a
// whole turn, so effects are task-level: the fixture is observed before the
// first turn and after the last, never between steps.
//
// As in claudecode, the records come back with any error and the verdict is
// taken whenever the task started. An error wrapping ErrFatal means the run
// should stop; probe.ErrInterrupted and probe.ErrIncomplete say the task did
// not finish.
func (r Runner) RunTask(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
	if err := r.Auth.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFatal, err)
	}
	if r.Sandbox.Prlimit == "" {
		return nil, fmt.Errorf("%w: prlimit is required to cap the sandbox's processes", ErrFatal)
	}
	if err := os.MkdirAll(filepath.Join(r.Out, "raw"), 0o755); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFatal, err)
	}
	w, err := claudecode.NewWorkdir()
	if err != nil {
		return nil, err
	}
	defer w.Remove() // also deletes the credential copy
	if err := PrepareHome(w, r.Auth, r.MaxTurns); err != nil {
		return nil, err
	}
	if err := oracle.Materialize(w.Fixture(), t.Fixture); err != nil {
		return nil, fmt.Errorf("fixture: %w", err)
	}
	before, err := oracle.Observe(w.Fixture())
	if err != nil {
		return nil, err
	}
	sid, err := newUUID()
	if err != nil {
		return nil, err
	}
	turns, err := r.Plan(t, tier, w, sid)
	if err != nil {
		return nil, err
	}
	base := probe.Record{Run: r.RunID, Driver: "cli", Agent: probe.AgentGeminiCLI, Model: r.Model, Profile: "native", ToolVersion: r.Sandbox.Install.Version,
		Task: t.ID, Partition: t.Partition, Category: t.Category, Tier: tier, Features: t.Features}
	var all []probe.Record
	var turnErr error
	complete := true
	for _, turn := range turns {
		b := base
		b.Turn = turn.N
		recs, ok, err := r.runTurn(ctx, turn, b)
		all = append(all, recs...)
		if err != nil {
			turnErr = fmt.Errorf("task %s %s turn %d: %w", t.ID, tier, turn.N, err)
			break
		}
		if !ok {
			complete = false
			r.logf("task %s %s: turn %d did not finish; skipping its follow-ups\n", t.ID, tier, turn.N)
			break
		}
	}
	all, err = claudecode.LabelTask(all, before, w.Fixture(), r.logf, t.ID, tier)
	switch {
	case turnErr != nil || err != nil:
		return all, errors.Join(turnErr, err)
	case ctx.Err() != nil:
		return all, fmt.Errorf("task %s %s: %w", t.ID, tier, probe.ErrInterrupted)
	case !complete:
		return all, fmt.Errorf("task %s %s: %w", t.ID, tier, probe.ErrIncomplete)
	}
	return all, nil
}

// Exit codes the CLI uses for failures that do not depend on the task
// (NOTES.md, "Exit codes"). 53 (turn limit) and 130 (cancelled) are
// per-task and are not here.
var fatalExits = map[int]string{
	41: "authentication failed",
	44: "the CLI's own sandbox failed",
	52: "configuration error",
	55: "workspace not trusted",
}

// fatalResult names result error types that every later task would repeat.
var fatalResult = map[string]string{
	"FatalAuthenticationError":     "authentication failed",
	"AccountSuspendedError":        "the account is suspended",
	"TerminalQuotaError":           "quota exhausted",
	"FatalConfigError":             "configuration error",
	"FatalUntrustedWorkspaceError": "workspace not trusted",
	"ProjectIdRequiredError":       "a Google Cloud project is required (set " + ProjectEnv + ")",
}

// runTurn runs one invocation, saving its stream, and parses it. ok is false
// when the turn timed out, was interrupted, or its stream has no result
// event. The records parsed come back with any error. Every credential
// value is redacted from what is saved (claudecode.Exec).
func (r Runner) runTurn(ctx context.Context, turn Turn, base probe.Record) ([]probe.Record, bool, error) {
	secrets := r.Auth.Secrets()
	res, err := claudecode.Exec(ctx, turn.Argv, r.Sandbox.Env(r.Auth), turn.Raw, r.TurnTimeout, secrets)
	if err != nil {
		return nil, false, err
	}
	stderrPath := trimExt(turn.Raw) + ".stderr"
	killed := res.TimedOut || res.Interrupted
	s, perr := ParseSession(bytes.NewReader(res.Stream), base)
	if r.Sandbox.Strace != "" {
		ex, err := claudecode.TraceExecve(trimExt(turn.Raw)+".strace", secrets)
		if err != nil {
			return s.Records, false, err
		}
		if len(s.Records) > 0 {
			s.Records[len(s.Records)-1].Execve = ex
		}
	}
	if perr != nil {
		if why, ok := fatalExits[res.Code]; ok && !killed {
			return s.Records, false, fmt.Errorf("%w: the CLI exited %d (%s); see %s", ErrFatal, res.Code, why, stderrPath)
		}
		return s.Records, false, perr
	}
	name := filepath.Base(turn.Raw)
	for _, w := range s.Warnings {
		r.logf("turn %s: CLI warning: %s\n", name, claudecode.OneLine(w))
	}
	if s.Denials > 0 {
		r.logf("turn %s: the CLI's policy refused %d tool call(s); see policy_violation in the raw stream\n", name, s.Denials)
	}
	if s.Result != nil && s.Result.IsError() {
		r.logf("turn %s: error (%s): %s\n", name, claudecode.OneLine(s.Result.ErrorType), claudecode.OneLine(s.Result.Message))
		if why, ok := fatalResult[s.Result.ErrorType]; ok {
			return s.Records, false, fmt.Errorf("%w: %s (%s)", ErrFatal, why, claudecode.OneLine(s.Result.Message))
		}
	}
	if why, ok := fatalExits[res.Code]; ok && !killed {
		return s.Records, false, fmt.Errorf("%w: the CLI exited %d (%s); see %s", ErrFatal, res.Code, why, stderrPath)
	}
	switch {
	case res.Interrupted:
		r.logf("turn %s interrupted\n", name)
	case res.TimedOut:
		r.logf("turn %s timed out after %s\n", name, r.TurnTimeout)
	case s.Result == nil:
		r.logf("turn %s: stream ended without a result (exit %d); see %s\n", name, res.Code, stderrPath)
	}
	return s.Records, !killed && s.Result != nil, nil
}

func (r Runner) logf(format string, a ...any) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, format, a...)
	}
}

func trimExt(p string) string { return p[:len(p)-len(filepath.Ext(p))] }

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
