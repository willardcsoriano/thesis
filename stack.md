## Overview

SynapseOS is built on a deliberate two-language split: Go owns the runtime (everything the user touches), and Python 3.12 owns the build pipeline (fine-tuning and evaluation, which run offline). Ollama acts as the bridge — it serves Qwen2.5-Coder-3B-Instruct via a REST API that both languages can call identically, decoupling the application from the inference engine. No IPC between Go and Python is needed; they are entirely separate concerns that never run at the same time. The deployed artifact is a single Go binary. Python is invoked only during model training and data analysis.

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
| TUI framework | [bubbletea](https://github.com/charmbracelet/bubbletea) |
| TUI styling | [lipgloss](https://github.com/charmbracelet/lipgloss) |
| Ollama client | HTTP streaming to `localhost:11434/api/generate` |
| Bash execution | `os/exec` — subprocess with stdout/stderr streaming |
| Confirmation gate | Custom: classifies command reversibility before dispatch |
| Undo log | Custom: records reversible operations for rollback |
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
| RAM | 4 GB |
| Storage | 10 GB |
| Display | Any terminal (SSH included; no display server required) |

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
