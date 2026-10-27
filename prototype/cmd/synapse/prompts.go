package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"synapseos/internal/loopevent"
	"synapseos/internal/ollama"
	"synapseos/internal/session"
)

// systemPrompt constrains the model to emit exactly one runnable command.
// This is intentionally strict and un-tuned: the point of this milestone is to
// measure the stock model's out-of-the-box quality, which sets the baseline
// that later LoRA fine-tuning (scope.md, Python pipeline) has to beat. Used
// only by the propose-only sample suite (proposeOnly) — the ad-hoc path uses
// loopSystemPrompt instead.
const systemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
Convert the user's request into a single bash command that accomplishes it.
Rules:
- Output ONLY the command. No explanation, no commentary, no markdown code fences.
- If the request needs multiple steps, combine them into one line with pipes or &&.
- Prefer standard, widely available utilities.
- If the request cannot be done with a shell command, output exactly: UNSUPPORTED`

// loopSystemPrompt drives the ad-hoc path's bounded multi-step loop (D21):
// given the task and, on later steps, what has already run and what it
// produced, propose exactly the next command, or DONE once nothing further
// is needed. This is what lets the loop handle tasks that need more than one
// command (e.g. "mkdir a destination, then move files into it") and correct
// a failed attempt using its own error output — without the model ever
// deciding to skip the classifier/confirmation gate for a later step; that
// gate is enforced by runAdHoc, not by anything the model is trusted to do.
const loopSystemPrompt = `You are the command translator for a Linux system running Debian 13 (Trixie).
Decide the single next step needed to make progress on the task. Reply with an "action" of "run", "done", or "unsupported":
- "run": there is a bash command that makes progress. Put it, and only it, in "command" — no explanation, no commentary, no markdown code fences.
- "done": the steps already run for the CURRENT task have fully accomplished it. Leave "command" empty.
- "unsupported": the task cannot be done with a shell command at all. Leave "command" empty.

The prompt may contain two clearly separate sections. Do not confuse them:
- "Earlier in this session" is background from PREVIOUS, ALREADY-FINISHED tasks. Use it only to understand what words like "it", "that", or "the file" refer to. It is NEVER progress on the current task.
- "Steps already run" is progress on the CURRENT task, and only those steps count toward finishing it.

