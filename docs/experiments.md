# SynapseOS — Experiments

## Overview

This is the register of every experiment the thesis runs or promises: what each one measures, whether it is built, the exact command, whether it calls a language model, and what it needs to run. It exists so that "run everything, including every model size" means a known list rather than a guess. Each experiment's design stays in its own home document, linked from the table; this file owns only the register, the plan for the model-size comparison that Chapter 3 Section 2.1(d) promises and nothing yet plans, and the hardware for running it all in one burst. The short version: the CPU-only correctness experiments already run on a rented 8-core box in about two and a half minutes. The model comparison can be run in about a day on hourly rented hardware once four things are fixed first. The user study cannot be rented at all: it needs participants and ethics approval.

## Table of Contents

- [Overview](#overview)
- [How to read this](#how-to-read-this)
- [The register](#the-register)
  - [CPU-only correctness experiments](#cpu-only-correctness-experiments)
  - [Experiments that call a language model](#experiments-that-call-a-language-model)
  - [Experiments that need credentials, people, or something not yet built](#experiments-that-need-credentials-people-or-something-not-yet-built)
- [The model-size comparison](#the-model-size-comparison)
  - [Two questions, measured on different hardware](#two-questions-measured-on-different-hardware)
  - [What has to be fixed before the comparison is evidence](#what-has-to-be-fixed-before-the-comparison-is-evidence)
  - [Candidates and tiers](#candidates-and-tiers)
  - [Size of the run](#size-of-the-run)
- [Hardware for a one-day burst](#hardware-for-a-one-day-burst)
- [What one day can and cannot cover](#what-one-day-can-and-cannot-cover)

## How to read this

- **Status** is one of *run* (built and has committed results), *built* (runs, no committed result yet), *partial*, or *promised* (in the paper or `scope.md`, not built).
- **Model** says whether the experiment calls a language model. Only those need a model server, and only they change when the model does.
- **Runs on** names the cheapest place it can run: *box* is the headless verification box (`testing-environment.md`, "Fast verification box"), *model* is a machine with Ollama, *researcher* is the researcher's own machine (credentials stay there), *people* means human participants.
- Every result is written under a new dated name beside the frozen files, never over them, and says which data was development and which was held out.

## The register

### CPU-only correctness experiments

These never call a model. They run on the verification box in one pass of `tools/verify-box/run.sh`, which on 2026-10-20 took about two and a half minutes on a Hetzner `cpx42` with peak memory 1147 MB (`prototype/pilot/verify_2026-10-20.txt`).

| Experiment | Measures | Status | Command (from `prototype/`) | Home |
|---|---|---|---|---|
| Round-7 corpus evaluation, with the three ablations | Silent loss, capture coverage and needless confirmations for seven systems on 777 held-out commands (RQ4, RQ5) | run | `go run ./cmd/listpilot pilot/corpus7.jsonl > OUT.jsonl`, then `pilot/analyze.py` and `pilot/stats.py` | `recoverability-analysis.md` §10 |
| Recovery check | Execute in the sandbox, undo, compare the tree (RQ3) | run | `RC_NO_CURATED=1 python3 pilot/parallel_rc.py pilot/corpus7.jsonl OUT.txt` | `recoverability-analysis.md` |
| Composition differential test | 4,000 random compound lines executed, undone and compared per seed | run | `make composecheck COMPOSE_SEED=<seed>` | `recoverability-analysis.md` §12 |
| Gated test suite | Format, vet, every test with no silent skip | run | `SYNAPSE_REQUIRE_SANDBOX=1 make ci` | `CLAUDE.md` |
| External baselines | Codex CLI's check and cc-safety-net on the same 777 | run | the block in `external-baselines.md` | `external-baselines.md` |
| Snapshot cost benchmark | What snapshotting before every command would cost, against capture | run | `bash pilot/bench_snapshot.sh` | `algorithms.md` |
| Mutation testing | Whether the tests catch injected faults | run once | no script in the repo; how it was run is not recorded | `recoverability-analysis.md` §9 |

### Experiments that call a language model

All of these talk to Ollama, and all take the model as a parameter, so each can be repeated for every model size. They are what a model-size sweep actually consists of.

| Experiment | Measures | Status | How the model is chosen | Calls per run, per model | Home |
|---|---|---|---|---|---|
| Layer 7: proficiency-tiered requests | Whether plain, interface-level and technical phrasings of 12 tasks reach the intended end state. **D40 makes it the required gate for any model change** | run on 3B only | `SYNAPSE_MODEL`; repeats `SYNAPSE_LAYER7_REPEATS` (default 3) | 12 × 3 × 3 = 108 loop runs, each up to 5 steps; 480 s on 3B on 2026-09-14, machine not recorded | `../prototype/testing-plan.md` Layer 7 |
| Layer 3: live classifier agreement | How the classifier judges what the model proposes | run on 3B and 7B | `SYNAPSE_LIVE_MODELS=a,b,…` | 25 | `testing-plan.md` Layer 3 |
| Layer 4: typed operations against raw bash | Reliability of the two output paths | run on 3B and 7B | `SYNAPSE_LIVE_MODELS` | 10 | `testing-plan.md` Layer 4 |
| Layer 8: talk-or-act probe | Whether the model answers or acts, across 5 registers | run on 3B and 7B | `SYNAPSE_MODEL`; `SYNAPSE_PROBE_REPEATS` (default 3) | 69 | `testing-plan.md` Layer 8 |
| Model bench | 8 sample tasks and 8 human-style requests, executed in a playground | run on 1.5B and 3B | `manual-tests/model-bench.sh <model>` | 16, the loop ones up to 5 steps | `manual-tests/model-bench.sh` |
| Model-generated corpus partition | Commands the model writes for NL2Bash descriptions, labelled by the sandbox oracle | run once, 3B | `go run ./cmd/genpartition -model <tag> -n 350` | 350 | `algorithms.md` |

The Layer tests are behind the `live` build tag: `go test -tags live ./cmd/synapse/... -run <TestName> -v`, with `TestLayer7UtteranceTiers`, `TestLiveClassifierAgreement`, `TestLayer4TypedOperationReliability` and `TestLayer8ToolCallFork`.

### Experiments that need credentials, people, or something not yet built

| Experiment | Status | Why it cannot simply be rented | Home |
|---|---|---|---|
| Agent command study (Claude Code, Gemini CLI; DeepSeek and Kimi through an API driver) | partial: Claude driver run on a 20-run pilot; Gemini driver never run live; API driver not built; 68-task suite not frozen | Uses the researcher's own CLI credentials, which never go on a rented box | `agent-command-study.md` |
| Manual stories and milestone walkthroughs | run 2026-10-08 (stories); m5 and m6 never completed (open-problems row 8) | A person at a real terminal | `../prototype/manualtest/STORIES.md`, `../prototype/manual-tests/` |
| User study: 40 participants, SUS, NASA-TLX, interviews, with a 5-person pilot | promised; blocked on ethics approval (open-problems row 7) | Participants, IRB, three physical lab machines | `scope.md`, Chapter 3 |
| Replay and scoring of participants' commands | promised; the logger is built, the scorer is not | Needs the study's data | `scope.md` |
| Intent-parsing accuracy with a two-rater rubric | promised | Human raters | `scope.md`, Chapter 3 §2.3 |
| OSWorld shell subset | promised | Not built | `scope.md` |
| LoRA fine-tuning of the 3B model | promised as possible ("may be") | Not built; training would need a GPU, unlike everything else here | `scope.md` |

## The model-size comparison

Chapter 3 Section 2.1(d) (printed p. 78) promises that the model for each larger memory tier is chosen before the pilot study by "scoring candidates on the same natural-language requests by execution and timing them under everyday desktop load", on "a server with enough memory to run every model tier side by side, each in its own virtual machine sized as Table 3.2 describes and loaded with a web browser and everyday applications" (p. 54–55). No script, protocol or candidate list exists for it yet. This section is that protocol.

### Two questions, measured on different hardware

The comparison answers two separate questions, and they cannot share one machine.

1. **Accuracy.** Does a model reach the intended end state? This depends on the model's weights, not on the speed of the machine, so it can run on a rented GPU, where every size finishes within a day.
2. **Deployability.** Is the model fast enough on the machine a person owns, with a desktop and a browser open? The paper promises CPU-only inference with no GPU (Table 3.2), so this has to be timed on CPU virtual machines sized to each tier. A GPU timing says nothing about it.

One check is needed before the GPU accuracy numbers can stand in for CPU ones. The same weights at temperature 0 can still produce different text on GPU and CPU kernels. So the accuracy sweep is repeated on CPU for one tier's model, and the end states are compared. If they disagree, accuracy is measured on CPU too, and the sweep takes longer.

### What has to be fixed before the comparison is evidence

- **Layer 7 decides by scraping the transcript** (open-problems row 2, `said()`). D40 makes Layer 7 the gate for every model comparison, so until it reads typed events instead, a model can pass by what it prints rather than by what it does.
- **The Layer 7 tasks were developed against the 3B model.** They are development data for 3B and would favour it. A held-out set of requests, written and frozen before any candidate runs, is needed for a fair comparison. Layer 7 then runs on both sets, and each result is reported with the set it came from.
- **The model bench lets `~` reach the real home directory** (open-problems row 25). It must be fixed before the bench runs any model unattended.
- **Nothing generates "everyday desktop load".** The tier machines need a repeatable load (a browser with fixed pages and the desktop session) before timings mean what the paper says. This is a small script, but it does not exist.

### Candidates and tiers

Decided 2026-10-22 as proposed below; the decision and what was rejected are recorded in `decisions.md` D46.

| Decision | Decided | Reason |
|---|---|---|
| Which models | `qwen2.5-coder` at 0.5b, 1.5b, 3b, 7b, 14b and 32b; `qwen3` dense at 4b (which `drift.md` says the next reaudit must weigh) and its neighbours; `qwen3-coder:30b-a3b` as the mixture-of-experts candidate the paper names | One family across the whole size range, plus the two newer lines the paper and `drift.md` already commit to |
| A 70B-class model | Leave it out | At Ollama's default 4-bit quantization the smallest is about 43 GB of weights (`llama3.3:70b`), so no personal machine in the tiers could hold it. Testing it would answer a question the thesis does not ask |
| The memory tiers | 4 GB and 8 GB (Table 3.2), then 16 GB and 32 GB | Table 3.2 fixes only the two minimum tiers; the larger ones are not yet defined anywhere |
| Cores per tier VM | 2, 4, 8 and 16 cores, matching Hetzner's `cx23`, `cx33`, `cx43` and `cx53` | Core count changes CPU inference speed as much as memory does, so it has to be fixed per tier rather than taken from whatever the rented type offers |
| Repeats | Layer 7's default of 3 | Already the suite's setting; changing it changes every earlier result's comparability |

### Size of the run

The model files, from the Ollama library on 2026-10-22, set the minimum memory: `qwen2.5-coder` 0.5b 398 MB, 1.5b 986 MB, 3b 1.9 GB, 7b 4.7 GB, 14b 9.0 GB, 32b 20 GB; `qwen3-coder:30b-a3b` 19 GB. Real use is higher by the context cache and runtime overhead, so the 32B and 30B models need a 24 GB GPU at least, and only fit the 32 GB CPU tier.

The only measured speed is 3B on an unrecorded machine: Layer 7's 108 runs in 480 s. There is no measurement for any larger model, and none is guessed here. The first hour of the burst measures one Layer 7 pass per size, and the rest of the day is planned from that.

## Hardware for a one-day burst

Checked live on 2026-10-22. Prices and stock change: recheck before renting (`drift.md`).

| Role | Machine | Price | Why |
|---|---|---|---|
| CPU correctness experiments | Hetzner Cloud `cpx42` (8 shared vCPU, 16 GB) | €0.1314 an hour | Measured: the whole set in about 2.5 minutes |
| Accuracy sweep, every size up to 32B | One GPU rented by the second: RunPod A100 80 GB at $1.79 an hour (Secure Cloud), or an RTX 4090 24 GB at $0.89 | about $43 for a full 24 hours on the A100 | Holds every candidate. Hetzner Cloud has no GPU types, and Hetzner's dedicated GPU servers carry a one-time setup fee equal to about a month's rent (GEX45: €209 setup, €214 a month), which only pays off over weeks |
| Deployability timing | One Hetzner Cloud VM per tier: `cx23` (2 vCPU, 4 GB), `cx33` (4, 8 GB), `cx43` (8, 16 GB), `cx53` (16, 32 GB), each with the desktop and the load script | about €0.01 to €0.06 an hour each | CPU-only, sized to Table 3.2 as the paper promises; all four side by side cost under €3 a day, or about €7.50 with `cpx62` in place of `cx53`. `cx53` showed no stock in any location on 2026-10-22, so the 32 GB tier may need `cpx62` (16 shared vCPU, 32 GB, €0.2452 an hour) |

Sources: `hcloud server-type list` (willard-mba15 project), https://www.hetzner.com/dedicated-rootserver/matrix-gpu/, https://www.runpod.io/pricing, https://ollama.com/library/qwen2.5-coder/tags, https://ollama.com/library/qwen3-coder/tags, https://ollama.com/library/llama3.3/tags.

Not verified: GPU stock at the hour of renting, the RunPod prepaid-credit minimum for a new account, and whether the project's Hetzner core limit allows four tier VMs plus the verification box at once.

## What one day can and cannot cover

In one day, once the four fixes above are in: every CPU correctness experiment, the full accuracy sweep (Layers 3, 4, 7 and 8 and the model bench) for every candidate on one GPU, the GPU-to-CPU agreement check, and the deployability timings on the four tier VMs. The model-generated partition is regenerated only if the chosen study model is not the 3B.

Not in one day, whatever the hardware: the agent command study, which waits on the API driver and a frozen suite and runs on the researcher's machine; anything with participants or raters; OSWorld; and fine-tuning.
