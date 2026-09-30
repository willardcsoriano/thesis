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
- **bubblewrap** (`bwrap`) — required on the evaluation and study machines. The effect analysis (`internal/effects`) resolves run-time targets by running read-only commands inside a `bwrap` sandbox (whole filesystem read-only, other namespaces unshared), and the corpus oracle (`internal/oracle`) executes each corpus command in one to label it. Without `bwrap` resolution is switched off and any command that needed it asks for confirmation: safe, but more cautious than the measured system, so results gathered without it do not count. Install with the distribution's package manager (`apt install bubblewrap` on Debian); it needs unprivileged user namespaces.
- **Python 3** — for the pilot's scoring script (`pilot/analyze.py`, standard library only).
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

# Persistent multi-task session (M4) — one process, issue several tasks in a row
go run ./cmd/synapse repl

# Full-screen TUI mode (M5) — streaming, scrollback; needs a real terminal
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
| `SYNAPSE_NUM_CTX` | `8192` | Context window requested from Ollama, in tokens |
| `SYNAPSE_KEEP_ALIVE` | `30m` | How long Ollama keeps the model loaded after a request (`-1` never unloads); avoids a cold reload mid-session |
| `SYNAPSE_ANALYSIS` | `strict` | Effect analysis in the confirmation gate. `strict`: the analysis can add a confirmation to what the list asks and captures what it can; `capture`: capture only, no added confirmations; `off`: the list classifier alone. The list is always consulted; the analysis can add a confirmation, never remove one |

## Make targets

Run from `prototype/`. The Makefile finds Go on `PATH` or at `~/.local/go/bin/go`, and sets `SYNAPSE_OLLAMA` to `http://127.0.0.1:$(OLLAMA_PORT)`, default port 11435, which avoids a port-11434 conflict seen on the development machine (override with `OLLAMA_PORT=11434`).

| Target | What it does |
|---|---|
| `make build` / `make test` | Build `bin/synapse`; run `go test ./...` |
| `make run` | Built-in sample suite, propose-only, no filesystem changes |
| `make task TASK="…"` | One ad-hoc task through the real execute path |
| `make repl` / `make tui` / `make undo` | Persistent loop; full-screen TUI; reverse the last command |
| `make ollama-serve` / `ollama-serve-bg` / `ollama-pull` / `ollama-status` | Start Ollama in the foreground or detached on `OLLAMA_PORT`; pull `qwen2.5-coder:3b`; check reachability |
| `make pilot` | Re-run the list-versus-analysis pilot, round 2 (below) |
| `make corpus4` / `make round4` | Regenerate the automated development corpus (`SEED=20260922`, `pilot/corpus4.jsonl`) from sandboxed execution; score L0, L1, and the analysis against it. A held-out corpus is a fresh seed (`20260923`, `pilot/corpus5.jsonl`) |
| `make ci` | `fmt-check`, `vet`, and `test`: what CI runs |
| `make fmt-check` / `make vet` | The formatting check (`gofmt -l`) and `go vet` on their own |
| `make deps-check` / `make deps-hoard` | List missing dependencies and the one install line (`../distro/check.sh`); download every dependency into `../distro/hoard/` (`../distro/hoard.sh --all`) |
| `make recoverycheck` | Execute-and-diff proof, in a sandbox, that the capture plan restores the prior state |
| `make clean` | Remove `bin/` |

## Re-running the pilot

The pilot (`../docs/algorithms.md`, "Pilot — is a list already enough?") compares three systems over a frozen command corpus (rounds 1 to 3 hand-labelled; round 4 onward labelled by the sandbox oracle, below): the current pattern-list classifier (L0), a list flipped to fail closed (L1), and the effect analysis (ALG). It never executes a corpus command; the only commands it runs are ALG's read-only resolvers.

```sh
make pilot          # verifies pilot/corpus.sha256, runs cmd/listpilot, writes pilot/results.txt
```

`make pilot` runs round 2 against `pilot/corpus.jsonl` (a shared fixture). Round 2b uses `pilot/corpus2.jsonl`, which adds a per-command fixture; run it by hand:

```sh
(cd pilot && sha256sum -c corpus2.sha256)
go run ./cmd/listpilot pilot/corpus2.jsonl > pilot/classifier_out2.jsonl
python3 pilot/analyze.py pilot/corpus2.jsonl pilot/classifier_out2.jsonl
```

The raw NL2Bash download (`pilot/nl2bash_all.cm`, GPL-3.0) is git-ignored; `make_corpus.py` needs it to rebuild the corpus, and it is fetched from the NL2Bash repository. Changing the corpus changes its hash, which fails `make pilot` on purpose: an edited corpus must not quietly change results.

## The automated corpus

From round 4 the evaluation corpus has no human labels and no second annotator. `internal/oracle` runs each command in a bubblewrap sandbox on a fixture and labels it from the tree diff: `R` if nothing that existed was lost, `C` if something was removed, overwritten, replaced, retargeted, or re-moded. A same-inode move is not a loss; mtimes and `.git` internals are ignored. `cmd/corpusgen` draws the corpus from three partitions: N (NL2Bash commands), T (templates), and E (an external partition that is never executed and is labelled `U` by construction). It shares no code with `internal/effects`, so the labels cannot inherit the analysis's mistakes. The same seed gives the same candidates; seed 20260922 is the development corpus and 20260923 the held-out one (`pilot/corpus5.jsonl`, drawn with `cmd/corpusgen` directly, since the Makefile's `corpus4` target writes the development path, once the analyser is frozen):

```sh
make corpus4                 # SEED=20260922, the development corpus: pilot/corpus4.jsonl
make round4                  # verify the corpus hash, score L0/L1/ALG, write pilot/results_round4.txt
go run ./cmd/effexplain 'find . -name "*.o" -delete'     # what the analysis makes of one command
go run ./cmd/effexplain -corpus pilot/corpus4.jsonl N351  # ... or of one corpus id
```

Both need `bwrap` (see Prerequisites). Dependencies for the eventual bootable image are collected with `make deps-check` and `make deps-hoard`; the manifest and scripts live in `../distro/` and the download directory `../distro/hoard/` is git-ignored.

## Layout

```
prototype/
├── cmd/synapse/main.go          # entrypoint — one-shot task, persistent repl (M4), and undo subcommands
├── internal/ollama/client.go    # Ollama REST client (the only code that knows the inference engine)
├── internal/classifier/         # reversibility classifier (pattern-matches known-irreversible command shapes)
├── internal/executor/           # os/exec subprocess dispatch, stdout/stderr capture, exit-code surfacing
├── internal/tui/                # TUI mode (M5): bubbletea model, streaming, scrollback
├── internal/undo/               # undo mechanisms: directory-diff, content-backup, trash, metadata, git-reset, inverse commands (package and service state)
├── internal/typedops/           # typed file operations (F4 experiment; not wired into the default runtime path)
├── internal/effects/            # effect analysis (A1/A2): parser walk, rule table, dry-run resolvers, package and service state (pkgstate.go), verdict and capture plan, fail-closed baseline
├── internal/gate/               # joins the effect analysis to the confirmation gate and the undo mechanisms; on by default (SYNAPSE_ANALYSIS=strict)
├── internal/oracle/             # ground truth for the evaluation corpus: runs a command in a bubblewrap sandbox and labels it from the tree diff
├── internal/recoverycheck/      # execute, undo, and compare proof that a capture plan restores the prior state
├── cmd/listpilot/               # runs L0, L1, and the effect analysis over a corpus; never executes a corpus command
├── cmd/corpusgen/               # builds the automated corpora (partitions N, T, E); shares no code with internal/effects
├── cmd/effexplain/              # prints what the analysis makes of one command or one corpus id
├── cmd/recoverycheck/           # command-line driver for internal/recoverycheck
└── pilot/                       # frozen pilot corpora, scoring script, and recorded results
```

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `ollama not reachable at http://localhost:11434` | Server not running — `ollama serve` |
| `ollama returned 404 ... model not found` | Model not pulled — `ollama pull qwen2.5-coder:3b` |
| Command output wrapped in ``` ``` ``` fences | Expected from small models; the skeleton strips them (`cleanCommand`) |
| Generation takes 30–60s | Normal for CPU inference on first token; the client has no timeout by design |
