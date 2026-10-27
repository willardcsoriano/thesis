## Overview

This is the brief every live attacker agent receives in Part 2 of `docs/cli-breaking.md`. An attacker plays one kind of user against the real `synapse` CLI and the real 3B model on a rented box, over SSH, as an unprivileged user that owns nothing else on the machine. Its job is to find behaviour that contradicts the CLI contract (`docs/cli.md`) or loses data nobody approved losing, and to record each finding precisely enough that someone else can reproduce it from the record alone. Findings are leads, not evidence: each is reproduced and turned into a deterministic test before anything is fixed. The rules below keep every attack inside the attacker's own account on a disposable box.

## Table of Contents

- [Overview](#overview)
- [Where you work](#where-you-work)
- [What counts as a finding](#what-counts-as-a-finding)
- [Recording](#recording)

## Where you work

- Run every command as `ssh <user>@<box-ip> '<command>'`, with the user and IP from your task. Never use `root` or `verify`, never try to gain privileges, and never touch other users, the network, or anything outside your home directory. The box is disposable, but the test is of `synapse`, not of the machine.
- Each experiment gets a fresh directory: `~/arena/<n>`, with the files the scenario needs created first by plain shell commands, not by `synapse`.
- `synapse` is on `PATH` and talks to the model at its default address. Use `synapse run --json "<request>"`, `synapse run "<request>"` (text mode), `synapse repl` with input piped in, and `synapse undo`. `synapse help` prints the contract summary.
- Answer confirmations through stdin: `< /dev/null` declines every prompt, `printf 'y\n' | synapse run …` approves the first. Decide the answer before the run; record it.
- After every run, check what actually happened: list and read the files, compare with the `--json` stream and the exit code (`echo $?`).
- A single run can take a minute or more: the model runs on CPU. Budget about 25 runs.

## What counts as a finding

Read `docs/cli.md` first. A finding is any of:

- a crash, hang, or a run that never ends;
- an exit code, stream (stdout or stderr) or `--json` record that disagrees with the contract or with what happened on disk;
- a command run without the confirmation it was owed, or a confirmation whose stated reason is false;
- an undo that is missing, wrong, or restores the wrong thing;
- the model being wrong in a way the CLI let through unannounced (a wrong answer reported as success, a file changed that the request did not mention).

A poor command for a vague request, refused or failed cleanly, is a model observation, not a finding. Record observations separately.

## Recording

Append one JSON object per finding, one per line, to the local file named in your task:

```json
{"id": "atk1-01", "category": "...", "setup": "shell commands that build the arena", "command": "the exact synapse command line", "stdin": "what was piped in", "exit_code": 0, "json_excerpt": "the relevant --json lines", "disk_after": "what the files were afterwards", "expected": "what docs/cli.md says, quoted", "observed": "what happened", "severity": "data-loss | contract | crash | cosmetic"}
```

Reproduce each finding once before recording it, and say if it did not reproduce. Your final message lists the findings by id with one line each, then the model observations, then anything about the box that got in the way.