Rules:
- If the CURRENT task has no steps run yet, always choose "run" — never "done", no matter what earlier tasks did.
- Combine steps into one line with pipes or && where you reasonably can, but if a step depends on seeing the result of a previous command first, propose only that next step.
- Prefer standard, widely available utilities.
- To find out where you are, run pwd. Never run cd to check: a directory change does not carry over from one command to the next.
- Commands run in the working directory given below. Words like "here", "this folder", "this directory", or "the current folder" refer to THAT directory: write a relative path or ".", never an absolute path to somewhere else.
- NEVER output a placeholder path. /path/to/folder, /path/to/file, /your/directory and similar are not real paths and the command will fail. If the task does not name a path, it means the working directory — use a relative path.
- Do not substitute a well-known system directory for one the task did not mention. "the log files here" means log files in the working directory, not /var/log.
- If the request asks about more than one distinct thing, only choose "done" once a step already run has checked each of them — a step that answers one part of the request does not make the others done.`

// stepFormat constrains the model's reply for proposeStep to this exact
// shape, verified against Ollama 0.34's documented structured-output support
// (a JSON schema in the "format" field of /api/generate). This is what
// removes a whole class of model-specific parsing bugs found by hand — a
// stray "$ " copied from an example, markdown fences, restating the command
// after the DONE sentinel — rather than adding another prompt sentence or
// text-cleanup rule for each one as it turns up. Any model or server that
// does not honor it still gets a fair shot: decodeStepDecision falls back to
// the legacy free-text parse (cleanCommand) when the reply is not valid JSON
// in this shape, which is also what keeps every scripted test response that
// predates this unchanged.
var stepFormat = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action":  map[string]any{"type": "string", "enum": []string{"run", "done", "unsupported"}},
		"command": map[string]any{"type": "string"},
	},
	"required": []string{"action", "command"},
}

// stepDecision is stepFormat's Go shape.
type stepDecision struct {
	Action  string `json:"action"`
	Command string `json:"command"`
}

// decodeStepDecision reads a stepFormat-shaped reply. ok is false when raw is
// not valid JSON in this shape at all — an older or non-conforming model, or
// any of the many existing tests that script a bare command string — and the
// caller falls back to treating raw as free text.
func decodeStepDecision(raw string) (cmd string, ok bool) {
	var d stepDecision
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &d); err != nil {
		return "", false
	}
	switch d.Action {
	case "run":
		return cleanCommand(d.Command), true
	case "done":
		return doneSentinel, true
	case "unsupported":
		return "UNSUPPORTED", true
	default:
		return "", false
	}
}

// defaultNumCtx is the context window requested from Ollama, in tokens.
//
// This must be set explicitly and cannot be left to the server's default.
// Measured against a live server (Session 29): Ollama defaults num_ctx to
// 2048 regardless of what the model supports, and it enforces that limit by
// *silently truncating* the prompt — a ~6000-token prompt came back with
// prompt_eval_count 2050 and a perfectly normal-looking response, having
// never seen most of its own input. There is no error and no warning to
// detect. qwen2.5-coder:3b itself reports context_length 32768, so 2048 was
// never a model limit, only an unstated server default.
//
// 8192 is chosen as a deliberate middle: 4x the silent default, enough for
// the multi-step loop plus M6's cross-task history, and far short of the
// model's 32768 ceiling because the KV cache grows with this value and the
// deployment target is CPU-only with modest RAM (stack.md: 4 GB minimum).
// Override with SYNAPSE_NUM_CTX when a larger window is worth the memory.
const defaultNumCtx = 8192

// generationOptions builds the per-request options map. Every call into the
// model goes through here so num_ctx can never be accidentally omitted from
// one path and set on another — the failure mode that would reintroduce
// silent truncation on whichever path was missed.
func generationOptions() map[string]any {
	return map[string]any{
		"temperature": 0,
		"num_ctx":     envIntOr("SYNAPSE_NUM_CTX", defaultNumCtx),
	}
}

// envIntOr reads a positive integer from the environment, falling back to
// the default on absence, unparseable input, or a non-positive value. A bad
// value degrades to the working default rather than failing the run: a typo
// in an env var should not stop a user from getting work done.
func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "warning: ignoring invalid %s=%q, using %d\n", key, v, fallback)
		return fallback
	}
	return n
}

const doneSentinel = "DONE"

// doneOffer is the sentence that offers DONE, added to a step prompt only once
// something has run (buildStepPrompt says why).
const doneOffer = "Output DONE if the task is already fully accomplished."

// stepOutputChars caps how much of a single step's stdout/stderr gets fed
// back into the next step's prompt. A command like a recursive find can
// produce output far larger than useful context; this is a blunt truncation,
// not the compression M6's session context will eventually do for the TUI.
const stepOutputChars = 500

// answerOutputChars is the per-step output budget when summarising for the
// user. It is larger than stepOutputChars because the two serve opposite
// ends: the step prompt only needs enough context to decide the next command,
// whereas the answer has to contain the values the user actually asked for,
// and truncating a listing before the line they wanted produces a confident
// wrong answer rather than a short one.
const answerOutputChars = 2000

// answerTimeout bounds the summarising call. It is short because the work is
// already done by the time it runs: a slow answer must degrade to no answer,
// never to a slow task.
const answerTimeout = 30 * time.Second

// compactSystemPrompt drives the background call that condenses older
// session history (D42). It is deliberately not the same prompt as
// answerSystemPrompt: that one writes for the person; this one writes for a
// later call to this same model, so the audience, and therefore what is
// worth keeping, differs — a follow-up needs the referent, not a pleasant
// sentence.
const compactSystemPrompt = `You are condensing the older part of a conversation with a command-line assistant, so a later request can still resolve references like "it", "that file", or "the folder" without the original turns.

