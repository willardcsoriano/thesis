## Overview

This folder holds all SynapseOS prototype code — the runnable artifact behind the thesis, kept separate from the written chapters and planning docs at the repo root. It is a Go module (`synapseos`) that grows one milestone at a time from a walking skeleton into the full conversational-shell runtime described in `../docs/scope.md`. This README is the oversight view: current status and where to look — the detail lives in the linked docs so this page stays readable at a glance. For how to run it see `setup.md`; for what gets built in what order and when each step is "done" see `build-order.md`. Everything targets the stack fixed in `../docs/decisions.md` (D8): Go runtime, Ollama serving Qwen2.5-Coder-3B, CPU-only, single binary.

## Table of Contents

- [Overview](#overview)
- [Status at a Glance](#status-at-a-glance)
- [Documentation](#documentation)
- [Quickstart](#quickstart)

## Status at a Glance

Current milestone: **M1 — CLI mode: propose, classify, execute — complete** (all three stages validated live against Ollama 2026-07-12, and covered by automated tests as of Session 21 — see `build-order.md`). **Foundational Hardening (F1–F5), added Session 22, pulled forward ahead of TUI work — complete as of Session 26.** **M4 (persistent CLI loop, `synapse repl`) complete as of Session 27** and **M5 (TUI mode) complete as of Session 28**, and **M6 (session context — cross-task memory) complete as of Session 29**, and **M7 (session logger) complete as of Session 31**. Next up: M8 (GUI mode).

Three interface modes, ship-scoped by D19/D20 and rescoped by D27: **CLI** (one-shot invocation, M1, done), **TUI** (persistent terminal session, M4 + M5, done), **GUI** (the TUI launched fullscreen over a live XFCE desktop, with a participant-accessible fallback to that desktop, M8). D27 settled SynapseOS as an agentic layer running *over* an ordinary desktop rather than replacing the desktop shell, session manager, and application launcher — which is why GUI mode is now session plumbing rather than a second interface to build. CLI mode was built out to full completion (propose + execute + confirmation gate) before TUI work starts — TUI wraps that already-working core in an interactive surface rather than building execution and confirmation from scratch. M3 was split into two sub-milestones 2026-08-21: M4 proves persistent multi-turn session lifecycle cheaply (plain stdin loop, reusing M1's `runLoop` as-is), before M5 spends effort on the bubbletea/lipgloss rendering layer — see `build-order.md` for the full rationale. M4 is an internal build checkpoint, not a fourth interface mode (D19 still ships exactly three).

Ordered by what actually happened, not by number — M2 and M3 were built inside M1 and shipped through CLI mode long before any TUI, and the former M8 (undo telemetry) folded into M7. Numbers stay fixed as identifiers; the sequence reflects the real build.

| Milestone | What it adds | Status |
|---|---|---|
| M1 | NL → Ollama → proposed bash, reversibility classification, `os/exec` execution — *is* CLI mode (D19) | ✅ done, validated live, test-covered |
| M2 | Execution engine — *built as part of M1 and shipped via CLI, before any TUI existed* | ✅ done (in M1) |
| M3 | Confirmation gate — *built as part of M1 and shipped via CLI, before any TUI existed* | ✅ done (in M1) |
| F1 | Classifier coverage for content-mutating commands (`sed -i`, `awk -i`, `tee`, `truncate`) | ✅ done |
| F2 | Mechanical undo log (`internal/undo`), disk-persisted, `synapse undo` subcommand | ✅ done |
| F3 | Rigorous, model-parameterized engine test suite | ✅ done |
| F4 | Typed-operation (filesystem-MCP-style) reliability experiment | ✅ done |
| F5 | Guiltless undo hardening — trash, git-reset capture, permission-metadata backup | ✅ done |
| M4 | Persistent multi-turn stdin loop (`synapse repl`) wrapping M1's `runLoop`, no rendering — interim risk-reduction step | ✅ done |
| M5 | bubbletea/lipgloss TUI chat loop with token streaming and scrollback, wrapping M4's loop | ✅ done |
| M6 | Session context (rolling window, output compression) — cross-task memory, `context`/`clear` | ✅ done |
| F6 | Act on F4's typed-ops verdict — a research-design decision, not just wiring | ⬜ |
| M7 | Session logger (study telemetry) + `undo_invoked` events (absorbed the former M8) | ✅ done |
| M8 | GUI mode — the TUI fullscreen over a live XFCE desktop, plus the fallback (D20, D27); session plumbing only | ⬜ |
| A1 | Command effect model + labelled recoverability corpus | ⬜ |
| A2 | Compositional recoverability classifier — **the thesis's algorithmic contribution (D29)** | ⬜ |
| A3 | Algorithm evaluation against both baselines; writable without waiting on IRB | ⬜ |
| V1 | End-to-end validation: run every manual suite by hand before the study | ⬜ |
| P1+ | Fine-tuning pipeline → study instruments → analysis | ⬜ |

Per-milestone goals, dependencies, and definition-of-done: **`build-order.md`**.

## Documentation

| Doc | Purpose |
|---|---|
| `setup.md` | Prerequisites, install, run commands, environment, layout |
| `build-order.md` | Milestone sequence, gates, and definition-of-done |
| `testing-plan.md` | The rigorous, model-parameterized engine test plan (F3/F4) — six layers from deterministic unit tests through a typed-operation reliability experiment |
| `../docs/scope.md` | Full deliverables registry (what must exist) |
| `../docs/decisions.md` | Architecture rationale (D1–D26) |
| `../docs/interface-modes.md` | CLI/TUI/GUI boundaries — shared core, what each mode reuses vs. builds fresh, GUI session plumbing |
| `../docs/algorithms.md` | Design record for everything built rather than adopted — including the recoverability-analysis contribution (D29) — plus the adoption ledger |
| `../docs/safety-model.md` | Classifier verdict + undo taxonomy; the baseline the recoverability algorithm must beat |
| `../docs/stack.md` | Implementation stack reference |
| `../docs/vision.md` | Product vision and thesis hypothesis |

## Quickstart

```sh
go run ./cmd/synapse            # run the sample task suite (needs Ollama running)
```

Full setup — including installing Ollama and pulling the model — is in `setup.md`.
