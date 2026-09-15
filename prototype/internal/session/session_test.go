package session

import (
	"strings"
	"testing"
)

func steps(pairs ...string) []Step {
	var out []Step
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Step{Command: pairs[i], Result: pairs[i+1]})
	}
	return out
}

// --- the property M6 exists for --------------------------------------

// TestRenderCarriesTheReferentForAFollowUp is the whole point of this
// package: a follow-up like "move it to Downloads" is only resolvable if
// the previous turn's subject survives into the rendered history.
func TestRenderCarriesTheReferentForAFollowUp(t *testing.T) {
	c := New()
	c.Append("create a file called report.pdf", steps("touch report.pdf", "exit 0"))

	got := c.Render()
	if !strings.Contains(got, "report.pdf") {
		t.Errorf("rendered history lost the referent a follow-up would need:\n%s", got)
	}
	if !strings.Contains(got, "create a file called report.pdf") {
		t.Errorf("rendered history lost the task text:\n%s", got)
	}
}

func TestRenderIsEmptyWithNoHistory(t *testing.T) {
	if got := New().Render(); got != "" {
		t.Errorf("Render() = %q, want empty so the caller can omit the section entirely", got)
	}
}

// --- what does and does not get recorded -----------------------------

// TestAppendIgnoresTasksThatDidNothing verifies budget isn't spent
// remembering that nothing happened — a task with no executed steps
// leaves nothing a later turn could refer back to.
func TestAppendIgnoresTasksThatDidNothing(t *testing.T) {
	c := New()
	c.Append("do something impossible", nil)
	c.Append("", steps("ls", "ok"))
	c.Append("   ", steps("ls", "ok"))

	if c.Len() != 0 {
		t.Errorf("Len() = %d, want 0 — empty tasks and step-less tasks should not be recorded", c.Len())
	}
}

// TestAppendTruncatesResults pins D10's compression requirement: verbose
// output must never be stored at full length, because one find(1) dump
// would evict the turns that actually carry a referent.
func TestAppendTruncatesResults(t *testing.T) {
	c := NewWithBudget(100000, 50) // large budget so trimming can't confound this
	huge := strings.Repeat("x", 5000)
	c.Append("find everything", steps("find /", huge))

	got := c.Render()
	if strings.Contains(got, huge) {
		t.Error("full raw output was stored; it must be truncated before entering history")
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation should be visible in the stored result:\n%s", got)
	}
}

// --- the rolling window ----------------------------------------------

func TestTrimDropsOldestFirst(t *testing.T) {
	c := NewWithBudget(60, 20) // tight budget, forces eviction
	c.Append("first task", steps("echo one", strings.Repeat("a", 100)))
	c.Append("second task", steps("echo two", strings.Repeat("b", 100)))
	c.Append("third task", steps("echo three", strings.Repeat("c", 100)))

	got := c.Render()
	if strings.Contains(got, "first task") {
		t.Errorf("oldest turn should have been evicted first:\n%s", got)
	}
	if !strings.Contains(got, "third task") {
		t.Errorf("newest turn must always survive:\n%s", got)
	}
}

// TestTrimNeverDropsTheNewestTurn guards the one case where trimming
// would be self-defeating: a follow-up almost always refers to the turn
// immediately before it, so evicting that turn would pay the cost of
// keeping history while destroying its only reliable use.
func TestTrimNeverDropsTheNewestTurn(t *testing.T) {
	c := NewWithBudget(1, 10000) // budget so small nothing legitimately fits
	c.Append("the only task", steps("echo hello", strings.Repeat("z", 5000)))

	if c.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 — the newest turn must survive even when it alone exceeds the budget", c.Len())
	}
	if !strings.Contains(c.Render(), "the only task") {
		t.Error("newest turn was dropped despite being the only one")
	}
}

