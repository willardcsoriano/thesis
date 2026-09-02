# SynapseOS — Safety Model: Classification, Confirmation, and Undo

## Overview

This is the reference for how SynapseOS decides whether a proposed command runs automatically or needs a "yes," and — separately — which of those commands can actually be undone afterward. These are two different questions answered by two different mechanisms: `internal/classifier` assigns a Reversible/Irreversible verdict (`decisions.md` D19, D22), and `internal/undo` decides, independently, whether there's a safety net once a command runs (D21, D23, D24, D25). Not every command that runs has an undo path, and — directly answering the question this doc exists to settle — no, not everything is technically undoable within this architecture, though as of D25 the undoable surface covers every classified-Irreversible shape that has a bounded, nameable target at all: a content copy for something that mutates existing data in place, a hardlink into trash for something that only removes a directory entry, a metadata record for a permission/ownership change, or (for git specifically) a captured commit SHA. What's left genuinely has no such target — a full disk overwrite, an unreviewed script's unknown effects, a killed process — not because building their undo was skipped. Read this when deciding whether a new classifier rule needs a matching undo mechanism, or when explaining to someone why "the model asked for confirmation" and "I can undo it" are not the same guarantee.

## Table of Contents

- [Overview](#overview)
- [The two independent questions](#the-two-independent-questions)
- [Taxonomy: every currently classified shape, and its undo path](#taxonomy-every-currently-classified-shape-and-its-undo-path)
- [So — is everything technically undoable?](#so-is-everything-technically-undoable)
- [Known gaps (flagged, not yet closed)](#known-gaps-flagged-not-yet-closed)
- [Cross-references](#cross-references)

## The two independent questions

1. **Is this command safe to run without asking?** — `classifier.Classify`/`ClassifyForDir`. Reversible commands run immediately; Irreversible commands block on an explicit confirmation.
2. **If it runs, can it be undone afterward?** — `internal/undo`. This applies *after* a command has already been allowed to run (either because it was Reversible, or because the user confirmed an Irreversible one) — it has nothing to do with whether the confirmation gate fired.

A command can be Irreversible (needs confirmation) and still have no undo path at all — irreversible **is the classifier's honest name for that risk**, not a promise that gets contradicted by the existence of `undo`. Five undo mechanisms exist, each narrow by design:

- **Directory-diff undo** (`undo.Snapshot`/`BuildEntry`/`Apply`) — for Reversible commands. Snapshots the working directory before and after, diffs what appeared vs. disappeared, and reconstructs moves/creations. Automatic, no confirmation needed to trigger recording (the command already ran without asking).
- **Content-backup undo** (`undo.BackupContent`/`Apply`, D23) — for confirmed Irreversible commands that overwrite a known file's *content* in place: `sed -i`, `awk -i inplace`, `truncate`, a truncating redirect, `tee` without `-a`, `cp` onto an existing destination (`classifier.CpOverwriteTarget`), and `dd`/`mkfs` when their destination is an existing regular file rather than a block device (`classifier.RawWriteOverwriteTarget`, D25). A full copy of the target file (content + mode) is taken immediately before execution — necessary here specifically because these commands write into the target's existing inode rather than replacing it (verified empirically for `cp`, Session 25: its destination's inode number is unchanged after an overwrite).
- **Trash undo** (`undo.TrashPreserve`/`Apply`, D24) — for confirmed Irreversible commands that only ever *remove a directory entry* without touching the target's underlying data: `rm` (`classifier.TrashTargets`) and `git clean -f` (`classifier.GitCleanDryRunCommand` runs a `-n` dry-run first to learn `git clean -f`'s own targets, since the command doesn't name them directly — D25). Because removing a directory entry never touches the data it pointed at, a hardlink taken beforehand keeps that data alive through the deletion at a cost independent of size — a gigabyte-sized tree costs the same to protect as an empty one, unlike a content backup. This is *not* interchangeable with content-backup: a command that mutates a target's content in place (like `cp`'s overwrite) would share the very data a hardlink is trying to protect, so trash only ever applies to pure removals.
- **Metadata-backup undo** (`undo.BackupMetadata`/`Apply`, D25) — for a confirmed recursive `chmod`/`chown` (`classifier.RecursivePermissionTargets`). Records each affected file's mode/uid/gid — not its content, and not a deletion either — the cheapest of the four per-file shapes, since it's a few bytes regardless of the file's own size.
- **Git-reset undo** (`undo.CaptureGitHead`/`Apply`, D25) — for a confirmed `git reset --hard` (`classifier.IsGitResetHard`). No file-level backup at all: the commit HEAD pointed at before the reset is captured as a single string, and undo is `git reset --hard <that sha>` — git's own object store already holds everything else needed.

## Taxonomy: every currently classified shape, and its undo path

| Shape | Verdict | Undo mechanism | Why |
|---|---|---|---|
| Everything not listed below (default) | Reversible | Directory-diff | No known destructive pattern matched |
| `sed -i` / `awk -i inplace` / `truncate` / truncating redirect (`>`) / `tee` w/o `-a` / `cp` onto an existing destination / `dd`/`mkfs` onto an existing regular file | Irreversible | **Content backup** (D23, D25) | Known, single (or enumerable) file target that gets mutated in place — cheap and precise to back up before running |
| `rm` / `git clean -f` | Irreversible | **Trash** (D24, D25) | Removing a directory entry never touches the target's data — a hardlink is nearly free regardless of size, and the real command still runs afterward unmodified |
| `chmod -R` / `chown -R` | Irreversible | **Metadata backup** (D25) | Neither content nor a deletion — just mode/ownership, a few bytes per file to record and restore regardless of tree size |
| `git reset --hard` | Irreversible | **Git-reset undo** (D25) | Committed work is recoverable via the current `HEAD` commit SHA, captured before the reset — cheaper than any file-level backup since it's a single string, not a copy |
| `dd` / `mkfs` onto a raw block device | Irreversible | None | Not a filesystem-level file or directory entry at all; a meaningful backup would mean imaging the whole disk or partition first — not proportionate |
| `shred` | Irreversible | None, deliberately | The tool's entire purpose is making content unrecoverable; trashing or backing it up first would defeat the reason someone chose `shred` over `rm` |
| `eval` | Irreversible | None | The string being evaluated is dynamically constructed — there is no fixed target to identify before it runs |
| `pkill` / `fuser -k` / dynamic kill target (`xargs`-piped, command-substitution) | Irreversible | None | Process state, not filesystem state — there is no file to back up, and "undo" would mean relaunching a killed process from unknown prior state |
| Fetch-exec (`curl`/`wget` piped or chained into a shell) / decode-exec (`base64 -d` into a shell) | Irreversible | None | The risk is "arbitrary unreviewed code executed," not "this file changed" — there is no single target, the effects are unbounded and unknown in advance |

## So — is everything technically undoable?

No, but as of D25 the undoable surface covers every Irreversible shape that has a *bounded, nameable target of some kind* — content, a directory entry, or metadata. Four mechanisms now divide that space between them, each matched to what the command actually changes rather than treated as one generic "backup" concept. What's left fails for reasons no such mechanism can address, because there's no target to protect at all:

- **No fixed target** (`eval`, fetch-exec, decode-exec) — nothing to preserve in advance, because what gets affected isn't known until the unreviewed code actually runs.
- **Wrong kind of target entirely** (`dd`/`mkfs` writing raw block/device data, not a filesystem-level file or directory entry).
- **Wrong kind of state** (`pkill`, `fuser -k`) — process state isn't file content or a directory entry; there's nothing in the filesystem to preserve at all.
- **Deliberately unrecoverable by design** (`shred`) — even where preserving the data first is *technically* possible, doing so would contradict the command's own stated purpose.

A full filesystem journal (every byte of every write, forever) could in principle make even these recoverable — that's a fundamentally different, much heavier architecture (closer to a copy-on-write filesystem or transactional journal than a targeted safety net), and out of scope here by design, matching this package's stated trade of a narrow, auditable mechanism over a general one.

## Known gaps (flagged, not yet closed)

None currently open — D25 closed every gap this doc had previously flagged (`git clean -f`, `git reset --hard`, recursive `chmod`/`chown`, and `dd`/`mkfs` onto a regular file). The remaining unaddressed Irreversible shapes (`dd`/`mkfs` onto a block device, `shred`, `eval`, process-kill, fetch/decode-exec) are documented above as genuinely out of scope, not deferred.

## Cross-references

- `decisions.md` D19 (classifier baseline), D21 (execution model), D22 (classifier scope widened), D23 (content-backup undo), D24 (trash undo), D25 (git-reset/git-clean/metadata/dd-mkfs undo) — the "why" behind each mechanism this doc catalogs.
- `prototype/internal/classifier/classifier.go` — `Classify`, `ClassifyForDir`, `ContentMutationTargets`, `CpOverwriteTarget`, `TrashTargets`, `RawWriteOverwriteTarget`, `RecursivePermissionTargets`, `IsGitResetHard`, `GitCleanDryRunCommand`; each rule's own comment explains its specific reasoning.
- `prototype/internal/undo/undo.go` — package doc comment covers the same mechanism split described here, from the code's perspective.
