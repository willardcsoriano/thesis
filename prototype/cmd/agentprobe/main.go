// Command agentprobe runs the agent command study (docs/agent-command-study.md):
// it gives each task of a suite to a real agent and records every command the
// agent runs.
//
//	agentprobe claude -tasks FILE [-tiers technical,novice] [-limit N] [-model M] [-out runs/] [-net isolated|host] [-dry-run]
//	agentprobe gemini -tasks FILE [-oauth-creds FILE] [same flags]
//	agentprobe score -run runs/<run-id> [-tasks FILE]
//
// The claude driver runs Claude Code headless inside bubblewrap with
// permissions skipped, authenticated only by CLAUDE_CODE_OAUTH_TOKEN from the
// operator's environment. The gemini driver runs the Gemini CLI the same way
// with approvals off, authenticated by a copy of one OAuth credential file the
// operator names (the personal subscription) or by GEMINI_API_KEY. Records go
// to OUT/<run-id>/events.jsonl, each turn's raw stream to OUT/<run-id>/raw/,
// and the tasks as they ran to OUT/<run-id>/tasks.jsonl.
//
// Both sandboxes get their own network namespace by default (-net isolated,
// claudecode.Network): the CLI reaches its API over the internet, but not
// the host's loopback, the host's other addresses or the LAN. It needs pasta
// (Debian package passt); without it agentprobe refuses to start rather
// than fall back. -net host shares the host's network instead.
package main

import (
	"bytes"
	"context"
	"encoding/json"
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
       agentprobe score -run DIR [-tasks FILE]

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
	tasksPath, tiers, model, out, runID, net string
	limit, maxTurns                          int
	timeout                                  time.Duration
	strace, dryRun                           bool
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
	fs.StringVar(&c.net, "net", string(claudecode.NetIsolated), "sandbox network: isolated (own namespace through pasta: internet yes, host loopback and LAN no) or host (the host's network, loopback services and LAN included)")
	return c
}

