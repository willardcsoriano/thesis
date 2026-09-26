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
