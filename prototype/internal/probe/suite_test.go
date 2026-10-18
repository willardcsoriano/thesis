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

// pathExtensions are the file extensions that make a slash-free token in a
// technical prompt count as a path. The list is the extensions the fixtures
// use; it is deliberately short, so ordinary words are never read as paths.
var pathExtensions = map[string]bool{
	"txt": true, "md": true, "pdf": true, "docx": true, "xlsx": true, "pptx": true,
	"csv": true, "json": true, "xml": true, "log": true, "conf": true, "bib": true,
	"sh": true, "py": true, "php": true, "jpg": true, "png": true, "mp3": true, "mp4": true,
	"tar": true, "gz": true, "tgz": true, "bz2": true, "zip": true, "deb": true, "iso": true,
}

// notFixturePaths lists, per task, path-like tokens of the technical prompt
// that are meant not to exist in the fixture. Each entry is either a path the
// command itself creates, or a token that only looks like a path.
var notFixturePaths = map[string][]string{
	// English "x/y" alternation in the NL2Bash description, not a path.
	"S-M044": {"normal/regular"},
	"S-M057": {"regular/normal"},
	"S-M128": {"regular/normal"},
	// Outputs: the command creates these.
	"E-number-photos":     {"trip_001.jpg", "trip_002.jpg"},
	"E-gather-pdfs":       {"PDFs"},
	"E-zip-thesis":        {"thesis.zip"},
	"E-merge-csv":         {"sales/all.csv"},
	"E-csv-to-json":       {"grades.json"},
	"E-backup-then-clean": {"myproject-backup.tar"},
	"P-move-then-panic":   {"Old"},
	// A sed expression, not a path.
	"E-draft-to-final": {"s/draft/final/g"},
}

// technicalPaths returns the path-like tokens of a technical prompt: tokens
// containing "/" or ending in a known extension. It skips, rather than
// guesses at, anything that is not a literal path: flags, globs, and shell
// expansions. Absolute paths are kept: a fixture holds only relative paths,
// so an absolute one names nothing the agent can act on, and that is exactly
// how the NL2Bash placeholders (/some/dir, /path/to/target-dir) slipped in.
func technicalPaths(prompt string) []string {
	var out []string
	for _, tok := range strings.Fields(prompt) {
		tok = strings.TrimLeft(tok, "\"'`(")
		tok = strings.TrimRight(tok, "\"'`),;:.")
		if tok == "" || tok == "." || tok == "/" {
			continue
		}
		if strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "~") ||
			strings.ContainsAny(tok, "*?[]{}$%=\\|<>&!^") {
			continue
		}
		ext := ""
		if i := strings.LastIndexByte(tok, '.'); i > 0 {
			ext = tok[i+1:]
		}
		if strings.Contains(tok, "/") || pathExtensions[ext] {
			out = append(out, filepath.Clean(tok))
		}
	}
	return out
}

func TestTechnicalPathsTokenizer(t *testing.T) {
	got := technicalPaths(`find . -name '*.pdf' -not -path './PDFs/*' -exec mv {} PDFs/ \; ` +
		`cp -r Documents/. /srv/app "$f" a.out grades.csv, './backups'`)
	want := []string{"PDFs", "Documents", "/srv/app", "grades.csv", "backups"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("technicalPaths = %q, want %q", got, want)
	}
}

func TestTechnicalPathsAreInFixture(t *testing.T) {
	tasks, err := LoadTasks(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		exists := map[string]bool{}
		for _, e := range task.Fixture {
			for p := filepath.Clean(e.Path); p != "." && p != "/"; p = filepath.Dir(p) {
				exists[p] = true
			}
		}
		allowed := map[string]bool{}
		for _, p := range notFixturePaths[task.ID] {
			allowed[p] = true
		}
		used := map[string]bool{}
		for _, p := range technicalPaths(task.Prompts[TierTechnical]) {
			switch {
			case allowed[p]:
				used[p] = true
			case !exists[p]:
				t.Errorf("%s: technical prompt names %q, which is not in the fixture", task.ID, p)
			}
		}
		for p := range allowed {
			if !used[p] {
				t.Errorf("%s: allowlisted path %q no longer appears in the technical prompt; remove it", task.ID, p)
			}
		}
	}
}

// filipinoFunctionWords are common Filipino function words. A task tagged
// taglish must use at least one in its novice text or follow-ups, so the tag
// means code-switching, not one borrowed word.
var filipinoFunctionWords = map[string]bool{
	"yung": true, "ang": true, "ng": true, "mga": true, "sa": true, "na": true, "pa": true,
	"mo": true, "ko": true, "naman": true, "lang": true, "kasi": true, "ba": true, "po": true,
}

var wordRE = regexp.MustCompile(`[a-z]+`)

func hasFilipinoFunctionWord(s string) bool {
	for _, w := range wordRE.FindAllString(strings.ToLower(s), -1) {
		// "paki-" is the polite-request prefix: pakidelete, pakiayos.
		if filipinoFunctionWords[w] || strings.HasPrefix(w, "paki") {
			return true
		}
	}
	return false
}

func TestTaglishTagHasFilipinoWords(t *testing.T) {
	if hasFilipinoFunctionWord("please sort my downloads by type") {
		t.Fatal("an English sentence was read as Taglish")
	}
	tasks, err := LoadTasks(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		tagged := false
		for _, f := range task.Features {
			tagged = tagged || f == "taglish"
		}
		if !tagged {
			continue
		}
		text := task.Prompts[TierNovice] + " " + strings.Join(task.FollowUps, " ")
		if !hasFilipinoFunctionWord(text) {
			t.Errorf("%s: tagged taglish, but the novice text has no Filipino function word: %q", task.ID, text)
		}
	}
}
