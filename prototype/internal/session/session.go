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
//   - A rolling window as the guaranteed fallback: Append drops the oldest
//     turns immediately, for free, whenever the budget is exceeded, and
//     never blocks on anything slower than that.
//   - Results are stored compact, never raw. A single `find /` dump would
//     otherwise crowd out the very turns that carry the referent a
//     follow-up depends on.
//
// Amended: summarization is now available too (Snapshot/Compact), but it is
// the caller's choice, not this package's. Condensing history costs a model
// call, and this package has no model — see ApproachingLimit's own comment
// for why that boundary is deliberate and where the actual summarization
// call belongs.
//
// The package deliberately has no dependency on Ollama, the filesystem, or
// any UI: it is a pure data structure with a rendering method, which is
// what lets the whole of it be tested without a model. Snapshot/Compact are
// exported specifically so a caller that *does* have a model can summarize
// without this package importing one; they take and return plain strings.
//
// Safe for concurrent use: a caller that wants summarization to cost no
// perceived latency has to run it in the background while the next task may
// already be appending, so every exported method takes the same mutex.
package session

import (
	"fmt"
	"strings"
	"sync"
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

// DefaultSummaryChars caps a compacted summary's stored length. Larger than
// DefaultResultChars: a summary stands in for potentially many turns at
// once, where a step result only ever has to carry one command's output.
const DefaultSummaryChars = 800

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
	mu sync.Mutex

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
	// compacted counts turns folded into a summary rather than dropped
	// outright, reported the same way dropped is: the user should always be
	// able to find out that something happened to their history, whichever
	// of the two it was.
	compacted int

	// gen counts every structural change (Append that trims, Compact,
	// Clear). Snapshot hands its caller the generation the snapshot was
	// taken at; Compact only applies if it is still current. Summarizing
	// runs in the background over however long a model call takes, and
	// history can change underneath it — a newer Append may already have
	// dropped exactly the turns being summarized, or a second Compact may
	// already have replaced them. Applying a summary anyway would silently
	// overwrite history it no longer accurately describes, which is worse
	// than the compaction simply not happening this time.
	gen int
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
	c.mu.Lock()
	defer c.mu.Unlock()

	compact := make([]Step, 0, len(steps))
	for _, s := range steps {
		compact = append(compact, Step{
			Command: strings.TrimSpace(s.Command),
			Result:  truncate(strings.TrimSpace(s.Result), c.resultChars),
		})
	}
	c.turns = append(c.turns, Turn{Task: task, Steps: compact})
	c.trim()
	// Any new turn invalidates an outstanding Snapshot, trim-triggered or
	// not: Compact reconstructs history from "the current newest keep
	// turns", and after a new Append that set has shifted even if nothing
	// was dropped — applying an old snapshot's summary against it would
	// silently discard whatever arrived in between.
	c.gen++
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
	c.mu.Lock()
	defer c.mu.Unlock()
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
func (c *Context) Render() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.render(c.turns)
}

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
	c.mu.Lock()
	defer c.mu.Unlock()
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turns = nil
	c.dropped = 0
	c.compacted = 0
	c.gen++
}

// Len reports how many turns are currently retained.
func (c *Context) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.turns)
}

// TakeDropped returns how many turns have been evicted since the last call
// and resets the counter, so a caller can report the loss exactly once.
func (c *Context) TakeDropped() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.dropped
	c.dropped = 0
	return n
}

// TakeCompacted returns how many turns have been folded into a summary since
// the last call and resets the counter, mirroring TakeDropped.
func (c *Context) TakeCompacted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.compacted
	c.compacted = 0
	return n
}

// compactThresholdRatio is how much of the budget history may use before it
// is worth summarizing proactively, ahead of trim's reactive, unconditional
// drop. Below Append's own trigger (100% of maxTokens) so summarization has
// a real chance to finish in the background before a drop would otherwise
// happen — summarizing at the same threshold trim already fires at would
// mean the drop usually wins the race.
const compactThresholdRatio = 0.6

// ApproachingLimit reports whether history is past compactThresholdRatio of
// the budget and worth condensing before trim is forced to drop a turn
// outright with nothing kept of it.
//
// This package cannot do that condensing itself — summarizing needs a model
// call, and the package doc explains why this type deliberately has none.
// ApproachingLimit, Snapshot, and Compact are the seam: a caller that does
// have a model checks this, takes a Snapshot, summarizes it however it
// likes (in the background, so nothing here is blocked on it), and applies
// the result with Compact.
func (c *Context) ApproachingLimit() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.turns) < 2 {
		return false // the newest turn is never summarized away; nothing to gain
	}
	return c.estimateTokens(c.render(c.turns)) > int(float64(c.maxTokens)*compactThresholdRatio)
}

// Snapshot returns the turns eligible for summarization — every turn except
// the newest keep — rendered as text, and the generation this snapshot was
// taken at. ok is false when there are not enough turns to summarize
// anything (keep or fewer).
func (c *Context) Snapshot(keep int) (text string, gen int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if keep < 0 {
		keep = 0
	}
	if len(c.turns) <= keep {
		return "", c.gen, false
	}
	return c.render(c.turns[:len(c.turns)-keep]), c.gen, true
}

// Compact replaces the turns a Snapshot(keep) returned with one synthetic
// turn holding summary, provided nothing has changed history since that
// snapshot was taken (gen still matches the current generation). Returns
// whether it applied. A stale snapshot is discarded silently: the summary
// no longer accurately describes current history, and falling back to
// trim's ordinary drop-oldest behavior is the correct outcome, not an
// error — this is a background optimization, never a requirement.
func (c *Context) Compact(gen, keep int, summary string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	summary = strings.TrimSpace(summary)
	if gen != c.gen || summary == "" || keep < 0 || len(c.turns) <= keep {
		return false
	}
	folded := len(c.turns) - keep
	synthetic := Turn{
		Task:  "(earlier conversation, summarized)",
		Steps: []Step{{Result: truncate(summary, DefaultSummaryChars)}},
	}
	c.turns = append([]Turn{synthetic}, c.turns[len(c.turns)-keep:]...)
	c.compacted += folded
	c.gen++
	return true
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + " ...(truncated)"
}
