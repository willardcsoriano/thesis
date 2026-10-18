package claudecode

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
)

// Runner runs tasks through the CLI, one sandbox per task and tier.
type Runner struct {
	Sandbox     Sandbox
	Token       string // value of TokenEnv; only ever placed in the child's environment
	Model       string // passed to --model; empty leaves the CLI's default
	MaxTurns    int    // passed to --max-turns; 0 means no limit
	TurnTimeout time.Duration
	RunID       string
	Out         string    // runs/<run-id>; raw streams go in Out/raw
	Log         io.Writer // progress and warnings; never receives the token
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
// can print exactly what would be executed.
func (r Runner) Plan(t probe.Task, tier string, w Workdir, sessionID string) ([]Turn, error) {
	prompt, ok := t.Prompts[tier]
	if !ok || prompt == "" {
		return nil, fmt.Errorf("task %s has no %s prompt", t.ID, tier)
	}
	prompts := append([]string{prompt}, t.FollowUps...)
	turns := make([]Turn, len(prompts))
	for i, p := range prompts {
		args, err := ClaudeArgs(p, sessionID, i, r.Model, r.MaxTurns)
		if err != nil {
			return nil, fmt.Errorf("task %s turn %d: %w", t.ID, i, err)
		}
		stem := filepath.Join(r.Out, "raw", fmt.Sprintf("%s-%s-t%d", t.ID, tier, i))
		trace := ""
		if r.Sandbox.Strace != "" {
			trace = stem + ".strace"
		}
		turns[i] = Turn{N: i, Prompt: p, Raw: stem + ".jsonl",
			Argv: r.Sandbox.Argv(w, trace, append([]string{r.Sandbox.Exe()}, args...))}
	}
	return turns, nil
}

// RunTask runs one task on one tier and returns its records, with the
// oracle's whole-task verdict on the last one. The CLI acts on its own for a
// whole turn, so effects are task-level: the fixture is observed before the
// first turn and after the last, never between steps.
func (r Runner) RunTask(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
	if r.Token == "" {
		return nil, fmt.Errorf("%s is not set", TokenEnv)
	}
	if err := os.MkdirAll(filepath.Join(r.Out, "raw"), 0o755); err != nil {
		return nil, err
	}
	w, err := NewWorkdir()
	if err != nil {
		return nil, err
	}
	defer w.Remove()
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
	base := probe.Record{Run: r.RunID, Driver: "cli", Model: r.Model, Profile: "native",
		Task: t.ID, Partition: t.Partition, Category: t.Category, Tier: tier, Features: t.Features}
	var all []probe.Record
	for _, turn := range turns {
		b := base
		b.Turn = turn.N
		recs, ok, err := r.runTurn(ctx, turn, b)
		if err != nil {
			return all, fmt.Errorf("task %s %s turn %d: %w", t.ID, tier, turn.N, err)
		}
		all = append(all, recs...)
		if !ok {
			r.logf("task %s %s: turn %d did not finish; skipping its follow-ups\n", t.ID, tier, turn.N)
			break
		}
	}
	after, err := oracle.Observe(w.Fixture())
	if err != nil {
		return all, err
	}
	v := oracle.Label(before, after)
	lost := v.Label == "C"
	if len(all) == 0 {
		if lost {
			r.logf("task %s %s: files were lost (%s) but no tool call was recorded\n", t.ID, tier, v.Reason)
		}
		return all, nil
	}
	all[len(all)-1].Lost = &lost
	return all, nil
}

// runTurn runs one invocation, saving its stream, and parses it. ok is false
// when the turn timed out or its stream has no result event.
func (r Runner) runTurn(ctx context.Context, turn Turn, base probe.Record) ([]probe.Record, bool, error) {
	raw, err := os.Create(turn.Raw)
	if err != nil {
		return nil, false, err
	}
	defer raw.Close()
	stderr, err := os.Create(trimExt(turn.Raw) + ".stderr")
	if err != nil {
		return nil, false, err
	}
	defer stderr.Close()

	if r.TurnTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.TurnTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, turn.Argv[0], turn.Argv[1:]...)
	cmd.Env = r.Sandbox.Env(r.Token)
	cmd.Stdin = nil
	cmd.Stdout = raw
	cmd.Stderr = stderr
	cmd.WaitDelay = 10 * time.Second
	runErr := cmd.Run()
	timedOut := ctx.Err() != nil
	var ee *exec.ExitError
	if runErr != nil && !errors.As(runErr, &ee) && !timedOut {
		return nil, false, fmt.Errorf("starting the sandbox: %w", runErr)
	}

	data, err := os.ReadFile(turn.Raw)
	if err != nil {
		return nil, false, err
	}
	if timedOut {
		// A killed CLI can leave half a line; keep only whole ones.
		if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i+1]
		} else {
			data = nil
		}
	}
	s, err := ParseSession(bytes.NewReader(data), base)
	if err != nil {
		return nil, false, err
	}
	if r.Sandbox.Strace != "" {
		if f, err := os.Open(trimExt(turn.Raw) + ".strace"); err == nil {
			ex, perr := Execve(f)
			f.Close()
			if perr != nil {
				return nil, false, perr
			}
			if len(s.Records) > 0 {
				s.Records[len(s.Records)-1].Execve = ex
			}
		}
	}
	switch {
	case timedOut:
		r.logf("turn %s timed out after %s\n", filepath.Base(turn.Raw), r.TurnTimeout)
	case s.Result == nil:
		r.logf("turn %s: stream ended without a result (exit %v); see the .stderr file\n", filepath.Base(turn.Raw), runErr)
	case s.Result.IsError:
		r.logf("turn %s: error (%s): %s %v\n", filepath.Base(turn.Raw), s.Result.Subtype, oneLine(s.Result.Text), s.Result.Errors)
		if authFailure(s.Result.Text) {
			// Every later task would fail the same way; stop rather than
			// spend the run producing empty records.
			return nil, false, fmt.Errorf("%w: the CLI could not authenticate (%s); check %s", ErrFatal, oneLine(s.Result.Text), TokenEnv)
		}
	}
	if s.PermissionMode != "" && s.PermissionMode != "bypassPermissions" {
		r.logf("turn %s: CLI reports permission mode %q, not bypassPermissions\n", filepath.Base(turn.Raw), s.PermissionMode)
	}
	if s.Result != nil && s.Result.Denials > 0 {
		r.logf("turn %s: the CLI refused %d tool call(s); see permission_denials in the raw stream\n", filepath.Base(turn.Raw), s.Result.Denials)
	}
	return s.Records, !timedOut && s.Result != nil, nil
}

// ErrFatal marks a failure every later task would repeat (the login was
// refused), so the caller stops the run instead of producing empty records.
var ErrFatal = errors.New("fatal")

// authFailure reports whether a result's text is the CLI failing to log in,
// as it words it ("Failed to authenticate. API Error: 401 ...").
func authFailure(text string) bool {
	return strings.Contains(text, "Failed to authenticate") || strings.Contains(text, "API Error: 401")
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
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
