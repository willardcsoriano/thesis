package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"synapseos/internal/oracle"
	"synapseos/internal/probe"
)

// score reads RUN/events.jsonl, scores every record against its task's fixture,
// and writes RUN/scored.jsonl and RUN/report.txt. It never calls a model and
// never runs a captured command, so an old run can be re-scored at any time.
func score(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agentprobe score", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tasksPath := fs.String("tasks", "", "the task suite the run used (fixtures come from it)")
	runDir := fs.String("run", "", "run directory holding events.jsonl")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tasksPath == "" || *runDir == "" {
		fmt.Fprintln(stderr, "agentprobe score: -tasks and -run are required")
		return 2
	}
	tasks, err := probe.LoadTasks(*tasksPath)
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
	for i := range recs {
		fx, ok := fixtures[recs[i].Task]
		if !ok {
			// Scoring against the wrong state would give a verdict about a
			// different folder; refuse instead.
			fmt.Fprintf(stderr, "agentprobe score: task %s is not in %s\n", recs[i].Task, *tasksPath)
			return 1
		}
		if err := probe.Score(ctx, &recs[i], fx); err != nil {
			fmt.Fprintf(stderr, "agentprobe score: %s step %d: %v\n", recs[i].Task, recs[i].Step, err)
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

func readRecords(path string) ([]probe.Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var recs []probe.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var r probe.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		recs = append(recs, r)
	}
	return recs, sc.Err()
}
