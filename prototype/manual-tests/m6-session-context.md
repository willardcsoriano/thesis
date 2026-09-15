## Overview

This is the hands-on manual test suite for **milestone M6 — session context**, created as that milestone's final step per the `manual-tests/` convention. It covers only what M6 added: memory *between* tasks, so a follow-up like "move it to Downloads" knows what *it* is. The propose/classify/confirm/execute mechanics underneath are unchanged and already covered by `m1-cli-mode-and-undo-safety-net.md`. Six steps, roughly fifteen minutes, everything confined to a disposable scratch directory. M6 is the milestone where the prototype stops being a sequence of independent one-shots and starts being a conversation — and whether that *feels* true is something only a person typing real follow-ups can judge.

## Automated coverage — what's already been machine-verified

`internal/session` is at 98.1% across 16 tests: the rolling window drops oldest-first, never evicts the newest turn, reports evictions rather than forgetting silently, truncates results, and calibrates its token estimate from real `prompt_eval_count` measurements. Two invariants are mutation-verified — the newest-turn guard and the eviction reporting. At the loop level, further tests confirm a completed turn is recorded, prior turns actually reach the next prompt, a partially-failed task is still remembered, one-shot CLI mode stays stateless, and `context`/`clear` are answered locally rather than sent to the model.

What none of that proves: **whether the model actually resolves a reference correctly.** Those tests assert the history is *assembled* into the prompt — not what a 3B model concludes from reading it. That gap is real and already bit once: with history wired in and every automated test green, the live follow-up "rename it to renamed.txt" returned `DONE`, because the model read the previous task's steps as having already completed the current one. It took a live run to catch, and a prompt fix to resolve. Judging reference resolution against the real model is this suite's whole job.

## Table of Contents

- [Overview](#overview)
- [Automated coverage — what's already been machine-verified](#automated-coverage-whats-already-been-machine-verified)
- [0. Build and start a session](#0-build-and-start-a-session)
- [1. The headline claim — a follow-up resolves a reference](#1-the-headline-claim-a-follow-up-resolves-a-reference)
- [2. Inspect what's remembered](#2-inspect-whats-remembered)
- [3. Clear the memory](#3-clear-the-memory)
- [4. A failed task is still remembered](#4-a-failed-task-is-still-remembered)
- [5. Long session — eviction is announced, not silent](#5-long-session-eviction-is-announced-not-silent)
- [6. One-shot CLI mode stays stateless](#6-one-shot-cli-mode-stays-stateless)
- [What to check if something looks wrong](#what-to-check-if-something-looks-wrong)

## 0. Build and start a session

```sh
cd ~/repos/thesis/prototype   # moves into the project's prototype folder
go build -o bin/synapse ./cmd/synapse   # compiles the program
mkdir -p /tmp/synapse-m6 && cd /tmp/synapse-m6   # disposable scratch directory
~/repos/thesis/prototype/bin/synapse repl   # starts a persistent session
```

The greeting should now mention that follow-ups can refer back, and name `context` and `clear`. If it doesn't, you're running an older binary.

## 1. The headline claim — a follow-up resolves a reference

This is the one step that matters most. Type these as two **separate** tasks:

```
> create a file called notes.txt
> rename it to renamed.txt
```

**What to check:** the second task must propose something like `mv notes.txt renamed.txt`. The word "it" has no meaning on its own — the only way the model can resolve it is from the previous turn.

Failure modes worth telling apart:

- **It proposes `DONE` immediately** — the model read the earlier task as having already finished this one. This is the exact bug found and fixed during the build; if it's back, that's a real regression.
- **It asks about a different file, or invents one** — reference resolution failed. Note what it proposed.
- **It proposes a correct-shaped command on the wrong file** — a model-accuracy limit rather than a memory failure, but worth recording.

Confirm with `ls` afterward: you should have `renamed.txt` and no `notes.txt`.

Then try a second, harder follow-up — one where the referent is a *result* rather than a filename:

```
> count the lines in it
```

## 2. Inspect what's remembered

```
> context
```

Expect a compact list of remembered tasks with the commands each ran. This isn't a debug feature: reference resolution only works if you and the system agree on what "it" points at, and this is the only way to check that agreement **before** issuing a follow-up that deletes something.

**What to check:** are the tasks you actually ran listed, in order, with recognisable commands? Is it readable at a glance, or would you have to squint to spot a wrong referent?

## 3. Clear the memory

```
> clear
> context
```

The first should report how many tasks it forgot; the second should say nothing is remembered. Then confirm memory really is gone rather than merely hidden:

```
> rename it to something else
```

With no history, "it" is unresolvable — the model should ask for or invent a filename rather than correctly picking up the earlier one. If it still resolves to `renamed.txt`, `clear` didn't actually clear.

## 4. A failed task is still remembered

```
> create a file called temp.txt
> do my taxes
> context
```

The middle task should fail (`UNSUPPORTED`). What matters: `context` should still show the *first* task, and the session should still be usable. A failed task must not wipe or corrupt memory.

Then check a follow-up still works across the failure:

```
> delete temp.txt
```

## 5. Long session — eviction is announced, not silent

Run enough tasks to push older ones out of the budget. Six to eight small tasks should do it; vary them so you can tell what got dropped:

```
> create a.txt
> create b.txt
> create c.txt
> create d.txt
> create e.txt
> create f.txt
> create g.txt
> create h.txt
```

**What to check:** when older turns get dropped you should see a line saying so — something like `note: dropped N older turn(s) from memory to stay within the context budget.`

This is the property worth being fussy about. A rolling window that forgets *silently* is the worst possible failure mode: the system looks like it's working right up until it inexplicably doesn't remember something, with nothing to explain why. If turns vanish with no notice, that's a real bug.

Then run `context` and confirm the list matches what you'd expect to still be there — recent tasks present, oldest gone.

## 6. One-shot CLI mode stays stateless

Leave the session (`exit`), then run two **separate** one-shot invocations:

```sh
cd /tmp/synapse-m6
~/repos/thesis/prototype/bin/synapse "create a file called oneshot.txt"
~/repos/thesis/prototype/bin/synapse "rename it to oneshot2.txt"
```

**What to check:** the second one should *not* resolve "it". Each invocation is a fresh process with no memory, and that's deliberate — statelessness is what makes CLI mode safe to script (D19), not an oversight. If it somehow does resolve, memory is leaking across processes, which would be a genuine bug.

## What to check if something looks wrong

- **A follow-up resolves to the wrong thing** — run `context` and compare what you expected against what's actually remembered. If memory is right but resolution is wrong, that's a model-accuracy limit; if memory is wrong, that's ours.
- **Turns disappear with no notice** — a real bug. The eviction notice exists precisely so forgetting is never silent.
- **`clear` doesn't seem to take effect** — check whether the follow-up after it still resolves a stale referent; that distinguishes "cleared but display is wrong" from "not cleared."
- **Responses get noticeably slower as the session grows** — expected to a degree, since history adds prompt tokens. Worth reporting if it becomes unusable rather than merely slower; the budget is tunable via `SYNAPSE_NUM_CTX`.
- **Something references an 8K context limit** — stale. The effective window is whatever `SYNAPSE_NUM_CTX` is set to (default 8192); Ollama's own default is 2048 and truncates silently, which is why the runtime sets it explicitly.
- **Everything you're testing is uncommitted working-tree state** — `git status` in `prototype/` to see exactly what you're running.
