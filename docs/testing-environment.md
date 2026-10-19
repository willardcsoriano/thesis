## Overview

This document describes the disposable cloud VM used for the kind of testing the researcher's own 8GB MacBook Air cannot absorb safely: letting SynapseOS run generated shell commands against a real filesystem and a real desktop session, deliberately trying to break it, rather than only exercising it inside the sandboxed oracle (`prototype/internal/oracle`) that ground-truths the corpus. It also happens to be the first environment with a real TTY and a real GUI, which unblocks the manual tests in `prototype/manual-tests/` that have never been run (`open-problems.md` row 8). The box is provisioned and torn down often, snapshotted before a destructive round and rebuilt back to that snapshot after, rather than trusted to keep working indefinitely. The actual provisioning script lives outside this repo — in a separate, general-purpose ops repo, not duplicated here — per this project's own "one fact, one home" rule; this document is what stays reproducible without needing access to that repo.

## Table of Contents

- [Overview](#overview)
- [Why a separate machine](#why-a-separate-machine)
- [What's installed](#whats-installed)
- [How it's reached](#how-its-reached)
- [Resetting between destructive rounds](#resetting-between-destructive-rounds)
- [Fast verification box](#fast-verification-box)
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

## Fast verification box

A second, headless, short-lived VM for the slow verification runs. These are the round-7 recovery check, `make composecheck` on several seeds, and the full gated test suite. Each starts thousands of short sandboxes, so they are CPU-bound and already sharded across every core (`pilot/parallel_rc.py` uses `os.cpu_count()`). More cores shorten them almost linearly; RAM past about 16 GB does not. The development laptop has 4 cores.

**What it is for.** Runs that need no credentials: the corpus, composition, and test verification. It never holds the Claude token, the Gemini login, or any API key. Live agent-study runs stay on the researcher's machine.

**Size.** Use a dedicated-vCPU server type (shared vCPUs throttle under sustained load) with about 16 vCPU and 32 GB, running Debian 13 to match the development machine. Pick the type name in the provider's console or CLI at creation time. Names and prices change, so none is recorded here. Apply the same SSH-only baseline as the GUI VM (`debian-server-baselines`); its GUI script is not needed. Pay by the hour, and delete the server when the runs are done.

**Set-up.** On the VM, as an ordinary user:

```sh
sudo apt update && sudo apt install -y git make python3 curl
git clone https://github.com/willardcsoriano/thesis.git && cd thesis
git checkout <branch to verify>          # e.g. 105-effects-soundness
bash distro/check.sh                     # prints the one apt line for what is missing
sudo apt install -y <the packages check.sh printed>
bash distro/hoard.sh --toolchain         # Go tarball matching prototype/go.mod, checksum-verified
mkdir -p ~/.local && tar -C ~/.local -xzf distro/hoard/toolchain/go*.linux-amd64.tar.gz
bwrap --ro-bind / / --unshare-all --die-with-parent true && pasta --config-net --quiet -- true && echo sandbox ok
```

The repository is public, so cloning needs no credentials. The last line is the CI's own sandbox check. If it fails, stop there: every verification below depends on bubblewrap and pasta working.

**Runs.** From `prototype/`, record each run's commit and the machine (`git rev-parse HEAD; nproc; free -g`) with its output:

```sh
SYNAPSE_REQUIRE_SANDBOX=1 make ci                                   # gated suite: no silent skips
make composecheck                                                   # the recorded seed (COMPOSE_SEED in the Makefile)
make composecheck COMPOSE_SEED=<fresh seed>                         # repeat for each fresh seed
RC_NO_CURATED=1 python3 pilot/parallel_rc.py pilot/corpus7.jsonl pilot/out/recovery7-rerun.txt
~/.local/go/bin/go run ./cmd/listpilot pilot/corpus7.jsonl > pilot/out/classifier_out7-rerun.jsonl
```

**Choosing seeds.** Write the fresh seeds down before running them. The seed that exposed a defect is development data for that defect's fix: it can show the fix applies, but not that the analysis is sound. Use seeds that played no part in finding it.

**Bringing results back.** All outputs land in the git-ignored `pilot/out/`. Copy them to the development machine (`rsync -a <user>@<server-ip>:thesis/prototype/pilot/out/ prototype/pilot/out/`). Commit them under new, dated names beside the frozen files, each with a header naming the analyzer commit, and never over them: the committed round-7 result files are the paper's numbers. Compare the decision file with `pilot/classifier_out7.jsonl`, and report every changed decision. A verdict that differs between the laptop and the VM on the same commit is a bug to chase, not noise.

**Teardown.** Delete the server once the outputs are copied. Nothing on it is needed again: the next run starts from a fresh clone.

## What this unblocks

- `open-problems.md` row 8 — `m5-tui-mode.md` and `m6-session-context.md` can finally be run by a human, on a real TTY/GUI.
- Live adversarial testing of the safety gate and recoverability algorithm against a real desktop session, beyond what the sandboxed oracle's fixed corpus covers — the kind of open-ended, try-to-break-it testing that found `open-problems.md` row 21's pronoun-scope bug in the first place.
