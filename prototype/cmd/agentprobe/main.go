// Command agentprobe runs the agent command study (docs/agent-command-study.md):
// it gives each task of a suite to a real agent and records every command the
// agent runs.
//
//	agentprobe claude -tasks FILE [-tiers technical,novice] [-limit N] [-model M] [-out runs/] [-dry-run]
//	agentprobe gemini -tasks FILE [-oauth-creds FILE] [same flags]
//	agentprobe score -tasks FILE -run runs/<run-id>
//
// The claude driver runs Claude Code headless inside bubblewrap with
// permissions skipped, authenticated only by CLAUDE_CODE_OAUTH_TOKEN from the
// operator's environment. The gemini driver runs the Gemini CLI the same way
// with approvals off, authenticated by a copy of one OAuth credential file the
// operator names (the personal subscription) or by GEMINI_API_KEY. Records go
// to OUT/<run-id>/events.jsonl and each turn's raw stream to OUT/<run-id>/raw/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
	"synapseos/internal/probe/claudecode"
	"synapseos/internal/probe/geminicli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

const usage = `usage: agentprobe <driver> -tasks FILE [flags]
       agentprobe score -tasks FILE -run DIR

Drivers:
  claude   Claude Code, sandboxed (see internal/probe/claudecode/NOTES.md)
  gemini   Gemini CLI, sandboxed (see internal/probe/geminicli/NOTES.md)

Scoring:
  score    shape, interpreters and analysis verdict for every record of a run;
           writes DIR/scored.jsonl and DIR/report.txt, calls no model
`

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "score" {
		return score(ctx, args[1:], stdout, stderr)
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "claude":
		return runClaude(ctx, args[1:], getenv, stdout, stderr)
	case "gemini":
		return runGemini(ctx, args[1:], getenv, stdout, stderr)
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// common holds the flags every driver takes.
type common struct {
	tasksPath, tiers, model, out, runID string
	limit, maxTurns                     int
	timeout                             time.Duration
	strace, dryRun                      bool
}

func commonFlags(fs *flag.FlagSet, maxTurnsHelp string) *common {
	c := &common{}
	fs.StringVar(&c.tasksPath, "tasks", "", "task suite (JSONL, probe.Task per line)")
	fs.StringVar(&c.tiers, "tiers", strings.Join(probe.Tiers, ","), "comma-separated tiers to run")
	fs.IntVar(&c.limit, "limit", 0, "run only the first N tasks (0: all)")
	fs.StringVar(&c.model, "model", "", "model alias or id passed to --model (empty: the CLI's default)")
	fs.IntVar(&c.maxTurns, "max-turns", 25, maxTurnsHelp)
	fs.DurationVar(&c.timeout, "timeout", 15*time.Minute, "wall-clock limit per CLI invocation")
	fs.StringVar(&c.out, "out", "runs", "output directory; the run goes in OUT/<run-id>")
	fs.StringVar(&c.runID, "run-id", "", "run id (default: UTC timestamp plus the driver name)")
	fs.BoolVar(&c.strace, "strace", false, "capture every execve with strace (records argv[0] list per turn)")
	fs.BoolVar(&c.dryRun, "dry-run", false, "print the sandbox command lines and exit without running anything")
	return c
}

// setup is what every driver needs once its flags are parsed.
type setup struct {
	tasks  []probe.Task
	tiers  []string
	bwrap  string
	strace string // empty unless -strace
	runID  string
	out    string // OUT/<run-id>
}

// load validates the common flags and loads the suite. A non-zero code means
// stop with it; the reason has been printed.
func (c *common) load(stderr io.Writer) ([]probe.Task, []string, int) {
	if c.tasksPath == "" {
		fmt.Fprintln(stderr, "agentprobe: -tasks is required")
		return nil, nil, 2
	}
	tiers, err := parseTiers(c.tiers)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return nil, nil, 2
	}
	tasks, err := probe.LoadTasks(c.tasksPath)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return nil, nil, 1
	}
	if c.limit > 0 && c.limit < len(tasks) {
		tasks = tasks[:c.limit]
	}
	return tasks, tiers, 0
}

// host finds bubblewrap and, if asked for, strace, and fixes the run id.
func (c *common) host(driver string, stderr io.Writer) (setup, int) {
	var s setup
	if s.bwrap = oracle.Bwrap(); s.bwrap == "" {
		fmt.Fprintln(stderr, "agentprobe: bubblewrap (bwrap) is missing or cannot create namespaces; the CLI never runs unsandboxed")
		return s, 1
	}
	if c.strace {
		p, err := exec.LookPath("strace")
		if err != nil {
			fmt.Fprintln(stderr, "agentprobe: -strace given but strace is not on PATH")
			return s, 1
		}
		s.strace = p
	}
	s.runID = c.runID
	if s.runID == "" {
		s.runID = time.Now().UTC().Format("20060102T150405Z") + "-" + driver
	}
	s.out = filepath.Join(c.out, s.runID)
	return s, 0
}