// TestDroppedTurnsAreReportable is the anti-silence guard. A rolling
// window that forgets invisibly is the worst failure mode available here:
// the system appears to work, then inexplicably doesn't remember, and the
// user has no way to tell why. The caller must be able to say so.
func TestDroppedTurnsAreReportable(t *testing.T) {
	c := NewWithBudget(60, 20)
	for _, task := range []string{"one", "two", "three", "four"} {
		c.Append(task, steps("echo x", strings.Repeat("q", 100)))
	}

	n := c.TakeDropped()
	if n == 0 {
		t.Fatal("turns were evicted but TakeDropped() reported none — the loss would be invisible to the user")
	}
	if again := c.TakeDropped(); again != 0 {
		t.Errorf("TakeDropped() = %d on second call, want 0 — a loss must be reported exactly once, not repeatedly", again)
	}
}

// --- calibration ------------------------------------------------------

// TestCalibrateAdjustsTheEstimate verifies the measured ratio actually
// replaces the heuristic — this is what keeps the budget honest without
// shipping a tokenizer that would have to track the model.
func TestCalibrateAdjustsTheEstimate(t *testing.T) {
	c := New()
	before := c.estimateTokens(strings.Repeat("x", 400))

	c.Calibrate(400, 200) // measured: 2 chars per token, denser than the 4.0 default
	after := c.estimateTokens(strings.Repeat("x", 400))

	if after <= before {
		t.Errorf("estimate did not respond to calibration: before=%d after=%d", before, after)
	}
	if after != 200 {
		t.Errorf("estimate = %d, want 200 from the measured 2 chars/token ratio", after)
	}
}

// TestCalibrateIgnoresImplausibleInput guards against poisoning every
// later estimate with a zero or negative measurement, which would come
// from a caller that had nothing real to report.
func TestCalibrateIgnoresImplausibleInput(t *testing.T) {
	c := New()
	want := c.estimateTokens(strings.Repeat("x", 400))

	for _, tc := range [][2]int{{0, 100}, {100, 0}, {-5, 100}, {100, -5}} {
		c.Calibrate(tc[0], tc[1])
		if got := c.estimateTokens(strings.Repeat("x", 400)); got != want {
			t.Errorf("Calibrate(%d, %d) changed the estimate to %d, want it ignored (%d)", tc[0], tc[1], got, want)
		}
	}
}

// --- user-facing affordances -----------------------------------------

func TestClearForgetsEverything(t *testing.T) {
	c := New()
	c.Append("a task", steps("ls", "ok"))
	c.Clear()

	if c.Len() != 0 {
		t.Errorf("Len() = %d after Clear(), want 0", c.Len())
	}
	if c.Render() != "" {
		t.Errorf("Render() = %q after Clear(), want empty", c.Render())
	}
}

func TestSummaryExplainsEmptyStateInPlainLanguage(t *testing.T) {
	got := New().Summary()
	if !strings.Contains(got, "no conversation history") {
		t.Errorf("empty Summary() = %q, want it to say plainly that nothing is remembered", got)
	}
}

// TestSummaryListsRememberedTasks verifies the user can actually check
// what "it" would resolve against before issuing a destructive follow-up.
func TestSummaryListsRememberedTasks(t *testing.T) {
	c := New()
	c.Append("create report.pdf", steps("touch report.pdf", "exit 0"))
	c.Append("list the folder", steps("ls -la", "report.pdf"))

	got := c.Summary()
	for _, want := range []string{"2 task(s)", "create report.pdf", "list the folder", "touch report.pdf"} {
		if !strings.Contains(got, want) {
			t.Errorf("Summary() missing %q, got:\n%s", want, got)
		}
	}
}

func TestNewWithBudgetRejectsNonPositiveValues(t *testing.T) {
	c := NewWithBudget(0, -1)
	if c.maxTokens != DefaultMaxTokens {
		t.Errorf("maxTokens = %d, want the default %d", c.maxTokens, DefaultMaxTokens)
	}
	if c.resultChars != DefaultResultChars {
		t.Errorf("resultChars = %d, want the default %d", c.resultChars, DefaultResultChars)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 50); got != "short" {
		t.Errorf("truncate short string = %q, want unchanged", got)
	}
	if got := truncate("abcdefghij", 0); got != "abcdefghij" {
		t.Errorf("truncate with n<=0 = %q, want unchanged", got)
	}
	got := truncate(strings.Repeat("x", 100), 10)
	if !strings.HasPrefix(got, strings.Repeat("x", 10)) {
		t.Errorf("truncate should preserve the first n bytes, got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncate should mark that it cut, got %q", got)
	}
}
