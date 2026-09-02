## Overview

This folder holds one manual test suite per milestone — created as that milestone's final step (see `../build-order.md`'s Sequencing Principles), after the automated tests already pass. Its job is letting a human actually verify a milestone's claims in a real terminal, rather than trusting a status write-up. One file per milestone, scoped only to what that milestone added — not a single combined walkthrough that grows without bound — so a suite stays quick to run and easy to point someone at.

## Table of Contents

- [Overview](#overview)
- [Suites](#suites)
- [Convention for new suites](#convention-for-new-suites)

## Suites

| File | Milestone(s) | What it covers |
|---|---|---|
| `m2-cli-mode-and-undo-safety-net.md` | M2, Foundational Hardening (F1–F5) | Propose/classify/execute, the confirmation gate, and all five undo mechanisms (directory-diff, content-backup, trash, metadata backup, git-reset) |
| `m3a-persistent-loop.md` | M3a | `synapse repl` — a persistent multi-task session, specifically the risk that a confirmation answer and the next typed task never get confused with each other |
| `m3b-tui-mode.md` | M3b | `synapse tui` — full-screen rendering, token streaming, scrollback, and the confirmation gate as UI. Carries more weight than the others: a TUI cannot be driven by piped input, so nothing here has ever been run end-to-end by anyone |

## Convention for new suites

- **Scope: golden path, plus human-only edge cases — never exhaustive.** Exhaustive coverage is `testing-plan.md`'s job (the adversarial classifier corpus, executor chaos tests, live-model harness) — it's cheap to re-run and already does that well. A manual suite duplicating it adds real per-run cost (terminal time, model-inference latency) for zero new confidence. Test the realistic path first, then only edge cases a unit test *structurally cannot observe* — real terminal interaction, real timing, real stdin/stdout interleaving, things that only exist once a human is actually typing into a real process.
- **Open with a short "Automated coverage" section** — a few sentences naming the relevant test file(s), roughly how many tests / what coverage, and what they actually prove, so the reader has context on what's already been machine-verified before running anything by hand. Not exhaustive detail (link to `testing-plan.md` or `build-order.md` for that) — just enough that the reader knows which "flavor" of confidence already exists (logic proven against a mock) versus what this suite adds (behavior proven against the real binary, real terminal, real model).
- One file per milestone, named for what it tests (not the milestone number alone) — e.g. `m3a-persistent-loop.md`, not `m3a.md`.
- Only test what that milestone actually added. Mechanics already covered by an earlier suite (classification, confirmation, undo) don't need re-testing unless the milestone changed them.
- Every `.md` file in this folder needs the `## Overview` opener per this project's markdown convention, and gets added to the table above when created.
- Written for a non-expert reader running commands in a terminal for the first time — plain-language comments on every real command line, explicit "what to expect" after each step, no assumed familiarity with the underlying code.
