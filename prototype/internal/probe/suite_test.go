package probe

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"synapseos/internal/oracle"
)

// suitePath is the committed task suite, relative to this package.
const suitePath = "../../agentprobe/tasks.jsonl"

// noviceFeatures is the fixed tag vocabulary of the novice tier
// (docs/agent-command-study.md, "Novice tier"). A tag outside it would be
// reported as its own feature and silently split a measurement.
var noviceFeatures = map[string]bool{
	"vague-target": true, "wrong-model": true, "pronoun": true, "multi-intent": true,
	"question-as-action": true, "typo": true, "taglish": true, "underspecified": true, "urgent": true,
}

// categories are the spec's task categories, as slugs.
var categories = map[string]bool{
	"file-management": true, "search": true, "archives-backups": true, "data-wrangling": true,
	"text-editing": true, "packages-services": true, "system-info": true, "multi-step": true,
}

var nl2bashSource = regexp.MustCompile(`^nl2bash:[0-9]+$`)

func TestSuiteLoadsAndIsWellFormed(t *testing.T) {
	tasks, err := LoadTasks(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	per := map[string]int{}
	for _, task := range tasks {
		per[task.Partition]++
		if !categories[task.Category] {
			t.Errorf("%s: unknown category %q", task.ID, task.Category)
		}
		if len(task.Features) == 0 {
			t.Errorf("%s: novice prompt carries no feature tags", task.ID)
		}
		for _, f := range task.Features {
			if !noviceFeatures[f] {
				t.Errorf("%s: feature %q is not in the vocabulary", task.ID, f)
			}
		}
		switch task.Partition {
		case PartitionPersona:
			if len(task.FollowUps) == 0 {
				t.Errorf("%s: persona task without follow-ups", task.ID)
			}
			for i, u := range task.FollowUps {
				if strings.TrimSpace(u) == "" {
					t.Errorf("%s: follow-up %d is empty", task.ID, i)
				}
			}
		case PartitionNL2Bash:
			if !nl2bashSource.MatchString(task.Source) {
				t.Errorf("%s: partition S source %q is not nl2bash:N", task.ID, task.Source)
			}
			if len(task.FollowUps) != 0 {
				t.Errorf("%s: only persona tasks have follow-ups", task.ID)
			}
		default:
			if len(task.FollowUps) != 0 {
				t.Errorf("%s: only persona tasks have follow-ups", task.ID)
			}
		}
	}
	for _, p := range []string{PartitionNL2Bash, PartitionEveryday, PartitionPersona} {
		if per[p] == 0 {
			t.Errorf("partition %s is empty", p)
		}
	}
	t.Logf("tasks per partition: %v", per)
}

func TestSuiteFixturesMaterializeInsideRoot(t *testing.T) {
	tasks, err := LoadTasks(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if len(task.Fixture) == 0 {
			t.Errorf("%s: empty fixture", task.ID)
			continue
		}
		escapes := false
		for _, e := range task.Fixture {
			if !filepath.IsLocal(e.Path) {
				t.Errorf("%s: fixture path %q is not relative to the root", task.ID, e.Path)
				escapes = true
			}
			switch e.Kind {
			case "file", "dir":
			case "link":
				if filepath.IsAbs(e.Target) || !filepath.IsLocal(filepath.Join(filepath.Dir(e.Path), e.Target)) {
					t.Errorf("%s: link %q points outside the root (%q)", task.ID, e.Path, e.Target)
					escapes = true
				}
			default:
				t.Errorf("%s: fixture entry %q has unknown kind %q", task.ID, e.Path, e.Kind)
			}
		}
		if escapes {
			continue // never write outside the temp dir
		}
		root := t.TempDir()
		if err := oracle.Materialize(root, task.Fixture); err != nil {
			t.Errorf("%s: fixture does not materialize: %v", task.ID, err)
			continue
		}
		snap, err := oracle.Observe(root)
		if err != nil {
			t.Errorf("%s: observe: %v", task.ID, err)
			continue
		}
		for _, e := range task.Fixture {
			if _, ok := snap[filepath.Clean(e.Path)]; !ok {
				t.Errorf("%s: fixture entry %q missing after materialize", task.ID, e.Path)
			}
		}
	}
}
