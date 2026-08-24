## Overview

This is the rigorous testing plan for SynapseOS's execution engine (`internal/classifier`, `internal/executor`, `internal/undo`, and `cmd/synapse`'s orchestration loop), written in Session 22 in response to a real gap found by hand: `sed -i`, `tee`, and `truncate` all auto-ran without confirmation despite destroying file content with no undo. A fixed example list — including the one that found that gap — can never be exhaustive; this document is the plan for testing *categories of danger* and *the model's actual behavior*, not just today's known examples, and for doing it in a way that answers a specific question the current single-model setup can't: is a given failure a property of the *engine*, or a property of *this one 3B model*. Six layers are defined below, ordered cheapest/most-deterministic first. Layers 1–2 are largely built; Layers 3–6 are the concrete next work (`build-order.md` F3/F4).

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

**Status: partially built (today's fix), needs generalizing.** F1 closed four specific gaps found by hand. The rigorous version of this layer replaces "specific gaps found by hand" with **a taxonomy of danger categories**, each with representative *and* adversarial variants, so the next gap is found by a systematic sweep rather than another live-testing accident:

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

**Status: not built.** Every layer above tests the classifier against *hand-picked* strings. This layer tests it against what the model **actually proposes** in practice, which is a different and arguably more important question — the classifier only matters for commands the model actually generates.

**Method:** run a broad, categorized task corpus (larger than today's 8-task sample suite — needs building, see scope note below) through `propose`/`proposeStep` against a live model, and for every proposed command, run it through `Classify` and record the verdict. This is *not* scoring correctness of the bash (that's the separate, already-deferred Intent Parsing Accuracy Evaluator in `scope.md`) — it's asking "of everything this model actually tends to output, does anything slip past the classifier that a human reviewer would flag as risky." A human spot-check pass over the propose-only output is the actual test; the harness's job is to make that spot-check corpus large and reproducible rather than ad hoc.

**Deliverable:** a `prototype/playground/` script or Go test (behind a build tag — see Layer 6) that runs N tasks per category, logs `{task, proposed_command, verdict, reason}` as structured output, and flags anything classified Reversible for manual review before it's trusted as evidence the classifier is holding up in practice, not just against a fixed table.

## Layer 4 — Typed-Operation Reliability Experiment

**Status: not built** — this is `build-order.md` F4, the specific empirical question behind "should SynapseOS reimplement filesystem-MCP-style typed operations." Method:

1. Fix a task set of file-manipulation requests (reuse Layer 3's file-manipulation-category subset).
2. Run each task through **two paths**: (a) today's raw-bash-string + classifier path, and (b) a typed-operation path — the model called via Ollama's native tool-calling API with a small schema (`move_file(src, dst)`, `delete_file(path)`, `list_directory(path)`, etc.), dispatched through Go's own `os`/`io` stdlib, no MCP protocol or Node.js dependency.
3. Score each path on: **call-validity rate** (did the model emit a well-formed call/command at all), **task-success rate** (did it accomplish the request), and **safety-classification agreement** (would each path have caught the same set of dangerous requests).
4. The result decides whether F4 becomes real runtime scope or stays documented as "tried, not worth it yet" — not decided in advance.

## Layer 5 — Executor Chaos/Edge-Case Testing

**Status: not built.** `internal/executor` is at 100% coverage for its *happy path and simple failure* cases (Session 21), but hasn't been tested against the edge cases that show up once real, model-generated commands run against a real filesystem:

- A command that hangs indefinitely (does `context.WithTimeout` in `proposeStep`/`propose` actually bound the *executed* command too, or only the model-generation request? — worth double-checking directly, since `executor.Run` takes a `ctx` but `runLoop` passes the outer, unbounded `ctx` from `main()`, not a per-execution timeout)
- A command producing gigabytes of stdout (memory behavior of `bytes.Buffer`-based capture)
- Non-UTF8 / binary output (does capture and the later prompt-feedback truncation handle this without corrupting the byte stream or crashing)
- A command that writes to stdin expecting interactive input the harness never provides
- Concurrent/overlapping runs, relevant once M3a's persistent loop exists and a user could plausibly trigger overlapping undo-recording snapshots

## Layer 6 — Regression Harness

**Status: partially built.** Layers 1–2 are already fast, deterministic, and run by default under `go test ./...`. Layers 3–5 need live infrastructure (a running Ollama server, possibly multiple pulled models) and shouldn't slow down or break the default test run. **Method:** gate them behind a Go build tag (e.g. `//go:build live`) so `go test ./...` stays fast/deterministic by default, and `go test -tags live ./...` (or a dedicated `make test-live` target) runs the full suite including model-facing layers when a live Ollama server is available.

## Model Parameterization — a Cross-Cutting Requirement

Every layer that touches a live model must be **parameterized by model tag, never hardcoded** — this was flagged explicitly and is a hard requirement, not a nice-to-have: the whole point of Layer 3/4 is to be able to answer "is this a 3B-specific problem, or does it persist at 5B/7B" by re-running the *same* corpus against a different `SYNAPSE_MODEL` value with zero code changes. The infrastructure for this already exists at the single-run level (`SYNAPSE_MODEL` env var, already used by `main.go`); what Layer 3/4's harness adds is a **matrix mode** — run the same task/prompt corpus against a configured list of models in sequence, and produce a simple comparative report (model × task → verdict/success, even just structured stdout or a CSV — a dashboard is not required to call this done) so a reliability finding can be immediately checked against whether it's model-specific before it's treated as an engine-level conclusion.
