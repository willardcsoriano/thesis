// Command agentprobe runs the agent command study (docs/agent-command-study.md):
// it gives each task of a suite to a real agent and records every command the
// agent runs.
//
//	agentprobe claude -tasks FILE [-tiers technical,novice] [-limit N] [-model M] [-out runs/] [-dry-run]
//
// The claude driver runs Claude Code headless inside bubblewrap with
// permissions skipped, authenticated only by CLAUDE_CODE_OAUTH_TOKEN from the
// operator's environment. Records go to OUT/<run-id>/events.jsonl and each
// turn's raw stream to OUT/<run-id>/raw/.
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
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

const usage = `usage: agentprobe claude -tasks FILE [flags]

Drivers:
  claude   Claude Code, sandboxed (see internal/probe/claudecode/NOTES.md)
`

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "claude" {
		fmt.Fprint(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet("agentprobe claude", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tasksPath := fs.String("tasks", "", "task suite (JSONL, probe.Task per line)")
	tiersFlag := fs.String("tiers", strings.Join(probe.Tiers, ","), "comma-separated tiers to run")
	limit := fs.Int("limit", 0, "run only the first N tasks (0: all)")
	model := fs.String("model", "", "model alias or id passed to --model (empty: the CLI's default)")
	maxTurns := fs.Int("max-turns", 25, "agentic turns per CLI invocation, passed to --max-turns (0: no limit)")
	timeout := fs.Duration("timeout", 15*time.Minute, "wall-clock limit per CLI invocation")
	out := fs.String("out", "runs", "output directory; the run goes in OUT/<run-id>")
	runID := fs.String("run-id", "", "run id (default: UTC timestamp plus -claude)")
	useStrace := fs.Bool("strace", false, "capture every execve with strace (records argv[0] list per turn)")
	dryRun := fs.Bool("dry-run", false, "print the sandbox command lines and exit without running anything")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *tasksPath == "" {
		fmt.Fprintln(stderr, "agentprobe: -tasks is required")
		return 2
	}
	tiers, err := parseTiers(*tiersFlag)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 2
	}
	tasks, err := probe.LoadTasks(*tasksPath)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	if *limit > 0 && *limit < len(tasks) {
		tasks = tasks[:*limit]
	}
	token := getenv(claudecode.TokenEnv)
	if token == "" && !*dryRun {
		fmt.Fprint(stderr, tokenHelp)
		return 2
	}

	bw := oracle.Bwrap()
	if bw == "" {
		fmt.Fprintln(stderr, "agentprobe: bubblewrap (bwrap) is missing or cannot create namespaces; the CLI never runs unsandboxed")
		return 1
	}
	install, err := claudecode.ResolveInstall()
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	sb := claudecode.Sandbox{Bwrap: bw, Install: install}
	if *useStrace {
		if sb.Strace, err = exec.LookPath("strace"); err != nil {
			fmt.Fprintln(stderr, "agentprobe: -strace given but strace is not on PATH")
			return 1
		}
	}
	if *runID == "" {
		*runID = time.Now().UTC().Format("20060102T150405Z") + "-claude"
	}
	r := claudecode.Runner{Sandbox: sb, Token: token, Model: *model, MaxTurns: *maxTurns, TurnTimeout: *timeout,
		RunID: *runID, Out: filepath.Join(*out, *runID), Log: stderr}

	if *dryRun {
		return dry(r, tasks, tiers, token != "", stdout)
	}

	if err := os.MkdirAll(r.Out, 0o755); err != nil {
		fmt.Fprintln(stderr, "agentprobe:", err)
		return 1
	}
	eventsPath := filepath.Join(r.Out, "events.jsonl")
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
			recs, err := r.RunTask(ctx, t, tier)
			for _, rec := range recs {
				if aerr := probe.Append(events, rec); aerr != nil {
					fmt.Fprintln(stderr, "agentprobe:", aerr)
					return 1
				}
			}
			if err != nil {
				failed++
				fmt.Fprintln(stderr, "agentprobe:", err)
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

const tokenHelp = `agentprobe: ` + claudecode.TokenEnv + ` is not set, and the CLI is never given any other credential.
To run:
  1. In a terminal, run:  claude setup-token
     It opens a browser, then prints a one-year token. It saves the token nowhere.
  2. In the shell you will run agentprobe from, run:  export ` + claudecode.TokenEnv + `=<the token>
     Keep it out of files, the repository and shell history files you share.
  3. Run this agentprobe command again.
`

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

// dry prints, for every task, tier and turn, the exact host command line the
// run would execute and the environment it would get, with the token
// redacted. The workdir and session id are placeholders, made per task at run
// time.
func dry(r claudecode.Runner, tasks []probe.Task, tiers []string, haveToken bool, w io.Writer) int {
	env := claudecode.Redacted(r.Sandbox.Env("unset"))
	if !haveToken {
		fmt.Fprintf(w, "# note: %s is not set; a real run would refuse to start\n", claudecode.TokenEnv)
	}
	fmt.Fprintf(w, "# environment (nothing else from the operator's shell is passed):\n#   %s\n", strings.Join(env, "\n#   "))
	wd := claudecode.Workdir{Root: "<workdir>"}
	for _, t := range tasks {
		for _, tier := range tiers {
			turns, err := r.Plan(t, tier, wd, "<session-uuid>")
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