func runClaude(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agentprobe claude", flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := commonFlags(fs, "agentic turns per CLI invocation, passed to --max-turns (0: no limit)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tasks, tiers, code := c.load(stderr)
	if code != 0 {
		return code
	}
	token := getenv(claudecode.TokenEnv)
	if token == "" && !c.dryRun {
		fmt.Fprint(stderr, tokenHelp)
		return 2
	}
	s, code := c.host("claude", stderr)
	if code != 0 {
		return code
	}
	install, err := claudecode.ResolveInstall()
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	sb := claudecode.Sandbox{Bwrap: s.bwrap, Strace: s.strace, Install: install}
	r := claudecode.Runner{Sandbox: sb, Token: token, Model: c.model, MaxTurns: c.maxTurns, TurnTimeout: c.timeout,
		RunID: s.runID, Out: s.out, Log: stderr}

	if c.dryRun {
		var notes []string
		if token == "" {
			notes = append(notes, claudecode.TokenEnv+" is not set; a real run would refuse to start")
		}
		wd := claudecode.Workdir{Root: "<workdir>"}
		plan := func(t probe.Task, tier string) ([]dryTurn, error) {
			turns, err := r.Plan(t, tier, wd, "<session-uuid>")
			out := make([]dryTurn, len(turns))
			for i, tu := range turns {
				out[i] = dryTurn{tu.N, tu.Raw, tu.Argv}
			}
			return out, err
		}
		return dry(stdout, notes, claudecode.Redacted(sb.Env("unset")), nil, tasks, tiers, plan)
	}
	// The claude driver reports a failed login as a task error and keeps going.
	stop := func(err error) bool { return errors.Is(err, claudecode.ErrFatal) }
	return runTasks(ctx, r.RunTask, s.out, tasks, tiers, stop, stderr)
}

const tokenHelp = `agentprobe: ` + claudecode.TokenEnv + ` is not set, and the CLI is never given any other credential.
To run:
  1. In a terminal, run:  claude setup-token
     It opens a browser, then prints a one-year token. It saves the token nowhere.
  2. In the shell you will run agentprobe from, run:  export ` + claudecode.TokenEnv + `=<the token>
     Keep it out of files, the repository and shell history files you share.
  3. Run this agentprobe command again.
`

func runGemini(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agentprobe gemini", flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := commonFlags(fs, "model turns per CLI invocation, written as model.maxSessionTurns (0: no limit)")
	credsPath := fs.String("oauth-creds", "", "oauth_creds.json from \"Sign in with Google\" (the personal subscription); copied into each task's throwaway home")
	debug := fs.Bool("debug", false, "run the CLI with --debug so shell results carry exit codes (see NOTES.md)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	tasks, tiers, code := c.load(stderr)
	if code != 0 {
		return code
	}
	auth := geminicli.Auth{APIKey: getenv(geminicli.APIKeyEnv), Project: getenv(geminicli.ProjectEnv)}
	if *credsPath != "" {
		creds, err := geminicli.LoadOAuthCreds(*credsPath)
		if err != nil {
			fmt.Fprintln(stderr, "agentprobe:", err)
			return 2
		}
		auth.OAuthCreds = creds
	}
	if err := auth.Validate(); err != nil && (!c.dryRun || auth.Type() != "") {
		fmt.Fprintf(stderr, "agentprobe: %v.\n", err)
		fmt.Fprint(stderr, geminiAuthHelp)
		return 2
	}
	s, code := c.host("gemini", stderr)
	if code != 0 {
		return code
	}
	install, err := geminicli.ResolveInstall()
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	sb := geminicli.Sandbox{Bwrap: s.bwrap, Strace: s.strace, Install: install}
	r := geminicli.Runner{Sandbox: sb, Auth: auth, Model: c.model, MaxTurns: c.maxTurns, Debug: *debug,
		TurnTimeout: c.timeout, RunID: s.runID, Out: s.out, Log: stderr}

	if c.dryRun {
		var notes, files []string
		switch auth.Type() {
		case "":
			notes = append(notes, "no credential is configured; a real run would refuse to start")
		case geminicli.AuthOAuthPersonal:
			files = append(files, "~/.gemini/oauth_creds.json: a copy of "+*credsPath+" (contents not shown), deleted with the workdir")
		}
		settings := strings.TrimSpace(string(geminicli.Settings(auth.Type(), c.maxTurns)))
		files = append(files, "~/.gemini/settings.json:\n#     "+strings.ReplaceAll(settings, "\n", "\n#     "))
		wd := claudecode.Workdir{Root: "<workdir>"}
		plan := func(t probe.Task, tier string) ([]dryTurn, error) {
			turns, err := r.Plan(t, tier, wd, "<session-uuid>")
			out := make([]dryTurn, len(turns))
			for i, tu := range turns {
				out[i] = dryTurn{tu.N, tu.Raw, tu.Argv}
			}
			return out, err
		}
		return dry(stdout, notes, geminicli.Redacted(sb.Env(auth)), files, tasks, tiers, plan)
	}
	stop := func(err error) bool { return errors.Is(err, geminicli.ErrFatal) }
	return runTasks(ctx, r.RunTask, s.out, tasks, tiers, stop, stderr)
}

const geminiAuthHelp = `The Gemini CLI gets exactly one credential. Pick one:

  A. Your personal Google subscription (Google AI Pro/Ultra or the free individual tier):
     1. Sign in once under a separate config root, so your everyday ~/.gemini is not used:
          GEMINI_CLI_HOME=$HOME/.agentprobe-gemini gemini
        choose "Sign in with Google", finish in the browser, then type /quit.
        Use a Google account kept for the study if you can: the agent can read this
        credential, and it grants cloud-platform access until you revoke it.
     2. Run agentprobe with:  -oauth-creds $HOME/.agentprobe-gemini/.gemini/oauth_creds.json
        Each task gets a copy in its throwaway home; the file itself is never mounted.

  B. A Gemini API key from https://aistudio.google.com/app/apikey (billed to that
     project, not your subscription):
          export ` + geminicli.APIKeyEnv + `=<the key>
     Keep it out of files, the repository and shell history files you share.

Then run this agentprobe command again.
`

// taskFunc runs one task on one tier.
type taskFunc func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error)

