## Overview

This is the authoritative build sequence for the SynapseOS prototype: the order in which runtime features are built, what each milestone depends on, and the concrete condition that marks it done. It is the *gating* layer that the other docs do not provide — `../docs/scope.md` lists everything that must exist but not in what order, and `../docs/decisions.md` explains why each piece is shaped the way it is. Each milestone is an independently runnable vertical slice, not a horizontal layer, so the prototype works end-to-end at every step rather than only at the end. Milestones are sequenced risk-first: the least certain assumptions are tested earliest, and the highest-effort deferrable work (LoRA fine-tuning) is last, because earlier milestones tell us how much of it is actually needed. The `README.md` status table is the at-a-glance view; this file is the detail behind it.

## Table of Contents

- [Overview](#overview)
- [Sequencing Principles](#sequencing-principles)
- [Status Key](#status-key)
- [Runtime Milestones](#runtime-milestones)
  - [M2 — CLI mode: propose, classify, execute 🚧](#m2-cli-mode-propose-classify-execute-)
  - [M3 — TUI loop ⬜](#m3-tui-loop-)
  - [M4 — Execution engine — merged into M2 (2026-07-12)](#m4-execution-engine-merged-into-m2-2026-07-12)
  - [M5 — Confirmation gate — merged into M2 (2026-07-12)](#m5-confirmation-gate-merged-into-m2-2026-07-12)
  - [M6 — Session context ⬜](#m6-session-context-)
  - [M7 — Session logger ⬜](#m7-session-logger-)
  - [M8 — Undo log ⬜](#m8-undo-log-)
  - [M9 — GUI mode ⬜](#m9-gui-mode-)
- [Post-Runtime Phases](#post-runtime-phases)

## Sequencing Principles

- **Vertical slices.** Every milestone runs end-to-end. No milestone is a layer that only pays off later.
- **Risk-first.** The most uncertain assumption is retired earliest (M2: does a 3B model produce usable bash at all?).
- **Runtime before pipeline.** The Go runtime is built against the *stock* model first. Fine-tuning is deferred until its payoff is measurable.
- **Telemetry is not an afterthought.** The session logger (M7) is treated as load-bearing — it is the study's entire dataset — not bolted on before the study.
- **Done means testable.** A milestone is done only when its definition-of-done can be demonstrated, not when the code exists.

## Status Key

`✅ done` · `⬜ not started` · `🚧 in progress` · `⛔ blocked`

---

## Runtime Milestones

### M2 — CLI mode: propose, classify, execute 🚧

- **Goal:** Prove the riskiest assumption before building anything around it — can a local 3B model turn plain-English intent into usable bash? Then make CLI mode a genuinely complete, useful interface on its own (D19), not just a proof-of-concept that TUI later supersedes: propose a command, classify it as reversible/irreversible, auto-run it if safe, ask for confirmation if not. **Revised 2026-07-12:** this milestone absorbed the core logic originally scoped as separate M4/M5 milestones (see their entries below) — execution and the confirmation gate don't need a TUI to exist, and CLI mode isn't a real interface mode until it can actually do things, not just print suggestions. M3 (TUI) now depends on this being fully done, not just the walking-skeleton half of it.
- **Delivers:**
  - `internal/ollama` client (non-streaming generate + connectivity check) — **done, validated**
  - `cmd/synapse` harness: sample task suite (propose-only, for quality-eyeballing — deliberately does not auto-execute against the real filesystem) and an ad-hoc single-task path (`synapse "<task>"`) that proposes, classifies, and executes — **propose done and validated; classify + execute not yet built**
  - Reversibility classifier: pattern-matches a proposed command against known-irreversible shapes (`rm`, `dd`, `mkfs`, truncating redirects, …); reversible commands auto-run, irreversible ones print the command and block on an explicit y/n before running
  - `os/exec` subprocess dispatch with stdout/stderr capture and exit-code surfacing
- **Closes (`../docs/scope.md`):** *Ollama connectivity check*; first half of *Intent parser client* (non-streaming); *CLI mode*; *Bash execution engine*; *Confirmation gate*.
- **Depends on:** Ollama installed, `qwen2.5-coder:3b` pulled.
- **Done when:** `go run ./cmd/synapse` prints a proposed command for all sample tasks (✅ demonstrated); `synapse "<task>"` proposes a command, classifies it, auto-runs reversible commands and shows their output, and blocks irreversible commands on confirmation before running them; a downed server prints an actionable error instead of a stack trace.
- **Status:** Propose-only path validated against a live Ollama instance 2026-07-12 (see below) — 8/8 sample tasks produced a plausible command in 1.2–10s. Classify + execute are the actual next work, not yet started.
- **Risk retired:** Model viability at the 3B budget — the single largest empirical unknown in the whole runtime. **Retired 2026-07-12**, confirmed live against `qwen2.5-coder:3b`. Two findings from that run: (1) `cleanCommand` only strips triple-backtick fences, not inline single backticks — 3 of 8 outputs came back wrapped in single backticks, which would hand the executor a broken string as-is; needs fixing before execution lands. (2) At least one command was plausible but semantically wrong (`dpkg-query -Wf` piped through a `grep` pattern that only matches `dpkg -l`'s different output format) — a model-accuracy limitation, not a code bug, and exactly the kind of failure the eventual study is designed to measure.
- **Risk not yet retired:** Whether the reversibility classifier's pattern-matching approach is workable without false positives/negatives, and whether one-shot CLI confirmation (print + block on stdin) is a sane UX for M3's TUI to later build on.

### M3 — TUI loop ⬜

- **Goal:** Turn the one-shot CLI into an interactive, multi-turn chat surface, reusing the intent-parser, classifier, and executor built for CLI mode rather than rebuilding them.
- **Delivers:** Full-screen bubbletea/lipgloss interface — input box, scrollback, streamed token rendering — wired to the same `internal/ollama` client, reversibility classifier, and executor M2 delivers. Upgrades the Ollama client to streaming.
- **Closes:** *Conversational TUI*; completes *Intent parser client* (streaming).
- **Depends on:** M2 **fully complete** — propose, classify, and execute all working and demonstrated, not just the walking-skeleton shape. TUI is an interactive wrapper around an already-working core, not the place execution and confirmation get built for the first time.
- **Done when:** A user types natural language, sees the proposed command render in-session, approved/auto-run commands execute with output shown in-session, can issue several turns in a row, and quits cleanly.
- **Risk retired:** Interaction model and token-streaming feasibility in the TUI.

### M4 — Execution engine — merged into M2 (2026-07-12)

Originally scoped as its own milestone depending on M3 (execution wired into the TUI). Redefined: execution doesn't need a TUI to exist, and CLI mode isn't a complete interface without it — its scope (`os/exec` dispatch, stdout/stderr capture, exit-code surfacing) now lives in M2 above, delivered through the CLI first and reused by M3. This entry is kept for milestone-number continuity, not deleted, matching how decisions in `../docs/decisions.md` are marked superseded rather than removed.

### M5 — Confirmation gate — merged into M2 (2026-07-12)

Same reasoning as M4: the reversibility classifier doesn't need a TUI either. Its scope now lives in M2 above. Kept for milestone-number continuity.

### M6 — Session context ⬜

- **Goal:** Multi-turn memory so follow-up commands resolve against prior turns.
- **Delivers:** In-memory history; rolling window that drops oldest turns near 75% of the 8K ceiling; bash-output compression before history append (decision D10).
- **Closes:** *Session context manager*.
- **Depends on:** M3 (multi-turn context only makes sense in a persistent session — CLI mode is one-shot by design) and M2 (needs real command output to compress).
- **Done when:** "move it to Downloads" resolves "it" from the previous turn; verbose output is never stored at full length; context stays under the token ceiling across a long session.
- **Risk retired:** Context overflow and pronoun/reference resolution.

### M7 — Session logger ⬜

- **Goal:** Capture the study's entire dataset — get it right well before the study, not the week before.
- **Delivers:** Structured per-event log emitting every telemetry field in `../docs/scope.md`: timestamp (ms), event type, command string, execution latency, task ID, participant ID, condition.
- **Closes:** *Session logger*; *Telemetry fields logged per event*.
- **Depends on:** M2 (there must be execution and confirmation-gate events to log).
- **Done when:** Every event type — `task_start`, `command_issued`, `command_result`, `confirmation_triggered`, `undo_invoked`, `task_end` — is written with all fields, and the output is parseable by the Python log parser.
- **Risk retired:** Data-completeness — a missing field means re-running 20 irreproducible participant sessions.

### M8 — Undo log ⬜

- **Goal:** Make reversible operations actually reversible.
- **Delivers:** A record of reversible operations and an undo command that restores prior state.
- **Closes:** *Undo log*.
- **Depends on:** M2 (reversibility classification), M7 (undo events must be logged).
- **Done when:** A reversible operation (e.g. a move) can be undone to restore prior state, and the undo event is logged.
- **Risk retired:** The reversibility guarantee that sits behind the confirmation model.

### M9 — GUI mode ⬜

- **Goal:** The study-facing interface for novice users (decision D11).
- **Delivers:** A fullscreen borderless conversational window approximating the active desktop, wrapping the same runtime built in M2, M3, M6–M8, launched within an XFCE (X11 session) host per D12. Also delivers the participant-accessible XFCE fallback (D20): logs every invocation as its own telemetry event type, separate from the six events M7 defines. TUI and CLI modes remain the server/remote and scripting targets respectively.
- **Closes:** *SynapseOS machine — GUI mode* (infrastructure); *GUI-mode XFCE fallback*.
- **Depends on:** M2, M3, M6, M7, M8 (a complete runtime).
- **Done when:** The runtime runs fullscreen within an XFCE X11 session (not Wayland — XFCE's Wayland session remains experimental per D12) and is usable by a novice without terminal knowledge; TUI and CLI modes still work; the fallback reliably returns the participant to a usable XFCE session on both an outright SynapseOS crash and a manually-triggered invocation, and every invocation is captured in the session log.
- **Risk retired:** The novice-familiarity threat to study validity (a TUI study would conflate terminal unfamiliarity with interface quality). The fallback's reliability (does it actually recover the machine, every time) is a new risk this milestone must retire — an unreliable fallback is worse than none, since D20's validity argument depends on it working when invoked.

---

## Post-Runtime Phases

Detailed in `../docs/scope.md`; summarized here for sequencing. These follow the runtime and several are gated by external approval.

| Phase | Contains | Gate |
|---|---|---|
| **P1 — Fine-tuning pipeline** (Python) | LoRA fine-tune, dataset prep, accuracy evaluator, GGUF export | Gated by M2 result — build only as much as the stock-model quality warrants |
| **P2 — Study instruments & execution** | Task suite, questionnaires (SUS, NASA-TLX), consent/info sheets, facilitator materials, pilot, main study | ⛔ IRB / ethics approval before recruitment |
| **P3 — Data & analysis** (Python) | Log parser, metric computation, anonymization, statistics, qualitative coding | Depends on M7 log format and P2 data |

> The one line that drives the order: **build the runtime against the stock model, prove M2 first, and let its result decide how much of P1 is worth building.**
