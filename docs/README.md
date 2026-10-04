# Documentation Map

## Overview

This is the index to every document in the SynapseOS project: what each one owns, what it deliberately does *not* own, and the order to read them in when arriving cold. It exists because this repo treats duplicated content as a defect rather than as redundancy — a fact recorded in two files is a fact that will eventually disagree with itself, and that has already happened here more than once. Each document below answers exactly one question, and the boundary between them is stated so that new material has an obvious home. Read the four-file core if you need to understand the project; read the whole table before adding a document, because the file you want almost certainly exists. Files are grouped by what they are *for*, not by where they sit on disk.

## Table of Contents

- [Overview](#overview)
- [Start here](#start-here)
- [Tier 1 — Authoritative](#tier-1-authoritative)
- [Tier 2 — Reference](#tier-2-reference)
- [Tier 3 — Logs](#tier-3-logs)
- [Tier 4 — Working notes](#tier-4-working-notes)
- [The thesis itself](#the-thesis-itself)
- [Where does this belong?](#where-does-this-belong)
- [Rules that keep this map true](#rules-that-keep-this-map-true)
- [Change log for this map](#change-log-for-this-map)

## Start here

Arriving cold, in this order:

1. **`vision.md`** — what SynapseOS is and who it is for. Twenty minutes.
2. **`open-problems.md`** — what is currently broken, blocked, or undecided. One page, and the fastest way to know where things actually stand.
3. **`session.md`** — what happened last and what is pending. This is the handoff.
4. **`../prototype/README.md`** — what is actually built right now.
5. **`decisions.md`** — why anything is the way it is. Skim the index; read the entries you need.

That is enough to work. Everything else is consulted when a specific question arises.

## Tier 1 — Authoritative

These decide things. If one of them disagrees with another document, these win.

| File | Owns | Does *not* own |
|---|---|---|
| `decisions.md` | Every architecture and study-design decision, D1–D31. Append-only; decisions are superseded **in place** with a dated note, never rewritten or deleted | Status, deliverables, or how to build anything |
| `scope.md` | The complete deliverables checklist and the three-track critical path (build / ethics / algorithm). If the thesis promised it exists, it is listed here | *Why* a deliverable exists, or the order it gets built in |
| `prior-art.md` | What already exists for a problem, surveyed **before** anything is built for it — adopt, borrow the pattern, or build, with the evidence for whichever it was. The engineering counterpart to Chapter 2's academic review | The design of what gets built (that is `algorithms.md`) |
| `algorithms.md` | The design record for everything the project **builds** rather than adopts — problem, why nothing existing does it, approach, failure modes, evaluation. Also the adoption ledger, and the candidates rejected. **Its contents are the contribution, by construction** | The survey that justified building (that is `prior-art.md`), the decision to pursue it (`decisions.md`), or the build order (`build-order.md`) |
| `safety-model.md` | The classifier-verdict and undo-mechanism taxonomy: which command shape gets which undo path, and why | The recoverability algorithm — that is `algorithms.md` Entry 1. This file is the baseline it must beat |
| `vision.md` | Product vision, the falsifiable hypothesis, the audience, the non-goals, and the three horizons | Current status of anything |
| `../prototype/build-order.md` | Milestone sequence M1–M8, the F-track, and the Algorithm Track A1–A3; each milestone's dependencies and definition-of-done | The deliverables registry (that is `scope.md`) |

## Tier 2 — Reference

These explain things. Consulted, not read front to back.

| File | Owns |
|---|---|
| `interface-modes.md` | CLI/TUI/GUI boundaries — what the shared core is, what each mode reuses versus builds, and the GUI session plumbing |
| `layers.md` | Where SynapseOS sits in the Linux stack, the "is this an OS / a distro?" question, and the glossary |
| `drift.md` | Every claim in the project that goes stale — model choices, benchmark numbers, library APIs — and how to recheck each. **Read before citing any figure as current** |
| `stack.md` | The concrete implementation stack |
| `../prototype/testing-plan.md` | The six-layer engine test plan (F3/F4) |
| `../prototype/setup.md` | Prerequisites, install, run commands |
| `../prototype/manual-tests/` | One hands-on walkthrough per milestone, for what automated tests cannot cover |
| `diagrams/` | Standalone HTML figures used in the paper and in explanation |

## Tier 3 — Logs

These remember things. They are append-mostly and they grow.

| File | Owns | Note |
|---|---|---|
| `session.md` | Session-by-session handoff: what changed, what is pending, the "Files to Know" index | **Not tracked by git** — it is in `.gitignore` deliberately, so it exists only on the working machine and has no history or backup |
| `open-problems.md` | **The live register — everything currently broken, blocked, or undecided.** Holds only open items; a resolved row moves out to `retrospective.md`, so the file stays bounded by what is outstanding rather than by project history | Tracked |
| `retrospective.md` | Process log: blockers hit, wrong turns taken, and the reusable lesson from each — and the archive resolved rows move into. Distinct from `session.md`, which records *what shipped* rather than *what was learned* | Tracked |

## Tier 4 — Working notes

`docs/notes/` — not authoritative, and nothing should depend on them for correctness. Grouped into a subfolder so the top level of `docs/` contains only files something else references.

| File | Owns |
|---|---|
| `notes/future-features.md` | Deferred ideas, explicitly out of thesis scope |
| `notes/wordbank.md` | Terminology for the paper. Dormant, but live: `scope.md` has an open deliverable to re-verify these terms at final compilation |
| `notes/brainstorm.md` | Unresolved ideas. Dormant, but live: D8 references it for an open reconsideration |

## The thesis itself

Not documentation — the artifact.

| Path | What it is |
|---|---|
| `../thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html` | **The sole living document** for Chapters 1–3. Its paired `.pdf` is what gets graded and must be re-exported after every edit |
| `../research-methods/archive/submitted-2026-07-10/` | The frozen graded snapshot. Historical record — never edited |

## Where does this belong?

The question that prevents this map from decaying.

| If you are recording… | It goes in |
|---|---|
| A choice, with the reasoning and what was rejected | `decisions.md` |
| Something that must exist before the thesis is done | `scope.md` |
| What to build next and when it counts as finished | `../prototype/build-order.md` |
| A classifier rule or undo mechanism | `safety-model.md` |
| How an existing tool already solves a problem we have | `prior-art.md` |
| Something the project **builds** rather than adopts | `algorithms.md` — after `prior-art.md` shows why, written before the code and amended after |
| Something the project **adopts** instead of building | `algorithms.md`'s adoption ledger |
| What shipped this session | `session.md` |
| Something currently broken, blocked, or undecided | `open-problems.md` — log it when you hit it, not when you fix it |
| A mistake worth not repeating, or a problem now resolved | `retrospective.md` |
| A fact that will be wrong in six months | `drift.md` |
| An idea for after the thesis | `notes/future-features.md` |

If it fits none of these, that is the only case where a new document is warranted — and it needs a row here before it is written.

## Rules that keep this map true

- **Three tiers, one direction.** Dirty work happens in `prototype/`, soft documentation here, finalized work in `research-methods/`. Each tier has a different standard of finish — the prototype iterates and throws things away, these docs are honest and current but not polished, and the chapters are held to publication standard. Settle a thing in the cheapest tier that can settle it, then render it upward.

  The rule is economic rather than procedural. A chapter edit is the most expensive operation in this repo: it forces a re-export, a re-derivation of hardcoded TOC page numbers against the actual PDF, an ITRD compliance re-check, and the HTML/PDF pair must move together. A doc edit costs prose alone. A code change costs nothing the compiler and tests don't catch for free. Polishing a chapter about an idea that is not settled means paying the highest cost twice.

  So: **never fix a chapter before fixing the doc it should have come from**, and never write a doc for something the prototype has not yet shown to be true. When a chapter and a doc disagree, the doc is right and the chapter is stale by definition. Every serious error found in the audit of Chapters 1–3 came from prose that had drifted from the system it described, or that asserted something no document here ever decided.
- **Adopt by default; build only what cannot be adopted** (`vision.md`, Principles), and **survey before building**. The order is `prior-art.md` → `algorithms.md` → code: find out who has already solved it, and only then design. A build decision that cannot name what was checked is a preference, not a finding. The consequence for documentation is that `algorithms.md` stays short on purpose, that its adoption ledger is maintained with equal care — a short contribution list is only credible beside a long adoption list — and that negative survey results are never deleted once the thing is built, because they are the only evidence the contribution claim has.
- **One fact, one home.** Duplication is a defect. Link instead. The clearest recent failure: milestone status lived in four files at once and drifted between them, which is why `roadmap.md` was retired.
- **Supersede, don't rewrite.** A decision that changes gets a dated revision note in place. The old reasoning is the record of why the change was needed.
- **Every `.md` opens with `## Overview`**, roughly 4–6 sentences, standing on its own for a reader with thirty seconds.
- **Never hand-write a Table of Contents.** A PostToolUse hook generates it. Edit `.md` files with the Edit/Write tools rather than shell commands, or the hook will not fire and the TOC will silently rot.

## Change log for this map

**2026-09-13** — `prior-art.md` added (survey before building; the practice behind the adopt-first principle) and `open-problems.md` added (live register of what is open; resolved rows move out to `retrospective.md`). `algorithms.md` added, taking the recoverability specification out of `safety-model.md`, which keeps the taxonomy.

**2026-09-12** — Map created. `roadmap.md` retired: its cross-horizon status was the fourth copy of a status already in `scope.md`, `build-order.md`, and `prototype/README.md`, and its horizon framing moved into `vision.md`. `future-features.md`, `wordbank.md`, and `brainstorm.md` moved into `notes/`. `layers.md` trimmed of the mode-by-mode table that `interface-modes.md` owns. `safety-model.md` gained the D29 algorithm specification.
