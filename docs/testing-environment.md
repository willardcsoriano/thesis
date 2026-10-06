## Overview

This document describes the disposable cloud VM used for the kind of testing the researcher's own 8GB MacBook Air cannot absorb safely: letting SynapseOS run generated shell commands against a real filesystem and a real desktop session, deliberately trying to break it, rather than only exercising it inside the sandboxed oracle (`prototype/internal/oracle`) that ground-truths the corpus. It also happens to be the first environment with a real TTY and a real GUI, which unblocks the manual tests in `prototype/manual-tests/` that have never been run (`open-problems.md` row 8). The box is provisioned and torn down often, snapshotted before a destructive round and rebuilt back to that snapshot after, rather than trusted to keep working indefinitely. The actual provisioning script lives outside this repo — in a separate, general-purpose ops repo, not duplicated here — per this project's own "one fact, one home" rule; this document is what stays reproducible without needing access to that repo.

## Table of Contents

- [Overview](#overview)
- [Why a separate machine](#why-a-separate-machine)
- [What's installed](#whats-installed)
- [How it's reached](#how-its-reached)
- [Resetting between destructive rounds](#resetting-between-destructive-rounds)
- [What this unblocks](#what-this-unblocks)

## Why a separate machine

Three reasons, not one:

1. **Risk isolation.** Destructive testing means commands that are *supposed* to be tried against real data loss, including cases designed to find gaps in the recoverability algorithm. That shouldn't share a disk with the actual development environment, git history, or anything else that matters if a test goes further than intended.
2. **RAM.** The development machine is an 8GB 2015 MacBook Air — already tight for Ollama plus a browser plus everything else, before adding a second desktop session on top.
3. **A real TTY and a real GUI.** `prototype/manual-tests/m5-tui-mode.md` and `m6-session-context.md` need an actual terminal a human can type into; the TUI (`docs/interface-modes.md`) needs an actual display. Neither is exercised by the automated test suite or the sandboxed oracle, and until this VM existed, neither had ever actually been run.

## What's installed

Provisioned by `scripts/gui-test.sh` in the `debian-server-baselines` repo (external — see "one fact, one home" in `docs/README.md`; this list is kept current here so the setup stays reproducible without that repo):

- XFCE desktop, Firefox ESR, Chromium
- Go, resolved against the current stable release of the same line this project pins in `prototype/go.mod` (`go 1.25.0`) — kept in sync by re-reading that file, not duplicated as a version number here
- Ollama with `qwen2.5-coder:3b` pulled — the same model `prototype/setup.md` specifies for local inference
- `bubblewrap` and `build-essential` — the sandbox the effect-analysis resolvers run under (`prototype/internal/effects`, per the root `CLAUDE.md`), and what anything else under test needs to compile

Nothing on the VM clones this repo or holds a credential for it; that's done by hand, with the researcher's own git credentials, after logging in.

## How it's reached

The VM exposes nothing but SSH (22) to the internet — the same baseline UFW policy every box in `debian-server-baselines` uses. The GUI is VNC, bound to `127.0.0.1:5901` only, reached by tunneling:

```sh
ssh -N -L 5901:localhost:5901 <user>@<server-ip>
```

then pointing a VNC client at `localhost:5901`.

## Resetting between destructive rounds

Hetzner Cloud snapshot/rebuild, not a restore-from-backup dance:

```sh
# before a destructive round
hcloud server create-image <server> --type snapshot --description "pre-test $(date +%F)"

# after, to reset back to it
hcloud server rebuild --image <snapshot-id> <server>
```

Full procedure, including the Cloud Firewall that mirrors UFW, is in `debian-server-baselines/docs/HETZNER-CLOUD.md`.

## What this unblocks

- `open-problems.md` row 8 — `m5-tui-mode.md` and `m6-session-context.md` can finally be run by a human, on a real TTY/GUI.
- Live adversarial testing of the safety gate and recoverability algorithm against a real desktop session, beyond what the sandboxed oracle's fixed corpus covers — the kind of open-ended, try-to-break-it testing that found `open-problems.md` row 21's pronoun-scope bug in the first place.
