package main

// Brakes: rules by which the loop stops or qualifies a run on its own, for the
// judgement a small model lacks. 1.5B proposes commands that are mostly right
// but cannot tell that a task is finished: after `wc -l log.txt` printed the
// answer it proposed the same command again, and it renamed a file and then
// renamed it back (docs/experiments.md, "Harnessing the 1.5B model"). A brake
// never makes a wrong command right, and never runs anything; it stops a run
// that has stopped making progress, or keeps an unsupported claim from being
// stated as fact. The stop on a repeated success is in runLoop.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"synapseos/internal/effects"
	"synapseos/internal/gate"
)

// move is one rename or move a step made: the data at from now lives at to.
type move struct{ from, to string }

// movesOf returns the moves the analysis predicted for a command, nil when it
// predicted none or could not be read.
func movesOf(d *gate.Decision) []move {
	if d == nil || d.Analysis == nil {
		return nil
	}
	var ms []move
	for _, e := range d.Analysis.Effects {
		if e.Kind == effects.Remove && e.MovedTo != "" {
			ms = append(ms, move{e.Path, e.MovedTo})
		}
	}
	return ms
}

// reversesLast reports whether the command the analysis describes in d does
// nothing but move back what the previous step moved: every effect is a read,
// one of its moves, or the file a move creates, and every move is the inverse of one
// in prev. The analysis knows this before anything runs, because a move is a
// removal whose data survives at another path. An analysis with anything it
// could not resolve, or any other effect, is not a reversal.
func reversesLast(prev []move, d *gate.Decision) bool {
	if len(prev) == 0 || d == nil || d.Analysis == nil || len(d.Analysis.Issues) > 0 || len(d.Analysis.States) > 0 {
		return false
	}
	ms := movesOf(d)
	if len(ms) == 0 {
		return false
	}
	created := map[string]bool{}
	for _, m := range ms {
		created[m.to] = true
	}
	for _, e := range d.Analysis.Effects {
		switch {
		case e.Kind == effects.Read:
		case e.Kind == effects.Remove && e.MovedTo != "":
		case e.Kind == effects.Create && created[e.Path]:
		default:
			return false
		}
	}
	undone := map[move]bool{}
	for _, p := range prev {
		undone[move{p.to, p.from}] = true
	}
	for _, m := range ms {
		if !undone[m] {
			return false
		}
	}
	return true
}

// claimPattern finds what a reply could state that the output must back: a
// number, or a file name with an extension.
var (
	numberPattern = regexp.MustCompile(`\d+(?:\.\d+)?`)
	filePattern   = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_.-]*\.[A-Za-z0-9]{1,8}\b`)
)

// unsupportedClaims returns the numbers and file names in a reply that nothing
// the task said or the steps showed contains: not the task, a command, its
// output or its errors, nor a path the analysis said it moved. A count of the
// lines a step printed, or of the files it moved, backs a number too, since
// "2 files" after an ls of two names is read off the output, not made up. It finds an invented figure or file, not a wrong reading of a right one:
// "no warnings" after a search for the wrong word states no number.
func unsupportedClaims(reply, task string, history []loopStep) []string {
	var src strings.Builder
	src.WriteString(task)
	counts := map[string]bool{}
	for i, h := range history {
		// The reply is written from the steps numbered, with their exit codes
		// (answerFromHistory), so "step 2" and "exit code 0" are read off what
		// it was shown. On the first box round these were most of what was
		// withheld.
		fmt.Fprintf(&src, "\nstep %d exit code %d", i+1, h.result.ExitCode)
		src.WriteString("\n" + h.command + "\n" + h.result.Stdout + "\n" + h.result.Stderr)
		if out := strings.TrimSpace(h.result.Stdout); out != "" {
			counts[strconv.Itoa(len(strings.Split(out, "\n")))] = true
		}
		// What a step moved is known from the analysis even when it printed
		// nothing: "moved a.pdf and b.pdf, 2 files" after mv *.pdf PDFs/.
		if len(h.moves) > 0 {
			counts[strconv.Itoa(len(h.moves))] = true
			for _, m := range h.moves {
				src.WriteString("\n" + m.from + "\n" + m.to)
			}
		}
	}
	seen := src.String()
	numbers := map[string]bool{}
	for _, n := range numberPattern.FindAllString(seen, -1) {
		numbers[n] = true
	}
	var missing []string
	done := map[string]bool{}
	for _, f := range filePattern.FindAllString(reply, -1) {
		f = strings.TrimRight(f, ".")
		if numberPattern.MatchString(f) && numberPattern.FindString(f) == f {
			continue // a decimal such as 1.5 is a number, checked below
		}
		if !done[f] && !strings.Contains(seen, f) {
			missing = append(missing, f)
		}
		done[f] = true
	}
	for _, n := range numberPattern.FindAllString(reply, -1) {
		if !done[n] && !numbers[n] && !counts[n] {
			missing = append(missing, n)
		}
		done[n] = true
	}
	return missing
}

// withheldReply is told instead of a reply that stated what nothing showed.
// It says what kind of claim it was, not the claim: a notice is part of what
// the person was told, and repeating "2" there told them the number after
// all, and scored it as told in Layers 7 and 9 (ship review, 2026-10-27). The
// reply itself is kept as a note.
func withheldReply(claims []string) string {
	kind := "a number"
	for _, c := range claims {
		if filePattern.MatchString(c) && !(numberPattern.FindString(c) == c) {
			kind = "a file name"
			if len(claims) > 1 {
				kind = "a file name and other details"
			}
			break
		}
	}
	if kind == "a number" && len(claims) > 1 {
		kind = "numbers"
	}
	return fmt.Sprintf("I've left out my summary, because it mentioned %s that none of the commands showed. What the commands printed is above.", kind)
}

// foundNothing reports whether the last step looked for something and found
// nothing: it changed no file, and exited 1, which is how grep says no line
// matched, or printed nothing. A reply built on it ("there are no warnings")
// may rest on a search for the wrong word, and the person should know which
// search it was.
func foundNothing(history []loopStep) (string, bool) {
	if len(history) == 0 {
		return "", false
	}
	last := history[len(history)-1]
	if !last.readOnly || last.result.TimedOut {
		return "", false
	}
	empty := strings.TrimSpace(last.result.Stdout) == ""
	if last.result.ExitCode == 1 || (last.result.ExitCode == 0 && empty) {
		return last.command, true
	}
	return "", false
}

// foundNothingNote is told after the reply when foundNothing holds.
func foundNothingNote(cmd string) string {
	return "The last command found nothing: " + cmd + "\nIf you expected a result, it may have looked for the wrong name or word."
}

// changesNothing reports whether the analysis found a command only reads: no
// effect but reading, nothing unresolved, no package or service change.
// effects.Analysis.ReadOnly is stricter, counting a read as an effect.
func changesNothing(d *gate.Decision) bool {
	if d == nil || d.Analysis == nil || len(d.Analysis.Issues) > 0 || len(d.Analysis.States) > 0 {
		return false
	}
	for _, e := range d.Analysis.Effects {
		if e.Kind != effects.Read {
			return false
		}
	}
	return true
}