// setup is what every driver needs once its flags are parsed.
type setup struct {
	tasks   []probe.Task
	tiers   []string
	bwrap   string
	strace  string // empty unless -strace
	prlimit string
	net     claudecode.Network
	runID   string
	out     string // OUT/<run-id>
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

// host finds bubblewrap, prlimit, the network's tools and, if asked for,
// strace, and fixes the run id. The isolated network is also built once
// with nothing in it, except in a dry run, which runs nothing.
func (c *common) host(driver string, stderr io.Writer) (setup, int) {
	var s setup
	mode, err := claudecode.ParseNetMode(c.net)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return s, 2
	}
	// Input is refused before the host is examined, so an unsafe run id is
	// reported as such on every machine, sandbox or not.
	s.runID = c.runID
	if s.runID == "" {
		s.runID = time.Now().UTC().Format("20060102T150405Z") + "-" + driver
	}
	if err := probe.CheckID("run", s.runID); err != nil {
		// The run id names the run's folder under -out; "../x" would put it elsewhere.
		fmt.Fprintln(stderr, "agentprobe:", err)
		return s, 2
	}
	if s.bwrap = oracle.Bwrap(); s.bwrap == "" {
		fmt.Fprintln(stderr, "agentprobe: bubblewrap (bwrap) is missing or cannot create namespaces; the CLI never runs unsandboxed")
		return s, 1
	}
	if s.prlimit = claudecode.Prlimit(); s.prlimit == "" {
		fmt.Fprintln(stderr, "agentprobe: prlimit (util-linux) is missing or under a directory the sandbox hides; it caps the sandbox's processes")
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
	if s.net, err = claudecode.ResolveNetwork(mode); err == nil && !c.dryRun {
		err = s.net.Probe()
	}
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe: -net "+c.net+":", err)
		return s, 1
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
	if !c.dryRun {
		frozen, unfreeze, err := claudecode.Freeze(install.PackageDir)
		if err != nil {
			fmt.Fprintln(stderr, "agentprobe:", err)
			return 1
		}
		defer unfreeze()
		install.PackageDir = frozen
	}
	sb := claudecode.Sandbox{Bwrap: s.bwrap, Strace: s.strace, Prlimit: s.prlimit, Net: s.net, Install: install}
	r := claudecode.Runner{Sandbox: sb, Token: token, Model: c.model, MaxTurns: c.maxTurns, TurnTimeout: c.timeout,
		RunID: s.runID, Out: s.out, Log: stderr}

	if c.dryRun {
		notes := netNotes(s.net)
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
	// A refused login, or output that cannot be written, would repeat for every task.
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
	if !c.dryRun {
		frozen, unfreeze, err := claudecode.Freeze(install.PackageDir)
		if err != nil {
			fmt.Fprintln(stderr, "agentprobe:", err)
			return 1
		}
		defer unfreeze()
		install.PackageDir = frozen
	}
	sb := geminicli.Sandbox{Bwrap: s.bwrap, Strace: s.strace, Prlimit: s.prlimit, Net: s.net, Install: install}
	r := geminicli.Runner{Sandbox: sb, Auth: auth, Model: c.model, MaxTurns: c.maxTurns, Debug: *debug,
		TurnTimeout: c.timeout, RunID: s.runID, Out: s.out, Log: stderr}

	if c.dryRun {
		notes, files := netNotes(s.net), []string(nil)
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

// netNotes says in a dry run what the network chain does beyond its argv.
func netNotes(n claudecode.Network) []string {
	if !n.Isolated() {
		return []string{"-net host: the sandbox shares the host's network; the agent can reach services on the host's loopback (Ollama, dev servers) and the LAN"}
	}
	return []string{
		"-net isolated: pasta gives the sandbox its own network namespace; the ip rules below refuse private, link-local, CGNAT and multicast ranges and the host's own addresses, and resolv.conf names only " + claudecode.DNSForward + ", pasta's DNS forwarder",
		"the driver holds an exclusive lock on <workdir>/lifeline while each turn runs and releases it when it kills the turn; the namespace kills the sandbox once it can take the lock",
	}
}

// taskFunc runs one task on one tier.
type taskFunc func(ctx context.Context, t probe.Task, tier string) ([]probe.Record, error)

// Status is one line of OUT/status.jsonl: how one task run on one tier
// ended. events.jsonl holds the records; a task run is complete only when a
// status line says so. A line is written after the task's records, so a run
// killed outright leaves the task it was on without one.
type Status struct {
	Task    string `json:"task"`
	Tier    string `json:"tier"`
	Records int    `json:"records"`
	// Status is "complete", "incomplete" (a turn timed out or ended without
	// a result), "interrupted" (the operator stopped the run), "error", or
	// "fatal" (an error that ended the run).
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// runSuite is the run's own copy of the tasks it ran, under OUT/<run-id>.
const runSuite = "tasks.jsonl"

// writeSuite writes tasks one per line, refusing to replace a file.
func writeSuite(path string, tasks []probe.Task) error {
	var b bytes.Buffer
	for _, t := range tasks {
		line, err := json.Marshal(t)
		if err != nil {
			return err
		}
		b.Write(append(line, '\n'))
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(b.Bytes())
	return errors.Join(werr, f.Sync(), f.Close())
}

// runTasks runs every task on every tier, appending records to
// OUT/events.jsonl and one Status per task run to OUT/status.jsonl. stop,
// when set, says which errors end the run at once (a failure every later
// task would repeat); other errors are counted and the run goes on. An
// interrupt ends the run after the task in progress has been recorded.
func runTasks(ctx context.Context, runTask taskFunc, out string, tasks []probe.Task, tiers []string, stop func(error) bool, stderr io.Writer) int {
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	eventsPath, statusPath := filepath.Join(out, "events.jsonl"), filepath.Join(out, "status.jsonl")
	var files [2]*os.File
	for i, p := range []string{eventsPath, statusPath} {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			fmt.Fprintf(stderr, "agentprobe: %v (pick a new -run-id rather than mixing two runs)\n", err)
			return 1
		}
		defer f.Close()
		files[i] = f
	}
	events, status := files[0], files[1]
	// The suite as it ran, beside its records: a later edit to the suite
	// file would otherwise rescore this run against fixtures it never saw.
	if err := writeSuite(filepath.Join(out, runSuite), tasks); err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}

	failed, incomplete := 0, 0
	for _, t := range tasks {
		for _, tier := range tiers {
			if ctx.Err() != nil {
				fmt.Fprintf(stderr, "agentprobe: interrupted; completed task runs are listed in %s\n", statusPath)
				return 1
			}
			fmt.Fprintf(stderr, "%s %s ...\n", t.ID, tier)
			recs, err := runTask(ctx, t, tier)
			for _, rec := range recs {
				var line bytes.Buffer
				if aerr := probe.Append(&line, rec); aerr != nil {
					fmt.Fprintln(stderr, "agentprobe:", aerr)
					return 1
				}
				if aerr := appendWhole(events, line.Bytes()); aerr != nil {
					fmt.Fprintf(stderr, "agentprobe: %v; records so far in %s\n", aerr, eventsPath)
					return 1
				}
			}
			st := Status{Task: t.ID, Tier: tier, Records: len(recs), Status: "complete"}
			fatal := false
			switch {
			case err == nil:
			case errors.Is(err, probe.ErrInterrupted) || ctx.Err() != nil:
				st.Status = "interrupted"
			case stop != nil && stop(err):
				st.Status, fatal = "fatal", true
			case errors.Is(err, probe.ErrIncomplete):
				st.Status = "incomplete"
			default:
				st.Status = "error"
			}
			if err != nil {
				st.Error = err.Error()
			}
			b, _ := json.Marshal(st) // plain strings and ints: cannot fail
			if serr := appendWhole(status, append(b, '\n')); serr != nil {
				fmt.Fprintln(stderr, "agentprobe:", serr)
				return 1
			}
			switch st.Status {
			case "incomplete":
				incomplete++
				fmt.Fprintln(stderr, "agentprobe:", err)
			case "error", "fatal":
				failed++
				fmt.Fprintln(stderr, "agentprobe:", err)
			}
			if fatal {
				fmt.Fprintf(stderr, "agentprobe: stopping the run; records so far in %s\n", eventsPath)
				return 1
			}
		}
	}
	if ctx.Err() != nil {
		fmt.Fprintf(stderr, "agentprobe: interrupted; completed task runs are listed in %s\n", statusPath)
		return 1
	}
	fmt.Fprintf(stderr, "records in %s, task status in %s\n", eventsPath, statusPath)
	if incomplete > 0 {
		fmt.Fprintf(stderr, "agentprobe: %d task runs did not finish (timed out or no result)\n", incomplete)
	}
	if failed > 0 {
		fmt.Fprintf(stderr, "agentprobe: %d task runs failed\n", failed)
		return 1
	}
	return 0
}

// appendWhole appends line to f so the file is never left holding part of
// it: a short write (a full disk) is cut back to where the line began, so
// the file stays whole JSON lines.
func appendWhole(f *os.File, line []byte) error {
	at, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		if terr := f.Truncate(at); terr != nil {
			return fmt.Errorf("%w (and cutting the partial line failed: %v)", err, terr)
		}
		return err
	}
	return nil
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
		if slices.Contains(out, t) {
			// It would run every task twice on that tier, into one record set.
			return nil, fmt.Errorf("tier %q given twice", t)
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
