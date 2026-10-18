package probe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Tiers lists every tier in reporting order.
var Tiers = []string{TierTechnical, TierInterface, TierPlain, TierNovice}

// LoadTasks reads a JSONL task suite and refuses one that is malformed:
// a duplicate id, an unknown partition, or a missing tier prompt stops the
// load rather than silently shrinking a run.
func LoadTasks(path string) ([]Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tasks []Task
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var t Task
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if err := t.validate(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("%s:%d: duplicate task id %q", path, line, t.ID)
		}
		seen[t.ID] = true
		tasks = append(tasks, t)
	}
	return tasks, sc.Err()
}

func (t Task) validate() error {
	if t.ID == "" {
		return fmt.Errorf("task without id")
	}
	switch t.Partition {
	case PartitionNL2Bash, PartitionEveryday, PartitionPersona:
	default:
		return fmt.Errorf("task %s: unknown partition %q", t.ID, t.Partition)
	}
	for _, tier := range Tiers {
		if t.Prompts[tier] == "" {
			return fmt.Errorf("task %s: no %s prompt", t.ID, tier)
		}
	}
	if t.Source == "" {
		return fmt.Errorf("task %s: no source", t.ID)
	}
	return nil
}
