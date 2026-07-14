## Overview

This is a disposable sandbox for manually exercising `synapse`'s file-search, organization, and text-processing behavior without pointing it at your real home directory. `seed.sh` regenerates a small synthetic file tree under `sandbox/` — a few backdated PDFs, some Desktop screenshots with no destination folder yet, a log file with mixed INFO/ERROR lines, and a tab-separated data file — covering the two sample-task categories that are actually filesystem-shaped (file search & organization; text & data processing). `sandbox/` is gitignored: it's mutated (moved, deleted, rewritten) by whatever you test against it, so it's meant to be thrown away and regenerated, not preserved.

## Table of Contents

- [Overview](#overview)
- [Regenerate](#regenerate)
- [Using it with `synapse`](#using-it-with-synapse)

## Regenerate

```sh
bash prototype/playground/seed.sh
```

Safe to rerun any time — it deletes and rebuilds `sandbox/` from scratch.

## Using it with `synapse`

**Always name the sandbox path explicitly in your prompt, and use an absolute path.** The model has no idea this playground exists — if you ask for "my Desktop" or "my home folder" without qualifying it, it'll propose a command against your *real* `~/Desktop`, not this fixture. A relative path (`playground/sandbox/...`) is also unreliable: in live testing the model sometimes prepended a leading `/`, turning it into an absolute path from filesystem root and pointing at nothing. Absolute paths worked every time. From the repo root, that's `$(pwd)/prototype/playground/sandbox/...`:

```sh
go run ./cmd/synapse "find all PDF files modified in the last 7 days in $(pwd)/prototype/playground/sandbox/home/user/Documents"
go run ./cmd/synapse "move every screenshot in $(pwd)/prototype/playground/sandbox/home/user/Desktop into a folder called Screenshots"
go run ./cmd/synapse "count how many lines in $(pwd)/prototype/playground/sandbox/home/user/Documents/access.log contain the word error"
```

After a run, `find prototype/playground/sandbox -type f` (or just look) shows what actually happened — compare it against what you asked for. Rerun `seed.sh` to reset before the next test.
