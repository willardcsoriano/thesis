## Overview

SynapseOS is built on a deliberate two-language split: Go owns the runtime (everything the user touches), and Python 3.12 owns the build pipeline (fine-tuning and evaluation, which run offline). Ollama acts as the bridge — it serves Qwen2.5-Coder-3B-Instruct via a REST API that both languages can call identically, decoupling the application from the inference engine. No IPC between Go and Python is needed; they are entirely separate concerns that never run at the same time. The deployed artifact is a single Go binary. Python is invoked only during model training and data analysis. **Ollama's own role here is not fully settled** — `decisions.md` D8 raised an open, unresolved reconsideration of whether its full orchestration shell is worth keeping given SynapseOS's fixed single-model/single-target deployment, versus embedding the inference engine more directly; the description below reflects the current, not necessarily final, state.

## Table of Contents

- [Overview](#overview)
- [Stack](#stack)
  - [Inference Layer](#inference-layer)
  - [Go — Runtime](#go-runtime)
  - [Python 3.12 — Build / Evaluation Pipeline](#python-312-build-evaluation-pipeline)
- [Minimum Runtime Environment](#minimum-runtime-environment)
- [Data Flow (runtime)](#data-flow-runtime)

## Stack

### Inference Layer

| Component | Role |
|---|---|
| **Ollama** | Model lifecycle management; serves Qwen via REST API at `localhost:11434` |
| **Qwen2.5-Coder-3B-Instruct** | Intent parser — NL → bash translation |
| Quantization | Q4_K_M (~1.8 GB); CPU inference, no GPU required |
| Cloud opt-in | Any OpenAI-compatible API endpoint; user-supplied API key replaces Ollama endpoint |

---

### Go — Runtime

The session manager, TUI, and execution engine. Ships as a single binary.

| Concern | Library |
|---|---|
| TUI framework | [bubbletea](https://github.com/charmbracelet/bubbletea) — v2 line, module path `charm.land/bubbletea/v2` (moved off `github.com/charmbracelet/...`, confirmed via `go get`), requires Go ≥ 1.25 (`prototype/go.mod` bumped for M3b — done) |
| TUI styling | [lipgloss](https://github.com/charmbracelet/lipgloss) — v2 line, module path `charm.land/lipgloss/v2`, same Go ≥ 1.25 requirement |
| TUI scrollback | [bubbles](https://github.com/charmbracelet/bubbles)/`viewport` — v2 line, module path `charm.land/bubbles/v2` |
| Ollama client | HTTP to `localhost:11434/api/generate`, non-streaming today (`Stream: false`, `internal/ollama.Client.Generate`) — streaming (`stream: true`, newline-delimited JSON per token, `done: true` on the final line) is M3b's step 4, not yet built; verified current wire format via Ollama's own API docs before planning that step, not assumed |
| Bash execution | `os/exec` — subprocess with stdout/stderr streaming |
| Confirmation gate | Custom: classifies command reversibility before dispatch |
| Undo log | Custom: records reversible operations for rollback, and confirmed irreversible ones too via dedicated per-shape mechanisms (content backup, trash, metadata backup, git-reset capture — see `safety-model.md`) |
| Session context | In-memory conversation history passed as prompt context |

**Why Go here:** goroutines are a natural fit for streaming model tokens into the TUI as they arrive. Single binary deployment — no virtualenv, no pip installs on the evaluation machine.

---

### Python 3.12 — Build / Evaluation Pipeline

Runs offline. Never executes during the user study.

| Concern | Library |
|---|---|
| LoRA fine-tuning | [Unsloth](https://github.com/unslothai/unsloth) + [PEFT](https://github.com/huggingface/peft) |
| Base model loading | [Transformers](https://github.com/huggingface/transformers) (HuggingFace) |
| Training dataset | NL2Bash corpus + verified InterCode subset + custom SynapseOS task suite |
| Evaluation (NL2Bash accuracy) | Execution-based scoring per Westenfelder et al. [2] |
| Statistical analysis | pandas, scipy, pingouin |
| Qualitative coding | Manual (Braun & Clarke reflexive thematic analysis) |

**Why Python here:** the ML fine-tuning ecosystem (Transformers, PEFT, Unsloth) is Python-only with no viable alternative.

---

## Minimum Runtime Environment

| Component | Minimum |
|---|---|
| OS | Debian 13 (Trixie) |
| CPU | x86-64 + AVX2; 2 vCPU minimum |
| RAM | 4 GB (CLI/TUI, D19) / 8 GB recommended (GUI, D12) |
| Storage | 10 GB |
| Display | CLI/TUI: any terminal (SSH included; no display server required). GUI: XFCE desktop, X11 session (D12) — SynapseOS launches fullscreen within it, with a participant-accessible fallback back to XFCE (D20) |

SynapseOS presents its own identity regardless of mode; Debian (all three modes) and XFCE (GUI mode only) are the reused, unadvertised substrate underneath — see `layers.md` and decisions D12, D19, D20.

## Data Flow (runtime)

```
User
 │  natural language
 ▼
TUI (Go / bubbletea)
 │  prompt + context
 ▼
Ollama REST API  ──→  Qwen2.5-Coder-3B-Instruct
 │  shell command (streamed)
 ▼
Confirmation Gate (Go)
 │  approved / logged to undo stack
 ▼
Execution Engine (Go / os/exec)
 │  stdout / stderr
 ▼
TUI (Go / bubbletea)
 │  rendered response
 ▼
User
```
