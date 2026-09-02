## Overview

This is the hands-on manual test suite for **milestone M2 (CLI mode) and Foundational Hardening (F1–F5)** — not a reference doc, a script to follow. It assumes Ollama is already installed and running with the model pulled (see `setup.md` if not) and walks through building the binary, then six concrete steps: a safe propose-only look, a real auto-run reversible task, triggering the confirmation gate on an irreversible one and undoing it, and specifically exercising the trash/git-reset/permission-metadata undo mechanisms F5 added. Everything destructive happens in a disposable scratch directory, never your real files. There's also a section on recording a full transcript of your run, since SynapseOS itself doesn't keep one yet — useful if you want to hand someone else the actual log instead of a paraphrase. Use this whenever you want to verify the prototype behaves the way a session claims it does, rather than taking a status report on faith. **M3a's persistent session (`synapse repl`) has its own dedicated suite** — see `m3a-persistent-loop.md` — since testing it thoroughly needs a different shape of walkthrough than the one-shot invocations here.

## Table of Contents

- [Overview](#overview)
- [Automated coverage — what's already been machine-verified](#automated-coverage-whats-already-been-machine-verified)
- [Recording your session](#recording-your-session)
- [0. Build](#0-build)
- [1. Safe first look — propose-only, touches nothing](#1-safe-first-look-propose-only-touches-nothing)
- [2. A real reversible task — auto-runs, no prompt](#2-a-real-reversible-task-auto-runs-no-prompt)
- [3. Trigger the confirmation gate, then undo it](#3-trigger-the-confirmation-gate-then-undo-it)
- [4. Try the trash mechanism (rm)](#4-try-the-trash-mechanism-rm)
- [5. Try the git-reset mechanism](#5-try-the-git-reset-mechanism)
- [6. Try the permission-metadata mechanism](#6-try-the-permission-metadata-mechanism)
- [What to check if something looks wrong](#what-to-check-if-something-looks-wrong)

## Automated coverage — what's already been machine-verified

`internal/classifier` and `internal/executor` are at 100% coverage (a 40+ case adversarial corpus for the classifier — danger categories, not just today's known shapes); `internal/ollama` and `internal/undo` sit in the high 80s%, with dedicated tests per undo mechanism (trash, content backup, metadata, git-reset) against real files and real throwaway git repos. Full detail: `testing-plan.md`. What it doesn't prove: none of it ran against the real model's actual output or in a real terminal a real person is typing into — that's what the steps below are for.

## Recording your session

There's no built-in activity/session log yet — that's its own deliberately deferred milestone (**M7 — Session logger** in `build-order.md`), not built. `~/.synapse/undo.log` exists today, but it only records the commands that got a safety net, not the full picture (declined confirmations, model latency stats, and `UNSUPPORTED` responses never show up there).

For a complete transcript — the model's proposal, the classification reason, the confirmation prompt, *your typed answer*, and the execution output — use `script`. It records everything shown on your terminal, not just what the program prints, so it captures your `[y/N]` answers too, which a plain `| tee` would miss.

First, make the folder these transcripts live in — `script` won't create it for you:

```sh
mkdir -p ~/.synapse/test-logs   # creates the folder that will hold your recorded transcripts, next to synapse's own undo.log
```

**Whole walkthrough in one file** (recommended — start this before Step 0):

```sh
script ~/.synapse/test-logs/session.txt   # starts recording everything shown in this terminal into that file
# ... now you're inside a recorded shell — run through every step below here ...
exit   # stops recording
```

**One command at a time**, if you'd rather record individual steps separately:

```sh
script -a -c "~/repos/thesis/prototype/bin/synapse 'delete doomed.txt'" ~/.synapse/test-logs/session.txt   # runs just this one command and adds it to the log file
```

After any confirmed-irreversible step, `cat ~/.synapse/undo.log` shows the actual JSON entry SynapseOS recorded for that step — worth cross-checking against the transcript. When you're done, send the `.txt` file (or paste its contents) over.

## 0. Build

```sh
cd ~/repos/thesis/prototype   # moves into the project's prototype folder
script -a -c "go build -o bin/synapse ./cmd/synapse" ~/.synapse/test-logs/session.txt   # compiles the program into a runnable file, and logs this step
```

`go build` prints nothing when it succeeds — only errors show up on screen, so seeing just `Script started...`/`Script done.` and no error text means it worked. Confirm the binary actually got made:

```sh
ls -la bin/synapse
```

You should see a file there with today's timestamp. If instead you see `no such file or directory`, the build failed and the error would have printed above — scroll up (or check `~/.synapse/test-logs/session.txt`) for what it said.

## 1. Safe first look — propose-only, touches nothing

```sh
script -a -c "./bin/synapse" ~/.synapse/test-logs/session.txt   # runs the freshly built program with no task given, and logs this step
```

Runs the built-in 8-task sample suite and only prints what the model *would* run, for every task category. Nothing executes — good for a quick sanity check that Ollama is reachable and the model responds at all.

## 2. A real reversible task — auto-runs, no prompt

Work in a throwaway directory so you're never testing against real files:

```sh
mkdir -p /tmp/synapse-test && cd /tmp/synapse-test   # creates a disposable test folder and moves into it
touch a.log b.log notes.txt   # creates three empty sample files to test with
script -a -c "~/repos/thesis/prototype/bin/synapse 'move all the log files into a folder called archive'" ~/.synapse/test-logs/session.txt   # tells synapse what you want done in plain English, and logs this step
```

Watch it propose a command, classify it Reversible, run it immediately, and print the result. Check with `ls` afterward — `archive/` should hold `a.log`/`b.log`, `notes.txt` should be untouched.

## 3. Trigger the confirmation gate, then undo it

Still in `/tmp/synapse-test`:

```sh
echo "port: 8080" > config.yaml   # creates a sample settings file containing one line of text
script -a -c "~/repos/thesis/prototype/bin/synapse 'change the port in config.yaml to 9090 using sed'" ~/.synapse/test-logs/session.txt   # asks synapse to edit that file, and logs this step
```

Expect a proposed `sed -i ...` command, then `blocked: ... is irreversible — sed -i overwrites the file in place...`, then a `[y/N]` prompt. Answer **y** — it edits the file for real, but takes a content backup first without you having to ask.

```sh
script -a -c "~/repos/thesis/prototype/bin/synapse undo" ~/.synapse/test-logs/session.txt   # asks synapse to reverse its last change, and logs this step
```

Expect a preview line `restore content: /tmp/synapse-test/config.yaml`, a confirmation prompt, and — if you say yes — `config.yaml` back to `port: 8080`.

## 4. Try the trash mechanism (rm)

```sh
cd /tmp/synapse-test   # moves back into the scratch test folder
echo "keep me" > doomed.txt   # creates a sample file that's about to be deleted
script -a -c "~/repos/thesis/prototype/bin/synapse 'delete doomed.txt'" ~/.synapse/test-logs/session.txt   # asks synapse to delete the file, and logs this step
```

Confirm it. `doomed.txt` should genuinely be gone (`ls` to check). Then:

```sh
script -a -c "~/repos/thesis/prototype/bin/synapse undo" ~/.synapse/test-logs/session.txt   # asks synapse to bring the deleted file back, and logs this step
```

Expect `restore from trash: /tmp/synapse-test/doomed.txt`, and the file back with its original content after confirming. This one costs the same to protect regardless of file size — try it on a large file if you want to feel that it's a hardlink, not a copy (near-instant either way).

## 5. Try the git-reset mechanism

Only if you have a git repo handy, or make a scratch one:

```sh
mkdir -p /tmp/synapse-git-test && cd /tmp/synapse-git-test   # creates a separate scratch folder for this test and moves into it
git init -q && git config user.email you@example.com && git config user.name you   # sets up a throwaway git project so there's history to roll back
echo v1 > f.txt && git add . && git commit -qm first   # creates a file and saves it as the first checkpoint
echo v2 > f.txt && git add . && git commit -qm second   # changes the file and saves a second checkpoint
script -a -c "~/repos/thesis/prototype/bin/synapse 'undo the last commit with git reset hard'" ~/.synapse/test-logs/session.txt   # asks synapse to roll back to the first checkpoint, and logs this step
```

Confirm it, check `cat f.txt` shows `v1`. Then `synapse undo` — expect `reset git HEAD back to: <sha>`, and `f.txt` back to `v2` after confirming.

## 6. Try the permission-metadata mechanism

```sh
mkdir -p /tmp/synapse-test/target   # creates a folder to test permission changes on
echo x > /tmp/synapse-test/target/file.txt   # creates a sample file inside it
chmod 640 /tmp/synapse-test/target/file.txt   # sets that file's starting permissions (who's allowed to read/write it)
cd /tmp/synapse-test   # moves into the parent scratch folder
script -a -c "~/repos/thesis/prototype/bin/synapse 'make everything in target world-writable recursively'" ~/.synapse/test-logs/session.txt   # asks synapse to open up permissions on the folder, and logs this step
```

Confirm it, check `ls -l target/file.txt` shows wide-open permissions. Then `synapse undo` — expect `restore permissions: .../target/file.txt`, and the original `0640` mode back after confirming.

**That's the full M2 + Foundational Hardening suite.** If you also want to try `synapse repl` (the persistent multi-task session, M3a), that's `m3a-persistent-loop.md` — a separate suite, since it needs a different recording shape (one continuous session instead of per-command invocations) and its own edge cases to test properly.

## What to check if something looks wrong

- **Model proposes something plausible but wrong** (wrong flag, wrong file) — that's a model-accuracy limit, not a bug in the classifier/executor/undo mechanics. Worth noting, not worth panicking over.
- **Nothing happens / connection refused** — `curl -s http://localhost:11434/api/tags` should return JSON; if not, `ollama serve` isn't running (see `setup.md`).
- **Which commands get a safety net and which don't, and why** — `../docs/safety-model.md` is the full reference table.
- **Everything you're testing here is uncommitted working-tree state**, not a released build — `git status` in `prototype/` if you want to see exactly what you're running.
