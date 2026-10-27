package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
)

// score reads RUN/events.jsonl, scores every record, and writes
// RUN/scored.jsonl and RUN/report.txt. It never calls a model.
//
// By default each task run is replayed from its fixture in the host-hiding
// sandbox and every shell command is analysed against the state the agent's
// earlier steps left, from the directory the agent was in
// (probe.ScoreReplayed). The captured commands run only inside that sandbox,
// never on the host. -initial-state instead analyses every command against
// the task's starting fixture from the root, the scorer's old behaviour,
// which misjudges any step that depends on an earlier one; those verdicts are
// marked analysis_state "initial" and kept out of the headline counts.
func score(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agentprobe score", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tasksPath := fs.String("tasks", "", "the task suite the run used, for a run older than its own copy (RUN/tasks.jsonl)")
	runDir := fs.String("run", "", "run directory holding events.jsonl")
	initial := fs.Bool("initial-state", false, "LEGACY: analyse every command against the task's starting fixture instead of the replayed pre-step state (verdicts marked \"initial\", kept out of the headline)")
	jobs := fs.Int("j", runtime.NumCPU(), "task runs replayed at once")
	stepLimit := fs.Duration("step-limit", probe.DefaultStepLimit, "time limit for one replayed step")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runDir == "" {
		fmt.Fprintln(stderr, "agentprobe score: -run is required")
		return 2
	}
	if *jobs < 1 {
		*jobs = 1
	}
	path, err := suiteFor(*runDir, *tasksPath)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 2
	}
	*tasksPath = path
	tasks, err := probe.LoadTasks(path)
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 1
	}
	fixtures := map[string][]oracle.Entry{}
	for _, t := range tasks {
		fixtures[t.ID] = t.Fixture
	}
	recs, err := readRecords(filepath.Join(*runDir, "events.jsonl"))
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 1
	}
	if err := applyStatus(recs, filepath.Join(*runDir, "status.jsonl")); err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 1
	}
	for _, r := range recs {
		if _, ok := fixtures[r.Task]; !ok {
			// Scoring against the wrong state would give a verdict about a
			// different folder; refuse instead.
			fmt.Fprintf(stderr, "agentprobe score: task %s is not in %s\n", r.Task, *tasksPath)
			return 1
		}
	}

	if *initial {
		for i := range recs {
			if err := probe.Score(ctx, &recs[i], fixtures[recs[i].Task]); err != nil {
				fmt.Fprintf(stderr, "agentprobe score: %s step %d: %v\n", recs[i].Task, recs[i].Step, err)
				return 1
			}
		}
	} else {
		bwrap := oracle.Bwrap()
		if bwrap == "" {
			// No silent fallback to the starting fixture: that is the flaw
			// the replay exists to remove.
			fmt.Fprintln(stderr, "agentprobe score: replaying needs bubblewrap (bwrap), which is missing or cannot create namespaces; -initial-state scores against the starting fixture instead, labelled as such")
			return 1
		}
		if err := replayAll(ctx, recs, fixtures, probe.ReplayOptions{Bwrap: bwrap, StepLimit: *stepLimit}, *jobs); err != nil {
			fmt.Fprintln(stderr, "agentprobe score:", err)
			return 1
		}
	}

	scored, err := os.Create(filepath.Join(*runDir, "scored.jsonl"))
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 1
	}
	defer scored.Close()
	for _, r := range recs {
		if err := probe.Append(scored, r); err != nil {
			fmt.Fprintln(stderr, "agentprobe score:", err)
			return 1
		}
	}
	report, err := os.Create(filepath.Join(*runDir, "report.txt"))
	if err != nil {
		fmt.Fprintln(stderr, "agentprobe score:", err)
		return 1
	}
	defer report.Close()
	probe.WriteReport(io.MultiWriter(report, stdout), recs)
	return 0
}

