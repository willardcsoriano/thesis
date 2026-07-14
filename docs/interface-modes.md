# SynapseOS — Interface Modes: CLI, TUI, GUI

## Overview

This is the reference for how SynapseOS's three interface modes — CLI, TUI, and GUI — relate to each other and to the shared Go runtime underneath them. All three wrap the same core (`internal/ollama`, `internal/classifier`, `internal/executor`); what differs is process lifecycle (one-shot vs. persistent), how model output is rendered (a single blocking print vs. streamed tokens vs. a fullscreen takeover), and audience (scripting/automation, an interactive terminal session, and the study's novice-facing kiosk). Read this when you need to know which mode owns a given piece of behavior, why a boundary is drawn where it is, or what a later mode inherits versus builds fresh. For *why* each mode exists at all, see `decisions.md` (D11, D12, D19, D20); for build sequencing and current status, see `build-order.md`.

## Table of Contents

- [Overview](#overview)
- [1. Shared Core](#1-shared-core)
- [2. Boundaries at a Glance](#2-boundaries-at-a-glance)
- [3. CLI Mode (D19, M2 — done)](#3-cli-mode-d19-m2-done)
- [4. TUI (Terminal User Interface) Mode (D11, M3 — next)](#4-tui-terminal-user-interface-mode-d11-m3-next)
  - [Technical Stack](#technical-stack)
  - [Loop Cycle](#loop-cycle)
  - [What TUI Reuses vs. Adds](#what-tui-reuses-vs-adds)
- [5. GUI Takeover Mode (D11, D12, M9 — study prototype)](#5-gui-takeover-mode-d11-d12-m9-study-prototype)
  - [The Takeover Mechanism (No Custom Compositor)](#the-takeover-mechanism-no-custom-compositor)
    - [Session registration — `/usr/share/xsessions/synapseos.desktop`](#session-registration-usrsharexsessionssynapseosdesktop)
    - [Startup kiosk script — `/usr/bin/synapseos-session`](#startup-kiosk-script-usrbinsynapseos-session)
  - [Packaging the GUI Application](#packaging-the-gui-application)
  - [The XFCE Fallback (D20)](#the-xfce-fallback-d20)
- [6. Post-Thesis "Overlay Mode" (D13 — deferred, not built)](#6-post-thesis-overlay-mode-d13-deferred-not-built)
- [7. Cross-References](#7-cross-references)

## 1. Shared Core

Every mode is a thin wrapper around the same three packages — nothing mode-specific happens inside them, and nothing about them assumes which mode is calling:

- **`internal/ollama`** — the only code that talks to the model. `Generate` sends a non-streaming prompt and waits for the full response (what CLI mode uses); a streaming variant is TUI's one piece of unbuilt shared-core work (see §3).
- **`internal/classifier`** — `Classify(cmd string) (Verdict, string)` pattern-matches a proposed command against known-irreversible shapes (`rm`, `dd`, `mkfs`, `shred`, `git reset --hard`, `git clean -f`, truncating redirects) and returns `Reversible` or `Irreversible` plus a human-readable reason. It has no notion of a terminal, a prompt, or a UI — it is pure logic, which is exactly what lets every mode reuse it unmodified.
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
| Process lifecycle | One-shot: propose → classify → (confirm) → execute → exit | Persistent: runs until the user quits | Persistent: launched at login, fills the session |
| State across turns | None — each invocation is independent | In-memory rolling history within the session (M6) | Same as TUI (wraps it) |
| Model output rendering | Printed once generation finishes (blocking call) | Streamed token-by-token into a scrollable viewport | Same streaming, inside fullscreen chrome |
| Confirmation gate UX | Print the command + reason, block on stdin `y`/`N` | Render inline in the chat view, wait for a keypress | Same inline pattern, fullscreen |
| Audience / use case | Scripting, automation, one-off remote commands over SSH | Interactive terminal session, local or remote | Study Condition A — novice users, no terminal exposure |
| Built by | **M2 — done** | M3 — next | M9 |
| Escape hatch | N/A (process just exits) | N/A (it's already a normal terminal) | XFCE fallback, logged and excluded from primary analysis (D20) |

**The one-line answer to "isn't TUI just CLI with a nicer UI?"**: mostly, but not only — the propose/classify/execute logic is identical and reused verbatim, but persistence (a session that outlives one task) and streaming (rendering tokens as they arrive instead of waiting for the full response) are real architectural additions, not visual polish. TUI is the first mode where "session" is a meaningful concept at all.

## 3. CLI Mode (D19, M2 — done)

CLI mode is the one-shot interface: `synapse "<task>"` runs exactly one request through propose → classify → confirm-if-needed → execute, then exits. No conversation history, no persistent process — every invocation starts cold. This is deliberate, not a limitation to fix later: it's what makes CLI mode viable for scripting and one-off remote commands (`ssh host synapse "..."` behaves exactly like any other single-purpose CLI tool).

Because there's no session to render into, the confirmation gate is the simplest possible implementation: print the blocked command and why, then block on a single line of stdin. TUI reuses the same yes/no *decision* logic but renders the prompt differently (§4) — the UX difference is a rendering concern, not a logic difference, which is why it lives in the mode layer and not the shared core.

Entry point: `cmd/synapse/main.go`. The built-in 8-task sample suite (`synapse` with no arguments) is a quality smoke test only — it calls `propose` but deliberately skips classify/execute, so running it never touches the real filesystem.

## 4. TUI (Terminal User Interface) Mode (D11, M3 — next)

TUI mode turns the one-shot CLI into a persistent, interactive session — the default target for local terminals and remote SSH connections where a real back-and-forth is wanted, as opposed to CLI mode's single-shot, script-friendly invocation.

### Technical Stack

- **Framework:** [bubbletea](https://github.com/charmbracelet/bubbletea), implementing the Elm Architecture (Model-Update-View loop) in Go.
- **Styling & layout:** [lipgloss](https://github.com/charmbracelet/lipgloss) for borders, grids, and typography.
- **Viewport:** bubbletea's native scrollable viewport for terminal output.

### Loop Cycle

1. **Model:** conversation history, current input, cursor position, loading/spinner state, execution logs.
2. **Update:** listens for keystrokes; on `Enter`, fires an asynchronous `tea.Cmd` that calls `internal/ollama`'s streaming `Generate` variant (the one piece of shared-core work TUI adds — CLI never needed streaming since it renders once at the end). Tokens arrive over a goroutine channel into the view. If `internal/classifier` flags the proposed command as irreversible, the loop pauses in-view for confirmation instead of blocking on `bufio.Reader` the way CLI does.
3. **View:** renders the input box, scrollable viewport, and status indicators via ANSI escape sequences.

### What TUI Reuses vs. Adds

| Reused unchanged | New in TUI |
|---|---|
| `internal/classifier` — same `Classify` call, same verdicts | Persistent process / session loop (bubbletea) |
| `internal/executor` — same `Run` call, same `Result` shape | Streaming `Generate` variant in `internal/ollama` |
| The reversibility-gate *decision logic* | In-session confirmation rendering (vs. blocking stdin read) |
| | Multi-turn context (M6 — depends on TUI existing, not part of M3 itself) |

## 5. GUI Takeover Mode (D11, D12, M9 — study prototype)

GUI mode is the fullscreen, study-facing interface for Condition A (novice users, no terminal exposure). It wraps the same core as TUI — propose/classify/execute, streamed rendering, in-session confirmation — inside a fullscreen takeover rather than a terminal window.

### The Takeover Mechanism (No Custom Compositor)

Instead of a full desktop session manager (`xfce4-session`) bringing up panels and desktop icons, the display manager (LightDM/GDM) boots a custom, minimal X11 session.

#### Session registration — `/usr/share/xsessions/synapseos.desktop`

```ini
[Desktop Entry]
Name=SynapseOS
Comment=Conversational Session Layer for Linux
Exec=/usr/bin/synapseos-session
Type=Application
DesktopNames=SynapseOS
```

#### Startup kiosk script — `/usr/bin/synapseos-session`

```bash
#!/bin/bash
# 1. Start the XFCE window manager in daemon mode in the background.
# Provides window focusing, borders, and keybindings without panels.
xfwm4 --daemon &

# 2. Run the SynapseOS app. 'exec' replaces the shell script process.
# If SynapseOS exits/closes, the X session terminates, logging the user out.
exec /usr/bin/synapseos-gui --fullscreen
```

### Packaging the GUI Application

- **Option A — fullscreen terminal wrapper (humblest prototype):** configure a fast terminal emulator (`kitty`, `xfce4-terminal`) to launch borderless and fullscreen, running the TUI binary directly. With no desktop panels, the user is locked into this fullscreen terminal.
- **Option B — Go webview wrapper:** wrap the Go program in a lightweight webview/GUI window (`github.com/webview/webview`, `fyne.io/fyne`), fullscreen and undecorated.

### The XFCE Fallback (D20)

A participant-accessible path back to the underlying XFCE session, for when SynapseOS becomes unresponsive or the participant wants to stop mid-task. Every invocation is logged as its own telemetry event (task ID, timestamp, separate from M7's six standard event types); any task where it's invoked is scored "did not complete via SynapseOS" and excluded from the primary completion-time/error-rate analysis, with fallback-invocation rate reported as its own secondary metric. This is what keeps the participant safety net from silently contaminating the study's core causal claim — see `decisions.md` D20 for the full reasoning, and D12 for why the fallback needed reopening in the first place.

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
| What order are these built in, and what's each milestone's definition of done? | `build-order.md` M2 (CLI), M3 (TUI), M9 (GUI) |
| What has the paper (Ch.3) committed to describing? | `research-methods/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html` Table 3.2 and Section 2.1 |
| Where does each mode sit relative to the OS layers (kernel, userland, session layer)? | `layers.md` |
