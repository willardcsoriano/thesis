# SynapseOS — Interface Modes: CLI, TUI, GUI

## Overview

This is the reference for how SynapseOS's three interface modes — CLI, TUI, and GUI — relate to each other and to the shared Go runtime underneath them. All three wrap the same core (`internal/ollama`, `internal/classifier`, `internal/executor`); what differs is process lifecycle (one-shot vs. persistent), how model output is rendered (a single blocking print vs. streamed tokens), and audience (scripting/automation, an interactive terminal session, and the study's novice-facing fullscreen session). Since D27 the third of those is the *second* one launched fullscreen over a running XFCE desktop rather than a separately-built interface, so the meaningful boundary in this document is between one-shot and persistent, not between terminal and graphical. Read this when you need to know which mode owns a given piece of behavior, why a boundary is drawn where it is, or what a later mode inherits versus builds fresh. For *why* each mode exists at all, see `decisions.md` (D11, D12, D19, D20); for build sequencing and current status, see `build-order.md`.

## Table of Contents

- [Overview](#overview)
- [1. Shared Core](#1-shared-core)
- [2. Boundaries at a Glance](#2-boundaries-at-a-glance)
- [3. CLI Mode (D19, M1 — done)](#3-cli-mode-d19-m1-done)
- [4. TUI (Terminal User Interface) Mode (D11, M4 + M5 — done)](#4-tui-terminal-user-interface-mode-d11-m4-m5-done)
  - [Technical Stack](#technical-stack)
  - [Loop Cycle](#loop-cycle)
  - [What TUI Reuses vs. Adds](#what-tui-reuses-vs-adds)
- [5. GUI Mode (D11, D12, D27, M8 — study prototype)](#5-gui-mode-d11-d12-d27-m8-study-prototype)
  - [Session Mechanism](#session-mechanism)
    - [Session registration — `distro/synapseos.desktop`, installed as `/usr/share/xsessions/synapseos.desktop`](#session-registration-distrosynapseosdesktop-installed-as-usrsharexsessionssynapseosdesktop)
    - [Startup script — `distro/synapseos-session`, installed as `/usr/bin/synapseos-session`](#startup-script-distrosynapseos-session-installed-as-usrbinsynapseos-session)
  - [Packaging](#packaging)
  - [The XFCE Fallback (D20, simplified by D27)](#the-xfce-fallback-d20-simplified-by-d27)
- [6. Post-Thesis "Overlay Mode" (D13 — deferred, not built)](#6-post-thesis-overlay-mode-d13-deferred-not-built)
- [7. Cross-References](#7-cross-references)

## 1. Shared Core

Every mode is a thin wrapper around the same three packages — nothing mode-specific happens inside them, and nothing about them assumes which mode is calling:

- **`internal/ollama`** — the only code that talks to the model. `Generate` sends a non-streaming prompt and waits for the full response (what CLI and REPL mode use); `GenerateStream` delivers the same result token-by-token over NDJSON (what TUI mode uses, opt-in per mode). A stream that ends without a completion marker is rejected outright rather than returned short — truncated text would still be a runnable command, and a different one.
- **`internal/classifier`** — `Classify(cmd string) (Verdict, string)` pattern-matches a proposed command against known-irreversible shapes and returns `Reversible` or `Irreversible` plus a human-readable reason. Started as a short list (`rm`, `dd`, `mkfs`, `shred`, `git reset --hard`, `git clean -f`, truncating redirects) and has since grown substantially (F1: `sed -i`/`awk -i inplace`/`truncate`/unsafe `tee`; F3/D22: recursive `chmod`/`chown`, fetch-decode-exec, bare `eval`) — see `build-order.md` F1/F3 for the current full rule set. It has no notion of a terminal, a prompt, or a UI — it is pure logic, which is exactly what lets every mode reuse it unmodified.
- **`internal/executor`** — `Run(ctx, cmd) Result` dispatches through `sh -c`, capturing stdout/stderr and the exit code. Also mode-agnostic: it doesn't know or care whether its caller is a one-shot process or a long-running session.

```
                     ┌─────────────────────────────┐
                     │   internal/ollama, classifier,│
                     │   executor  (shared core)     │
                     └───────────────┬───────────────┘
                                     │
              ┌──────────────────────┼──────────────────────┐
              ▼                      ▼                      ▼
        [ CLI mode ]           [ TUI mode ]            [ GUI mode ]
       one-shot process      persistent terminal      persistent fullscreen
       synapse "<task>"      session (bubbletea)       kiosk session (XFCE)
```

A mode's job is only ever: collect input, drive the core, render output. Every mode-specific line of code should be justifiable as "input collection," "rendering," or "session lifecycle" — the moment mode-specific code starts reimplementing classification or execution, that's a sign the logic belongs back in the shared core instead.

## 2. Boundaries at a Glance

| | **CLI** (D19) | **TUI** (D11) | **GUI** (D11, D12) |
|---|---|---|---|
| Process lifecycle | One-shot per invocation: a bounded, gated multi-step loop (propose → classify → (confirm) → execute → feed result back → repeat until done or step cap, D21) — not a single command | Persistent: runs until the user quits | Persistent: launched at login, fills the session |
| State across turns | None — each invocation is independent | In-memory rolling history within the session (M6) | Same as TUI (wraps it) |
| Model output rendering | Printed once generation finishes (blocking call) | Streamed token-by-token into a scrollable viewport | Same streaming, inside fullscreen chrome |
| Confirmation gate UX | Print the command + reason, block on stdin `y`/`N` | Render inline in the chat view, wait for a keypress | Same inline pattern, fullscreen |
| Audience / use case | Scripting, automation, one-off remote commands over SSH | Interactive terminal session, local or remote | Study Condition A — novice users, no terminal exposure |
| Built by | **M1 — done** | M4 (interim loop) + M5 (rendering) — **both done** | M8 |
| Escape hatch | N/A (process just exits) | N/A (it's already a normal terminal) | XFCE fallback, logged and excluded from primary analysis (D20) |

**The one-line answer to "isn't TUI just CLI with a nicer UI?"**: mostly, but not only — the propose/classify/execute logic is identical and reused verbatim, but persistence (a session that outlives one task) and streaming (rendering tokens as they arrive instead of waiting for the full response) are real architectural additions, not visual polish. TUI is the first mode where "session" is a meaningful concept at all.

## 3. CLI Mode (D19, M1 — done)

CLI mode is the one-shot interface: `synapse "<task>"` runs one request through a bounded, gated multi-step loop (D21) — propose → classify → confirm-if-needed → execute → feed the result back so the model can propose the next step or signal done, repeating until complete or a hard step cap is hit — then exits. Not single-command execution: a task that genuinely needs several distinct actions (e.g. "make a folder, then move matching files into it") is handled within one invocation, with every step independently classified and gated, not just the first. No conversation history *across* invocations, no persistent process — every invocation starts cold. This is deliberate, not a limitation to fix later: it's what makes CLI mode viable for scripting and one-off remote commands (`ssh host synapse "..."` behaves exactly like any other single-purpose CLI tool).

Because there's no session to render into, the confirmation gate is the simplest possible implementation: print the blocked command and why, then block on a single line of stdin. TUI reuses the same yes/no *decision* logic but renders the prompt differently (§4) — the UX difference is a rendering concern, not a logic difference, which is why it lives in the mode layer and not the shared core.

Entry point: `cmd/synapse/main.go`. The built-in 8-task sample suite (`synapse` with no arguments) is a quality smoke test only — it calls `propose` but deliberately skips classify/execute, so running it never touches the real filesystem.

## 4. TUI (Terminal User Interface) Mode (D11, M4 + M5 — done)

TUI mode turns the one-shot CLI into a persistent, interactive session — the default target for local terminals and remote SSH connections where a real back-and-forth is wanted, as opposed to CLI mode's single-shot, script-friendly invocation.

**Built in two sub-milestones (split 2026-08-21, see `build-order.md`):** M4 proved the persistent-session mechanics alone — a plain stdin loop wrapping M1's already-tested `runLoop`, no rendering — done as of Session 27, before M5 built the bubbletea/lipgloss layer below (done as of Session 28). This was a build-sequencing decision only; TUI mode itself is still one mode, unchanged from D11, and M4 is not something a user is meant to run as a deliverable in its own right.

### Technical Stack

- **Framework:** [bubbletea](https://github.com/charmbracelet/bubbletea), implementing the Elm Architecture (Model-Update-View loop) in Go.
- **Styling & layout:** [lipgloss](https://github.com/charmbracelet/lipgloss) for borders, grids, and typography.
- **Viewport:** `bubbles/v2/viewport` for scrollable output (a separate module from bubbletea itself).
- **Module paths:** all three live under `charm.land/…/v2`, not `github.com/charmbracelet/…` — the path moved with the v2 line. Requires Go ≥ 1.25.

### Loop Cycle

**As built (Session 28), which differs from this section's original sketch in one important way.** The sketch had `Update` calling the model directly. It does not — TUI mode never reimplements any part of the propose/classify/confirm/execute loop. The *same* `runLoop` that CLI and REPL mode use is injected as a `tui.TaskRunner` and driven on its own goroutine, so every reversibility verdict, confirmation gate, and undo-journal write is literally the same code in every mode and cannot drift between them.

1. **Model:** transcript, viewport scroll state, current input, whether a task is running, and any outstanding confirmation prompt.
2. **Update:** on `Enter`, launches the injected runner on a goroutine and returns immediately — `Update` must never block. Two channels bridge the synchronous loop into the event loop: the runner's `io.Writer` output arrives as messages (streamed token-by-token, since TUI passes `withTokenStreaming`), and when the loop hits an irreversible step its `confirmFn` publishes a confirmation request and *blocks* until `Update` — having rendered the prompt and taken a keypress — sends the verdict back. Ctrl+C mid-task cancels that task's context only; the session survives.
3. **View:** renders the transcript through a `viewport`, plus either the input box, a working indicator, or the confirmation prompt — the last being the only bordered, colored element in the interface, so an irreversible-command gate can never be mistaken for ordinary output.

### What TUI Reuses vs. Adds

| Reused unchanged | New in TUI |
|---|---|
| `internal/classifier` — same `Classify` call, same verdicts | Persistent process / session loop (bubbletea) |
| `internal/executor` — same `Run` call, same `Result` shape | `GenerateStream` in `internal/ollama` (opt-in per mode; CLI/REPL stay non-streaming) |
| `runLoop` itself — the whole propose/classify/confirm/execute loop, injected and driven, never reimplemented | In-session confirmation rendering (vs. blocking stdin read), and viewport scrollback |
| | Multi-turn context (M6 — depends on TUI existing, not part of M4/M5 itself) |

## 5. GUI Mode (D11, D12, D27, M8 — study prototype)

GUI mode is the fullscreen, study-facing interface for Condition A (novice users, no terminal exposure).

**Rescoped 2026-09-12 by D27, and the change is structural rather than cosmetic.** SynapseOS is an agentic layer running *over* an ordinary XFCE desktop, not a replacement for the desktop shell, session manager, and application launcher. GUI mode is therefore **the existing TUI, launched fullscreen, with the XFCE session running beneath it** — not a second rendering layer, not a webview, not a custom session. The two packaging options previously weighed below collapse to the first one, and the "takeover" framing is retired: nothing is taken over.

What follows describes the session plumbing that remains. It is built (`distro/synapseos.desktop`, `distro/synapseos-session`, `distro/install-session.sh`), not yet tried through a real login/logout cycle.

### Session Mechanism

The display manager starts a session that launches the TUI fullscreen on top of a normal XFCE session, rather than in place of one.

#### Session registration — `distro/synapseos.desktop`, installed as `/usr/share/xsessions/synapseos.desktop`

```ini
[Desktop Entry]
Name=SynapseOS
Comment=Conversational Session Layer for Linux
Exec=/usr/bin/synapseos-session
Type=Application
DesktopNames=SynapseOS
```

#### Startup script — `distro/synapseos-session`, installed as `/usr/bin/synapseos-session`

```bash
#!/bin/bash
# 1. Start the ordinary XFCE session. The user's desktop, file manager, and
#    panels come up and stay up — SynapseOS layers on top of a working
#    desktop rather than substituting for one (D27).
xfce4-session &
sleep 2

# 2. Launch the TUI fullscreen on top of it. If it exits, the XFCE session
#    underneath is still there; the participant lands on a usable desktop
#    rather than being logged out.
exec xfce4-terminal --fullscreen --hide-menubar --hide-toolbar --hide-borders --hide-scrollbar \
  -x /usr/bin/synapse tui
```

`distro/install-session.sh` installs both files plus the built `synapse` binary. Run it, then log out — "SynapseOS" appears as a session choice at the greeter, next to "Xfce Session".

### Packaging

`xfce4-terminal` launched borderless and fullscreen, running the TUI binary — already part of the XFCE desktop this session starts, so no new dependency. There is no second GUI application to build. The webview/Fyne option previously listed here was dropped with D27: it existed to make a replacement session feel like a desktop application, and there is no longer a replacement session. `kitty`, mentioned in earlier drafts of this section, is not installed on the reference machine and is not needed; `xfce4-terminal` is preferred as the tool that already exists.

The practical consequence for the study is that **GUI mode and TUI mode are the same program in a different frame**, which is also why the study-mode readiness checkpoint has a TUI fallback that costs the research nothing — the two conditions differ in presentation, not in the execution path, the safety gate, or the telemetry.

### The XFCE Fallback (D20, simplified by D27)

A participant-accessible path back to the desktop, for when SynapseOS becomes unresponsive or the participant wants to stop mid-task. Under D27 this is materially less fragile than it was: the XFCE session was never displaced, so falling back is ordinary window management rather than recovering a machine whose session manager has been replaced.

Every invocation is logged as its own telemetry event (task ID, timestamp, separate from M7's six standard event types); any task where it's invoked is scored "did not complete via SynapseOS" and excluded from the primary completion-time/error-rate analysis, with fallback-invocation rate reported as its own secondary metric. This is what keeps the participant safety net from silently contaminating the study's core causal claim — see `decisions.md` D20 for the full reasoning, and D12 for why the fallback needed reopening in the first place.

## 6. Post-Thesis "Overlay Mode" (D13 — deferred, not built)

Once the study concludes, SynapseOS can run as a standard desktop overlay instead of a full session takeover — out of scope for the thesis, recorded here only so the boundary with GUI mode is clear.

1. **Visibility:** the traditional XFCE desktop (panels, files, applications) stays fully visible and usable.
2. **Hotkey summon:** a global shortcut (e.g. `Super+Space`, via `xfconf-query`) toggles the window:
   ```bash
   synapseos-cli --toggle-window
   ```
3. **Floating window:** the GUI program runs as a borderless floating panel that slides in/out of focus — Spotlight/Alfred-style — letting the user invoke system tasks without leaving their normal desktop.

## 7. Cross-References

| Question | Where to look |
|---|---|
| Why do these three modes exist, and not some other split? | `decisions.md` D11 (TUI vs. GUI), D19 (CLI formalized as a third mode) |
| Why does GUI have no escape hatch by default, and why was that reopened? | `decisions.md` D12, D20 |
| What order are these built in, and what's each milestone's definition of done? | `build-order.md` M1 (CLI, done), M4 (interim loop, done)/M5 (rendering, next) (TUI), M8 (GUI) |
| Why does a single CLI-mode invocation run more than one command sometimes? | `decisions.md` D21 (bounded, gated multi-step loop; full autonomy considered and rejected) |
| What has the paper (Ch.3) committed to describing? | `research-methods/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html` Table 3.2 and Section 2.1 |
| Where does each mode sit relative to the OS layers (kernel, userland, session layer)? | `layers.md` |