Write a short paragraph naming what was done and any file, folder, package, or other resource a later request might refer back to. Do not narrate the commands themselves or their exit codes — only what changed and what it is called.

No markdown, no preamble, no sign-off.`

// compactKeepTurns is how many of the newest turns are always kept verbatim
// and never handed to the summarizer — session.Context.trim already assumes
// a follow-up overwhelmingly refers to the turn immediately before it, and
// summarizing that one away would defeat the reason it is kept at all.
const compactKeepTurns = 1

// compactionTimeout bounds the background summarization call. Generous
// relative to a normal step, deliberately: nothing in the foreground is
// waiting on it, so failing slow only costs a missed compaction this time —
// trim's ordinary drop-oldest behavior still protects the budget — never a
// delayed answer to the task the person actually asked for.
const compactionTimeout = 90 * time.Second

// maybeCompact starts summarizing older session history in the background
// once it is close to its budget, so a later turn does not have to be
// dropped outright with nothing kept of it. It never blocks the caller: the
// entire point is that summarizing costs a model call, and paying that cost
// synchronously would bother the user with exactly the slowness this exists
// to avoid. Best effort throughout — sc.Compact silently discards a summary
// that arrives after history has moved on (see its own doc comment), and a
// failed or slow call here simply means trim's free, immediate fallback
// keeps doing the job alone, same as before this existed.
func maybeCompact(client *ollama.Client, model string, sc *session.Context) {
	if sc == nil || !sc.ApproachingLimit() {
		return
	}
	text, gen, ok := sc.Snapshot(compactKeepTurns)
	if !ok {
		return
	}
	go func() {
		// context.Background, not the task's own ctx: the task's context is
		// cancelled the moment the task's goroutine returns (startTask's
		// deferred cancel), which is at most a few lines of code after this
		// goroutine is spawned — using it here would race the summarization
		// call against its own cancellation almost every time.
		ctx, cancel := context.WithTimeout(context.Background(), compactionTimeout)
		defer cancel()
		resp, err := client.Generate(ctx, model, compactSystemPrompt, text, generationOptions())
		if err != nil {
			return
		}
		sc.Compact(gen, compactKeepTurns, resp.Response)
	}()
}

// proposeStep asks the model for the next command given task and everything
// that has run so far, with the same deterministic decoding as propose.
func proposeStep(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, tokenSink io.Writer, sc *session.Context) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var prior, lastReply string
	var recent []string
	lastChanged := false
	if sc != nil {
		prior = sc.Render()
		recent = sc.Recent()
		lastReply, lastChanged = sc.LastReply()
	}
	// Best effort: if the working directory cannot be read, the prompt simply
	// omits it rather than failing the step. A missing line degrades to the
	// previous ungrounded behaviour; a hard error would take out a task that
	// is otherwise fine.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	var named []string
	if lastReply != "" && !lastChanged && cwd != "" {
		named = namedEntries(lastReply, cwd, true)
	}
	prompt := buildStepPromptRefs(task, history, prior, cwd, recent, named)
	opts := generationOptions()
	// A command never needs this many tokens. Without a cap, a model stuck
	// repeating itself generated until the 120-second timeout (finding
	// atk2-06); with it, the reply ends in seconds and is reported as cut off.
	opts["num_predict"] = maxStepTokens

	// Non-streaming is the default and stays the CLI/REPL behavior
	// (docs/interface-modes.md: CLI renders "once generation finishes").
	// Only a caller that supplies a sink — TUI mode — pays for streaming.
	if tokenSink == nil {
		resp, err := client.Generate(reqCtx, model, loopSystemPrompt, prompt, opts, stepFormat)
		if loopedReply(err) {
			resp, err = client.Generate(reqCtx, model, loopSystemPrompt, prompt, withRepeatPenalty(opts), stepFormat)
		}
		if err != nil {
			return nil, "", err
		}
		calibrate(sc, prompt, resp)
		if err := cutOff(resp); err != nil {
			return nil, "", err
		}
		cmd, ok := decodeStepDecision(resp.Response)
		if !ok {
			cmd = cleanCommand(resp.Response)
		}
		return resp, cmd, nil
	}

	sink := func(tok string) {
		// Best effort: a failed write to the UI must never abort a
		// generation that is otherwise fine.
		fmt.Fprint(tokenSink, tok)
	}
	resp, err := client.GenerateStream(reqCtx, model, loopSystemPrompt, prompt, opts, sink, stepFormat)
	if loopedReply(err) {
		resp, err = client.GenerateStream(reqCtx, model, loopSystemPrompt, prompt, withRepeatPenalty(opts), sink, stepFormat)
	}
	if err != nil {
		return nil, "", err
	}
	calibrate(sc, prompt, resp)
	if err := cutOff(resp); err != nil {
		return nil, "", err
	}
	// The command is still parsed from the fully assembled response, never
	// from the streamed fragments — streaming changes only when text
	// appears on screen, never what gets classified or executed. Combined
	// with GenerateStream refusing to return a truncated stream at all,
	// a partial generation can never reach the classifier.
	cmd, ok := decodeStepDecision(resp.Response)
	if !ok {
		cmd = cleanCommand(resp.Response)
	}
	return resp, cmd, nil
}

// maxStepTokens caps one step's reply; see proposeStep.
const maxStepTokens = 384

// cutOff refuses a reply the token cap ended. It is never parsed: a command
// cut short is not the command the model meant, and the free-text fallback
// would otherwise read truncated JSON as one.
func cutOff(resp *ollama.GenerateResponse) error {
	if resp.DoneReason == "length" {
		return fmt.Errorf("%w: it ran past %d tokens", errCutOff, maxStepTokens)
	}
	return nil
}

// errCutOff marks a reply the token cap ended.
var errCutOff = errors.New("the model's reply was cut off")

// calibrate feeds the real token cost of a prompt back into the session's
// estimator. Ollama reports prompt_eval_count for every generation, so the
// budget can be corrected against what the tokenizer actually charged
// rather than left on a characters-per-token guess — accuracy for free,
// and no tokenizer dependency that would have to track the model.
func calibrate(sc *session.Context, prompt string, resp *ollama.GenerateResponse) {
	if sc == nil || resp == nil {
		return
	}
	sc.Calibrate(len(loopSystemPrompt)+len(prompt), resp.PromptEvalCount)
}

// answerSystemPrompt drives the reply the user actually reads (D31). It is
// deliberately a separate prompt from loopSystemPrompt: that one is a
// translator constrained to emit a bare command, and asking the same
// persona to also write prose is how a command translator starts returning
// explanations where a command was expected.
const answerSystemPrompt = `You are reporting the outcome of a task to the person who asked for it.

