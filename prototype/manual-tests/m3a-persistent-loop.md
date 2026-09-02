## Overview

This is the hands-on manual test suite for milestone **M3a — persistent CLI loop**, created as that milestone's final step per the `manual-tests/` convention (see `m2-cli-mode-and-undo-safety-net.md` for the M2 + Foundational Hardening suite this one sits alongside). It only exercises what M3a specifically added — `synapse repl`, a persistent multi-task session — not the classify/confirm/execute mechanics underneath it, which `m2-cli-mode-and-undo-safety-net.md` already covers and which don't change here. Five steps: starting a session and running two tasks without the process restarting, leaving cleanly two different ways, confirming a failed task doesn't kill the session, and the one thing actually worth being careful about — that a confirmation answer and the next typed task never get confused with each other. There's also a note on the one thing that deliberately doesn't work yet (`undo` as a typed line inside the session). Run this before moving on to M3b to confirm M3a's own claim — a persistent session actually behaves correctly — rather than trusting the build log.

## Table of Contents

- [Overview](#overview)
- [Automated coverage — what's already been machine-verified](#automated-coverage-whats-already-been-machine-verified)
- [Recording your session](#recording-your-session)
- [0. Build (skip if you already have a fresh binary)](#0-build-skip-if-you-already-have-a-fresh-binary)
- [1. Start a session and run two tasks without restarting](#1-start-a-session-and-run-two-tasks-without-restarting)
- [2. Leave cleanly — `exit`/`quit`, and Ctrl+D](#2-leave-cleanly-exitquit-and-ctrld)
- [3. A failed or nonsense task doesn't end the session](#3-a-failed-or-nonsense-task-doesnt-end-the-session)
- [4. The one real risk: a confirmation answer must not get confused with the next task](#4-the-one-real-risk-a-confirmation-answer-must-not-get-confused-with-the-next-task)
- [5. What doesn't work yet — `undo` as a typed line](#5-what-doesnt-work-yet-undo-as-a-typed-line)
- [What to check if something looks wrong](#what-to-check-if-something-looks-wrong)

## Automated coverage — what's already been machine-verified

`cmd/synapse/repl_test.go` covers `runREPL` at 100%: multiple tasks in one process, clean exit (`exit`/`quit`/EOF), a failed task not ending the session, and the same interleaved-input risk Step 4 below asks you to feel for yourself — including a mutation test proving that specific check isn't passing by accident (`build-order.md`'s M3a entry has the detail). What it doesn't prove: those tests run against a mocked model and an in-memory input stream, not a real terminal — that gap is this suite's whole job.

## Recording your session

Same tool as `m2-cli-mode-and-undo-safety-net.md`, `script` — see that file for the full explanation of why (it captures your typed `[y/N]` answers too, which plain output logging would miss). Quick version, using its own log file so this suite's transcript doesn't mix with `m2-cli-mode-and-undo-safety-net.md`'s:

```sh
mkdir -p ~/.synapse/test-logs   # only needed once — skip if you already ran m2-cli-mode-and-undo-safety-net.md
script ~/.synapse/test-logs/m3a-session.txt   # starts recording this whole terminal session into that file
# ... now you're inside a recorded shell — run every step below here ...
exit   # stops recording, at the very end of the whole suite
```

Because every step below happens *inside* one continuous `synapse repl` session (or the shell around it), there's no per-command `script -a -c` wrapping here the way `m2-cli-mode-and-undo-safety-net.md` uses for its one-shot invocations — you start recording once, at the top, and everything typed and shown from here on is captured automatically.

## 0. Build (skip if you already have a fresh binary)

```sh
cd ~/repos/thesis/prototype   # moves into the project's prototype folder
go build -o bin/synapse ./cmd/synapse   # compiles the program into a runnable file
```

`go build` prints nothing on success — see `m2-cli-mode-and-undo-safety-net.md`'s Step 0 if that looks like nothing happened. Confirm with `ls -la bin/synapse` if unsure.

## 1. Start a session and run two tasks without restarting

Work in a throwaway directory, same discipline as `m2-cli-mode-and-undo-safety-net.md`:

```sh
mkdir -p /tmp/synapse-repl-test && cd /tmp/synapse-repl-test   # creates a disposable test folder and moves into it
~/repos/thesis/prototype/bin/synapse repl   # starts the persistent session
```

Expect a greeting line (`persistent session — type a task and press enter; type exit or quit (or Ctrl+D) to leave.`) and a `>` prompt. Now type two unrelated tasks, one after another, **without the binary restarting between them** — that's the entire point of M3a:

```
> create a file called one.txt
> create a file called two.txt
```

Each should propose, auto-run (both are reversible), and print its own result, landing back at a fresh `>` prompt each time. Once both are done, leave the session (`exit`, see Step 2) and check from your normal shell:

```sh
ls one.txt two.txt
```

Both files should exist. This confirms the core claim: one process handled two distinct tasks in a row.

## 2. Leave cleanly — `exit`/`quit`, and Ctrl+D

Two different ways to leave, both should exit without an error or a hang. Start a fresh session for this:

```sh
~/repos/thesis/prototype/bin/synapse repl
```

At the `>` prompt, type:

```
> exit
```

You should land back at your normal shell prompt immediately, no error message. Repeat once more, but this time leave by pressing **Ctrl+D** instead of typing `exit` — this sends end-of-file rather than a line of text, the way a real terminal user would close the session without thinking about the "right" word to type. Same expectation: back at your normal shell prompt, no error, no hang. (`quit` also works, same as `exit` — no need to test it separately, it's the identical code path.)

## 3. A failed or nonsense task doesn't end the session

Start a session again, and deliberately give it something it can't turn into a shell command:

```
> do my taxes
```

Expect the model to report it can't be done with a shell command (`UNSUPPORTED`), and — this is the actual thing to verify — **land back at a working `>` prompt afterward**, not exit the session. Confirm the session is still alive by giving it a real task right after:

```
> create a file called after-failure.txt
```

This should work normally. Then `exit`, and check `ls /tmp/synapse-repl-test/after-failure.txt` exists. One bad task not forcing a restart to try another is the whole reason a persistent loop is worth having.

## 4. The one real risk: a confirmation answer must not get confused with the next task

This is the part worth testing carefully, not just glancing at. Start a session in the same scratch folder:

```
> create a file called doomed2.txt
```

Then immediately, without leaving the session:

```
> delete doomed2.txt
```

Expect the usual `blocked: ... is irreversible` message and a `[y/N]` prompt. Answer:

```
y
```

Then, **on the very next line, immediately type another task** — don't pause, don't check anything in between:

```
> create a file called right-after.txt
```

What to check: the `y` you typed should be read as the confirmation answer (the delete goes through — `doomed2.txt` should be gone), and the `create a file called right-after.txt` line should be read as a fresh task on its own `>` prompt (`right-after.txt` should get created), **not** swallowed, **not** misread as a second confirmation answer, and **not** treated as part of the same task as the delete. Leave the session and check:

```sh
ls /tmp/synapse-repl-test/doomed2.txt /tmp/synapse-repl-test/right-after.txt
```

Expect `doomed2.txt: No such file or directory` and `right-after.txt` present. If instead the second task never ran, or the delete never happened despite typing `y`, that's exactly the failure mode this step exists to catch — worth reporting back with the full transcript rather than retrying and hoping it was a fluke.

## 5. What doesn't work yet — `undo` as a typed line

While still comfortable inside a session, try:

```
> undo
```

Expect this to be sent to the model as a task (it'll likely come back `UNSUPPORTED` or propose something nonsensical) — **not** trigger the actual undo mechanism. That's a known, deliberate gap: `undo` only exists as a separate top-level invocation (`synapse undo`), not as a recognized word inside `repl` yet. To actually undo something you did inside a session, leave first (`exit`), then from your normal shell:

```sh
~/repos/thesis/prototype/bin/synapse undo
```

## What to check if something looks wrong

- **A task's model output looks plausible but wrong** — same as `m2-cli-mode-and-undo-safety-net.md`: a model-accuracy limit, not something this milestone's own mechanics are responsible for.
- **The session hangs after answering a confirmation prompt** — that's the failure mode Step 4 is designed to surface; capture the full `script` transcript rather than closing the terminal, it's the only record of exactly what was typed and in what order.
- **`ls` after Step 1 shows only one file, not two** — the second task may not have run at all; check the transcript for whether a second `>` prompt even appeared.
- **Everything here is uncommitted working-tree state**, not a released build — `git status` in `prototype/` if you want to see exactly what you're running.
