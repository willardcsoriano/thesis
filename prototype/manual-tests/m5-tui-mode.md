## Overview

This is the hands-on manual test suite for **milestone M5 — TUI mode**, created as that milestone's final step per the `manual-tests/` convention. It covers only what M5 added: the inline terminal interface, token-by-token streaming, terminal-native scrollback, and the confirmation gate rendered as UI instead of a stdin prompt. The propose/classify/execute mechanics underneath are unchanged and already covered by `m1-cli-mode-and-undo-safety-net.md`. Six steps, roughly fifteen minutes, everything destructive confined to a disposable scratch directory. **This suite matters more than the previous two**: unlike CLI and REPL mode, a TUI cannot be driven by piping input, so it went into a real terminal almost entirely unexercised. Its first real run, on 2026-09-08, immediately found two rendering defects that every automated test had missed (see below) — which is the clearest possible evidence that running this by hand is not a formality.

## Automated coverage — what's already been machine-verified

`internal/tui` sits at 95.7% coverage across 29 tests, clean under `-race` over 20 repeats: the confirmation bridge (y/Y approves, n/N/esc/enter fail closed, unrelated keys ignored), task lifecycle, ctrl+c cancelling a task without ending the session, ordered printing into the scrollback (each finished line exactly once, in source order), and that the program never asks for the alternate screen or mouse reporting. `internal/ollama`'s streaming path adds 8 more, including a safety test that a truncated stream is rejected outright rather than handed on as a shorter command. Several are mutation-verified — the code was deliberately broken to confirm the test fails.

**Two of those tests exist because this suite's first real run found bugs they should have caught.** Both were fixed on 2026-09-08 and are worth knowing about while you re-run this:

- **Streamed fragments rendered as separate lines** — the model streams `UNS`, then `UPPORTED`, with no newline between them, and the transcript treated each chunk as a finished line, so the screen read `UNS` / `UPPORTED` stacked vertically. The old test had actually *encoded* the bug: it appended newline-less strings and asserted each became its own line, so it passed while asserting the wrong thing.
- **Output arrived out of source order** — starting a task registered a second listener on the event channel without consuming one, leaving two goroutines racing to receive from it. Delivery order to the renderer became undefined (`model reported…` printing above the `step 1:` line that precedes it), and it compounded: every task started added another receiver.

Neither needed a TTY to catch. What was actually missing was a test of the path output really takes — writer → event channel → renderer — rather than messages hand-constructed in the test. That test now exists (`TestTaskOutputReachesTheTranscriptInSourceOrder`) and both bugs were mutation-verified: reintroducing either one fails it.

What none of that proves: **every one of those tests bypasses the real terminal.** Bubble Tea needs an actual TTY, which the build environment does not have, so none of them exercise a real screen, a real resize, or a real keypress. Colors, borders, cursor placement, flicker, and whether streaming *feels* responsive are unverified by construction, and remain this suite's reason to exist — that part needs a human.

**Partially closed 2026-09-26:** `drive_tui.py` in this directory opens a real pty (the actual syscall path Bubble Tea needs — `openpty`, a `TIOCSWINSZ` window size, scripted writes to the master side), runs the compiled binary against it exactly as a terminal emulator would, and captures everything written back. This gets the binary rendering to a real screen under automated control — worth running after a change that touches `internal/tui` before this suite's human pass, since it catches a functional regression (wrong output, a stuck prompt, a crash) for free. It does not replace this suite: it cannot judge whether something *feels* responsive, and it found one anomaly it could not itself explain (garbled placeholder text after a task completes, `open-problems.md` row 39) — a case for a human to look at directly, not against.

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

The header and hint lines print below your shell history (the screen is not cleared: this is an inline interface, no alternate screen), with a bold header, dimmer hint lines, and a `>` input prompt at the bottom.

Check, and note anything off:

- Does the header render **bold and colored**, and the hints **dimmer** than normal text?
- Is the input prompt visible at the bottom, not pushed off-screen or overlapping?
- Any flicker, torn lines, or stray escape characters (`^[[0m` and similar) printed literally?

## 2. Streaming — watch a command appear token by token

This is what M5 added over the REPL. Type a task and watch **how** the answer arrives:

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

The output prints into the terminal's own scrollback, so use the terminal, not the program:

- Scroll up with the **mouse wheel**, the terminal's **scrollbar**, or **Shift+PgUp**. The whole conversation since you started, header included, should be reachable.
- **Click-drag while scrolling** (or drag past the edge) to highlight text that is off-screen, then copy it. This is the property the alternate-screen version could not offer: there, only what fit on screen could be selected.
- Nothing printed should appear twice or out of order, and new output should not fight your scroll position beyond what your terminal normally does.

Now the case that matters for safety — start a task that triggers a confirmation, and **while the y/N prompt is showing, scroll up**:

```
> delete every .txt file in this folder
```

You should be able to scroll back to read what was actually proposed *before* answering, and scrolling must not count as an answer. Being able to check before approving a destructive command is the whole reason this matters.

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

- The input box should stay visible and correctly sized. Already-printed lines reflow the way any terminal reflows scrollback.
- Nothing should be cut off, doubled, or left as leftover artifacts.

Then press **Ctrl+C at the idle prompt**. The program should exit cleanly and leave no stale prompt behind, with the conversation still in your scrollback. Verify the scratch files:

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