You are given their request, the commands that were run on their behalf, and the output those commands produced. Reply with one complete sentence that answers their request. Two sentences if the result genuinely needs it.

Rules:
- Always a complete sentence. Never reply with only a filename, a number, a path, or a copy of the output.
- Restate enough of the request that the answer stands alone when read by itself.
- Use the actual values from the output.
- If the commands produced no output and succeeded, say what was done.
- If the task did not succeed, say plainly what went wrong.
- Do not describe which commands ran, and do not mention the shell.
- A command's output describes only what that command actually queried. Do not attribute a value in it to something else the request asked about, even if the request mentioned several things and the output happens to have several values — check what the command itself did before deciding what a value means.
- If the request asked about more than one thing and the commands above only cover some of them, say what you can from the output and say plainly that the rest was not checked, instead of guessing.
- No markdown, no code blocks, no preamble, no sign-off.

Examples:

Request: how many files are in logs
Output: 4
Reply: There are 4 files in the logs folder.

Request: rename notes.txt to renamed.txt
Output: (none)
Reply: notes.txt has been renamed to renamed.txt.

Request: what files are in this folder
Output: renamed.txt
Reply: This folder contains one file, renamed.txt.

Request: list the contents of a folder called archives
Output: ls: cannot access 'archives': No such file or directory
Reply: There is no folder called archives here.