// replayAll replays every task run of recs, jobs at a time, writing the
// scored records back in place. Records keep their order in recs.
func replayAll(ctx context.Context, recs []probe.Record, fixtures map[string][]oracle.Entry, opt probe.ReplayOptions, jobs int) error {
	groups := probe.RunGroups(recs)
	work := make(chan int)
	errs := make([]error, len(groups))
	var wg sync.WaitGroup
	for range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range work {
				idx := groups[g]
				run := make([]probe.Record, len(idx))
				for k, i := range idx {
					run[k] = recs[i]
				}
				if err := probe.ScoreReplayed(ctx, run, fixtures[run[0].Task], opt); err != nil {
					errs[g] = fmt.Errorf("%s %s: %w", run[0].Task, run[0].Tier, err)
					continue
				}
				for k, i := range idx {
					recs[i] = run[k] // each index belongs to one group only
				}
			}
		}()
	}
	for g := range groups {
		work <- g
	}
	close(work)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// readRecords reads every record of events.jsonl. Lines have no length limit:
// capture keeps an agent's command whole, however long, so scoring must too
// (open-problems row 46).
func readRecords(path string) ([]probe.Record, error) {
	var recs []probe.Record
	err := eachLine(path, func(line int, b []byte) error {
		var r probe.Record
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
		recs = append(recs, r)
		return nil
	})
	return recs, err
}

// applyStatus marks each record with how its task run ended, from the run's
// status.jsonl, so the report keeps incomplete runs out of the headline. A run
// with no status.jsonl predates it and is left unmarked; a task run missing
// from an existing status.jsonl was cut off and is marked probe.RunMissing.
// runTasks writes one line per task run, once, into a file it creates, so a
// second line for the same task and tier means the file was edited or two
// runs were mixed: an error, rather than letting either line win.
func applyStatus(recs []probe.Record, path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	type key struct{ task, tier string }
	status := map[key]string{}
	err := eachLine(path, func(line int, b []byte) error {
		var st Status
		if err := json.Unmarshal(b, &st); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if st.Status == "" {
			return fmt.Errorf("%s:%d: a status line without a status", path, line)
		}
		k := key{st.Task, st.Tier}
		if _, dup := status[k]; dup {
			return fmt.Errorf("%s:%d: a second status line for task %s, tier %s; one run writes one per task run", path, line, st.Task, st.Tier)
		}
		status[k] = st.Status
		return nil
	})
	if err != nil {
		return err
	}
	for i := range recs {
		st, ok := status[key{recs[i].Task, recs[i].Tier}]
		if !ok {
			st = probe.RunMissing
		}
		recs[i].RunStatus = st
	}
	return nil
}

// eachLine calls fn for every non-empty line of path, with no line-length
// limit.
func eachLine(path string, fn func(line int, b []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for line := 1; ; line++ {
		b, rerr := r.ReadBytes('\n')
		if t := bytes.TrimSpace(b); len(t) > 0 {
			if err := fn(line, t); err != nil {
				return err
			}
		}
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// suiteFor is the suite a run is scored against: the copy the run wrote of
// the tasks it ran. -tasks is for a run made before runs kept one, and is
// refused when the run has its own and the two differ, because that would
// score the run against fixtures it never ran on.
func suiteFor(runDir, given string) (string, error) {
	own := filepath.Join(runDir, runSuite)
	ownTasks, err := probe.LoadTasks(own)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if given == "" {
			return "", fmt.Errorf("%s has no %s (a run older than its own copy); name the suite it ran with -tasks", runDir, runSuite)
		}
		return given, nil
	case err != nil:
		return "", err
	case given == "":
		return own, nil
	}
	givenTasks, err := probe.LoadTasks(given)
	if err != nil {
		return "", err
	}
	byID := map[string]probe.Task{}
	for _, t := range givenTasks {
		byID[t.ID] = t
	}
	for _, t := range ownTasks {
		g, ok := byID[t.ID]
		a, _ := json.Marshal(t)
		b, _ := json.Marshal(g)
		if !ok || !bytes.Equal(a, b) {
			return "", fmt.Errorf("task %s in %s differs from the run's own %s; omit -tasks to score the run as it ran", t.ID, given, own)
		}
	}
	return own, nil
}
