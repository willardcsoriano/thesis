package main

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// exitUsage is the exit code for a command line synapse does not accept. It
// is distinct from 1 (the task or the backend failed) so a script or an agent
// can tell "I called it wrong" from "it ran and something went wrong".
const exitUsage = 2

// mode is what one invocation asked synapse to do.
type mode int

const (
	modeHelp mode = iota
	modeVersion
	modeRun
	modeREPL
	modeScratch
	modeTUI
	modeTUIScratch
	modeUndo
	modeDemo
)

// invocation is a parsed command line.
type invocation struct {
	mode mode
	task string // set only for modeRun
	json bool   // run --json: events as JSON Lines on stdout
}

// usageError is a command line synapse refuses. It never reaches the model.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// parseArgs turns the command line into exactly one invocation, or refuses it.
//
// The rule it exists to enforce: free text becomes a task only after an
// explicit `run` or `--`. Before this parser, every command line that was not
// an exact subcommand was joined into a task and sent to the model, so
// `synapse --help`, a mistyped subcommand, or `synapse undo --dry-run` became
// instructions the loop could turn into a command and, if the analysis judged
// it recoverable, run without asking. A command line is now either one this
// function recognises or a usage error, and a usage error never contacts the
// model.
func parseArgs(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{}, &usageError{"no command given"}
	}
	cmd, rest := args[0], args[1:]
	noArgs := func(m mode) (invocation, error) {
		if len(rest) > 0 {
			return invocation{}, &usageError{fmt.Sprintf("%s takes no arguments, got %q", cmd, strings.Join(rest, " "))}
		}
		return invocation{mode: m}, nil
	}
	switch cmd {
	case "help", "-h", "--help":
		return noArgs(modeHelp)
	case "version", "--version":
		return noArgs(modeVersion)
	case "repl":
		return noArgs(modeREPL)
	case "scratch":
		return noArgs(modeScratch)
	case "undo":
		return noArgs(modeUndo)
	case "demo":
		return noArgs(modeDemo)
	case "tui":
		switch {
		case len(rest) == 0:
			return invocation{mode: modeTUI}, nil
		case len(rest) == 1 && rest[0] == "scratch":
			return invocation{mode: modeTUIScratch}, nil
		}
		return invocation{}, &usageError{fmt.Sprintf("tui accepts only \"scratch\", got %q", strings.Join(rest, " "))}
	case "run":
		return parseTask(rest, "run")
	case "--":
		return taskFrom(rest, "--")
	}
	if strings.HasPrefix(cmd, "-") {
		return invocation{}, &usageError{fmt.Sprintf("unknown flag %q", cmd)}
	}
	return invocation{}, &usageError{fmt.Sprintf("unknown command %q (to give a task, use: synapse run %q)", cmd, strings.Join(args, " "))}
}

// parseTask reads `run`'s arguments: its flags, then the task. Flags come
// before the task and only --json exists; any other argument starting with "-"
// before the task is refused rather than read as words, so a flag added later
// cannot change the meaning of a command line accepted today. A task that
// itself starts with "-" goes after "--".
func parseTask(args []string, via string) (invocation, error) {
	asJSON := false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "--":
			inv, err := taskFrom(args[1:], via)
			inv.json = asJSON
			return inv, err
		case "--json":
			if asJSON {
				return invocation{}, &usageError{"--json given twice"}
			}
			asJSON = true
			args = args[1:]
		default:
			return invocation{}, &usageError{fmt.Sprintf("unknown flag %q for run (a task that starts with \"-\" goes after --)", args[0])}
		}
	}
	inv, err := taskFrom(args, via)
	inv.json = asJSON
	return inv, err
}

// maxTaskBytes bounds a task's text. A 120 KB task was sent whole, timed out
// or came back as nonsense (finding atk4-14); no spoken request is that long.
const maxTaskBytes = 16 << 10

func taskFrom(words []string, via string) (invocation, error) {
	task := strings.TrimSpace(strings.Join(words, " "))
	if task == "" {
		return invocation{}, &usageError{fmt.Sprintf("%s needs a task, e.g. synapse run \"list the files here\"", via)}
	}
	if len(task) > maxTaskBytes {
		return invocation{}, &usageError{fmt.Sprintf("the task is %d KiB; the limit is %d KiB, so describe it more briefly", len(task)>>10, maxTaskBytes>>10)}
	}
	return invocation{mode: modeRun, task: task}, nil
}

// isUsageError reports whether err is a refused command line.
func isUsageError(err error) bool {
	var u *usageError
	return errors.As(err, &u)
}

const usageText = `synapse turns a request in plain language into shell commands, runs the
ones it can undo, and asks before anything it cannot.

Usage:
  synapse run [--json] <task...>   run one task, e.g. synapse run "list the files here"
  synapse -- <task...>             same, for a task that starts with "-"
  synapse repl                     read tasks line by line, with memory of earlier ones
  synapse scratch                  read tasks line by line, without memory
  synapse tui [scratch]            the full-screen interface (scratch: without memory)
  synapse undo                     undo the most recent recorded command
  synapse demo                     propose commands for a built-in sample suite; runs nothing
  synapse help                     this text
  synapse version                  the build's version

run reads confirmations from stdin; answer y to approve. Anything else, or a
closed stdin, declines, so nothing unrecoverable runs unattended. Prompts and
diagnostics go to stderr; stdout carries only results.

--json writes one JSON object per line to stdout: command, result, answer,
notice, problem, note, and approval events, then a final
{"type":"end","outcome":...,"exit_code":...}.

Exit codes:
  0    done
  1    failed: a command could not run or kept failing, or undo failed
  2    the command line or environment was not accepted; nothing ran
  3    a confirmation was declined
  4    the request cannot be done with a shell command
  5    the step limit was reached before the task finished
  6    the model backend could not be reached or did not answer
  7    done, but a step could not be recorded for undo, so it cannot be undone
  129  stopped by a hang-up (SIGHUP)
  130  interrupted (Ctrl+C)
  143  stopped by SIGTERM, as timeout and supervisors send
`

// printUsage writes the help text. The environment section is printed from
// envVars, the list checkEnv validates against, so the help cannot name a
// variable or default the program does not have.
func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
	fmt.Fprintln(w)
	printEnvHelp(w)
}

// versionString reports the build from Go's embedded build information, so the
// version is never a literal that drifts from the source it was built from.
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "synapse (unknown build)"
	}
	v := info.Main.Version
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "+dirty"
			}
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev == "" {
		return "synapse " + v
	}
	return fmt.Sprintf("synapse %s (%s%s)", v, rev, dirty)
}
