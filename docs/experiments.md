# SynapseOS — Experiments

## Overview

This is the register of every experiment the thesis runs or promises: what each one measures, whether it is built, the exact command, whether it calls a language model, and what it needs to run. It exists so that "run everything, including every model size" means a known list rather than a guess. Each experiment's design stays in its own home document, linked from the table; this file owns only the register, the plan for the model-size comparison that Chapter 3 Section 2.1(d) promises and nothing yet plans, and the hardware for running it all in one burst. The short version: the CPU-only correctness experiments already run on a rented 8-core box in about two and a half minutes. The model comparison runs on hourly rented CPU servers: the harness aids have been measured on development and held-out data for 1.5B, 3B and 7B (runs A1–A5), and three of the four hardware tiers have been timed, with every result kept under `prototype/pilot/live/`. The user study cannot be rented at all: it needs participants and ethics approval.

## Table of Contents

- [Overview](#overview)
- [How to read this](#how-to-read-this)
- [The register](#the-register)
  - [CPU-only correctness experiments](#cpu-only-correctness-experiments)
  - [Experiments that call a language model](#experiments-that-call-a-language-model)
  - [Experiments that need credentials, people, or something not yet built](#experiments-that-need-credentials-people-or-something-not-yet-built)
- [The model-size comparison](#the-model-size-comparison)
  - [Two questions, measured on different hardware](#two-questions-measured-on-different-hardware)
  - [What makes the comparison evidence](#what-makes-the-comparison-evidence)
  - [Everyday desktop load](#everyday-desktop-load)
  - [Candidates and tiers](#candidates-and-tiers)
  - [Size of the run](#size-of-the-run)
  - [Feasibility run](#feasibility-run)
  - [Harnessing the 1.5B model (2026-10-25)](#harnessing-the-15b-model-2026-10-25)
  - [Measuring the aids (runs A1–A5, 2026-10-26 and 27)](#measuring-the-aids-runs-a1a5-2026-10-26-and-27)
  - [Tier timing (2026-10-27)](#tier-timing-2026-10-27)
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
| Layer 7: proficiency-tiered requests | Whether plain, interface-level and technical phrasings of 12 tasks reach the intended end state. **D40 makes it the required gate for any model change** | run on 1.5B, 3B and 7B, development and held-out, with and without aids (A1–A5, below) | `SYNAPSE_MODEL`; repeats `SYNAPSE_LAYER7_REPEATS` (default 3) | 12 × 3 × 3 = 108 loop runs, each up to 5 steps; 480 s on 3B on 2026-09-14, machine not recorded | `../prototype/testing-plan.md` Layer 7 |
| Layer 3: live classifier agreement | How the classifier judges what the model proposes | run on 3B and 7B | `SYNAPSE_LIVE_MODELS=a,b,…` | 25 | `testing-plan.md` Layer 3 |
| Layer 4: typed operations against raw bash | Reliability of the two output paths | run on 3B and 7B | `SYNAPSE_LIVE_MODELS` | 10 | `testing-plan.md` Layer 4 |
| Layer 8: talk-or-act probe | Whether the model answers or acts, across 5 registers | run on 3B and 7B | `SYNAPSE_MODEL`; `SYNAPSE_PROBE_REPEATS` (default 3) | 69 | `testing-plan.md` Layer 8 |
| Layer 9: multi-turn conversations | Whether follow-ups, corrections, undo and changes of topic reach the intended state, turn by turn, in one session | run on 1.5B, 3B and 7B, development and held-out (A1–A5, below) | `SYNAPSE_MODEL`; `SYNAPSE_LAYER9_SET`; `SYNAPSE_LAYER9_REPEATS` (default 3) | 17 development turns and 8 held-out turns, x 3 repeats | `testing-plan.md` Layer 9 |
| Model bench | 8 sample tasks and 8 human-style requests, the human-style ones executed under bubblewrap with the playground as home and the real home hidden | run on 1.5B and 3B | `manual-tests/model-bench.sh <model>` | 16, the loop ones up to 5 steps | `manual-tests/model-bench.sh` |
| Model-generated corpus partition | Commands the model writes for NL2Bash descriptions, labelled by the sandbox oracle | run once, 3B | `go run ./cmd/genpartition -model <tag> -n 350` | 350 | `algorithms.md` |

The Layer tests are behind the `live` build tag: `go test -tags live ./cmd/synapse/... -run <TestName> -v`, with `TestLayer7UtteranceTiers`, `TestLiveClassifierAgreement`, `TestLayer4TypedOperationReliability`, `TestLayer8ToolCallFork` and `TestLayer9Conversations`.

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

1. **Accuracy.** Does a model reach the intended end state? This depends on the model's weights, not on the speed of the machine. It was planned for a rented GPU; it runs on rented CPU servers instead (D53), where A5 below (both sets, 1.5B, 3B and 7B, with and without aids) took 75 minutes on 12 cores, and the numbers are the ones a CPU-only device would get.
2. **Deployability.** Is the model fast enough on the machine a person owns, with a desktop and a browser open? The paper promises CPU-only inference with no GPU (Table 3.2), so this has to be timed on CPU virtual machines sized to each tier. A GPU timing says nothing about it.

Measuring accuracy on CPU also removes the check a GPU sweep would have needed: the same weights at temperature 0 can produce different text on GPU and CPU kernels, so GPU accuracy would first have had to be shown to agree with CPU accuracy.

### What makes the comparison evidence

Four things had to be fixed before a comparison could count; all four were done on 2026-10-23.

- **Layer 7 scores what the person was told, from typed events**, not a scraped transcript, so a model cannot pass on the loop's own bookkeeping (`prototype/testing-plan.md`, Layer 7). Its verifiers are checked without a model: each rejects its untouched fixture and accepts a reference solution.
- **Layer 7 has a held-out set.** The development tasks (U1–U12) were tuned against the 3B model and would favour it; H1–H12 were written before any model ran on them and are frozen by hash. Every result names its set (`SYNAPSE_LAYER7_SET`). On 2026-10-25 the checks of U1 and U8 stopped requiring a folder named `PDFs/` or `logs/`, since their plain wordings ("in one place", "their own folder") name none: the 2026-10-23 feasibility run had scored qwen2.5-coder:14b a miss for gathering the PDFs into `pdfs/`. Any one folder holding both files passes at the plain tier (`plainAlso`); since 2026-10-27 the two tiers that name the folder require that name again, as D52 has a named reading kept (`TestLayer7NamedFolderTiers`). Between 2026-10-25 and 2026-10-27 those tiers accepted any folder, so the rounds run then scored them leniently. That run's U1 counts (its sample had no U8) are development data scored under the old check. The held-out checks were read for the same flaw (a check needing a name its plain wording never gives) and have none.
- **The model bench runs model commands in a sandbox** whose home is the playground, so `~` and absolute paths cannot reach the real home (`prototype/manual-tests/bench-sandbox.sh`).
- **The desktop load exists** (below).

### Everyday desktop load

The paper times each model "under everyday desktop load" on tier VMs "loaded with a web browser and everyday applications" (p. 54–55, 78). `tools/desktop-load/` generates that load, the same on every run and with no network:

- **Browser.** Chromium, as `distro/manifest.tsv` installs it, with three local pages from `tools/desktop-load/pages/` open as tabs: a dashboard redrawing a canvas chart 30 times a second and updating a 60-row table every second; a long article (300 sections built at load) that scrolls a little every 50 ms while its contents sidebar follows; and a webmail inbox of 1,500 messages that gains one and drops the oldest every 2 s and re-filters the whole list every 5 s. Every page's content comes from a fixed-seed generator and every structure is bounded, so the work is the same each run and the memory does not drift over a day. Chromium runs on a fresh profile with its own background networking off, and with timer throttling off, so a background tab or a window covered by the SynapseOS terminal does the same work as one in front.
- **Editor.** Mousepad holding `pages/notes.txt`, with default settings every run.
- **Settle.** `start.sh` returns only when both apps are alive and every page has run its load handler (each logs a line Chromium writes to the load's log), then waits 20 s more (`DESKTOP_LOAD_SETTLE`). A browser's first seconds (starting renderers, compiling scripts, laying out the article, the first garbage collections) cost CPU an open desktop no longer spends; the wait keeps that burst out of the first timing. It is a fixed wait, not "until quiet", because the pages never go quiet by design. If the pages have not loaded within 120 s (`DESKTOP_LOAD_TIMEOUT`), start fails and stops what it started.

Run it inside the tier VM's desktop session:

```sh
tools/desktop-load/start.sh     # up and settled; prints the status line; exit 3 if a load is already recorded
tools/desktop-load/status.sh    # desktop-load state=up apps=2/2 processes=15 rss_kb=1387052 pss_kb=498818
tools/desktop-load/stop.sh      # stops exactly what start.sh started, also after one app has crashed
```

The timing harness logs `status.sh`'s line next to each timing. `tools/tier-box/run.sh MODEL...` is that harness for one tier: on a server the size of the tier, CPU only, it runs `tools/model-box/run.sh` for the models twice, idle and then under the load in a headless X display (`xvfb-run`), and keeps both results with `status.sh`'s line from before and after the timed run. Through `rent.sh` the tiers map to Hetzner's shared types with the same memory: 2 cores and 4 GB `cpx22`, 4 and 8 GB `cpx32`, 8 and 16 GB `cpx42`, 16 and 32 GB `cpx62` (`RENT_TYPE=cpx32 RENT_FETCH=tier-box bash tools/rent/rent.sh -- bash tools/tier-box/run.sh qwen2.5-coder:3b`). A rented vCPU is not a laptop core (often half a physical core, shared with other tenants), so these timings compare tiers and models with each other; a claim about a particular device needs a run on one. `rss_kb` counts shared pages once per process and overstates a multi-process browser; `pss_kb` is the load's real share of memory. The figures above are from one run under Xvfb on the development machine, not from a tier VM. `stop.sh` never matches by name: it stops the PIDs `start.sh` recorded (checked against their start time), their process groups, and the processes carrying the run's random token in their environment, which catches Chromium's crash handler after it leaves the group. `bash tools/desktop-load/test.sh` checks all of this against a real Chromium and Mousepad on Xvfb, with a decoy `sleep` and a decoy Mousepad that must survive every stop.

### Candidates and tiers

Decided 2026-10-22 as proposed below; the decision and what was rejected are recorded in `decisions.md` D46.

| Decision | Decided | Reason |
|---|---|---|
| Which models | `qwen2.5-coder` at 0.5b, 1.5b, 3b, 7b, 14b and 32b; `qwen3` dense at 4b (which `drift.md` says the next reaudit must weigh) and its neighbours; `qwen3-coder:30b` (30B total, 3B active; tag checked against the Ollama registry 2026-10-23) as the mixture-of-experts candidate the paper names | One family across the whole size range, plus the two newer lines the paper and `drift.md` already commit to |
| A 70B-class model | Leave it out | At Ollama's default 4-bit quantization the smallest is about 43 GB of weights (`llama3.3:70b`), so no personal machine in the tiers could hold it. Testing it would answer a question the thesis does not ask |
| The memory tiers | 4 GB and 8 GB (Table 3.2), then 16 GB and 32 GB | Table 3.2 fixes only the two minimum tiers; the larger ones are not yet defined anywhere |
| Cores per tier VM | 2, 4, 8 and 16 cores, on Hetzner's `cpx22`, `cpx32`, `cpx42` and `cpx62` (D53; first planned as `cx23` to `cx53`) | Core count changes CPU inference speed as much as memory does, so it has to be fixed per tier rather than taken from whatever the rented type offers |
| Repeats | Layer 7's default of 3 | Already the suite's setting; changing it changes every earlier result's comparability |

### Size of the run

The model files, from the Ollama registry on 2026-10-23, set the minimum memory: `qwen2.5-coder` 0.5b 0.40 GB, 1.5b 0.99 GB, 3b 1.93 GB, 7b 4.68 GB, 14b 8.99 GB, 32b 19.85 GB; `qwen3` 1.7b 1.36 GB, 4b 2.50 GB, 8b 5.23 GB; `qwen3-coder:30b` (the mixture-of-experts model, 3B active) 18.56 GB. About 64.5 GB in all. Real use is higher by the context cache and runtime overhead, so the 32B and 30B models need a 24 GB GPU at least, and only fit the 32 GB CPU tier.

The full comparison is 216 Layer 7 runs per model (24 tasks, 3 tiers, 3 repeats), each cut at 3 minutes. Measured speeds up to 14B are under "Tier timing" below.

### Feasibility run

`tools/model-box/run.sh` answers whether a given machine can run the whole comparison in about a day, before renting for a day. For each candidate it pulls the model, measures Ollama's own prompt and generation rates on a fixed request with synapse's options, times a sample of Layer 7 development tasks (U1, U4, U7, U10, one repeat: 12 runs), and extrapolates to the 216 runs. The held-out set is never run here, so its results stay unseen until the comparison. A model that cannot fit in the machine's free memory is recorded as such, not run. Its preflight also reports CPUs, memory, any GPU, and whether bubblewrap works, which decides whether a machine can run the sandboxed parts (it may not inside a container, as on RunPod).

It runs on any Debian or Ubuntu machine with the repository: on Hetzner after `tools/verify-box/setup.sh`, the same way as the verification box. It uses a running Ollama at `OLLAMA_URL`, or fetches Ollama's official archive (`distro/hoard.sh --ollama`) and serves it for the run. Results go to `~/model-box/results.tsv`, one row per model with the estimate in hours; a row marked LOWER BOUND had runs cut at the 3-minute cap. Rerunning resumes.

```sh
scp tools/verify-box/setup.sh root@<server-ip>: && ssh root@<server-ip> bash setup.sh main
ssh verify@<server-ip> 'cd thesis && setsid nohup bash tools/model-box/run.sh >/dev/null 2>&1 < /dev/null &'
ssh verify@<server-ip> cat model-box/results.tsv      # one row per model as each finishes
```

Pass model tags after `run.sh` to time only those (for example `qwen2.5-coder:32b qwen3-coder:30b`). It runs detached, like the verification box, so a dropped connection does not stop it.

### Harnessing the 1.5B model (2026-10-25)

How far the loop, not the model, can carry `qwen2.5-coder:1.5b` (Q4_K_M). All development data, on the researcher's laptop shared with other work, so the times are not timings. The held-out sets of Layers 7 and 9 were not run.

| Run | Plain | Interface | Technical | Clean exits |
|---|---|---|---|---|
| Layer 7 baseline, 108 runs | 21/36 | 25/36 | 36/36 | 2/108; 79 hit the step limit |
| With the stop on a repeated success (`repeated_success`) | 21/36 | 27/36 | 33/36 | 0/108 said done; 88 stopped on a repeat |
| Layer 9, with the stop | 28 of 51 turns, 7 of 24 whole conversations | | | |

- **1.5B cannot judge that a task is finished.** After `wc -l log.txt` printed `4 log.txt`, it proposed the same command again, with or without the JSON format; 3B answered done. Asking whether a step was done before asking for the next command changed nothing over 69 runs, and was reverted. A separate yes or no question ("have these steps accomplished the task?") was answered yes for 3 of 4 unfinished cases by 1.5B and right for all 8 by 3B (cases written for the check, so indicative only).
- **The baseline's 36/36 technical was partly luck.** For "mv report.txt summary.txt" it renamed the file and back again until the step limit, landing on the right name at step 5. With the stop it ends after the first rename back, and fails.
- **The stop is how 1.5B finishes at all**: a run ends one short request after the work, instead of at the step limit. It never runs the repeat, so a side effect cannot happen twice.
- **Other misses, by cause:** the model looped inside a reply until Ollama aborted it ("prediction aborted, token repeat limit reached", 4 of 108 runs, reported as `propose_error`); `UNSUPPORTED` for doable requests ("give report.txt a different name"); a wrong command concept (`ls -lh | sort -rh` choosing the `total` line, `awk` without `-F,` on a CSV, `WARNING` for `WARN`); and, once, `rm -rf ./*` for "get rid of the temporary files here", which the gate held for approval and Layer 7 auto-approves.
- **In conversations** the misses are references: "no, I meant the other text file" and "put the two text files in it". Undo worked both ways, by the REPL's `undo` and by "put it back where it was".

**Reading:** the commands are mostly right; the judgement around them (is it finished, which file is meant) is what 1.5B lacks, and a prompt does not supply it. Rules in the loop can stand in for some of it, as the stop does.

**Second round, on a rented box (2026-10-25).** The loop gained three brakes and two aids (`cmd/synapse/brakes.go`, `prompts.go`): a step that only moves back what the step before it moved is not run (`reversed_step`); a reply naming a number or file that no command printed, nor the analysis predicted, is withheld and the person told why; after a read-only step that found nothing, the person is told which search it was; the step prompt lists the working folder's names; and a reply Ollama aborted for looping is retried once with `repeat_penalty` 1.2. The round ran with `tools/layer-box/run.sh` on a Hetzner cpx52 (12 cores, 4 shards): both models, both layers, in about 7 minutes. Development data.

| Model | Layer 7 plain | interface | technical | Layer 9 turns | whole conversations |
|---|---|---|---|---|---|
| 1.5B, first round (laptop) | 21/36 | 25/36 | 36/36 | 28/51 | 7/24 |
| 1.5B, this round | 24/36 | 26/36 | 33/36 | 27/51 | 6/24 |
| 3B, first round (laptop) | 24/36 | 33/36 | 33/36 | 42/51 | 18/24 |
| 3B, this round | 24/36 | 33/36 | 36/36 | 46/51 | 20/24 |

1.5B now finishes every run by a brake rather than the step limit (90 stops on a repeat, 3 on a reversal, none at the limit) but is no more accurate: its gains and losses are within a few runs, and its conversations stay at a quarter right. 3B said done itself in 94 of 108 runs, so the brakes rarely touch it, and it gained 4 conversation turns. **The rounds differ in more than the brakes**, so the change cannot be credited to them alone: the box ran Ollama 0.40.11 against the laptop's 0.34.1 (`distro/hoard.sh --ollama` fetches the newest release, unpinned), and from this round the live layers run with the analysis on (`testing-plan.md`, Layer 7). An ablation on one box, with and without the brakes, is what would separate them.

**Running a round.** Live Layer 7 and 9 runs go on a rented box, not the development laptop (4 shared threads: the same round took about 1.5 hours there). One command rents, runs, copies the results back and deletes the box (`testing-environment.md`, "Renting automatically"):

```sh
bash tools/rent/rent.sh -- bash tools/layer-box/run.sh qwen2.5-coder:1.5b qwen2.5-coder:3b
```

`tools/layer-box/run.sh` splits each model's development tasks and conversations across `LAYER_BOX_SHARDS` (default 4) copies of one test binary against one Ollama serving that many requests at once, and `merge.py` sums the shards as one run. The held-out sets run only with `LAYER_BOX_SET=heldout`, and the second held-out sets only with `LAYER_BOX_SET=heldout2`. `LAYER_BOX_AIDS` (for example `"none peek,tools,refs all"`) runs each model once per `SYNAPSE_AIDS` setting on the same box, and `tools/layer-box/compare_aids.py RESULTS_DIR` prints each setting's score beside `none` and, per task and turn, which ones it made better and which worse: equal totals can hide an aid that fixed one task and broke another.

**Third round (2026-10-25, the first through `rent.sh`).** The reply check now also accepts step numbers and exit codes, which the reply is written from: on the second round most withheld replies named "0" (an exit code) or "1" and "2" (step numbers). Same box type, Ollama 0.40.11 from the pin, model digests recorded. 1.5B: Layer 7 24/28/33 of 36, Layer 9 28 of 51 turns and 7 of 24 conversations, 3 replies withheld in conversations. 3B: Layer 7 24/33/35, Layer 9 44 of 51 turns and 19 of 24 conversations, none withheld. Against the second round, on the same Ollama, the differences are a few runs either way. Two causes are mixed in them and not yet separated: a withheld reply is not part of what is scored, so the corrected check changes what some tasks are scored on, and runs at temperature 0 still differed between the rounds, likely from serving four requests at once (not checked).

### Measuring the aids (runs A1–A5, 2026-10-26 and 27)

Layer 7's U1 and U4 accept a second fair reading of the plain phrasing, and the harness aids (`SYNAPSE_AIDS`) are recorded in D52. Each run below compared settings on one rented `cpx52` (12 shared cores), Ollama 0.40.11 from the pin, model digests in each `preflight.txt`, three repeats per task. The runs are named A1–A5 so they cannot be confused with the analyser's corpus rounds 1–7 (`pilot/results_round*.txt`), which are a different experiment. What `all` switches on grew between runs, so a column compares settings within its run, not across runs.

| Run | Code | Data | Kept in `prototype/pilot/live/` |
|---|---|---|---|
| A1 | first aids (`peek`, `tools`, `refs`), each alone | development, 3B only | lost with its box; the 3B table is in D52 |
| A2 | second set (`lint`, `literal`, the extended `refs` and `tools`) | development | `2026-10-26-aids-A2-dev` |
| A3 | third pair (`tools` re-ask, `refs` last reply) | development | `2026-10-26-aids-A3-dev`; 1.5B's Layer 9 with aids did not finish |
| A4 | the code as first shipped for review | development and held-out | `2026-10-27-aids-A4-dev`, `-A4-heldout` |
| A5 | the code after review, as merged (main `f5301c3`) | development and held-out | `2026-10-27-aids-A5-dev`, `-A5-heldout` |

**A5 is the record**; A4 differs from it only by the review fixes and agrees with it within a few runs. Runs passed, `none` → `all`:

| Model | Layer 7 development (of 108) | Layer 9 development (of 51 turns) | Layer 7 held-out (of 108) | Layer 9 held-out (of 24 turns) |
|---|---|---|---|---|
| 1.5B | 86 → 97 | 28 → 38 | 69 → 72 | 18 → 17 |
| 3B | 95 → 105 | 44 → 47 | 93 → 92 | 15 → 19 |
| 7B | 96 → 102 | 51 → 51 | 78 → 85 | 21 → 21 |

A4 for comparison: 3B 95 → 105 and 44 → 47 on development, 92 → 93 and 16 → 16 held out; 7B 75 → 85 held out on Layer 7.

**Reading.** On the development sets the aids lift every size, and 3B with them passes 105 of 108 Layer 7 runs. On the held-out sets they do not carry over for 3B on Layer 7 (93 → 92, with one task, H5 plain, moving by one run), while 7B gains 7 there. 3B's held-out conversations went 15 → 19 in A5 but 16 → 16 in A4: at about 2 runs in 108 of serving noise and whole tasks flipping at temperature 0, that is not yet evidence either way. The aids were designed on the development misses, so the held-out numbers are the ones that count, and they say the aids fix the mistakes they were built for rather than 3B's mistakes in general.

**What 3B still misses with every aid on (A5):**

- Development: U1 plain ("put all my pdfs together in one place", read as a merge), 0 of 3: it gives up even when told `pdfunite` is installed and what it does; 7B does the same. C8 turns 2 and 3, 1 of 3 each: one of "the two text files" is moved, the other left.
- Held out, Layer 7: H2 plain and interface, H4 plain and interface, H8 plain, all 0 of 3 and all completed with a wrong result; H5 plain 2 of 3. 7B misses H2, H4 and H5 as well, so most of these are hard for both sizes.
- Held out, Layer 9: K3 turn 2 (2 of 3), K4 turns 1 and 2 (1 of 3 each).

These held-out misses have now been read, so the held-out set is spent for tuning: any fix made from them is measured on the fresh set (`prototype/testing-plan.md`, the second held-out sets N1–N12 and Q1–Q4, frozen by `pilot/layer7_heldout2.sha256` and `pilot/layer9_heldout2.sha256`).

`python3 tools/layer-box/compare_aids.py prototype/pilot/live/<run>` prints each setting beside `none` and the tasks each one made better or worse. `tools/rent/keep.py` copies a result from the git-ignored `tools/rent/out/` to `prototype/pilot/live/`, without the box's address, and refuses to write over a kept result.

### Tier timing (2026-10-27)

`tools/tier-box/run.sh` on one rented server per tier, CPU only (AMD EPYC, shared cores), Ollama 0.40.11, 12 Layer 7 development runs per model, once idle and once under the desktop load in a headless X display. Seconds per run (the whole loop: prompt, propose, run, reply), idle / under load:

| Model | 2 cores, 4 GB (`cpx22`) | 4 cores, 8 GB (`cpx32`) | 8 cores, 16 GB (`cpx42`) |
|---|---|---|---|
| 0.5B | 4.2 / 4.1 | | |
| 1.5B | 7.2 / 6.5 | 3.5 / 3.6 | |
| 3B | 10.9 / 12.0 | 5.6 / 6.5 | 3.6 / 4.6 |
| 7B | | 10.8 / 11.5 | 6.6 / 7.4 |
| 14B | | | 11.7 / 13.8 |

Generation rate in tokens a second, idle / under load: 3B 17.1 / 16.5 on 2 cores, 33.7 / 31.6 on 4, 49.1 / 34.3 on 8; 7B 15.6 / 13.1 on 4 and 20.8 / 16.8 on 8; 14B 10.5 / 8.5 on 8. Kept in `prototype/pilot/live/2026-10-27-tier-*`, each with `results.tsv`, `preflight.txt` and the load's status line from before and after.

- **Each cell is one run.** The 2-core tier ran twice idle: 3B took 10.3 s and 10.9 s a run, so differences under about 10% are not established, and where the load cell is faster than idle (0.5B and 1.5B on 2 cores) that is noise. Repeats are planned.
- **3B runs on every tier**, at about 6 s a request on the 4-core minimum with Chromium and an editor open, and about 12 s on 2 cores. 7B takes about 11 s on 4 cores.
- **The first 2-core run** (`tier-cpx22-1`) has no load half: `xvfb-run` needs `xauth`, which the box lacked; `tier-box/run.sh` now installs it.
- The 16-core tier (`cpx62`) was not run: the project's shared-core limit (about 20, with the development server holding 8) left 12. A raise was approved on 2026-10-28.

## Hardware for a one-day burst

Checked live on 2026-10-22. Prices and stock change: recheck before renting (`drift.md`).

| Role | Machine | Price | Why |
|---|---|---|---|
| CPU correctness experiments | Hetzner Cloud `cpx42` (8 shared vCPU, 16 GB) | €0.1314 an hour | Measured: the whole set in about 2.5 minutes |
| Accuracy sweep, every size up to 32B (not used: accuracy runs on CPU, D53) | One GPU rented by the second: RunPod A100 80 GB at $1.79 an hour (Secure Cloud), or an RTX 4090 24 GB at $0.89 | about $43 for a full 24 hours on the A100 | Holds every candidate. Hetzner Cloud has no GPU types, and Hetzner's dedicated GPU servers carry a one-time setup fee equal to about a month's rent (GEX45: €209 setup, €214 a month), which only pays off over weeks |
| Deployability timing | One Hetzner Cloud VM per tier, as used since 2026-10-27 (D53): `cpx22` (2 vCPU, 4 GB), `cpx32` (4, 8 GB), `cpx42` (8, 16 GB), `cpx62` (16, 32 GB), each with the load (`tools/desktop-load`) | `cpx42` €0.1314 and `cpx62` €0.2452 an hour (2026-10-22); a tier run takes under half an hour | CPU-only, sized to Table 3.2 as the paper promises. First planned on `cx23` to `cx53`; `cx53` showed no stock in any location on 2026-10-22, and one family across all four tiers keeps the CPU the same |

Sources: `hcloud server-type list` (the project's account), https://www.hetzner.com/dedicated-rootserver/matrix-gpu/, https://www.runpod.io/pricing, https://ollama.com/library/qwen2.5-coder/tags, https://ollama.com/library/qwen3-coder/tags, https://ollama.com/library/llama3.3/tags.

Not verified: GPU stock at the hour of renting, and the RunPod prepaid-credit minimum for a new account. Core limits: until 2026-10-28 the project's shared-core limit was about 20, the development VM holding 8, so rentals ran in turn within 12 cores and the 16-core tier could not run; the dedicated-core limit refused a single 16-core `ccx43`. A raise was requested and approved on 2026-10-28 (Console, Limits, Request change); the Console's Limits page has the new figures.

## What one day can and cannot cover

In one day: every CPU correctness experiment, the accuracy runs (Layers 7 and 9) for the candidates up to 14B on rented CPU servers, and the deployability timings on the four tier VMs. Layers 3, 4 and 8 and the model bench for every candidate, and 32B accuracy on CPU, have not been sized yet. The model-generated partition is regenerated only if the chosen study model is not the 3B.

Not in one day, whatever the hardware: the agent command study, which waits on the API driver and a frozen suite and runs on the researcher's machine; anything with participants or raters; OSWorld; and fine-tuning.
