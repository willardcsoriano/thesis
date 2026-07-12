## Overview

This folder holds all SynapseOS prototype code — the runnable artifact behind the thesis, kept separate from the written chapters and planning docs at the repo root. It is a Go module (`synapseos`) that grows one milestone at a time from a walking skeleton into the full conversational-shell runtime described in `../docs/scope.md`. This README is the oversight view: current status and where to look — the detail lives in the linked docs so this page stays readable at a glance. For how to run it see `setup.md`; for what gets built in what order and when each step is "done" see `build-order.md`. Everything targets the stack fixed in `../docs/decisions.md` (D8): Go runtime, Ollama serving Qwen2.5-Coder-3B, CPU-only, single binary.

## Table of Contents

- [Overview](#overview)
- [Status at a Glance](#status-at-a-glance)
- [Documentation](#documentation)
- [Quickstart](#quickstart)

## Status at a Glance

Current milestone: **M2 — CLI mode: propose, classify, execute** (propose validated live against Ollama 2026-07-12; classify + execute not yet built — see `build-order.md`).

Three interface modes, ship-scoped by D19/D20: **CLI** (one-shot invocation, M2), **TUI** (persistent terminal session, M3), **GUI** (fullscreen study takeover with a participant-accessible XFCE fallback, M9). CLI mode is being built out to full completion (propose + execute + confirmation gate) before TUI work starts — TUI wraps that already-working core in an interactive surface rather than building execution and confirmation from scratch.

| Milestone | What it adds | Status |
|---|---|---|
| M2 | NL → Ollama → proposed bash, reversibility classification, `os/exec` execution — *is* CLI mode (D19) | 🚧 propose done + validated; classify/execute next |
| M3 | bubbletea/lipgloss TUI chat loop, wrapping M2's core | ⬜ blocked on M2 completion |
| M4 | *(merged into M2 — see `build-order.md`)* | — |
| M5 | *(merged into M2 — see `build-order.md`)* | — |
| M6 | Session context (rolling window, output compression) | ⬜ |
| M7 | Session logger (study telemetry) | ⬜ |
| M8 | Undo log | ⬜ |
| M9+ | GUI mode + XFCE fallback (D20) → fine-tuning pipeline → study instruments | ⬜ |

Per-milestone goals, dependencies, and definition-of-done: **`build-order.md`**.

## Documentation

| Doc | Purpose |
|---|---|
| `setup.md` | Prerequisites, install, run commands, environment, layout |
| `build-order.md` | Milestone sequence, gates, and definition-of-done |
| `../docs/scope.md` | Full deliverables registry (what must exist) |
| `../docs/decisions.md` | Architecture rationale (D1–D20) |
| `../docs/stack.md` | Implementation stack reference |
| `../docs/vision.md` | Product vision and thesis hypothesis |
| `../docs/roadmap.md` | Cross-horizon status index |

## Quickstart

```sh
go run ./cmd/synapse            # run the sample task suite (needs Ollama running)
```

Full setup — including installing Ollama and pulling the model — is in `setup.md`.
