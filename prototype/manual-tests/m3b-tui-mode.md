## Overview

This is the hands-on manual test suite for **milestone M3b — TUI mode**, created as that milestone's final step per the `manual-tests/` convention. It covers only what M3b added: the full-screen terminal interface, token-by-token streaming, scrollback, and the confirmation gate rendered as UI instead of a stdin prompt. The propose/classify/execute mechanics underneath are unchanged and already covered by `m2-cli-mode-and-undo-safety-net.md`. Six steps, roughly fifteen minutes, everything destructive confined to a disposable scratch directory. **This suite matters more than the previous two**: unlike CLI and REPL mode, a full-screen TUI cannot be driven by piping input, so nobody — including the assistant that built it — has ever run this end-to-end. Your terminal is genuinely the first.

## Automated coverage — what's already been machine-verified

`internal/tui` sits at 96.3% coverage across 27 tests, clean under `-race`: the confirmation bridge (y/Y approves, n/N/esc/enter fail closed, unrelated keys ignored), task lifecycle, ctrl+c cancelling a task without ending the session, viewport sizing, and scroll-preservation. `internal/ollama`'s streaming path adds 8 more, including a safety test that a truncated stream is rejected outright rather than handed on as a shorter command. Several are mutation-verified — the code was deliberately broken to confirm the test fails.

What none of that proves: **every one of those tests bypasses the real terminal.** Bubble Tea needs an actual TTY, which the build environment does not have, so the compiled binary has never rendered to a real screen, never redrawn on a resize, and never had a human key pressed at it. Colors, borders, alt-screen behavior, cursor placement, flicker, and whether streaming *feels* responsive are all unverified by construction. That gap is this suite's entire purpose.

## Table of Contents

- [Overview](#overview)
- [Automated coverage — what's already been machine-verified](#automated-coverage-whats-already-been-machine-verified)
- [Recording your session](#recording-your-session)
- [0. Build and launch](#0-build-and-launch)
- [1. First look — does it render at all](#1-first-look-does-it-render-at-all)
- [2. Streaming — watch a command appear token by token](#2-streaming-watch-a-command-appear-token-by-token)
- [3. The confirmation gate as UI](#3-the-confirmation-gate-as-ui)
- [4. Scrollback, including mid-confirmation](#4-scrollback-including-mid-confirmation)
- [5. Cancel a task without losing the session](#5-cancel-a-task-without-losing-the-session)
- [6. Resize and quit](#6-resize-and-quit)
- [What to check if something looks wrong](#what-to-check-if-something-looks-wrong)

## Recording your session

`script` records a full transcript, but be aware it captures raw terminal control codes, so a TUI transcript is far noisier than the CLI ones — it will be full of escape sequences. It is still worth having if something goes wrong, just don't expect it to read cleanly.

```sh
mkdir -p ~/.synapse/test-logs   # only needed once — skip if you already ran an earlier suite
script ~/.synapse/test-logs/m3b-session.txt   # starts recording this whole terminal session
```

For visual problems specifically — misaligned borders, wrong colors, flicker — a screenshot or a short screen recording is far more useful than the transcript. Note them down as you go; that observation is the actual deliverable of this suite.

## 0. Build and launch

```sh
cd ~/repos/thesis/prototype   # moves into the project's prototype folder
go build -o bin/synapse ./cmd/synapse   # compiles the program
mkdir -p /tmp/synapse-tui-test && cd /tmp/synapse-tui-test   # disposable scratch directory
~/repos/thesis/prototype/bin/synapse tui   # launches TUI mode
```

`go build` prints nothing on success. If the launch fails with `could not open TTY`, you are not in a real terminal — this mode cannot run inside an IDE output pane or a piped shell.

## 1. First look — does it render at all

The screen should clear and be replaced by a full-screen interface (alt-screen), showing a bold header, dimmer hint lines, and a `>` input prompt.

Check, and note anything off:

- Does the header render **bold and colored**, and the hints **dimmer** than normal text?
- Is the input prompt visible at the bottom, not pushed off-screen or overlapping?
- Any flicker, torn lines, or stray escape characters (`^[[0m` and similar) printed literally?

## 2. Streaming — watch a command appear token by token

This is what M3b added over the REPL. Type a task and watch **how** the answer arrives:

```
> list the files in this directory
```

The proposed command should appear **progressively**, a fragment at a time, rather than materialising all at once after a pause. That difference is the entire point of streaming — on a CPU-only 3B model the wait is real, and this is what makes it feel responsive instead of frozen.

Worth noting honestly either way: does it actually feel better than the REPL's wait-then-print, or is the difference marginal in practice? A candid answer here is more useful than a confirmation.

## 3. The confirmation gate as UI

Create something disposable, then ask for it to be deleted:

```
> create a file called doomed.txt
> delete doomed.txt
```

When the irreversible step is reached, the confirmation should appear as a **bordered, colored box** — deliberately the only element in the interface styled that way, so a destructive gate can never be mistaken for ordinary output.

Check:

- Is the block reason readable, and does the box clearly stand out from surrounding text?
- Press **`z`** first (an unrelated key). Nothing should happen — the prompt stays up, and `z` must *not* appear in the input field.
- Now press **`n`**. The command should be declined and `doomed.txt` should still exist.
- Repeat the delete and press **`y`**. It should go through the trash mechanism exactly as in CLI mode.

## 4. Scrollback, including mid-confirmation

Generate enough output to overflow the screen:

```
> show me detailed information about every file in /etc
```

While it runs and after it finishes, press **PgUp** and **PgDn**.

- Scrolling up should move back through the transcript.
- **While scrolled up, new output must not yank you back to the bottom.** This is the property that matters: if it jumps while you are reading, that is a real bug, not a nitpick.
- When you are already at the bottom, new output *should* follow automatically.

Now the case this was built for — start a task that triggers a confirmation, and **while the y/N prompt is showing, press PgUp**:

```
> delete every .txt file in this folder
```

You should be able to scroll back to read what was actually proposed *before* answering, and scrolling must not count as an answer. Being able to check before approving a destructive command is the whole reason scroll stays live here.

## 5. Cancel a task without losing the session

```
> wait for five minutes then say done
```

While it is running, press **Ctrl+C once**.

- The task should cancel, and you should land back at a usable `>` prompt.
- **The session must not exit.** Confirm by running another task straight after:

```
> create a file called after-cancel.txt
```

## 6. Resize and quit

While the TUI is open, **resize the terminal window** — drag it narrower and shorter, then wider.

- The transcript should reflow and the input box stay visible and correctly sized.
- Nothing should be cut off, doubled, or left as leftover artifacts.

Then press **Ctrl+C at the idle prompt**. The program should exit cleanly and your original shell contents should reappear (that's alt-screen restoring). Verify the scratch files:

```sh
ls /tmp/synapse-tui-test
```

## What to check if something looks wrong

- **Literal escape codes on screen** (`^[[1m`, `[0m`) — a rendering/terminal-compatibility problem, worth reporting with your `$TERM` value (`echo $TERM`).
- **Streaming looks identical to the REPL** (one pause, then everything at once) — worth reporting; it may mean the stream is being buffered somewhere.
- **The view jumps to the bottom while you are scrolled up reading** — a real bug; the automated test says this cannot happen, so if you see it, the test is missing a case and I want to know.
- **Model proposes something plausible but wrong** — a model-accuracy limit, not a TUI bug. Same as every earlier suite.
- **The confirmation box does not visually stand out** — a design failure worth fixing, since it is the one element that must never be skimmed past.
- **Everything you're testing is uncommitted working-tree state**, not a released build — `git status` in `prototype/` to see exactly what you're running.
