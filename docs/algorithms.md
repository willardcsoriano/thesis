# SynapseOS — Algorithm Design Record

## Overview

This file is the design record for everything SynapseOS **builds** rather than adopts. The project's standing rule is to adopt by default (`vision.md`, Principles): the kernel, userland, desktop, window manager, inference server, model, shell parser, and terminal rendering are all taken off the shelf and none are reimplemented. What remains after adopting everything possible is the original work — so the contents of this file *are* the thesis's contribution, by construction rather than by assertion. There is **one** entry, deliberately: recoverability analysis of generated shell commands (D29), resting on an **effect semantics** that gives it a formal model rather than a pattern list. One contribution with depth is worth more than several with none, and the rejected candidates below are kept visible because a short list is only credible when what was turned down is recorded beside it. Each entry states the problem, the alternatives weighed and why they lost, the approach, its failure modes, and how it will be evaluated against a named baseline. Read `safety-model.md` for the taxonomy this must subsume, and `decisions.md` for the decision that put an entry here at all.

## Table of Contents

- [Overview](#overview)
- [What belongs here](#what-belongs-here)
- [When an entry gets written, and when it gets amended](#when-an-entry-gets-written-and-when-it-gets-amended)
- [Adopted, deliberately — the other side of the ledger](#adopted-deliberately-the-other-side-of-the-ledger)
- [The formal model — an effect semantics for generated shell commands](#the-formal-model-an-effect-semantics-for-generated-shell-commands)
  - [The object](#the-object)
  - [Soundness, which is one-sided on purpose](#soundness-which-is-one-sided-on-purpose)
  - [What this model does not cover](#what-this-model-does-not-cover)
- [Entry 1 — Recoverability analysis of generated shell commands](#entry-1-recoverability-analysis-of-generated-shell-commands)
  - [Problem statement](#problem-statement)
  - [Why nothing existing does this](#why-nothing-existing-does-this)
  - [Approach](#approach)
  - [Failure modes](#failure-modes)
  - [Evaluation](#evaluation)
- [Cross-references](#cross-references)
- [Candidates not taken](#candidates-not-taken)
  - [Briefly promoted, then pulled back — 2026-09-14](#briefly-promoted-then-pulled-back-2026-09-14)

## What belongs here

An entry is warranted when the project **builds or materially improves** something, and it must be able to answer all five questions below. An entry is written *before* the corresponding chapter text, never after — the chapters render this record, they do not originate it (`README.md`, Rules).

1. **Problem** — stated precisely enough that someone else could attempt it.
2. **Why nothing existing does this** — the adopt-first rule means this is the burden of proof. If an off-the-shelf component solves it, use that instead and write no entry.
3. **Approach** — specific enough to implement from.
4. **Failure modes** — including what it fails closed on, and what it cannot do at all.
5. **Evaluation** — against a named baseline, on a metric chosen before the results exist.

An entry that cannot answer (2) is not a contribution; it is integration, and integration belongs in `build-order.md`.

## When an entry gets written, and when it gets amended

**Both sides of implementation, with different content each time.**

**Before any code**, an entry states the problem, the prior-art verdict that justifies building at all, the intended approach, and — the part that matters most — **the evaluation design, including the metrics and the baseline**.

Committing to the metrics before the results exist is not bookkeeping. Metrics chosen after seeing how something performs are chosen, however unconsciously, because the thing performs well on them; a reader cannot distinguish that from a result, and the practice has a name and a poor reputation. Declaring "false-negative rate is reported separately rather than folded into accuracy" *before* that rate is known is what makes the eventual number worth reading. It is also the cheapest available signal that a contribution was reasoned about rather than assembled.

**After implementation**, the entry is **amended, never rewritten** — the same convention `decisions.md` uses. What the design got wrong, complexity and cost as measured rather than estimated, failure modes that only building revealed, anything dropped and why, and the results against the metrics fixed earlier.

The amendment is the valuable half. A specification that matches its implementation perfectly is indistinguishable from one written afterwards, and reads that way. *"Designed X, implementation showed Y, changed to Z because…"* is evidence of engineering; a clean spec is evidence of nothing. Keep both versions visible.

## Adopted, deliberately — the other side of the ledger

Kept current because it is half the argument: a short contribution list is only credible next to a long adoption list.

| Concern | Adopted | Why not built |
|---|---|---|
| Kernel, init, userland | Debian 13 | Writing one is what "OS development" means in the hard sense, and it is explicitly not this project |
| Desktop, window manager | XFCE (X11) | The agent layers over it (D27); reimplementing it would remove capability from the user |
| Model inference | Ollama | Model lifecycle, quantised loading, and serving are hard to reproduce and not the research question (D8) |
| Language model | Qwen2.5-Coder-3B-Instruct | Training one is a different thesis (D3) |
| Terminal rendering | bubbletea / bubbles / lipgloss | Rendering a scrollable transcript is solved; see `decisions.md` D26 for the seam that keeps it replaceable |
| Shell execution | `bash -o pipefail` | The system's whole premise is driving the existing toolchain (D7) |
| Shell parsing | an existing POSIX shell parser | Writing a shell grammar is a solved, tedious, and error-prone exercise; the contribution is what is computed *over* the tree, never the tree itself |
| Undo for version-controlled state | git's object store | A captured commit SHA is cheaper and more reliable than any file-level copy (D25) |
| Capture mechanisms | hardlinks, git, XDG trash, file copies | Each is an adopted primitive. Entry 1's recovery plan chooses *among* them; it does not implement any of them |

## The formal model — an effect semantics for generated shell commands

**Status:** specified, not yet built. Milestones A1–A3 in `../prototype/build-order.md`. Decision: `decisions.md` D29.

This is the model underlying Entry 1, not a separate contribution. It is recorded as its own section because "what formal model supports your algorithm" is a question the entry has to answer, and answering it inside the entry buried it.

### The object

An **effect** is what a command would do to the system, expressed independently of the command's name: a kind (`read`, `create`, `write`, `remove`, `metadata`), a resolved target path, and the pre-image that would have to be captured to reverse it. An **effect set** is the effects of a whole command, including every branch of its operator structure.

Deriving it has two halves.

**Effect extraction.** For each leaf command, derive its effects, resolved against the working directory. Argument-aware, because flags change effects and not merely behaviour: `rm -i`, `cp -n`, `chmod` with and without `-R`, `mv` onto an existing path versus a new one.

**Composition.** Propagate effects upward through the operator structure. Sequencing accumulates them; `&&` and `||` make later effects conditional on an earlier exit status; pipelines connect stdout to stdin without filesystem effect but change the status that gates what follows; redirects introduce write effects not present in any command in the pipeline; command substitution introduces effects computable only at runtime. `xargs` and `find -exec` multiply a command across an argument set that is not known statically.

### Soundness, which is one-sided on purpose

Anything not statically resolvable — a path from command substitution, an unexpanded variable, a glob that may match a block device — is over-approximated as unrecoverable rather than assumed benign.

This is the only correctness property the system actually needs, and it is asymmetric: over-approximating destructiveness costs a confirmation prompt, while under-approximating destroys a user's data. The evaluation below reports the two error directions separately rather than averaging them into one accuracy figure that would hide the one that matters.

### What this model does not cover

Effects outside the filesystem — network mutations, package-manager database state, running services, anything on a remote host — are not represented. A command whose principal effect is one of those is classified unrecoverable by the soundness rule, which is safe but blunt, and it is why the `apt`/`systemctl` gap in `safety-model.md` is a real limitation rather than a missing pattern. Extending the effect domain beyond the filesystem is the most obvious continuation of this work and is not claimed as part of it.

## Entry 1 — Recoverability analysis of generated shell commands

**Status:** specified, not yet built. Milestones A1–A3 in `../prototype/build-order.md`. Decision: `decisions.md` D29.

### Problem statement

> Given an arbitrary shell command produced by a language model, decide whether its effects are recoverable; and where they are not, compute the minimal set of pre-images sufficient to restore the prior state.

Two outputs, not one: a **verdict** (recoverable / recoverable-with-capture / unrecoverable) and, for the middle case, a **recovery plan** — which mechanisms to apply, to which targets, before the command runs. Both were part of this entry as originally specified; they are one contribution because neither is useful alone.

### Why nothing existing does this

Everything catalogued in `safety-model.md` is a hand-maintained list of named command shapes, and that approach has now visibly reached its stated limit.

- **It cannot enumerate its domain.** The package-manager gap is not an oversight to be patched; it is the predictable behaviour of an enumeration over a space that is not enumerable. Patching `apt` leaves `snap`, `pip`, `npm`, `docker rm`, `zfs destroy`, and whatever ships next.
- **It does not compose.** Every rule matches a command in isolation, while `rm -rf /tmp/x && git reset --hard && sed -i s/a/b/ f.txt` needs trash, git-SHA capture, and content backup simultaneously.
- **It reasons about names, not effects.** `cp a b` is recoverable or destructive depending on whether `b` exists — already handled by `ClassifyForDir` as a special case, which is the pattern-list approach admitting the problem one command at a time.

Shell linters (`shellcheck`) analyse shell source for correctness and style, not reversibility of effect. Privilege mechanisms (`sudo`, `polkit`) gate on who is acting, not on the magnitude of what is about to happen. Neither is a baseline for this.

For the recovery plan specifically, the capture mechanisms are all adopted (see the ledger above); what does not exist is the *selection* among them for a given command. Filesystem snapshotting (Timeshift, btrfs, ZFS) is whole-volume and time-based — correct, coarse, and ignorant of what the next command will touch. Version control covers tracked files only. The XDG trash covers unlink and nothing else. None computes a per-command minimal cover.

### Approach

**Verdict.** A predicate over the effect set: recoverable if every effect in it has an available mechanism that can restore its pre-image; recoverable-with-capture if that holds once the plan has run; unrecoverable otherwise.

**Plan.** A minimum-cost cover of the effect set. Each effect is matched to the cheapest sufficient mechanism, under a cost model where a hardlink is independent of file size while a content copy is not, a single git SHA covers an arbitrary number of tracked-file effects at once, and overlapping targets are captured once rather than per-effect.

### Failure modes

Over-classification on anything unresolvable, by design. No coverage of non-filesystem effects. A correct verdict on an effect set derived from a *mis-parsed* command is still wrong, so parser fidelity bounds the whole result — which is why the parser is adopted rather than written. The plan's cost model is a static estimate and can be wrong. The gap between planning and executing is a time-of-check-to-time-of-use window: a file modified by another process in between is captured in a state that no longer matches. Capture can itself fail — no space, no permission — and must fail the command closed rather than proceed unprotected.

### Evaluation

**Baselines.** The current pattern-list classifier is the lower bound and is already implemented, which makes the comparison honest rather than a straw man. Full pre-emptive snapshot of the working directory is the trivial upper bound on safety and the worst on cost — it recovers everything and is unusable, which is precisely what "minimal" has to earn against.

**Metrics.**

| Metric | Why it is the one that matters |
|---|---|
| **False-negative rate** | An unrecoverable command classified recoverable destroys data. This is the safety metric and is reported on its own, not folded into accuracy |
| False-positive rate | Over-classification causes confirmation fatigue, a documented failure mode of gate designs and a threat to the study |
| Recovery success rate | Of commands the plan claimed to cover, how many actually restored prior state — verified by executing in a sandbox and diffing |
| Storage/time overhead | Against the full-snapshot baseline; this is what "minimal" has to earn |
| Composition coverage | Proportion of compound commands handled without falling back to unrecoverable |

**Corpus.** A few hundred shell commands with ground-truth labels for verdict and, where applicable, correct recovery plan. Sources: NL2Bash (reference [24]'s corpus gives realistic, human-written commands), commands actually generated by the prototype during development, and hand-constructed compound cases covering each operator. Labelling protocol and inter-rater agreement are required, because "is this recoverable" is a judgement and a single annotator's labels are not evidence.

**Threat to this evaluation, stated up front:** a corpus assembled partly from commands the prototype itself generated is biased toward the shapes this model produces. Report per-source results rather than pooling, and treat NL2Bash-derived commands as the generalisation set.

## Cross-references

- `decisions.md` D29 — the decision to make recoverability analysis the thesis's algorithmic contribution, and the alternatives it was chosen over.
- `decisions.md` D32 — the manuscript restructure that makes this RQ1, and the scope limits placed on it.
- `safety-model.md` — the taxonomy this algorithm has to subsume, and the pattern-list baseline it is measured against.
- `prior-art.md` — the survey that justifies building rather than adopting.
- `../prototype/build-order.md` — the Algorithm Track (A1 corpus, A2 classifier, A3 evaluation) that builds it.
- `../prototype/internal/classifier/`, `../prototype/internal/undo/` — the current implementation, which is the lower bound.

## Candidates not taken

Weighed as the thesis's algorithmic contribution and rejected. Recorded because a rejected candidate is evidence the chosen one was chosen, and because several remain available as extensions.

| Candidate | Why not |
|---|---|
| **Pre-execution verification** — does the generated command do what was asked? | Genuinely strong, and supported by the finding that verification is what makes command-line agents outperform screen-based ones. But nothing of it is built, and its evaluation entangles with model quality: a weak result cannot be separated from a weak generator. Remains the strongest available extension |
| **Failure recovery and reformulation** — on a failed step, retry, reformulate, decompose, or abort | A real observed defect: the loop repeats an identical failing command to the step cap. But with a 3B model it is not possible to separate the algorithm's contribution from the model's ceiling, and a panel will ask |
| **Context compression** — what to retain within a token budget | Too thin to carry a thesis alone. Partially built already (`internal/session`, D10) |
| **Risk-tiered gating** — graded friction rather than a binary gate | Interesting, and connects to confirmation fatigue as a documented failure mode of gate designs. Too thin alone; a natural extension of A1 once recoverability is computed rather than matched |
| **Termination policy** — when to stop the loop | Not a contribution on its own. Now has a measured defect behind it (below), which makes it a good *engineering* fix and still not a thesis |
| **Prompt grounding and the answer layer** — stating the working directory, summarising results | Deliberately not claimed. Both are prompt engineering: measurably valuable (grounding moved Layer 7's plain tier from 50.0% to 66.7%) and not algorithmic. Claiming them would invite scrutiny the credible entry would then have to survive |

### Briefly promoted, then pulled back — 2026-09-14

Risk-tiered gating and termination policy were written up as Entries 3 and 4 on 2026-09-14, in response to a reviewer asking "what specific algorithm are you proposing?", on the argument that neither was standalone any more once an effect set existed to define them over. That argument is not wrong, but the motive was: four procedures answer that question more impressively than one, and the original judgements — "too thin alone", "not a contribution on its own" — were not actually overturned by anything the project had learned. They are restored above, unchanged.

One contribution with depth is worth more than four with none, and a reviewer who finds the weakest of four has discredited all of them. What the episode did produce is kept: the effect semantics is now stated explicitly as Entry 1's formal model rather than left implicit in its approach, which was a real gap.

**The termination defect is real and stays recorded** — as an engineering problem, in `open-problems.md` row 16 and `../prototype/testing-plan.md`, not as a contribution. Layer 7 (2026-09-14) recorded a task reaching the correct end state at step 2, the loop failing to recognise it, continuing, and destroying the result; and separately, offering the model a completion option causing it to declare completion before running anything, dropping the technical tier from 100% to 58.3%.
