## Overview

This document describes the disposable cloud VM used for the kind of testing the researcher's own 8GB MacBook Air cannot absorb safely: letting SynapseOS run generated shell commands against a real filesystem and a real desktop session, deliberately trying to break it, rather than only exercising it inside the sandboxed oracle (`prototype/internal/oracle`) that ground-truths the corpus. It also happens to be the first environment with a real TTY and a real GUI, which unblocks the manual tests in `prototype/manual-tests/` that have never been run (`open-problems.md` row 8). The box is provisioned and torn down often, snapshotted before a destructive round and rebuilt back to that snapshot after, rather than trusted to keep working indefinitely. The actual provisioning script lives outside this repo — in a separate, general-purpose ops repo, not duplicated here — per this project's own "one fact, one home" rule; this document is what stays reproducible without needing access to that repo. A second, headless box for the slow verification runs is set up and driven entirely from this repo (`tools/verify-box/`): eight shared cores run the gated suite, the composition draws and the round-7 rerun in minutes, for cents.

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

A second, headless, short-lived VM for the slow verification runs: the gated test suite, `make composecheck` on several seeds, and the round-7 recovery check. They start thousands of short sandboxes, are CPU-bound, and are already sharded across every core (`pilot/parallel_rc.py` uses `os.cpu_count()`). The development laptop has 2 cores (4 threads, a 2015 Core i5-5350U) and shares them with everything else open on it.

**What it is for.** Runs that need no credentials: the corpus, composition, and test verification. It never holds the Claude token, the Gemini login, a Hetzner API token, or any API key. Live agent-study runs stay on the researcher's machine.

**Size.** On 2026-10-20 a shared 8-vCPU, 16 GB server (Hetzner `cpx42`) ran the whole set below in about 2 minutes: the gated suite in 28 s, each 4,000-line composition draw in about 21 s, the round-7 recovery check in 7 s. Peak memory in use was about 1 GB, so 8 GB is ample. Cores set the speed; RAM past that does not. Shared vCPUs did not hold it back. Use Debian 13, to match the development machine, and an amd64 type: `setup.sh` installs the amd64 Go toolchain. Type names, stock and prices change, so check `hcloud server-type list` at creation time. Pay by the hour, and delete the server when the runs are done.

**Access.** The server has no baseline beyond this: it lives for hours, logs in only with an SSH key, and sits behind a Cloud Firewall that accepts SSH from the researcher's current public IP alone, attached at creation so it is never reachable more widely. A hardening baseline is deliberately not applied: its kernel and AppArmor settings would make the box differ from the development machine, and a verdict that differs between them should mean a bug, not a configuration. A home IP can change mid-session; when SSH times out, point the rule at the new address.

```sh
ip=$(curl -s -4 https://api.ipify.org)
printf '[{"direction":"in","protocol":"tcp","port":"22","source_ips":["%s/32"]}]' "$ip" > /tmp/fw.json
hcloud firewall create --name verify-ssh-only --rules-file /tmp/fw.json      # once; later: firewall replace-rules verify-ssh-only --rules-file /tmp/fw.json
hcloud server create --name verify --type cpx42 --location nbg1 --image debian-13 --ssh-key <your key name> --firewall verify-ssh-only
```

**Set-up.** `tools/verify-box/setup.sh` runs as root on the fresh server. It creates the ordinary user `verify` (the sandboxes, and the identity tests that look for the operator's name, must not run as root), installs every apt row of `distro/manifest.tsv` except the desktop and agent-CLI packages, unpacks the checksum-verified Go toolchain, and runs CI's sandbox check:

```sh
scp tools/verify-box/setup.sh root@<server-ip>: && ssh root@<server-ip> bash setup.sh <branch to verify>
```

It ends by printing `sandbox ok` and the commit it checked out. If the sandbox check fails, stop there: every verification below depends on bubblewrap and pasta working. The repository is public, so cloning needs no credentials.

If `~/.ssh/config` shares connections (`ControlMaster`) with a `ControlPath` that names only the host (`%h`), `ssh verify@<server-ip>` silently reuses an open `root@` connection and runs as root. Name the user and port in the path (`~/.ssh/cm-%r@%h:%p`), or pass `-S none`.

**Choosing seeds.** Write the fresh seeds down before running them. The seed that exposed a defect is development data for that defect's fix: it can show the fix applies, but not that the analysis is sound. Use seeds that played no part in finding it. `python3 -c 'import secrets; print(*[secrets.randbelow(9_000_000)+1_000_000 for _ in range(3)])'` draws three.

**Runs.** `tools/verify-box/run.sh` runs, one after another: `SYNAPSE_REQUIRE_SANDBOX=1 make ci` (no silent skips), `make composecheck` on the Makefile's recorded seed and on each fresh seed, `RC_NO_CURATED=1 python3 pilot/parallel_rc.py pilot/corpus7.jsonl pilot/out/recovery7-rerun.txt`, and `go run ./cmd/listpilot pilot/corpus7.jsonl > pilot/out/classifier_out7-rerun.jsonl`. It logs the commit, the machine, each step's exit status and time, and the peak memory to `~/verify.log`, and each step's output to `~/<step>.out`. Start it detached, so a dropped connection does not stop it:

```sh
scp tools/verify-box/run.sh verify@<server-ip>:
ssh verify@<server-ip> 'setsid nohup bash run.sh <seed> <seed> <seed> >/dev/null 2>&1 < /dev/null &'
ssh verify@<server-ip> cat verify.log       # until it says "all done"
```

An exit status other than 0 is read in its `.out` file before anything else is compared.

**Bringing results back.** The outputs land in the git-ignored `pilot/out/` on the server. Copy them, and the logs, to the development machine (`rsync -a verify@<server-ip>:thesis/prototype/pilot/out/ <somewhere>/` and `rsync -a 'verify@<server-ip>:*.out' 'verify@<server-ip>:verify.log' <somewhere>/`). Commit them under new, dated names beside the frozen files, each with a header naming the analyzer commit, and never over them: the committed round-7 result files are the paper's numbers. Compare the decision file with `pilot/classifier_out7.jsonl`, and report every changed decision. A verdict that differs between the laptop and the VM on the same commit is a bug to chase, not noise.

**Teardown.** Delete the server once the outputs are copied (`hcloud server delete verify`). Nothing on it is needed again: the next run starts from a fresh server. The firewall is free and can stay for the next one.

## What this unblocks

- `open-problems.md` row 8 — `m5-tui-mode.md` and `m6-session-context.md` can finally be run by a human, on a real TTY/GUI.
- Live adversarial testing of the safety gate and recoverability algorithm against a real desktop session, beyond what the sandboxed oracle's fixed corpus covers — the kind of open-ended, try-to-break-it testing that found `open-problems.md` row 21's pronoun-scope bug in the first place.
