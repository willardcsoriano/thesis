# SynapseOS — Prior Art

## Overview

This file is where a problem gets researched before anything is built for it. The project's standing rule is to adopt by default and build only what cannot be adopted (`vision.md`, Principles), and `algorithms.md` makes *"why nothing existing does this"* the burden of proof for any entry claiming to be a contribution — this is where the evidence that discharges or fails that burden is recorded. It is the engineering counterpart to Chapter 2's academic review: that chapter surveys what has been *published*, this file surveys what has been *shipped*, including tools whose source is closed and whose behaviour therefore has to be observed rather than read. Entries are kept whether the answer was adopt, borrow the pattern, or build — a survey that concluded "build" is the most valuable kind, because it is the only thing that makes a novelty claim defensible. Read this before opening a design discussion, and add to it before writing code for anything non-trivial.

## Table of Contents

- [Overview](#overview)
- [How to use this file](#how-to-use-this-file)
- [Entries](#entries)
  - [Progressive disclosure in a transcript with a text input](#progressive-disclosure-in-a-transcript-with-a-text-input)
  - [Typed operations versus raw shell (MCP filesystem/bash servers)](#typed-operations-versus-raw-shell-mcp-filesystembash-servers)
  - [Agentic command-execution safety (gemini-cli)](#agentic-command-execution-safety-gemini-cli)
  - [Recovery coverage in coding agents (Claude Code, Aider)](#recovery-coverage-in-coding-agents-claude-code-aider)
- [Standing candidates](#standing-candidates)
- [Sourcing rules](#sourcing-rules)
- [Cross-references](#cross-references)

## How to use this file

**Before building anything non-trivial, survey first and record it here.** An entry does not need to be long; it needs to be honest about what was actually checked.

Each entry answers four questions:

1. **What are we trying to do?** Stated as a problem, not as a proposed solution — a solution-shaped question finds only tools that already match the solution.
2. **Who has solved this, and how?** With a link to source or documentation. Closed-source tools count, but their behaviour is *observed* rather than read, and entries must say which.
3. **Can we adopt, borrow, or must we build?** Three outcomes, in descending order of preference:
   - **Adopt** — use the existing thing directly. Record the version and why it fits.
   - **Borrow the pattern** — the code does not fit, but the interaction or algorithm does. Record what specifically is being copied, and credit it.
   - **Build** — nothing existing does this. This is the only outcome that justifies an `algorithms.md` entry, and it requires naming what was checked and why each candidate failed.
4. **What did the survey cost us to learn?** Anti-patterns, abandoned approaches, and known failure modes found along the way. This is usually the most reusable part of an entry and the part most often lost.

**Negative results stay.** An entry recording "we checked these five and none fit" is the evidence behind a build decision. Deleting it later, once the thing is built, destroys the only support the contribution claim has.

**Verified versus observed.** Anything read in source or official documentation is marked verified. Anything inferred from using a closed-source product, from a blog post, or from a changelog is marked observed, with what was actually seen. The distinction matters because this file is cited in defence of design decisions, and an observed behaviour can change under you without notice.

## Entries

*Surveys in progress are listed here as they complete. An entry is added before the corresponding code, not after.*

### Progressive disclosure in a transcript with a text input

**Outcome: borrow the pattern. Nothing drops in, and nothing needs inventing.**

**The problem.** The transcript carries four things per step — the generated command, its raw stdout/stderr, its exit code, and the natural-language answer. `vision.md`'s principle is that the answer is the surface and the evidence sits beneath it, reachable but not imposed. The interface therefore needs collapse-and-reveal. The complication is that a text input at the bottom consumes nearly every keystroke, so there is no free single-key binding.

**Nothing in the Charm ecosystem drops in — verified by reading the package list, not the README.** `charm.land/bubbles/v2@v2.2.1` contains exactly `cursor, filepicker, help, key, list, paginator, progress, spinner, stopwatch, table, textarea, textinput, timer, tree, viewport`. There is no accordion, collapsible, or disclosure widget, and the community component list (`charm-and-friends/additional-bubbles`) has none either.

`bubbles/v2/tree` is the only real collapse primitive — `ToggleCurrentNode`, `Open`/`Close`/`IsOpen`, `▼`/`▶` indicators, and it handles multi-line nodes. It is nonetheless the wrong fit: it owns its own viewport and cursor, so it would *replace* our viewport rather than sit inside it, and its default keymap is single-letter (`enter`, `l`, `h`, `j/k`, `f`, `b`, `g/G`) which is exactly what a text input cannot coexist with. It is also absent from bubbles v1, absent from the README, and has no example in bubbletea's repository — so it is newer and less exercised than its presence suggests.

**The model to copy is `charmbracelet/crush`**, because it has our exact shape: Bubble Tea v2, a textarea at the bottom, and a scrolling transcript of tool calls with raw output. It did not use `bubbles/tree`; it built the mechanism, and the mechanism is small:

- An `Expandable` interface with a single `ToggleExpanded() bool`.
- A collapse threshold of ~10 lines that **refuses to collapse when only one line would be hidden** — otherwise the user spends a keystroke to reveal one line, which reads as broken.
- A self-documenting marker that names the key: `… (N lines hidden) [click or space to expand]`. Never a bare ellipsis.
- Scroll anchoring on toggle: capture whether the user was following the bottom *before* expanding, and re-pin afterwards. Four lines, and the non-obvious part — expanding a 400-line blob mid-transcript otherwise teleports the reader.

**The keybinding answer is modal focus, not a free key.** There is no convergence on a key safe beside a text input, and the established solution is to stop looking for one: scope bindings to a focus context. crush routes on a focus enum and uses `tab` to move between editor and transcript; only inside transcript focus does `space` toggle. aerc does the same thing with explicit context sections (`[messages]`, `[compose]`, `[compose::editor]`) for precisely this reason. Bubbletea's own `examples/chat` has no focus model at all — every key goes to the textarea — so the canonical example is our current design and does not solve this.

A second, ctrl-modified global is worth adding for users who never discover `tab`. **It cannot be `ctrl+d`: it was bound to viewport half-page-down in our own `internal/tui/tui.go` when this was written (the viewport is gone since D38, but `ctrl+d` is still the terminal's end-of-input key).** `ctrl+e` or `ctrl+r` are free here. Avoid `alt+` combinations as the primary affordance — terminals and window managers intercept them.

**Anti-patterns, each already paid for by someone else.** Undiscoverable hidden state, collapsing that hides almost nothing, expansion that yanks scroll position, and — the one that matters most here — **expansion fighting text selection**. Collapsed content cannot be selected or grepped, and crush's click handler explicitly bails when a selection is in progress so that dragging to select does not toggle. The mitigations are an explicit copy binding that copies the *full* content regardless of collapsed state, and a non-collapsing path that always exists: our CLI mode already is that path, which is an argument for leaving it verbose rather than a deficiency in it.

**One finding is a safety constraint rather than a UX preference.** The exit code and the reversibility verdict must never be collapsible — only the command text and the raw output. A confirmation gate that hides the evidence for its own warning behind a keystroke is a safety regression wearing the costume of an improvement, and it would undercut the consent argument D30 rests on.

**What this means for us.** The disclosure work is a borrowed pattern of perhaps a hundred lines, not a component to find and not a design to invent. It is gated on one thing that is genuinely ours: the core currently hands interfaces an `io.Writer` — a flat byte stream — so the TUI cannot tell a command from its output from the answer. Structure has to reach the interface before any of this is implementable. That seam is the real decision, and it belongs in `decisions.md` before code.

### Typed operations versus raw shell (MCP filesystem/bash servers)

**Status: survey open, but the local experiment is already done and the decision is owed.** See F4 and F6 in `../prototype/build-order.md`.

**The problem.** A language model asked to act on a filesystem can be given either a shell to write into, or a set of typed operations to call. The shell reaches everything the system can do and is unreliable in proportion; typed operations are reliable in proportion to how narrow they are. The question is which the runtime dispatches file manipulation through — and, separately, what an existing implementation of the typed approach has already learned that we would otherwise rediscover.

**What we already measured, before surveying anyone else.** F4 built `internal/typedops` — five operations (`find_files`, `move_files`, `delete_files`, `rename_files`, `copy_file`) dispatched through Go's own `os`/`io`, with no MCP server and no Node dependency — and compared it against the raw-bash path on the same tasks with the same 3B model:

| Path | Call validity | Task success |
|---|---|---|
| Raw bash | 80% | 60% |
| Typed operations | **100%** | **100%** |

A second finding from the same experiment is worth as much as the first: **Ollama's native tool-calling API never populated `tool_calls` for `qwen2.5-coder` — 0 out of 20 across the full matrix.** **Reconfirmed and widened 2026-09-15** (`cmd/synapse/toolcall_probe_test.go`, Layer 8): 0 tool calls in 69 attempts at 3B and 0 in 23 at **7B**, with a system prompt explicitly instructing the model to call tools and a `run_bash` tool offered alongside the typed registry. Size is therefore not the variable — the behaviour is identical across the two model sizes tested, so it is a property of the model family or of Ollama's template handling for it, not of parameter count. **Corrected the same day, by testing one layer lower.** The first reading of that result — that the coding-agent architecture is unavailable on this stack — was wrong, and wrong in an instructive way: the 0/69 was measured through our own client, which reads only the `tool_calls` field. A direct `curl` to `/api/chat` shows the model *does* emit tool calls; it emits them as JSON in the **`content`** field, and Ollama does not parse them into `tool_calls`. `ollama show` confirms the model declares a `tools` capability and its template renders tools seven times. The capability was never missing — the structured parsing was. The architecture is therefore reimplementable, and the work is a tolerant parser rather than a different model.

**What the model actually produces, which is what the parser has to survive.** Measured 2026-09-15 against `qwen2.5-coder:3b`:

- It *does* fork. "thanks, that worked" returned plain prose and no JSON, while three work requests returned tool-call JSON.
- It invents tools. "hello" produced `{"name": "print_message", ...}` — a function never offered. Unknown names must be rejected, not dispatched.
- It confuses schema with value. Arguments came back as `{"command": {"type": "string", "value": "wc -l log.txt"}}` and, in one case, with a duplicate `command` key carrying the schema and then the value. A parser reading `arguments.command` as a string finds an object.

This is the gap frontier tool-calling APIs close for you: strict output formats learned in post-training, plus server-side validation. Reimplementing the harness is easy — it is schema serialisation, parse, dispatch, loop. Reimplementing the *reliability* is the actual work, and it lands as argument-shape normalisation, unknown-tool rejection, and a repair-retry path. Typed dispatch therefore works here only via a freeform-JSON fallback, not via the model's advertised function-calling support. Anything adopted from the MCP world that assumes native tool calling does not transfer to this model without that fallback. Recorded in `drift.md`, because a future Ollama or model release could change it.

**What is still worth surveying, and what is not.** Not worth re-running: whether typed operations are more reliable than raw bash on a small local model. That is answered. Worth surveying, because it is where an existing implementation's scars are:

- **Operation set design.** Which primitives the reference MCP filesystem server exposes, and — more informative — which it deliberately does *not*. The boundary between "enough to be useful" and "so many that the model picks wrongly" is exactly the thing that is expensive to learn by experiment.
- **Path safety.** How roots, traversal, and symlink escape are constrained. This is security-critical and is precisely the kind of thing that should be copied rather than reasoned out from scratch.
- **Error surfaces.** What a failed operation returns to the model, and whether that shape lets it recover. This project has an observed failure where the model repeats an identical failing command to the step cap; a well-designed error return may be most of the fix.
- **Bash-server safety.** How MCP bash/shell servers gate destructive commands, which is directly comparable to `safety-model.md`'s taxonomy and may be a stronger baseline than the pattern list currently named in `algorithms.md`. **Surveyed 2026-09-20 — it is not a stronger baseline.** The reference filesystem server (`modelcontextprotocol/servers`, `src/filesystem`) tags each tool with a `destructiveHint` — but the hint is a **fixed property of the tool type**, set once in the tool's own definition, not computed from the call's actual arguments: `move_file` is always `destructiveHint: true`, whether or not the destination already exists. This is the identical failure this project's own pattern list already names in `algorithms.md` Entry 1 — reasoning about a name (or here, a tool type) rather than an effect — now independently confirmed in the official MCP spec's own convention, not just in this project's baseline. There is also no recovery mechanism behind the hint at all: no backup, no undo, nothing beyond a `dryRun` preview mode offered on one tool (`edit_file`). The hint tells a client "be careful," and stops there.

**Why this is not simply "adopt MCP."** The protocol assumes a server process and a client that speaks it. SynapseOS is a single local binary with no Node runtime and a deliberately minimal dependency surface (D8), and F4's implementation already showed the *pattern* transfers without the *protocol*. The likely outcome is **borrow the pattern, not the plumbing** — but that is a conclusion this entry should reach on evidence, not assert in advance.

**Interaction with the recoverability algorithm (D29) — the part that matters most.** Typed operations change what the algorithm has to analyse, and the direction is not obviously favourable:

- A typed call carries its effects on its face. `delete_files(paths)` names exactly what it touches; `rm -rf $(find . -name '*.tmp')` does not, and cannot be resolved statically at all. If file manipulation moves to typed dispatch, the cases where recoverability is *easy* to compute leave the bash path.
- What remains on the bash path is package management, process control, and arbitrary tools — which is where recoverability is genuinely hard and, in several cases, undecidable.

Two readings, and F6 should pick one deliberately rather than inherit it. It **sharpens** the contribution if the story is "typed dispatch handles the common case with certainty; the algorithm handles the irreducible remainder" — that is a cleaner division than one algorithm claiming to cover everything. It **undercuts** the contribution if the algorithm is left holding only cases it cannot decide, and reports a false-negative rate dominated by commands no static analysis could ever resolve. The honest test is whether the corpus in `algorithms.md` Entry 1 still contains enough decidable-but-non-trivial commands once the typed subset is removed.

**Open, and owed.** F6 requires a `decisions.md` entry recording the call and its reasoning, and — if typed operations are adopted — Chapter 3 must state plainly that file-manipulation tasks are dispatched differently from the rest, because reporting typed-ops reliability as the system's reliability without that sentence would misdescribe what was measured.

### Agentic command-execution safety (gemini-cli)

**Status: surveyed 2026-09-20, from published docs and the public issue tracker — not from source, per the sourcing rules below (gemini-cli is Apache 2.0, so source reading is not actually restricted here; this pass used docs and issues and a source-level read remains open if more detail is later needed).**

**What it does.** `google-gemini/gemini-cli` gates shell commands with `tools.core`/`tools.exclude` — an allowlist and denylist matched against a command's **prefix** (`run_shell_command(git)` permits `git ...`). Chained commands (`&&`, `||`, `;`) are split and each part is checked against the same lists. No reversibility analysis exists anywhere in the mechanism; it is a permission decision only, exactly as this project's own pattern-list baseline is.

**Why it matters beyond confirming the obvious.** The splitting logic has a live, currently-open bug (issue #11766) that is close to a direct demonstration of this study's "enumeration does not compose" argument (`algorithms.md` Entry 1): `true && rm important_file.txt` executes in full even with `rm` denylisted, because only the first command in the chain is validated — the fix (validate every command in a chain, not just the first) patches this one shape without addressing the underlying claim that pattern matching over composed commands does not generalize. This is independent, real-world evidence for the thesis's structural argument, not an invented example.

**Not adopted.** The prefix-list mechanism itself is the thing being improved on, not a pattern to borrow. What is worth noting for `decisions.md` if ever relevant: gemini-cli's chain-splitting approach is one candidate shape for how a pattern-based system tries (and here, fails) to handle composition — useful as a concrete negative example, not as a design source.

### Recovery coverage in coding agents (Claude Code, Aider)

**Status: surveyed 2026-09-20, from each product's published documentation. Claude Code is closed source, so this is recorded as *observed documented behaviour* per the sourcing rules below; no source was read. Scope is recovery only — how each tool undoes what it did — not its permission model, which the gemini-cli entry above already covers.**

**What they do.** Both split the work in two. File edits go through the agent's own typed editing tools, and those edits are recoverable: Claude Code checkpoints file state before each turn and offers `/rewind`; Aider commits each edit to git automatically and offers `/undo`. Everything else goes through a raw shell tool.

**What they do not do.** Neither recovers what the shell tool does. Claude Code's checkpointing documentation states it directly: "Checkpointing does not track files modified by Bash commands", naming `rm file.txt`, `mv old.txt new.txt`, and `cp source.txt dest.txt` as changes that "cannot be undone through rewind". Aider's documentation scopes `/undo` and its automatic commits to "changes that Aider itself makes to files" and says nothing about shell commands. Claude Code's documentation also lists further gaps — subagent edits, symlinked and hard-linked paths, changes made outside the session — and describes checkpoints as "not a replacement for version control".

**Why it matters.** The design both tools converged on is the hybrid F6 asks about: typed dispatch for file edits, with recovery attached, and a shell for the remainder, with none. That is independent support for F4's result (typed operations 100%/100% against raw bash 80%/60%), and it places the recoverability algorithm precisely: the region these tools leave uncovered is shell-command effects, which is the region `algorithms.md` Entry 1 targets. It also bears on the baseline. The pattern-list classifier with `internal/undo` behind it already recovers a bash `rm` (trash) and a bash `sed -i` (content backup), which Claude Code's documentation says its own mechanism cannot — so the current baseline is not a weak one, and the algorithm has to beat it rather than beat nothing.

**Limits of this survey.** Documentation states what a tool claims, not everything it does; a claim that a tool has no shell recovery is only as strong as the docs are complete. Cursor and the other agents in the standing-candidates table have not been checked for this, and the Claude Code and Aider findings should not be generalised to them.

**Not adopted as a mechanism.** Nothing here is code or a mechanism to borrow. The entry is evidence for the F6 decision, recorded as D33 in `decisions.md` on 2026-09-20.

## Standing candidates

Things worth checking first for any new problem in this project's space, so that each survey does not restart from nothing.

| Domain | Worth checking |
|---|---|
| Terminal UI patterns | Charm's `bubbles` component library and Charm's own applications; lazygit, k9s, tig, htop, ranger, ncdu |
| Agentic command execution | `google-gemini/gemini-cli` **surveyed 2026-09-20 — see the entries above**; Aider and Claude Code surveyed for recovery coverage only (docs, not source); still open: Aider's permission model, OpenHands, `block/goose`, Cline, Continue, SWE-agent, `charmbracelet/crush`, Alibaba's ANOLISA `cosh-ng`, Warp |
| Typed operations for models | The reference MCP filesystem and bash/shell servers — operation-set design, path-safety constraints, error-return shape. Destructive-command gating **surveyed 2026-09-20 — see the entry above**; operation-set design and path-safety constraints remain open |
| Shell parsing and static analysis | `mvdan/sh` (Go shell parser), ShellCheck, bashlex |
| Undo and recovery | git's object model, `trash-cli`, OverlayFS and filesystem snapshotting, `fsmonitor` |
| Local model serving | Ollama, llama.cpp, LM Studio |
| Study instrumentation | Standard HCI instruments before writing any custom questionnaire |

## Sourcing rules

Two constraints on what may be studied and cited here, both learned while surveying coding agents.

**Proprietary source is out of bounds, including leaks.** Claude Code is proprietary — its repository carries `© Anthropic PBC. All rights reserved` and a request to license it openly was closed as not planned. Repositories circulating decompiled, de-minified, or leaked copies of it exist and are **excluded from this file on purpose**. Behaviour observed by *using* a closed product, or read from its published documentation, is legitimate prior art and is recorded as *observed*; its source code is not. A contribution claim resting on reverse-engineered proprietary code is a defect that cannot be repaired later, and it is unnecessary — the openly licensed set is large.

**A retired project can be the better source.** `google-gemini/gemini-cli` stopped serving requests for free, Pro, and Ultra users on 18 June 2026 and was superseded by the closed-source Antigravity CLI, but the repository remains unchanged under Apache 2.0. For studying architecture that makes it *more* useful than a live project, not less: it is complete, production-grade, absorbed roughly six thousand community contributions, and will never move under a citation. It is also, with its successor closed, the last fully-open mature agent from a major vendor — which is a reason to mine it now rather than later.

## Cross-references

- `vision.md` — the adopt-by-default principle this file operationalises.
- `algorithms.md` — where a survey that concluded *build* graduates to a design record; its "why nothing existing does this" question is answered from here.
- `decisions.md` — where the resulting choice is recorded once made.
- `../research-methods/consolidated/` — Chapter 2, the academic counterpart to this file.
