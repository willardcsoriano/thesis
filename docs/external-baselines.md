## Overview

The corpus evaluation compares the recoverability analysis against SynapseOS's own pattern list, which the team wrote. To answer the obvious objection, that a home-made baseline might have been made weak, this document records a second comparison against open-source command-safety checks other people ship: the dangerous-command check in OpenAI's Codex CLI, and cc-safety-net, a guard used with Claude Code, Codex, Gemini CLI and eleven other agents. Both were run over the same 777 held-out commands with the same sandbox-verified ground truth. On the 325 file-destroying commands, they let 73–97% through silently, against 31% for SynapseOS's own list and 0% for the analysis. The home-made baseline is therefore stronger than the real tools, not weaker, and the comparison in the paper is conservative. These results are not in the paper; they support answers to questions.

## Table of Contents

- [Overview](#overview)
- [Why this was done](#why-this-was-done)
- [What was compared](#what-was-compared)
- [What was excluded, and why](#what-was-excluded-and-why)
- [Method](#method)
- [Results](#results)
- [How to read the results fairly](#how-to-read-the-results-fairly)
- [Reproducing it](#reproducing-it)
- [Files](#files)

## Why this was done

The "pattern list" baseline (`prototype/internal/classifier`) was written in-house in July 2026, before any coding agent was surveyed (`docs/prior-art.md`). It works the way the field does, matching commands against patterns judged dangerous, but because the team wrote it, a reader can fairly ask whether it was made weak so the analysis would look good. The answer had to come from lists the team did not write.

## What was compared

| Baseline | Source | License | What it checks |
|---|---|---|---|
| **Codex CLI** (`CODEX`) | `github.com/openai/codex`, `codex-rs/shell-command/src/command_safety/is_dangerous_command.rs` and `bash.rs`, commit `1f4c473` (2026-09-01) | Apache-2.0 | On Linux and macOS, only forced deletion: `rm` with `-f`/`--force`, also seen through `sudo`, `env`, `trap` and nested `bash -c`. A script that does not parse is not checked. |
| **cc-safety-net** (`CCSN-standard`, `-strict`, `-paranoid`) | `github.com/kenryu42/cc-safety-net`, npm package `cc-safety-net@2.6.1` | MIT | A rules engine for destructive file and git operations. Run in all three of its presets. |

Both are checks that run before a command executes and either let it through or stop it for the user. Neither has an undo mechanism, so they are scored on silent loss and needless asks, not on recovery.

## What was excluded, and why

- **destructive_command_guard** (`Dicklesworthstone/destructive_command_guard`, about 6,100 stars, around 50 rule packs). Its license is MIT with a rider that withholds all rights from OpenAI, Anthropic and anyone acting on their behalf, explicitly including "testing, analyzing … or incorporating the Software … into any … evaluation harness". This comparison was carried out by an Anthropic model on the team's behalf, so using it was at best legally ambiguous, and it was left out.
- **Codex's former "known safe" allowlist** (`is_safe_command.rs`). It no longer exists in the current Codex source; Codex now relies on its sandbox and the forced-`rm` check above. Only what Codex currently ships was used.
- **gemini-cli** has user-configured allow and deny lists with no built-in dangerous-command list to compare (`docs/prior-art.md`).

## Method

- **Codex was ported, and the port is proven faithful by Codex's own tests.** `prototype/internal/baselines/codex` reproduces the two source files function by function, using `mvdan.cc/sh` (already a dependency) where Codex uses tree-sitter. Codex's own unit tests are ported in `codex_test.go` and all pass. They include forced `rm` inside `if`, `for`, `$(…)`, `trap` and nested `bash -c`, and cases that must not be flagged. Each corpus command is evaluated as Codex receives a model's shell call: `["bash", "-lc", command]`.
- **cc-safety-net was run unmodified.** `prototype/pilot/ccsafetynet.mjs` calls the package's own exported `checkCommand` once per command per preset. It was installed with `--ignore-scripts`. Each call uses the command's own starting directory, laid out by `cmd/extbaselines -fixtures`, and a `HOME` inside it, so no personal configuration applies. A check found its answers identical whether the directory is under `/tmp` or the home directory, so the location does not bias it.
- **Scoring is unchanged.** Silent loss means a command that destroys data ran without asking and without a backup; needless asks are prompts on commands that lose nothing. The ground truth is the same sandbox labels the paper uses. `pilot/analyze.py` now accepts extra system files and gives byte-identical output without them.

## Results

**File-destroying commands only** (325 commands; the 80 external-effect commands such as `ssh` and `mount` are excluded because these tools do not claim to cover them):

| System | Silent loss | Needless asks (of 372 harmless) |
|---|---|---|
| Codex CLI | 314/325 = **96.6%** | 1 (0.3%) |
| cc-safety-net, Standard | 262/325 = **80.6%** | 7 (1.9%) |
| cc-safety-net, Strict | 257/325 = **79.1%** | 12 (3.2%) |
| cc-safety-net, Paranoid | 236/325 = **72.6%** | 15 (4.0%) |
| SynapseOS pattern list (the paper's baseline) | 100/325 = **30.8%** | 30 (8.1%) |
| Fail-closed list | 0/325 = 0% | 101 (27.2%) |
| **Recoverability analysis** | **0/325 = 0%** | **32 (8.6%)** |

All 405 dangerous commands, including external ones: Codex 97%, cc-safety-net 78–84%, the pattern list 41%, and the analysis 0% silent loss (`prototype/pilot/results_round7_external.txt`).

## How to read the results fairly

- **The paper's baseline is the strongest list tested.** It lets 31% through where the open-source checks let 73–97% through, so comparing the analysis against it understates the analysis's advantage over real tools.
- **The tools were built for a different threat.** Codex and cc-safety-net guard coding agents working inside a git repository, where deleting project files is routine and git is the safety net. They block catastrophic operations (`rm -rf` outside the project, `git reset --hard`), not everyday deletions. Even on Paranoid, cc-safety-net allows `rm notes.txt`, `rm -f *.log` and `find … -exec rm`. The corpus asks a different question: whether a person's files can be lost without warning. The numbers say these tools do not protect a desktop user's personal files. That is not a judgment of how well they do their own job.
- **Codex also has a sandbox,** which limits where commands can write. That is a separate layer from a list and is not part of this comparison.
- **The analysis matches the strictest protection without its cost.** It has the fail-closed list's zero silent loss with the pattern list's level of needless asks (8.6% against 8.1%).

## Reproducing it

From `prototype/`:

```sh
go run ./cmd/extbaselines -fixtures /tmp/fx pilot/corpus7.jsonl > pilot/external_baselines7.jsonl
mkdir -p /tmp/ccsn && cp pilot/ccsafetynet.mjs /tmp/ccsn/ && (cd /tmp/ccsn && npm init -y && npm install --ignore-scripts cc-safety-net@2.6.1)
P=$PWD
(cd /tmp/ccsn && node ccsafetynet.mjs "$P/pilot/corpus7.jsonl" /tmp/fx) >> pilot/external_baselines7.jsonl
python3 pilot/analyze.py pilot/corpus7.jsonl pilot/classifier_out7.jsonl pilot/external_baselines7.jsonl > pilot/results_round7_external.txt
python3 pilot/external_compare.py pilot/corpus7.jsonl pilot/classifier_out7.jsonl pilot/external_baselines7.jsonl
```


## Files

- `prototype/internal/baselines/codex/`: the Codex port and Codex's own tests.
- `prototype/cmd/extbaselines/`: scores the corpus with the Codex port and lays out per-command fixtures.
- `prototype/pilot/ccsafetynet.mjs`: runs cc-safety-net's own `checkCommand`.
- `prototype/pilot/external_baselines7.jsonl`: every verdict, one line per command per system (3,108 lines).
- `prototype/pilot/results_round7_external.txt`: the full `analyze.py` report with the external systems included.
- `prototype/pilot/external_compare.py` and `results_round7_external_files.txt`: the file-destroying-only comparison above.