// runTasks runs every task on every tier, appending records to
// OUT/events.jsonl. stop, when set, says which errors end the run at once
// (a failure every later task would repeat); other errors are counted and
// the run goes on.
func runTasks(ctx context.Context, runTask taskFunc, out string, tasks []probe.Task, tiers []string, stop func(error) bool, stderr io.Writer) int {
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	eventsPath := filepath.Join(out, "events.jsonl")
	events, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "agentprobe: %v (pick a new -run-id rather than mixing two runs)\n", err)
		return 1
	}
	defer events.Close()

	failed := 0
	for _, t := range tasks {
		for _, tier := range tiers {
			if ctx.Err() != nil {
				fmt.Fprintln(stderr, "agentprobe: interrupted")
				return 1
			}
			fmt.Fprintf(stderr, "%s %s ...\n", t.ID, tier)
			recs, err := runTask(ctx, t, tier)
			for _, rec := range recs {
				if aerr := probe.Append(events, rec); aerr != nil {
					fmt.Fprintln(stderr, "agentprobe:", aerr)
					return 1
				}
			}
			if err != nil {
				failed++
				fmt.Fprintln(stderr, "agentprobe:", err)
				if stop != nil && stop(err) {
					fmt.Fprintf(stderr, "agentprobe: stopping the run; records so far in %s\n", eventsPath)
					return 1
				}
			}
		}
	}
	fmt.Fprintf(stderr, "records in %s\n", eventsPath)
	if failed > 0 {
		fmt.Fprintf(stderr, "agentprobe: %d task runs failed\n", failed)
		return 1
	}
	return 0
}

func parseTiers(s string) ([]string, error) {
	var out []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !slices.Contains(probe.Tiers, t) {
			return nil, fmt.Errorf("unknown tier %q (want one of %s)", t, strings.Join(probe.Tiers, ","))
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("no tiers given")
	}
	return out, nil
}

// dryTurn is one planned invocation, as a dry run prints it.
type dryTurn struct {
	N    int
	Raw  string
	Argv []string
}

// dry prints, for every task, tier and turn, the exact host command line the
// run would execute, the environment it would get with secrets redacted, and
// the files written into the throwaway home. The workdir and session id are
// placeholders, made per task at run time.
func dry(w io.Writer, notes, env, files []string, tasks []probe.Task, tiers []string, plan func(probe.Task, string) ([]dryTurn, error)) int {
	for _, n := range notes {
		fmt.Fprintf(w, "# note: %s\n", n)
	}
	fmt.Fprintf(w, "# environment (nothing else from the operator's shell is passed):\n#   %s\n", strings.Join(env, "\n#   "))
	if len(files) > 0 {
		fmt.Fprintf(w, "# files written into the throwaway home:\n#   %s\n", strings.Join(files, "\n#   "))
	}
	for _, t := range tasks {
		for _, tier := range tiers {
			turns, err := plan(t, tier)
			if err != nil {
				fmt.Fprintln(w, "# error:", err)
				return 1
			}
			for _, turn := range turns {
				fmt.Fprintf(w, "# %s %s turn %d -> %s\n%s\n", t.ID, tier, turn.N, turn.Raw, claudecode.ShellQuote(turn.Argv))
			}
		}
	}
	return 0
}
