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
├── cmd/synapse/main.go        # entrypoint — milestone 2 skeleton (NL → proposed command)
└── internal/ollama/client.go  # Ollama REST client (the only code that knows the inference engine)
```

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `ollama not reachable at http://localhost:11434` | Server not running — `ollama serve` |
| `ollama returned 404 ... model not found` | Model not pulled — `ollama pull qwen2.5-coder:3b` |
| Command output wrapped in ``` ``` ``` fences | Expected from small models; the skeleton strips them (`cleanCommand`) |
| Generation takes 30–60s | Normal for CPU inference on first token; the client has no timeout by design |