Request: how much ram is in use and how much disk space is left
Commands: free -h | grep Mem | awk '{print $3, $4}'
Output: 5.9Gi 528Mi
Reply: 5.9GiB of RAM is currently in use (528MiB free), but disk space was not checked, so I can't answer that part.`

// answerFromHistory turns what actually happened into a sentence the user can
// read, which is the difference between a system that executes and one that
// answers (D31). The raw command, its output, and its exit status are still
// printed above this — the answer is the surface, the evidence sits beneath it
// (see vision.md, Principles).
//
// Best-effort by construction: a task that ran correctly must not be reported
// as failed because the summarising call did not come back. On any error the
// caller falls through to the mechanical completion line it printed before.
//
// With onPart set, the reply is streamed and each piece goes to onPart as the
// model writes it, so a person reads the first words instead of waiting for
// the last. The returned reply is still the fully assembled one, and a stream
// that breaks off is an error like any other: the pieces already given were
// never a reply (see loopevent.AnswerPart).
func answerFromHistory(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, onPart func(string)) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, answerTimeout)
	defer cancel()

	var b strings.Builder
	b.WriteString("Their request: ")
	b.WriteString(task)
	b.WriteString("\n\nWhat was run:\n")
	for i, st := range history {
		fmt.Fprintf(&b, "%d. %s\n   exit code: %d\n", i+1, st.command, st.result.ExitCode)
		if st.result.TimedOut {
			fmt.Fprintf(&b, "   note: this was stopped after %s without finishing.\n", stepExecutionTimeout)
		}
		if out := strings.TrimSpace(st.result.Stdout); out != "" {
			fmt.Fprintf(&b, "   output: %s\n", truncate(out, answerOutputChars))
		}
		if errOut := strings.TrimSpace(st.result.Stderr); errOut != "" {
			fmt.Fprintf(&b, "   errors: %s\n", truncate(errOut, answerOutputChars))
		}
	}

	var resp *ollama.GenerateResponse
	var err error
	if onPart == nil {
		resp, err = client.Generate(reqCtx, model, answerSystemPrompt, b.String(), generationOptions())
	} else {
		resp, err = client.GenerateStream(reqCtx, model, answerSystemPrompt, b.String(), generationOptions(), onPart)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Response), nil
}

// maybeAnswer applies the opt-out and otherwise produces the reply.
func maybeAnswer(ctx context.Context, client *ollama.Client, model, task string, history []loopStep, cfg loopConfig) (string, error) {
	if cfg.answerDisabled {
		return "", nil
	}
	// Only an event consumer gets the reply in pieces: it can redraw a live
	// line and then replace it with the whole reply. Text output cannot take
	// back what it printed if the stream breaks off, so CLI and REPL keep the
	// single complete reply (docs/interface-modes.md).
	var onPart func(string)
	if cfg.events != nil {
		onPart = func(s string) { cfg.events(loopevent.Event{Kind: loopevent.AnswerPart, Text: s}) }
	}
	return answerFromHistory(ctx, client, model, task, history, onPart)
}

// buildStepPrompt renders the task plus a truncated history of already-run
// commands and their results, in the shape loopSystemPrompt expects.
func buildStepPrompt(task string, history []loopStep, priorSession, cwd string) string {
	return buildStepPromptRefs(task, history, priorSession, cwd, nil, nil)
}

