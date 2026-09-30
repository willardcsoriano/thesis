## Overview

This file tracks algorithmic ideas noticed while building SynapseOS that are not the thesis's committed algorithm (recoverability analysis, `algorithms.md`) but could plausibly have been, or could become a second contribution, or a post-thesis one. It exists because of a standing doubt: recoverability analysis is the algorithm we are running with, not necessarily the best one this project could produce. Logging a candidate here costs nothing and commits to nothing; it is a place to write down "this could be a real algorithm" the moment it is noticed, so the option survives past the session that noticed it. Nothing here is scheduled or promised. A candidate graduates out of this file only by an explicit decision recorded in `decisions.md`, the same as any other direction change.

## Table of Contents

- [Overview](#overview)
- [How to use this](#how-to-use-this)
- [Candidates](#candidates)
  - [2026-09-26 — Composition/resolution ablation (measurement, not a new algorithm)](#2026-09-26-compositionresolution-ablation-measurement-not-a-new-algorithm)
  - [2026-09-26 — Intent-parsing / NL-to-command translation itself](#2026-09-26-intent-parsing-nl-to-command-translation-itself)
  - [2026-09-26 — Confirmation-gate policy as a decision problem](#2026-09-26-confirmation-gate-policy-as-a-decision-problem)
  - [2026-09-26 — Minimal-cost capture plan as a general set-cover instance](#2026-09-26-minimal-cost-capture-plan-as-a-general-set-cover-instance)
  - [2026-09-26 — Provisioning as a dependency-resolution problem](#2026-09-26-provisioning-as-a-dependency-resolution-problem)
  - [2026-09-27 — Request triage (chatter vs. task vs. unsupported vs. session command)](#2026-09-27-request-triage-chatter-vs-task-vs-unsupported-vs-session-command)
  - [2026-09-27 — Effect-grounded answering](#2026-09-27-effect-grounded-answering)

## How to use this

Add an entry as soon as a candidate occurs to you or comes up while building the distro — mid-implementation is the right time, not after. Each entry: a date, a one-line name, what problem it would solve, why it's a real algorithm rather than a feature, and a rough sense of whether it's additive (alongside recoverability) or a replacement for it. Don't rank them here; ranking is a decision, not a log.

## Candidates

### 2026-09-26 — Composition/resolution ablation (measurement, not a new algorithm)

Not a candidate itself, but the reason this file exists now: the adviser's question ("how much of the performance comes from the algorithm itself vs. the manually constructed knowledge tables?") is currently answered only in prose (`decisions.md` D34, `algorithms.md` "The rule table is a list"), not measured. Logged here as the trigger for keeping this list; see `open-problems.md` for the actual open item.

### 2026-09-26 — Intent-parsing / NL-to-command translation itself

The step before recoverability even applies: turning "clean up my downloads folder" into a shell command. Currently a prompt to a stock instruction-tuned model (`internal/ollama`), not a novel algorithm — it's the one piece of the pipeline where "algorithm" could mean something closer to what a reviewer expects (a learned or structured translation method) rather than a static-analysis pass over the output. Westenfelder et al. and the NL2Bash line of work already occupy this space, so a genuine contribution here would need a angle those don't have (e.g., grounding translation in the *current* filesystem state rather than translating blind, which recoverability analysis already partly enables). Additive at most, not a replacement — the recoverability question is orthogonal to how the command was produced.

### 2026-09-26 — Confirmation-gate policy as a decision problem

The gate currently asks whenever `class != Recoverable` (strict) or `class == Unrecoverable` (capture). Both are fixed policies. A policy that adapts to a user's own history (habituation, past overrides, task context) is a real sequential-decision problem, not just a threshold — closer to a bandit or a cost-sensitive classifier than a static rule. Not pursued: it needs participant data to fit against, which puts it downstream of the user study rather than able to substitute for it.

### 2026-09-26 — Minimal-cost capture plan as a general set-cover instance

`effects.Plan()` today is a greedy per-path mechanism choice (trash vs. content vs. metadata vs. inverse), not a solved general optimization. For the shapes seen so far greedy is provably optimal (mechanisms are non-overlapping in what they cover), so there is no measured gap to close. Logged in case a future effect shape (e.g. overlapping captures with a shared cheaper mechanism) breaks that and turns it into an actual set-cover / weighted-matching problem worth naming as such.

### 2026-09-26 — Provisioning as a dependency-resolution problem

`distro/hoard.sh` and the eventual provisioning script are currently a fixed, hand-written manifest (`distro/manifest.tsv`). A real dependency-closure computation (what `apt-get` already does) is the underlying algorithm; we consume it rather than reimplementing it, per the project's "prefer what exists" principle. Not a candidate for a thesis contribution — noted only so it isn't mistaken for one later.

### 2026-09-27 — Request triage (chatter vs. task vs. unsupported vs. session command)

Not pursued, and not a good candidate on its own. Today it is an exact-match table (`answerConversational` in `cmd/synapse/main.go`): a hand-written list, the thing this thesis argues against, and the right cheap answer for a problem this small. A learned router would need data we do not have and would compete with mature intent-classification work. The one property worth keeping is the error asymmetry: swallowing a real task as chatter is dangerous, sending chatter to the model only costs latency (26 s measured on the reference machine under memory pressure), so the router fails toward the model. That mirrors the one-sided soundness rule in the recoverability analysis, but as a design note, not a contribution. Additive at most.

### 2026-09-27 — Effect-grounded answering

The stronger of the two ideas found while testing the TUI, and the one tied to the existing algorithm. Found when "are we in the root dir?" was answered "We are now in the root directory" after `cd /`, which changed nothing: the model narrated a state change that never happened, and nothing checked the claim against reality. The effect analysis already knows what a command actually does (`cd` in a per-step shell persists nothing; a `mv` moves a path; a removal removes one). An answer composed from, or checked against, that effect set instead of the model's free text would make `vision.md`'s principle ("anything I tell you can be traced to a command that actually ran") a property the system verifies rather than one it hopes for. It would share the corpus and oracle machinery (`internal/oracle` already observes real effects) and could be measured the same way: claims of change in an answer versus the observed diff. Additive alongside recoverability, not a replacement, and the one candidate here with a natural evaluation already built. Not started; interim mitigation is the deterministic refusal of a lone `cd` and of `cd … && pwd` (`bareCd`, `cdThenPwd`).
