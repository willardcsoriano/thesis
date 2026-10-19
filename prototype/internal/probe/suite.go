package probe

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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

// ErrIncomplete marks a task run whose last turn did not finish (it timed
// out, or its stream had no result event). Its records are kept, but the
// task is not a completed one.
var ErrIncomplete = errors.New("a turn did not finish")

// ErrInterrupted marks a task run the operator interrupted.
var ErrInterrupted = errors.New("interrupted")

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// CheckID refuses an id that cannot be used as one file name component:
// task and run ids name files and folders under the run's output, so a '/'
// or a leading '.' would write outside it (../../x).
func CheckID(kind, id string) error {
	if !safeID.MatchString(id) {
		return fmt.Errorf("%s id %q: want 1 to 128 of A-Z a-z 0-9 . _ -, starting with a letter or digit", kind, id)
	}
	return nil
}

// MaxArg is the longest single argument Linux passes to a program
// (MAX_ARG_STRLEN, 128 KiB including the terminating NUL). A longer prompt
// would fail at exec with "argument list too long".
const MaxArg = 128<<10 - 1

// CheckPrompt refuses a prompt that cannot reach a CLI intact as one
// argument: one starting with '-' (read as a flag), one holding a NUL (exec
// refuses it), or one longer than MaxArg.
func CheckPrompt(p string) error {
	switch {
	case strings.HasPrefix(p, "-"):
		return errors.New("a prompt starting with '-' would be read as a flag")
	case strings.IndexByte(p, 0) >= 0:
		return errors.New("a prompt holds a NUL byte, which no argument can carry")
	case len(p) > MaxArg:
		return fmt.Errorf("a prompt of %d bytes is longer than one argument may be (%d)", len(p), MaxArg)
	}
	return nil
}

// OneLine makes text an agent can steer (a command, a CLI's message) safe
// to print to the operator's terminal: whitespace runs become one space,
// control and format characters (terminal escapes, bidirectional
// overrides) are shown as \uXXXX, invalid UTF-8 as U+FFFD, and the result is
// cut after max bytes on a character boundary, marked "...".
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			b.WriteRune(utf8.RuneError)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
		if b.Len() > max {
			return b.String() + "..."
		}
	}
	return b.String()
}

func (t Task) validate() error {
	if t.ID == "" {
		return fmt.Errorf("task without id")
	}
	if err := CheckID("task", t.ID); err != nil {
		return err
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
		if err := CheckPrompt(t.Prompts[tier]); err != nil {
			return fmt.Errorf("task %s %s: %w", t.ID, tier, err)
		}
	}
	for i, f := range t.FollowUps {
		if err := CheckPrompt(f); err != nil {
			return fmt.Errorf("task %s follow-up %d: %w", t.ID, i+1, err)
		}
	}
	if t.Source == "" {
		return fmt.Errorf("task %s: no source", t.ID)
	}
	return nil
}