// buildStepPromptRefs is buildStepPrompt with what the session changed most
// recently, newest first, and what the last reply named, for the refs aid's
// note on the request's "it" (referenceNote).
func buildStepPromptRefs(task string, history []loopStep, priorSession, cwd string, recent, named []string) string {
	// One rendering path, deliberately. There used to be an early return here
	// for the simplest case, and the divergence is what caused the 2026-09-14
	// technical-tier regression described at the trailer below: a first step
	// silently changed shape depending on whether a working directory was
	// known. A prompt that renders differently on a path nobody tests is a
	// prompt that will regress again.
	var b strings.Builder
	// The working directory is stated before anything else because it is what
	// "here" and "this folder" resolve to, and without it they resolve to
	// nothing: the model fills the hole with a literal placeholder
	// (`du -sh /path/to/folder`) or with a familiar system path (/var/log for
	// "the log files here"). Measured 2026-09-14 as the largest single cause
	// of Layer 7's plain-tier failures — roughly half of them — because plain
	// speakers say "here" where technical ones write ".".
	if cwd != "" {
		b.WriteString("Working directory: ")
		b.WriteString(cwd)
		b.WriteString("\n")
		// What is in it, as it is now: read again for every step, so it also
		// shows what the steps so far have changed.
		if here := folderListing(cwd); here != "" {
			b.WriteString("In it: ")
			b.WriteString(here)
			b.WriteString("\n")
		}
		if aidOn(aidPeek) {
			if p := peekSection(task, cwd); p != "" {
				b.WriteString(p)
			}
		}
		b.WriteString("\n")
	}
	// Earlier tasks come first so the current task reads as the most
	// recent thing said — which is what lets a pronoun in it bind to the
	// turn immediately above rather than to something further back.
	if priorSession != "" {
		b.WriteString(priorSession)
		b.WriteString("\n")
	}
	b.WriteString("Task: ")
	b.WriteString(task)
	// Beside the request, where a small model reads it (aids.go).
	if aidOn(aidLiteral) && literalCommand(task) {
		b.WriteString("\n" + literalNote)
	}
	if aidOn(aidRefs) {
		if note := referenceNote(task, recent, named); note != "" {
			b.WriteString("\n" + note)
		}
	}
	if len(history) > 0 {
		b.WriteString("\n\nSteps already run:\n")
	}
	for i, s := range history {
		fmt.Fprintf(&b, "%d. $ %s\n   exit code: %d\n", i+1, s.command, s.result.ExitCode)
		if s.result.TimedOut {
			fmt.Fprintf(&b, "   note: this command exceeded %s and was terminated before it could finish.\n", stepExecutionTimeout)
		}
		if out := strings.TrimSpace(s.result.Stdout); out != "" {
			fmt.Fprintf(&b, "   stdout: %s\n", truncate(out, stepOutputChars))
		}
		if errOut := strings.TrimSpace(s.result.Stderr); errOut != "" {
			fmt.Fprintf(&b, "   stderr: %s\n", truncate(errOut, stepOutputChars))
		}
	}
	// A small model reads the transcript above as context but not as an
	// instruction. Without this it re-proposes the command that just failed,
	// verbatim, until the step cap — observed live 2026-09-12.
	if failed := failedCommands(history); len(failed) > 0 {
		b.WriteString("\n\nThese commands already failed. Do not repeat any of them; the same command will fail the same way again:\n")
		for _, c := range failed {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("Read the error above and try a genuinely different approach: a different tool, a different path, or checking an assumption first. If the task cannot be done at all, output UNSUPPORTED.\n")
	}
	// DONE is offered only once something has actually run that could have
	// accomplished the task. Offering it on a step-less task invites the model
	// to read a request that already looks like a finished command — the
	// technical tier's "du -sh ." — as work already done, and answer DONE
	// before running anything.
	//
	// This is a measured regression, not a hypothetical: before 2026-09-14 a
	// first step with no history and no prior session took the early return
	// above and never saw this line. Adding the working directory routed every
	// first step through here, and Layer 7's technical tier fell 100% -> 58.3%
	// with every failure reporting "model reported nothing needs to be done".
	// It also resolves a standing contradiction — loopSystemPrompt already
	// says a task with no steps run always gets a command, never DONE, while
	// this trailer said otherwise whenever a prior session was present.
	// The two branches differ only in whether DONE is on offer. The question
	// itself is deliberately identical and deliberately not "what is the
	// FIRST command": that wording, tried on 2026-09-14, invites the model to
	// decompose a compound request and stop after part of it — "mkdir -p logs
	// && mv *.log logs/" ran the mkdir, reported "The logs folder has been
	// created", and exited cleanly with the files unmoved.
	b.WriteString("\nWhat command should run next?")
	if len(history) > 0 {
		b.WriteString(" " + doneOffer)
	}
	return b.String()
}

// loopedReply reports whether Ollama aborted a reply because the model kept
// repeating itself ("prediction aborted, token repeat limit reached", Ollama
// 0.34). 1.5B did this on 4 of 108 Layer 7 runs (2026-10-25), and the run then
// ended as though the model could not be reached.
func loopedReply(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token repeat limit reached")
}

// withRepeatPenalty is opts for the one retry after a looped reply: a penalty
// on repeated tokens (Ollama's repeat_penalty; 1.0 is off) to steer it out of
// the loop. Only the retry gets it, because a command repeats tokens, such as
// a path, legitimately.
func withRepeatPenalty(opts map[string]any) map[string]any {
	out := make(map[string]any, len(opts)+1)
	for k, v := range opts {
		out[k] = v
	}
	out["repeat_penalty"] = 1.2
	return out
}

// maxListed bounds how many names folderListing gives the model.
const maxListed = 40

// folderListing names what is in dir, folders marked with a slash, hidden
// entries left out, for the step prompt. A plain request names files as they
// look ("the other text file", "the two text files", "the temporary files"),
// and a model that cannot see them guesses: 1.5B listed nothing and moved
// files it had not named. It is read here, without running a command, and is
// empty when dir cannot be read. The names go only to the model the loop
// already sends command output to.
func folderListing(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var names []string
	more := 0
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if len(names) == maxListed {
			more++
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return "(empty)"
	}
	s := strings.Join(names, ", ")
	if more > 0 {
		s += fmt.Sprintf(", and %d more", more)
	}
	return s
}

// truncate shortens s to at most n bytes, marking that it was cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + " ...(truncated)"
}

