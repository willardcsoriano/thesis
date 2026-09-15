// Package session holds a conversation's memory across tasks (M6, D10).
//
// The runtime already carries history *within* a single task — the bounded
// loop feeds each executed step back to the model so it can decide what to
// do next. What this package adds is memory *between* tasks, which is what
// makes a follow-up like "move it to Downloads" resolvable at all: without
// it, every task starts cold and "it" refers to nothing.
//
// Three deliberate constraints, all from D10:
//
//   - Session-scoped only. History lives in memory for one session and is
//     gone when it ends. No persistence layer, and therefore no storage,
//     no deletion UX, and no privacy surface to reason about.
//   - A rolling window, not summarization. When the budget is exceeded the
//     oldest turns are dropped. Summarizing would cost a second inference
//     call per compression event to buy relevance this scale does not need.
//   - Results are stored compact, never raw. A single `find /` dump would
//     otherwise crowd out the very turns that carry the referent a
//     follow-up depends on.
//
// The package deliberately has no dependency on Ollama, the filesystem, or
// any UI: it is a pure data structure with a rendering method, which is
// what lets the whole of it be tested without a model.
package session

import (
	"fmt"
	"strings"
)

// DefaultResultChars caps how much of a command's output is retained per
// step. Deliberately smaller than the in-task truncation limit: within a
// task, output is what the model reasons over to pick the next step, so it
// needs detail. Across tasks it only has to carry enough for a follow-up
// to resolve a reference, and every character spent here is one unavailable
// to an older turn that might hold the actual referent.
const DefaultResultChars = 200

// DefaultMaxTokens is the history budget in tokens. It is a fraction of the
// context window rather than the whole of it, because history shares that
// window with the system prompt, the current task, and the in-task step
// history — all of which the caller adds on top of whatever this returns.
const DefaultMaxTokens = 1500

// charsPerToken converts characters to an approximate token count.
//
// Four is the standard rule of thumb for English text and is used only to
// decide *before* sending whether to trim. It does not need to be exact:
// the caller feeds back Ollama's own prompt_eval_count via Calibrate, which
// corrects the ratio against what the tokenizer actually charged. Starting
// from a heuristic and correcting with measurement is both simpler and more
// accurate than shipping a tokenizer, and avoids taking on a dependency
// that would have to track the model.
const charsPerToken = 4.0

// Step is one executed command within a task.
type Step struct {
	Command string
	Result  string // already truncated to ResultChars
}

// Turn is one completed task and what it did.
type Turn struct {
	Task  string
	Steps []Step
}

// Context is a session's rolling conversation memory. The zero value is
// not usable; construct with New.
type Context struct {
	turns []Turn

	maxTokens   int
	resultChars int

	// ratio is the current chars-per-token estimate, refined by Calibrate
	// from real prompt_eval_count measurements.
	ratio float64

	// dropped counts turns evicted by trimming since the last report. The
	// caller drains this to tell the user memory was lost — silently
	// forgetting is the failure mode this package most needs to avoid,
	// because it looks exactly like the system working until it suddenly
	// does not.
	dropped int
}

// New returns an empty Context with the default budget.
func New() *Context {
	return &Context{
		maxTokens:   DefaultMaxTokens,
		resultChars: DefaultResultChars,
		ratio:       charsPerToken,
	}
}

// NewWithBudget returns an empty Context with an explicit token budget and
// per-step result cap. Non-positive values fall back to the defaults.
func NewWithBudget(maxTokens, resultChars int) *Context {
	c := New()
	if maxTokens > 0 {
		c.maxTokens = maxTokens
	}
	if resultChars > 0 {
		c.resultChars = resultChars
	}
	return c
}

// Append records a completed task, truncating each step's result and then
// trimming the oldest turns until the history fits the budget.
//
// A task with no steps is not recorded: it produced nothing a later turn
// could refer back to, so keeping it would spend budget to remember that
// nothing happened.
func (c *Context) Append(task string, steps []Step) {
	task = strings.TrimSpace(task)
	if task == "" || len(steps) == 0 {
		return
	}

	compact := make([]Step, 0, len(steps))
	for _, s := range steps {
		compact = append(compact, Step{
			Command: strings.TrimSpace(s.Command),
			Result:  truncate(strings.TrimSpace(s.Result), c.resultChars),
		})
	}
	c.turns = append(c.turns, Turn{Task: task, Steps: compact})
	c.trim()
}

// trim drops oldest-first until the rendered history fits the budget.
//
// The newest turn is never dropped, even if it alone exceeds the budget: a
// follow-up almost always refers to the turn immediately before it, so
// evicting that one would defeat the entire purpose of keeping history
// while still paying the cost of having it.
func (c *Context) trim() {
	for len(c.turns) > 1 && c.estimateTokens(c.render(c.turns)) > c.maxTokens {
		c.turns = c.turns[1:]
		c.dropped++
	}
}

// Calibrate refines the chars-per-token ratio from a real measurement:
// promptChars is what was sent, promptTokens is what Ollama reported
// charging for it (GenerateResponse.PromptEvalCount).
//
// This is what makes the budget honest without shipping a tokenizer. The
// ratio is only updated on plausible input — a zero or negative token count
// means the caller had nothing real to report, and adopting it would
// poison every later estimate.
func (c *Context) Calibrate(promptChars, promptTokens int) {
	if promptChars <= 0 || promptTokens <= 0 {
		return
	}
	c.ratio = float64(promptChars) / float64(promptTokens)
}

func (c *Context) estimateTokens(s string) int {
	if c.ratio <= 0 {
		c.ratio = charsPerToken
	}
	return int(float64(len(s)) / c.ratio)
}

// Render returns the history as prompt text, or "" when empty so a caller
// can omit the section entirely rather than emit an empty heading.
func (c *Context) Render() string { return c.render(c.turns) }

func (c *Context) render(turns []Turn) string {
	if len(turns) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Earlier in this session:\n")
	for i, t := range turns {
		fmt.Fprintf(&b, "%d. Task: %s\n", i+1, t.Task)
		for _, s := range t.Steps {
			fmt.Fprintf(&b, "   $ %s\n", s.Command)
			if s.Result != "" {
				fmt.Fprintf(&b, "     %s\n", s.Result)
			}
		}
	}
	return b.String()
}

// Summary is a compact, user-facing view of what is currently remembered.
// This is not a debug aid: pronoun resolution only works if the user and
// the system agree on what "it" refers to, and this is the only way to
// check that agreement before issuing a destructive follow-up.
func (c *Context) Summary() string {
	if len(c.turns) == 0 {
		return "no conversation history yet — the next task starts fresh."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "remembering %d task(s):\n", len(c.turns))
	for i, t := range c.turns {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, t.Task)
		for _, s := range t.Steps {
			fmt.Fprintf(&b, "       $ %s\n", s.Command)
		}
	}
	return b.String()
}

// Clear forgets everything. The escape hatch for when a follow-up would
// otherwise resolve against a stale turn — without it the only remedy is
// restarting the session, which is a poor answer to a one-word problem.
func (c *Context) Clear() {
	c.turns = nil
	c.dropped = 0
}

// Len reports how many turns are currently retained.
func (c *Context) Len() int { return len(c.turns) }

// TakeDropped returns how many turns have been evicted since the last call
// and resets the counter, so a caller can report the loss exactly once.
func (c *Context) TakeDropped() int {
	n := c.dropped
	c.dropped = 0
	return n
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + " ...(truncated)"
}
