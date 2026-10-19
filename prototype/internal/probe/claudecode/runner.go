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
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
// can print exactly what would be executed. The run's output folder is
// masked inside the sandbox when it lies outside the hidden directories.
func (r Runner) Plan(t probe.Task, tier string, w Workdir, sessionID string) ([]Turn, error) {
	if err := probe.CheckID("task", t.ID); err != nil {
		return nil, err
	}
	prompt, ok := t.Prompts[tier]
	if !ok || prompt == "" {
		return nil, fmt.Errorf("task %s has no %s prompt", t.ID, tier)
	}
	sb := r.Sandbox
	if m := MaskFor(r.Out); m != "" {
		sb.Mask = append(slices.Clone(sb.Mask), m)
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
		if sb.Strace != "" {
			trace = stem + ".strace"
		}
		turns[i] = Turn{N: i, Prompt: p, Raw: stem + ".jsonl",
			Argv: sb.Argv(w, trace, append([]string{sb.Exe()}, args...))}
	}
	return turns, nil
}

// RunTask runs one task on one tier and returns its records, with the
// oracle's whole-task verdict on the last one. The CLI acts on its own for a
// whole turn, so effects are task-level: the fixture is observed before the
// first turn and after the last, never between steps.
//
// The records come back with any error, and the verdict is taken whenever
// the task started: a turn that failed half way still ran what it ran. The
// error wraps ErrFatal when every later task would fail the same way,
// probe.ErrInterrupted when ctx was cancelled, and probe.ErrIncomplete when
// a turn timed out or ended without a result.
func (r Runner) RunTask(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error) {
	if r.Token == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrFatal, TokenEnv)
	}
	if r.Sandbox.Prlimit == "" {
		return nil, fmt.Errorf("%w: prlimit is required to cap the sandbox's processes", ErrFatal)
	}
	if err := os.MkdirAll(filepath.Join(r.Out, "raw"), 0o755); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFatal, err)
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
	all, err = LabelTask(all, before, w.Fixture(), r.logf, t.ID, tier)
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

// LabelTask observes the fixture after the task and puts the whole-task
// verdict on the last record. Both CLI drivers use it.
func LabelTask(all []probe.Record, before oracle.Snapshot, fixture string, logf func(string, ...any), task, tier string) ([]probe.Record, error) {
	after, err := oracle.Observe(fixture)
	if err != nil {
		return all, err
	}
	v := oracle.Label(before, after)
	lost := v.Label == "C"
	if len(all) == 0 {
		if lost {
			logf("task %s %s: files were lost (%s) but no tool call was recorded\n", task, tier, OneLine(v.Reason))
		}
		return all, nil
	}
	all[len(all)-1].Lost = &lost
	return all, nil
}

// runTurn runs one invocation, saving its stream, and parses it. ok is false
// when the turn timed out, was interrupted, or its stream has no result
// event. The records parsed come back with any error.
//
// The stream and stderr go to their files through a Redactor, so the token
// never reaches disk even when the agent prints it, and the strace file is
// redacted once strace has closed it.
func (r Runner) runTurn(ctx context.Context, turn Turn, base probe.Record) ([]probe.Record, bool, error) {
	secrets := []string{r.Token}
	res, err := Exec(ctx, turn.Argv, r.Sandbox.Env(r.Token), turn.Raw, r.TurnTimeout, secrets)
	if err != nil {
		return nil, false, err
	}
	name := filepath.Base(turn.Raw)
	s, perr := ParseSession(bytes.NewReader(res.Stream), base)
	if r.Sandbox.Strace != "" {
		ex, err := TraceExecve(trimExt(turn.Raw)+".strace", secrets)
		if err != nil {
			return s.Records, false, err
		}
		if len(s.Records) > 0 {
			s.Records[len(s.Records)-1].Execve = ex
		}
	}
	if perr != nil {
		return s.Records, false, perr
	}
	switch {
	case res.Interrupted:
		r.logf("turn %s interrupted\n", name)
	case res.TimedOut:
		r.logf("turn %s timed out after %s\n", name, r.TurnTimeout)
	case s.Result == nil:
		r.logf("turn %s: stream ended without a result (exit %d); see the .stderr file\n", name, res.Code)
	case s.Result.IsError:
		r.logf("turn %s: error (%s): %s %s\n", name, OneLine(s.Result.Subtype), OneLine(s.Result.Text), OneLine(strings.Join(s.Result.Errors, "; ")))
		if authFailure(s.Result.Text) {
			// Every later task would fail the same way; stop rather than
			// spend the run producing empty records.
			return s.Records, false, fmt.Errorf("%w: the CLI could not authenticate (%s); check %s", ErrFatal, OneLine(s.Result.Text), TokenEnv)
		}
	}
	if s.PermissionMode != "" && s.PermissionMode != "bypassPermissions" {
		r.logf("turn %s: CLI reports permission mode %q, not bypassPermissions\n", name, s.PermissionMode)
	}
	if s.Result != nil && s.Result.Denials > 0 {
		r.logf("turn %s: the CLI refused %d tool call(s); see permission_denials in the raw stream\n", name, s.Result.Denials)
	}
	return s.Records, !res.TimedOut && !res.Interrupted && s.Result != nil, nil
}

// Ran is how one sandboxed CLI invocation ended.
type Ran struct {
	Stream      []byte // the stream as saved (redacted), cut to whole lines when the CLI was killed
	Code        int    // exit status; -1 when killed by a signal
	TimedOut    bool   // the turn's own time limit ended it
	Interrupted bool   // the caller's context ended it (the operator's Ctrl-C)
}