// propose sends task to the model with deterministic decoding (temperature
// 0, so the smoke test is reproducible and matches how the offline accuracy
// evaluator will score the model) and returns the cleaned command it proposed.
func propose(ctx context.Context, client *ollama.Client, model, task string) (*ollama.GenerateResponse, string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	resp, err := client.Generate(reqCtx, model, systemPrompt, task, generationOptions())
	if err != nil {
		return nil, "", err
	}
	return resp, cleanCommand(resp.Response), nil
}

// cleanCommand strips the formatting small models add despite instructions:
// triple-backtick code fences, a whole-line wrapped in a single pair of
// inline backticks (observed in live testing 2026-07-12: 3 of 8 sample-suite
// responses came back backtick-wrapped instead of as a bare command), and
// surrounding whitespace. It is defensive parsing, not a sanitizer — it does
// not validate that the result is safe or even syntactically valid shell;
// that's the classifier and the shell itself.
func cleanCommand(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i != -1 {
			s = s[i+1:] // drop the opening ```lang line
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if len(s) > 1 && strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "`"), "`")
		s = strings.TrimSpace(s)
	}
	// A shell prompt copied from an example ("$ df -h") is not part of the
	// command; left in, it parses as a command named "$" and the gate asks
	// about something that is not there.
	for strings.HasPrefix(s, "$ ") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "$ "))
	}
	return s
}
