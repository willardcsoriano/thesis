## Overview

This folder holds all SynapseOS prototype code — the runnable artifact behind the thesis, kept separate from the written chapters and planning docs at the repo root. It is a Go module (`synapseos`) that grows one milestone at a time from a walking skeleton into the full conversational-shell runtime described in `../scope.md`. This README is the oversight view: current status and where to look — the detail lives in the linked docs so this page stays readable at a glance. For how to run it see `setup.md`; for what gets built in what order and when each step is "done" see `build-order.md`. Everything targets the stack fixed in `../decisions.md` (D8): Go runtime, Ollama serving Qwen2.5-Coder-3B, CPU-only, single binary.

## Table of Contents

- [Overview](#overview)
- [Status at a Glance](#status-at-a-glance)
- [Documentation](#documentation)
- [Quickstart](#quickstart)

## Status at a Glance

Current milestone: **M2 — Walking skeleton** (runs; prints proposed bash, executes nothing).

| Milestone | What it adds | Status |
|---|---|---|
| M2 | NL → Ollama → proposed bash (no execution) | ✅ done |
| M3 | bubbletea/lipgloss TUI chat loop | ⬜ next |
| M4 | Execution engine (`os/exec`, streamed output) | ⬜ |
| M5 | Confirmation gate (reversible vs. irreversible) | ⬜ |
| M6 | Session context (rolling window, output compression) | ⬜ |
| M7 | Session logger (study telemetry) | ⬜ |
| M8 | Undo log | ⬜ |
| M9+ | GUI mode → fine-tuning pipeline → study instruments | ⬜ |

Per-milestone goals, dependencies, and definition-of-done: **`build-order.md`**.

## Documentation

| Doc | Purpose |
|---|---|
| `setup.md` | Prerequisites, install, run commands, environment, layout |
| `build-order.md` | Milestone sequence, gates, and definition-of-done |
| `../scope.md` | Full deliverables registry (what must exist) |
| `../decisions.md` | Architecture rationale (D1–D11) |
| `../stack.md` | Implementation stack reference |
| `../vision.md` | Product vision and thesis hypothesis |
| `../roadmap.md` | Cross-horizon status index |

## Quickstart

```sh
go run ./cmd/synapse            # run the sample task suite (needs Ollama running)
```

Full setup — including installing Ollama and pulling the model — is in `setup.md`.
