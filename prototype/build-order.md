## Overview

This is the authoritative build sequence for the SynapseOS prototype: the order in which runtime features are built, what each milestone depends on, and the concrete condition that marks it done. It is the *gating* layer that the other docs do not provide — `../docs/scope.md` lists everything that must exist but not in what order, and `../docs/decisions.md` explains why each piece is shaped the way it is. Each milestone is an independently runnable vertical slice, not a horizontal layer, so the prototype works end-to-end at every step rather than only at the end. Milestones are sequenced risk-first: the least certain assumptions are tested earliest, and the highest-effort deferrable work (LoRA fine-tuning) is last, because earlier milestones tell us how much of it is actually needed. The `README.md` status table is the at-a-glance view; this file is the detail behind it.

## Table of Contents

- [Overview](#overview)
- [Sequencing Principles](#sequencing-principles)
- [Status Key](#status-key)
- [Runtime Milestones](#runtime-milestones)
  - [M2 — Walking skeleton 🚧](#m2-walking-skeleton-)
  - [M3 — TUI loop ⬜](#m3-tui-loop-)
  - [M4 — Execution engine ⬜](#m4-execution-engine-)
  - [M5 — Confirmation gate ⬜](#m5-confirmation-gate-)
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

### M2 — Walking skeleton 🚧

- **Goal:** Prove the riskiest assumption before building anything around it — can a local 3B model turn plain-English intent into usable bash?
- **Delivers:** `internal/ollama` client (non-streaming generate + connectivity check); `cmd/synapse` harness that runs a sample task suite and prints each proposed command with latency and token counts. No execution.
- **Closes (`../docs/scope.md`):** *Ollama connectivity check*; first half of *Intent parser client* (non-streaming).
- **Depends on:** Ollama installed, `qwen2.5-coder:3b` pulled.
- **Done when:** `go run ./cmd/synapse` prints a proposed command for all sample tasks; a downed server prints an actionable error instead of a stack trace.
- **Status:** Code satisfies the shape of the definition-of-done, but per this file's own "done means testable" rule, it hasn't actually been demonstrated — never run against a live Ollama instance. The error-path half (downed server) is trivially true by default; the half that actually retires the risk (does the model produce usable bash) is unverified.
- **Risk retired:** Model viability at the 3B budget — the single largest empirical unknown in the whole runtime. **Not yet retired — this is the actual next step.**

### M3 — TUI loop ⬜

- **Goal:** Turn the one-shot harness into an interactive, multi-turn chat surface.
- **Delivers:** Full-screen bubbletea/lipgloss interface — input box, scrollback, streamed token rendering — wired to the intent parser. Upgrades the Ollama client to streaming.
- **Closes:** *Conversational TUI*; completes *Intent parser client* (streaming).
- **Depends on:** M2.
- **Done when:** A user types natural language, sees the proposed command render in-session, can issue several turns in a row, and quits cleanly.
- **Risk retired:** Interaction model and token-streaming feasibility in the TUI.

### M4 — Execution engine ⬜

- **Goal:** Actually run the proposed command and show its output.
- **Delivers:** `os/exec` subprocess dispatch with live stdout/stderr streaming into the TUI; exit-code surfacing.
- **Closes:** *Bash execution engine*.
- **Depends on:** M3.
- **Done when:** An approved command executes; stdout/stderr stream live into the interface; a non-zero exit is shown clearly.
- **Risk retired:** Subprocess streaming and rendering under verbose output.

### M5 — Confirmation gate ⬜

- **Goal:** Never run an irreversible action without explicit consent.
- **Delivers:** A reversibility classifier that inspects a pending command and blocks irreversible ones (`rm`, `dd`, `mkfs`, truncating redirects, …) until the user confirms; reversible ones pass.
- **Closes:** *Confirmation gate*.
- **Depends on:** M4.
- **Done when:** Irreversible commands are blocked pending confirmation; reversible commands proceed; the classification decision is visible to the user.
- **Risk retired:** The trust/safety model — the precondition for giving a conversational agent any authority.

### M6 — Session context ⬜

- **Goal:** Multi-turn memory so follow-up commands resolve against prior turns.
- **Delivers:** In-memory history; rolling window that drops oldest turns near 75% of the 8K ceiling; bash-output compression before history append (decision D10).
- **Closes:** *Session context manager*.
- **Depends on:** M4 (needs real command output to compress).
- **Done when:** "move it to Downloads" resolves "it" from the previous turn; verbose output is never stored at full length; context stays under the token ceiling across a long session.
- **Risk retired:** Context overflow and pronoun/reference resolution.

### M7 — Session logger ⬜

- **Goal:** Capture the study's entire dataset — get it right well before the study, not the week before.
- **Delivers:** Structured per-event log emitting every telemetry field in `../docs/scope.md`: timestamp (ms), event type, command string, execution latency, task ID, participant ID, condition.
- **Closes:** *Session logger*; *Telemetry fields logged per event*.
- **Depends on:** M4 and M5 (there must be events to log).
- **Done when:** Every event type — `task_start`, `command_issued`, `command_result`, `confirmation_triggered`, `undo_invoked`, `task_end` — is written with all fields, and the output is parseable by the Python log parser.
- **Risk retired:** Data-completeness — a missing field means re-running 20 irreproducible participant sessions.

### M8 — Undo log ⬜

- **Goal:** Make reversible operations actually reversible.
- **Delivers:** A record of reversible operations and an undo command that restores prior state.
- **Closes:** *Undo log*.
- **Depends on:** M5 (reversibility classification), M7 (undo events must be logged).
- **Done when:** A reversible operation (e.g. a move) can be undone to restore prior state, and the undo event is logged.
- **Risk retired:** The reversibility guarantee that sits behind the confirmation model.

### M9 — GUI mode ⬜

- **Goal:** The study-facing interface for novice users (decision D11).
- **Delivers:** A fullscreen borderless conversational window approximating the active desktop, wrapping the same runtime built in M3–M8. TUI mode remains the server/remote target.
- **Closes:** *SynapseOS machine — GUI mode* (infrastructure).
- **Depends on:** M3–M8 (a complete runtime).
- **Done when:** The runtime runs fullscreen on a Wayland desktop and is usable by a novice without terminal knowledge; TUI mode still works.
- **Risk retired:** The novice-familiarity threat to study validity (a TUI study would conflate terminal unfamiliarity with interface quality).

---

## Post-Runtime Phases

Detailed in `../docs/scope.md`; summarized here for sequencing. These follow the runtime and several are gated by external approval.

| Phase | Contains | Gate |
|---|---|---|
| **P1 — Fine-tuning pipeline** (Python) | LoRA fine-tune, dataset prep, accuracy evaluator, GGUF export | Gated by M2 result — build only as much as the stock-model quality warrants |
| **P2 — Study instruments & execution** | Task suite, questionnaires (SUS, NASA-TLX), consent/info sheets, facilitator materials, pilot, main study | ⛔ IRB / ethics approval before recruitment |
| **P3 — Data & analysis** (Python) | Log parser, metric computation, anonymization, statistics, qualitative coding | Depends on M7 log format and P2 data |

> The one line that drives the order: **build the runtime against the stock model, prove M2 first, and let its result decide how much of P1 is worth building.**
