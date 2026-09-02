## Overview

This is the operational reference for running the SynapseOS prototype locally: what you need installed, how to bring up the inference server, how to run the binary, and how the module is laid out. It exists so the `README.md` can stay a high-level dashboard while the setup details live here. The prototype is a Go module (`synapseos`) that talks to a local Ollama server; Ollama is not bundled and must be installed separately. Everything is CPU-only by design (decision D8) — no GPU is required. If a command here fails, the most common cause is that Ollama is not running or the model has not been pulled.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Install Ollama and the Model](#install-ollama-and-the-model)
- [Run](#run)
- [Environment](#environment)
- [Layout](#layout)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- **Go 1.24+** — verify with `go version`.
- **Ollama** — the inference server. Not bundled; install it and pull the model (below).
- **A C compiler** (`build-essential` on Debian/Ubuntu) — optional. Only needed for `go test -race`; building and running the prototype itself needs nothing beyond the Go toolchain. `go test -race` requires cgo, which needs a real C compiler (`CGO_ENABLED=1 go test -race ./...`; without a compiler this fails with `cgo: C compiler "gcc" not found`).

## Install Ollama and the Model

Ollama's installer is interactive, so run it in your own shell (in a Claude Code session you can prefix with `!` to run it in-session):

```sh
# Install — see https://ollama.com/download for platform-specific options
curl -fsSL https://ollama.com/install.sh | sh

# Start the server (serves localhost:11434)
ollama serve

# Pull the intent-parsing model (decision D3; ~1.8 GB at Q4_K_M)
ollama pull qwen2.5-coder:3b
```

Confirm the server is up: `curl -s http://localhost:11434/api/tags` should return JSON.

## Run

From this directory:

```sh
# Run the built-in sample suite (8 tasks across the 4 study categories)
go run ./cmd/synapse

# Run one ad-hoc task
go run ./cmd/synapse "find the 10 largest files under /var/log"

# Persistent multi-task session (M3a) — one process, issue several tasks in a row
go run ./cmd/synapse repl

# Full-screen TUI mode (M3b) — streaming, scrollback; needs a real terminal
go run ./cmd/synapse tui

# Reverse the most recent auto-run or confirmed command
go run ./cmd/synapse undo

# Build a binary instead of go run
go build -o bin/synapse ./cmd/synapse
./bin/synapse
```

## Environment

| Variable | Default | Purpose |
|---|---|---|
| `SYNAPSE_MODEL` | `qwen2.5-coder:3b` | Ollama model tag |
| `SYNAPSE_OLLAMA` | `http://localhost:11434` | Ollama endpoint |

## Layout

```
prototype/
├── cmd/synapse/main.go          # entrypoint — one-shot task, persistent repl (M3a), and undo subcommands
├── internal/ollama/client.go    # Ollama REST client (the only code that knows the inference engine)
├── internal/classifier/         # reversibility classifier (pattern-matches known-irreversible command shapes)
├── internal/executor/           # os/exec subprocess dispatch, stdout/stderr capture, exit-code surfacing
├── internal/tui/                # TUI mode (M3b): bubbletea model, streaming, scrollback
├── internal/undo/               # undo mechanisms: directory-diff, content-backup, trash, metadata, git-reset
└── internal/typedops/           # typed file operations (F4 experiment; not wired into the default runtime path)
```

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `ollama not reachable at http://localhost:11434` | Server not running — `ollama serve` |
| `ollama returned 404 ... model not found` | Model not pulled — `ollama pull qwen2.5-coder:3b` |
| Command output wrapped in ``` ``` ``` fences | Expected from small models; the skeleton strips them (`cleanCommand`) |
| Generation takes 30–60s | Normal for CPU inference on first token; the client has no timeout by design |