// Exec runs argv with env, saving stdout to raw and stderr beside it (both
// created new, mode 0600, with every secret redacted on the way), under a
// time limit when limit > 0. Both CLI drivers use it. Failing to create or
// write the output wraps ErrFatal: every later task would fail the same way.
func Exec(ctx context.Context, argv, env []string, raw string, limit time.Duration, secrets []string) (Ran, error) {
	var res Ran
	stdout, err := createNew(raw)
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrFatal, err)
	}
	defer stdout.Close()
	stderr, err := createNew(trimExt(raw) + ".stderr")
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrFatal, err)
	}
	defer stderr.Close()
	out, errOut := NewRedactor(stdout, secrets), NewRedactor(stderr, secrets)

	tctx := ctx
	if limit > 0 {
		var cancel context.CancelFunc
		tctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	cmd := exec.CommandContext(tctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.WaitDelay = 10 * time.Second
	runErr := cmd.Run()
	if err := errors.Join(out.Flush(), errOut.Flush()); err != nil {
		return res, fmt.Errorf("%w: saving the stream: %v", ErrFatal, err)
	}
	res.Interrupted = ctx.Err() != nil
	res.TimedOut = !res.Interrupted && tctx.Err() != nil
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		res.Code = ee.ExitCode()
	case res.TimedOut || res.Interrupted || errors.Is(runErr, exec.ErrWaitDelay):
		// Killed by the context, or exited while something held its output.
	default:
		return res, fmt.Errorf("starting the sandbox: %w", runErr)
	}
	if res.Stream, err = os.ReadFile(raw); err != nil {
		return res, err
	}
	if res.TimedOut || res.Interrupted {
		// A killed CLI can leave half a line; keep only whole ones.
		if i := bytes.LastIndexByte(res.Stream, '\n'); i >= 0 {
			res.Stream = res.Stream[:i+1]
		} else {
			res.Stream = nil
		}
	}
	return res, nil
}

// TraceExecve redacts the strace file at path and returns its execve list.
// A missing file (strace did not start) is no list and no error.
func TraceExecve(path string, secrets []string) ([]string, error) {
	if err := RedactFile(path, secrets); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Execve(f)
}

func createNew(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
}

// ErrFatal marks a failure every later task would repeat (the login was
// refused), so the caller stops the run instead of producing empty records.
var ErrFatal = errors.New("fatal")

// authFailure reports whether a result's text is the CLI failing to log in,
// as it words it ("Failed to authenticate. API Error: 401 ...").
func authFailure(text string) bool {
	return strings.Contains(text, "Failed to authenticate") || strings.Contains(text, "API Error: 401")
}

// OneLine makes text from the CLI, which the agent can steer, safe to print
// to the operator's terminal: whitespace runs become one space, control and
// format characters (terminal escapes, bidirectional overrides) are shown
// escaped, and it is cut to 200 bytes on a character boundary.
func OneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			b.WriteString(`�`)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
		if b.Len() > 200 {
			return b.String() + "..."
		}
	}
	return b.String()
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

// RedactedText is the placeholder a secret is replaced with on disk.
const RedactedText = "<redacted>"

// minSecret is the shortest secret redacted. A shorter value is no real
// credential, and replacing it would rewrite ordinary text in the stream.
const minSecret = 8

// Redactor writes to w with every occurrence of each secret replaced by
// RedactedText, including occurrences split across writes: it holds back
// the last len(longest secret)-1 bytes until more arrive or Flush. It only
// catches the exact value; a secret the agent encodes (base64, split over
// lines) passes through.
type Redactor struct {
	w       io.Writer
	secrets [][]byte
	keep    int
	buf     []byte
	err     error
}

// NewRedactor redacts secrets of at least minSecret bytes.
func NewRedactor(w io.Writer, secrets []string) *Redactor {
	r := &Redactor{w: w}
	for _, s := range secrets {
		if len(s) >= minSecret {
			r.secrets = append(r.secrets, []byte(s))
			r.keep = max(r.keep, len(s)-1)
		}
	}
	// Longest first, so a secret that contains another is replaced whole.
	slices.SortFunc(r.secrets, func(a, b []byte) int { return len(b) - len(a) })
	return r
}

func (r *Redactor) redact() {
	for _, s := range r.secrets {
		r.buf = bytes.ReplaceAll(r.buf, s, []byte(RedactedText))
	}
}

func (r *Redactor) Write(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	r.buf = append(r.buf, p...)
	r.redact()
	if n := len(r.buf) - r.keep; n > 0 {
		if _, err := r.w.Write(r.buf[:n]); err != nil {
			r.err = err
			return 0, err
		}
		r.buf = append(r.buf[:0], r.buf[n:]...)
	}
	return len(p), nil
}

// Flush writes what is held back. Call it once the writer is done.
func (r *Redactor) Flush() error {
	if r.err != nil {
		return r.err
	}
	r.redact()
	_, err := r.w.Write(r.buf)
	r.buf = r.buf[:0]
	r.err = err
	return err
}

// RedactFile rewrites the file at path with every secret replaced, when it
// holds one, and makes it private to the operator.
func RedactFile(path string, secrets []string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	rd := NewRedactor(&out, secrets)
	rd.Write(b)
	rd.Flush()
	if bytes.Equal(out.Bytes(), b) {
		return os.Chmod(path, 0o600)
	}
	tmp := path + ".redacting"
	if err := os.WriteFile(tmp, out.Bytes(), 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
