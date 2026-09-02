## Overview

This folder holds all SynapseOS prototype code — the runnable artifact behind the thesis, kept separate from the written chapters and planning docs at the repo root. It is a Go module (`synapseos`) that grows one milestone at a time from a walking skeleton into the full conversational-shell runtime described in `../docs/scope.md`. This README is the oversight view: current status and where to look — the detail lives in the linked docs so this page stays readable at a glance. For how to run it see `setup.md`; for what gets built in what order and when each step is "done" see `build-order.md`. Everything targets the stack fixed in `../docs/decisions.md` (D8): Go runtime, Ollama serving Qwen2.5-Coder-3B, CPU-only, single binary.

## Table of Contents

- [Overview](#overview)
- [Status at a Glance](#status-at-a-glance)
- [Documentation](#documentation)
- [Quickstart](#quickstart)

## Status at a Glance

Current milestone: **M2 — CLI mode: propose, classify, execute — complete** (all three stages validated live against Ollama 2026-07-12, and covered by automated tests as of Session 21 — see `build-order.md`). **Foundational Hardening (F1–F5), added Session 22, pulled forward ahead of TUI work — complete as of Session 26.** **M3a (persistent CLI loop, `synapse repl`) complete as of Session 27** and **M3b (TUI mode, `synapse tui` — full-screen rendering, token streaming, scrollback) complete as of Session 28**. Next up: M6 (session context).

Three interface modes, ship-scoped by D19/D20: **CLI** (one-shot invocation, M2, done), **TUI** (persistent terminal session, M3a + M3b, done), **GUI** (fullscreen study takeover with a participant-accessible XFCE fallback, M9). CLI mode was built out to full completion (propose + execute + confirmation gate) before TUI work starts — TUI wraps that already-working core in an interactive surface rather than building execution and confirmation from scratch. M3 was split into two sub-milestones 2026-08-21: M3a proves persistent multi-turn session lifecycle cheaply (plain stdin loop, reusing M2's `runLoop` as-is), before M3b spends effort on the bubbletea/lipgloss rendering layer — see `build-order.md` for the full rationale. M3a is an internal build checkpoint, not a fourth interface mode (D19 still ships exactly three).

| Milestone | What it adds | Status |
|---|---|---|
| M2 | NL → Ollama → proposed bash, reversibility classification, `os/exec` execution — *is* CLI mode (D19) | ✅ done, validated live, test-covered |
| F1 | Classifier coverage for content-mutating commands (`sed -i`, `awk -i`, `tee`, `truncate`) | ✅ done |
| F2 | Mechanical undo log (`internal/undo`), disk-persisted, `synapse undo` subcommand | ✅ done |
| F3 | Rigorous, model-parameterized engine test suite | ✅ done |
| F4 | Typed-operation (filesystem-MCP-style) reliability experiment | ✅ done |
| F5 | Guiltless undo hardening — trash, git-reset capture, permission-metadata backup | ✅ done |
| M3a | Persistent multi-turn stdin loop (`synapse repl`) wrapping M2's `runLoop`, no rendering — interim risk-reduction step | ✅ done |
| M3b | bubbletea/lipgloss TUI chat loop with token streaming and scrollback, wrapping M3a's loop | ✅ done |
| M4 | *(merged into M2 — see `build-order.md`)* | — |
| M5 | *(merged into M2 — see `build-order.md`)* | — |
| M6 | Session context (rolling window, output compression) | ⬜ up next |
| M7 | Session logger (study telemetry) | ⬜ |
| M8 | Undo log | ⬜ |
| M9+ | GUI mode + XFCE fallback (D20) → fine-tuning pipeline → study instruments | ⬜ |

Per-milestone goals, dependencies, and definition-of-done: **`build-order.md`**.

## Documentation

| Doc | Purpose |
|---|---|
| `setup.md` | Prerequisites, install, run commands, environment, layout |
| `build-order.md` | Milestone sequence, gates, and definition-of-done |
| `testing-plan.md` | The rigorous, model-parameterized engine test plan (F3/F4) — six layers from deterministic unit tests through a typed-operation reliability experiment |
| `../docs/scope.md` | Full deliverables registry (what must exist) |
| `../docs/decisions.md` | Architecture rationale (D1–D26) |
| `../docs/interface-modes.md` | CLI/TUI/GUI boundaries — shared core, what each mode reuses vs. builds fresh, GUI kiosk-takeover mechanics |
| `../docs/stack.md` | Implementation stack reference |
| `../docs/vision.md` | Product vision and thesis hypothesis |
| `../docs/roadmap.md` | Cross-horizon status index |

## Quickstart

```sh
go run ./cmd/synapse            # run the sample task suite (needs Ollama running)
```

Full setup — including installing Ollama and pulling the model — is in `setup.md`.
