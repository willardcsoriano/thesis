## Overview

This file records architectural and research design decisions that have been made and are no longer under discussion. Each entry states what was decided, why, and what was explicitly rejected so the reasoning is not lost. Decisions that are already reflected in a submitted chapter are noted as such. Decisions not yet written into a chapter (implementation-phase) are noted so they can be pulled into Thesis 1 without revisiting the rationale.

## Table of Contents

- [Overview](#overview)
- [Decisions](#decisions)
  - [D1 — Deployment target: Debian 13 (Trixie)](#d1-deployment-target-debian-13-trixie)
  - [D2 — Local-SLM-first, model-agnostic inference backend](#d2-local-slm-first-model-agnostic-inference-backend)
  - [D3 — Intent parsing model: Qwen2.5-Coder-3B-Instruct](#d3-intent-parsing-model-qwen25-coder-3b-instruct)
  - [D4 — Vision model and GUI tiers: OUT OF SCOPE](#d4-vision-model-and-gui-tiers-out-of-scope)
  - [D5 — Within-subjects user study design, 20 participants](#d5-within-subjects-user-study-design-20-participants)
  - [D6 — Statistical analysis approach](#d6-statistical-analysis-approach)
  - [D7 — CLI-only execution scope](#d7-cli-only-execution-scope)
  - [D8 — Implementation stack: Go runtime, Python build pipeline, Ollama inference server](#d8-implementation-stack-go-runtime-python-build-pipeline-ollama-inference-server)
  - [D9 — Study baseline: expert baseline design (participant's primary OS)](#d9-study-baseline-expert-baseline-design-participants-primary-os)
  - [D10 — Conversation memory model: session-scoped, rolling window, output compression](#d10-conversation-memory-model-session-scoped-rolling-window-output-compression)
  - [D11 — Study interface mode: GUI (fullscreen conversational interface)](#d11-study-interface-mode-gui-fullscreen-conversational-interface)
  - [D12 — Distro identity vs. implementation substrate: Debian + XFCE (GUI), bare Debian (TUI)](#d12-distro-identity-vs-implementation-substrate-debian-xfce-gui-bare-debian-tui)
  - [D13 — Product mode (post-thesis): agentic overlay on a visible traditional desktop](#d13-product-mode-post-thesis-agentic-overlay-on-a-visible-traditional-desktop)
  - [D14 — Novice/power-user classification: self-report confirmed by a behavioral screener](#d14-novicepower-user-classification-self-report-confirmed-by-a-behavioral-screener)
  - [D15 — Prior AI exposure: graded covariate, not a third grouping variable](#d15-prior-ai-exposure-graded-covariate-not-a-third-grouping-variable)
  - [D16 — Citation style: numbered ACM-style with DOIs/URLs, thesis-wide](#d16-citation-style-numbered-acm-style-with-doisurls-thesis-wide)
  - [D17 — Shared master bibliography; number [21] reserved/unused](#d17-shared-master-bibliography-number-21-reservedunused)
  - [D18 — Recruitment quota: minimum 2 participants per primary-OS background](#d18-recruitment-quota-minimum-2-participants-per-primary-os-background)
  - [D19 — CLI mode formalized as a third interface mode](#d19-cli-mode-formalized-as-a-third-interface-mode)
  - [D20 — GUI-mode fallback to XFCE: participant-accessible, logged, and excluded from primary analysis](#d20-gui-mode-fallback-to-xfce-participant-accessible-logged-and-excluded-from-primary-analysis)
  - [D21 — CLI-mode execution model: bounded, gated multi-step loop, not full autonomy](#d21-cli-mode-execution-model-bounded-gated-multi-step-loop-not-full-autonomy)
  - [D22 — Classifier scope widened beyond filesystem reversibility: fetch/decode-and-execute and recursive permission changes](#d22-classifier-scope-widened-beyond-filesystem-reversibility-fetchdecode-and-execute-and-recursive-permission-changes)
  - [D23 — `internal/undo` extended to back up file content before a confirmed content-mutating command runs](#d23-internalundo-extended-to-back-up-file-content-before-a-confirmed-content-mutating-command-runs)
  - [D24 — `internal/undo` gains a second, cheaper undo mechanism: hardlink-based trash for pure deletions](#d24-internalundo-gains-a-second-cheaper-undo-mechanism-hardlink-based-trash-for-pure-deletions)
  - [D25 — Four remaining known undo gaps closed: git reset --hard, git clean -f, recursive chmod/chown, and dd/mkfs onto a regular file](#d25-four-remaining-known-undo-gaps-closed-git-reset---hard-git-clean--f-recursive-chmodchown-and-ddmkfs-onto-a-regular-file)
  - [D26 — TUI mode drives the same execution loop, rather than reimplementing it](#d26-tui-mode-drives-the-same-execution-loop-rather-than-reimplementing-it)
  - [D27 — SynapseOS is an agentic layer *over* the existing desktop session, not a replacement for it](#d27-synapseos-is-an-agentic-layer-over-the-existing-desktop-session-not-a-replacement-for-it)
  - [D28 — Linux/XFCE is a methodological necessity, not the contribution](#d28-linuxxfce-is-a-methodological-necessity-not-the-contribution)
  - [D29 — The thesis's algorithmic contribution is recoverability analysis of generated shell commands](#d29-the-thesiss-algorithmic-contribution-is-recoverability-analysis-of-generated-shell-commands)
  - [D30 — Generated commands stay visible to the user](#d30-generated-commands-stay-visible-to-the-user)
  - [D31 — Results are reported in natural language, not dumped as raw output](#d31-results-are-reported-in-natural-language-not-dumped-as-raw-output)
  - [D32 — The algorithm becomes RQ1; the two contributions are independent, not subordinate](#d32-the-algorithm-becomes-rq1-the-two-contributions-are-independent-not-subordinate)
  - [D33 — File manipulation runs on typed operations; the shell handles the remainder](#d33-file-manipulation-runs-on-typed-operations-the-shell-handles-the-remainder)

## Decisions

### D1 — Deployment target: Debian 13 (Trixie)

**Status:** In SA3.1 (Table 3.1, Section 1.2, throughout)

SynapseOS targets bare Debian 13 (Trixie) as the deployment and evaluation platform. Debian is chosen because it is the lowest-common-denominator Linux base — no desktop environment assumed, no proprietary additions — and because the development machine runs Debian 13 natively, making it the honest evaluation baseline.

**Rejected:** Ubuntu 24.04 LTS (was the original target). Ubuntu adds a layer of Snap, GNOME Shell extensions, and Ubuntu-specific tooling that obscures what a clean Linux userland actually looks like.

---

### D2 — Local-SLM-first, model-agnostic inference backend

**Status:** In SA3.1 Section 2.1a and Section 1.4 Why

SynapseOS defaults to a locally-hosted small language model (SLM) for all inference. No internet connection or API key is required. Cloud model access is preserved as an explicit opt-in: users may supply an API key for a supported cloud provider to substitute the local SLM for any inference role.

**Why:** SynapseOS operates at the OS level and observes everything — every command, every file, every window on screen. Routing that data through a cloud API gives an external provider continuous visibility into the user's computing activity. On-device inference eliminates this exposure entirely. Cloud access is a deliberate user choice, not a system default.

**Rejected:** Cloud-LLM-first design. LLM dependency weakens the thesis contribution (the system becomes API glue rather than systems research) and creates a hard dependency on external services for a system intended to replace the desktop userland.

---

### D3 — Intent parsing model: Qwen2.5-Coder-3B-Instruct

**Status:** In SA3.1 Section 2.1a

The primary SLM for intent parsing (natural language → structured action / bash command) is Qwen2.5-Coder-3B-Instruct. It demonstrated the strongest NL2Bash accuracy among models below 7B parameters in controlled evaluation (26% baseline, 58% with prompting, best sub-7B) per Westenfelder et al. [2]. At Q4_K_M quantization, it requires approximately 1.8 GB of memory and runs on CPU without a dedicated GPU.

Fine-tuning via LoRA on the NL2Bash corpus + custom SynapseOS task suite is planned to push accuracy higher within the 3B parameter budget.

**Reaudited 2026-07-05:** Web research confirmed no small (<4B) model released since the original selection has NL2Bash- or shell-generation-specific benchmark evidence beating Qwen2.5-Coder-3B-Instruct. Candidates checked and rejected: **Qwen3-Coder-Next** (Feb 2026) — strong benchmarks (SWE-Bench Verified >70%) but disqualifies on hardware: MoE architecture with 80B *total* parameters (3B active per token), all experts must stay resident in memory for CPU inference, blowing the ~2 GB / 8 GB CPU-only budget by an order of magnitude regardless of active-parameter efficiency. **Phi-4-mini (3.8B)** — right size class (~2.2 GB Q4_K_M, MIT license) but no NL2Bash/shell-specific evidence, only general reasoning benchmarks (MMLU, GSM8K), which the Westenfelder et al. execution-based methodology does not treat as equivalent. **SmolLM3-3B** — beats base Qwen2.5-3B and Llama-3.2-3B, but not the code-specialized Coder variant this decision compares against; irrelevant to the actual comparison. **LiteCoder-Terminal benchmark** (arXiv 2605.29559, 2026) — a genuine terminal-agent benchmark, but tests nothing under 4B (smallest is Qwen3-4B-Instruct, scoring 1–14% pass@1 pre-fine-tune on Terminal-Bench variants); reinforces this decision's premise that LoRA fine-tuning is necessary at this size class rather than pointing at a better base model. The NDSS LAST-X 2026 citation blocker is resolved: authors are Jef Jacobs, Jorn Lapon, and Vincent Naessens (DistriNet, KU Leuven), "Local LLMs for NL2Bash: A Large-Scale Open-Source Model Evaluation for Bash Command Generation" — the per-model accuracy table inside the PDF remains unextracted (binary/FlateDecode, no available tool gets past it), so it cannot yet be cited for its specific ranking, only as corroborating literature. Added to SA3.1 Section 2.1a and the References list as [25]; the reconfirmation itself (rejected candidates and reasoning) is now also stated directly in Section 2.1a rather than living only in this file.

**Rejected:** Qwen2.5-VL-3B-Instruct as a unified model for both intent parsing and Tier 3 vision. The VL variant adds a vision encoder (~0.7–1B additional parameters), making it effectively ~3.7–4B total at ~2.5 GB Q4 — larger than Coder-3B and weaker at bash generation because Coder was trained on 5.5 trillion code tokens specifically. Qwen3-Coder-Next, Phi-4-mini, and SmolLM3-3B — see reaudit above.

---

### D4 — Vision model and GUI tiers: OUT OF SCOPE

**Status:** Superseded by D7. Not in any chapter.

Moondream 2 (Tier 3 vision fallback) and AT-SPI accessibility tree automation (Tier 2) were evaluated and explicitly removed from the architecture. The three-tier hybrid execution model (Tier 1 MCP, Tier 2 AT-SPI, Tier 3 Vision) has been replaced by a CLI-only execution pipeline. See D7.

Research data on Moondream 2 (1.9B params, ScreenSpot F1@0.5 = 80.4) and Qwen2.5-VL-3B is preserved in `research-methods/module 3/references/llm-selection-research.txt` for future reference.

---

### D5 — Within-subjects user study design, 20 participants

**Status:** In SA3.1 Section 3.2. **Revised 2026-09-12 (Session 31): the sample is now 40 participants (20 novice, 20 command-line fluent).** The heading keeps its original number and wording as an identifier; the figure below does not hold. The original 20 rested on a power analysis that was simply wrong — it claimed a medium effect (*d* = 0.5) at 80% power needed 17 participants, where a paired-samples comparison at α = 0.05 two-tailed needs 34. At 40 the study has roughly 87% power for *d* = 0.5 and detects down to *d* = 0.45, with margin for attrition and for the exclusions D14's screening rule produces. The condition × group interaction — which is what the study's central claim actually rests on — remains the least-powered comparison in the design and is reported descriptively rather than as a confirmatory test.

The user study uses a within-subjects design: all 20 participants complete both conditions (SynapseOS and conventional Debian desktop). Condition order is counterbalanced (10 participants complete SynapseOS first, 10 complete the desktop first). Participants are split into two populations: 10 novice users and 10 power users, recruited from Mapúa University – Makati.

**Why within-subjects:** Each participant serves as their own control, producing a direct comparison while requiring a smaller pool than between-subjects. Counterbalancing controls for the primary threat: the learning effect.

**Sample size justification:** A priori power analysis targeting 80% power at α = 0.05 with medium effect size (d = 0.5) yields a minimum of 17 participants for a within-subjects design — satisfied by n = 20.

---

### D6 — Statistical analysis approach

**Status:** In SA3.1 Section 3.3 and 3.4

Shapiro-Wilk normality test → paired-samples t-test (normal) or Wilcoxon signed-rank (non-normal), α = 0.05, Cohen's d effect size. Qualitative data via reflexive thematic analysis (Braun & Clarke [24]).

---

### D7 — CLI-only execution scope

**Status:** In SA3.1 Conceptual Framework, Section 2.1b, Section 1.6, Section 3.1

SynapseOS executes only shell-expressible operations. The system's capability boundary is the Linux command-line toolchain: file management, process control, text processing, package management, network configuration, and any application that exposes a command-line interface. GUI-only interactions are explicitly out of scope.

**Why:** GUI automation (AT-SPI, vision-based simulation) introduces unrealistic complexity and scope creep that cannot be reliably built and evaluated within a thesis timeline. Constraining to CLI produces a tractable, verifiable claim. The conversational interface value proposition holds fully in CLI scope — users still interact in natural language without needing to know the commands.

**Rejected:** Three-tier hybrid architecture (Tier 1 MCP, Tier 2 AT-SPI, Tier 3 Vision). The AT-SPI and vision tiers require deep integration with GUI toolkits that vary across applications and can break with updates — too fragile for a controlled study. Tier 1 (MCP) is also removed from the active architecture since it is application-specific and adds complexity without changing the research question.

**Also rejected:** Framing bash as a limitation. It is a design boundary. The thesis evaluates whether natural language input improves the CLI experience — a well-scoped, answerable question.

---

### D8 — Implementation stack: Go runtime, Python build pipeline, Ollama inference server

**Status:** In SA3.1 Table 3.1. **Reconsideration raised, unresolved 2026-07-15** — whether Ollama's full orchestration shell (model registry, multi-backend hardware detection, multi-client HTTP scheduling) is worth keeping given SynapseOS's fixed single-model/single-target/single-user deployment, versus embedding the inference engine more directly. Not yet decided; tracked in `notes/brainstorm.md`.

**Go** owns the runtime — everything the user touches during the study: the TUI session manager (bubbletea + lipgloss), bash subprocess execution and stdout/stderr streaming, Ollama API client (HTTP streaming to localhost:11434), confirmation gate, and undo log.

**Python 3.12** owns the build pipeline — everything that runs offline before the study: LoRA fine-tuning (Unsloth / PEFT), dataset preparation (NL2Bash corpus + custom task suite), and statistical evaluation scripts (pandas, scipy).

**Ollama** serves the model at runtime — it manages model lifecycle, quantization, and exposes a REST API that both Go (at runtime) and Python (during evaluation) can call identically. This decouples the application from the inference engine and makes the cloud opt-in straightforward (swap the Ollama endpoint for an OpenAI-compatible API).

**Why this split and not pure Python:** Go produces a single binary with no dependency hell, goroutines are a natural fit for streaming model output token-by-token into the TUI, and memory overhead is ~15 MB vs ~80–100 MB for the Python interpreter + deps. Python is kept only where it has no peer — the ML fine-tuning ecosystem (Transformers, PEFT, Unsloth) is Python-only.

**Rejected:** Pure Python runtime. Textual is good but bubbletea is better for this use case, and asyncio subprocess streaming is more complex than goroutines. Rejected: Go for fine-tuning — no viable Go ML training ecosystem exists.

---

### D9 — Study baseline: expert baseline design (participant's primary OS)

**Status:** In SA3.1 Section 3.2

Condition B is each participant's primary OS — Windows 11, macOS, or Linux with GNOME — on a dedicated baseline machine in the lab. Study uses three physical machines: one SynapseOS (Debian 13), one macOS, and one Windows 11 / Linux (GNOME) dual-boot machine — Windows and Linux share a machine (booted to separate partitions) rather than requiring a fourth, since mainstream x86-64 hardware runs Linux natively well. macOS remains a dedicated Apple-Silicon machine. The facilitator boots the dual-boot machine to the OS matching the participant's assignment before the session begins — no runtime switching. Each participant uses whichever baseline machine (or boot target) matches their daily driver.

**Why:** A fixed Linux desktop baseline (originally GNOME on Debian) would confound the result. Most participants have never used a Linux desktop; slow performance under that condition reflects OS unfamiliarity, not interface quality. The expert baseline design tests SynapseOS against what participants already know — a harder and more ecologically valid opponent. A positive result against a participant's home environment is a stronger claim than winning against a foreign system. Windows/Linux dual-boot (rather than a fourth machine) preserves this: each OS still runs natively at full performance, so the task-completion-time comparison isn't touched by the consolidation.

**Rejected:** Single fixed Linux baseline (GNOME/Debian) — confounds interface with OS familiarity. Three-OS between-subjects design — requires much larger n and loses within-subjects control. macOS-only or Windows-only baseline — excludes participants whose native platform differs. Full virtualization of all three baseline OSes on one shared machine — VM overhead on the baseline condition would confound the task-completion-time comparison against SynapseOS running on dedicated hardware, and macOS's software license prohibits virtualizing macOS on non-Apple hardware. Linux virtualized on the Windows machine (rather than dual-booted) — same VM-overhead confound, avoided entirely by native dual-boot instead.

---

### D10 — Conversation memory model: session-scoped, rolling window, output compression

**Status:** Reflected in SA3.1 Section 2.1; scope.md session context manager item. **Amended 2026-09-02 (Session 29) — the token ceiling this decision names was measured and is wrong in both directions.**

**Verified correction:** this entry assumed an "8K token limit (Qwen2.5-Coder-3B-Instruct hard ceiling)". Neither half holds. The model's own reported `context_length` is **32768**, not 8192 — so 8K was never the model's ceiling. More importantly, Ollama's *runtime* default `num_ctx` is **2048** regardless of model capability, and it **truncates silently**: a ~6000-token prompt came back with `prompt_eval_count = 2050` and a perfectly cheerful response, having never seen most of its own input. Setting `num_ctx: 8192` explicitly in the options map raised the same prompt to a measured 5535 tokens. Both figures were measured against the live server, not read from documentation.

Two consequences. First, any rolling-window budget written against "8K" would have been wrong by 4× in the direction that matters — the real ceiling was a quarter of the assumed one, and overflow produces no error to notice. Second, the runtime must set `num_ctx` explicitly rather than inherit a default; the budget below is only meaningful once it does. The rolling-window *design* is unchanged — only the number it is measured against, and the requirement to set it deliberately.

SynapseOS uses session-scoped in-memory conversation context. History lives in a Go slice of message structs for the duration of one login session and is cleared on logout or reboot. No persistence layer is required for the thesis prototype.

Context overflow is handled by a rolling window: when accumulated history approaches 75% of the 8K token limit (Qwen2.5-Coder-3B-Instruct hard ceiling), the oldest turns are dropped until the context fits within the budget. Bash command output is truncated before being appended to history — verbose raw output (full `ls -la`, `find` result dumps) is never stored at full length, only a compact result note. This prevents a single verbose command from consuming a disproportionate share of the token budget.

**Why:** Session-scoped memory eliminates the storage, privacy, and context-management complexity of persistent history entirely and keeps the evaluation environment clean and reproducible across participants. Rolling window is simpler than conversational summarization and sufficient at the 3B parameter scale. Output compression is necessary because a single `find /` result can run to thousands of tokens and would crowd out all prior context.

**Rejected:** Pure stateless (zero memory) — breaks pronoun resolution and multi-turn follow-up commands ("move it to Downloads" requires knowing what "it" was). Persistent memory across reboots — adds SQLite storage, session boundary logic, and history deletion UX; deferred to notes/future-features.md. Conversational summarization — adds a second inference call per compression event; more complex than rolling window and not warranted for thesis scope.

---

### D11 — Study interface mode: GUI (fullscreen conversational interface)

**Status:** Reflected in SA3.1 Table 3.1, paragraph after Table 3.2, and Section 3.5. **Rescoped by D27 (2026-09-12):** the study interface is still fullscreen, but it is the TUI launched over a live XFCE desktop rather than a separately-built graphical interface replacing the session. The reasoning below — that novices must not be studied through a bare terminal — is unaffected and is why fullscreen presentation is retained.

The user study evaluates SynapseOS in GUI mode — a fullscreen conversational interface running on a graphical desktop environment. This is the primary product target for general and non-technical users. The TUI mode (terminal-based, no display server required) is the server and remote deployment target and is not evaluated in the user study.

The thesis prototype GUI is a fullscreen borderless window approximating the active desktop aesthetic; the full wallpaper-layer active desktop (wlr-layer-shell integration) remains deferred to notes/future-features.md.

**Why:** The study population includes novice users for whom a terminal interface would introduce a significant familiarity barrier independent of SynapseOS's core capability. Evaluating in GUI mode tests the interface as it would be experienced by its intended general-user audience. A TUI-mode study of novice users would conflate terminal unfamiliarity with interface quality, undermining the validity of the comparison.

**Rejected:** TUI mode for the study — the Interface mode threat to validity, previously listed in Section 3.5, identified exactly this problem: novice user results in TUI mode would not represent the experience those users would have under the intended GUI implementation.

---

### D12 — Distro identity vs. implementation substrate: Debian + XFCE (GUI), bare Debian (TUI)

**Status:** Refines D1 and D11. Not yet in a chapter — record for final compilation. GUI mode's "no escape hatch" claim below is refined by D20 (participant-accessible fallback, logged and excluded from primary analysis) and **superseded by D27 (2026-09-12)**: XFCE is no longer a hidden substrate but a running, usable desktop that SynapseOS layers on top of. The distro-identity reasoning below still holds — a reused substrate under a distinct identity is how derivative distros work — but the "invisible XFCE" framing does not. **D28 additionally reframes why Debian/XFCE at all:** it is the only substrate that permits this, Windows and macOS being proprietary, which is a methodological necessity rather than a platform claim.

SynapseOS presents to the user as its own distribution — its own name and identity — while the substrate underneath is reused and never surfaced to the user. The substrate is mode-dependent:

- **GUI mode:** Debian 13 + **XFCE** as the host desktop environment, running its stable **X11** session (XFCE's Wayland session remains experimental as of 4.20 — unsuitable for a reproducible 20-participant study). XFCE supplies the display session and window management only; SynapseOS launches fullscreen within it, so the participant sees and interacts with nothing but the conversational interface. XFCE is chosen over a custom kiosk compositor because it is the lowest-effort, most standard traditional Linux DE — no new toolchain (no `cage`, no Wayland layer-shell work) is needed for the thesis prototype, while still being a genuine, vendor-neutral "traditional GUI environment" for D9's expert-baseline comparison.
- **TUI mode:** bare Debian 13, no desktop environment — unchanged from D1. Here SynapseOS is a local agentic shell: natural language in, a local SLM reasons over intent, proposes a shell command, and executes it under the confirmation gate. The interaction model is architecturally comparable to Claude Code's agentic CLI loop, but fully offline against an on-device model (D2, D3) rather than a cloud API — a useful reference point for describing the interaction model to a reader unfamiliar with OS-level agents.

**Why this is not dishonest:** distro identity has always been a branding and packaging layer over a reused base, invisible to the end user — Ubuntu never surfaces "Debian" to a desktop user, SteamOS never surfaces "Arch," Pop!_OS never surfaces "Ubuntu." SynapseOS qualifies as a distribution once packaged as a bootable Debian(+XFCE) derivative with its own default session, branding, and update channel (see `layers.md`, "When It Becomes a Distribution") — a real distro, with a reused and unadvertised substrate, exactly like its predecessors.

**Rejected:** a custom kiosk Wayland compositor (`cage`) as the GUI host for the thesis prototype — adds a new toolchain and packaging surface for no benefit over a fullscreen window in a standard DE session; the wallpaper-layer / `wlr-layer-shell` active-desktop mode remains the eventual post-thesis product target (`notes/future-features.md`). **Amended by D20:** this decision originally also rejected any participant path back to XFCE during Condition A, on the grounds that it would confound the within-subjects comparison (D5, D9). D20 reopens this — a participant-accessible fallback now exists — but keeps the comparison clean by scoring and excluding fallback-invoked tasks rather than by denying access. See D20 for the full reasoning.

---

### D13 — Product mode (post-thesis): agentic overlay on a visible traditional desktop

**Status:** Post-thesis product direction. Does not affect the study design (D5, D9, D11) or D12. Not in any chapter — vision/roadmap only.

Beyond the thesis prototype, the commercial product target is a second mode where the traditional desktop (Debian + XFCE) stays fully visible and usable, and SynapseOS is summoned on demand — a global hotkey (via XFCE's `xfconf` keyboard-shortcut settings) or a systray icon opens a floating conversational window; the user can otherwise operate the desktop manually (drag-and-drop, the file manager, any traditional app) exactly as before. This is a lower-effort near-term instantiation of the wallpaper-layer active-desktop concept already recorded in `notes/future-features.md` — the same idea, without requiring `wlr-layer-shell`/Tauri: XDG autostart plus a hotkey/systray launcher is sufficient.

This does not conflict with D12: D12 describes the *study* substrate (fullscreen takeover, no escape hatch, required for a clean within-subjects comparison); D13 describes the *product* substrate (full coexistence, by design). **Narrowed by D27 (2026-09-12):** with SynapseOS layering over a live desktop in every mode, the distance between the two shrank to whether it fills the screen by default (study) or is summoned into a visible desktop (product). They are different modes of the same runtime — same Go binary, same Ollama backend, same confirmation gate and execution engine (M1–M7) — the only difference is the shell wrapped around it, matching the existing TUI/GUI "one slot, two sets of clothes" pattern (`layers.md`).

**Why coexistence is deferred from the thesis:** allowing the participant to fall back to the traditional GUI during Condition A would confound the within-subjects comparison (D5) — a result could no longer be attributed to the conversational interface specifically. The strict takeover (D12) is what makes the study's causal claim clean; the overlay is what makes the eventual product adoptable. Building both from one runtime lets the study protect its validity while the product still ships the friction-free experience users will actually want.

**On the distro claim — weighing D12/D13 against hardening:** the default session (D12's takeover in the study; D13's overlay in the product) is the identity-defining reason SynapseOS can be called its own distribution — it is what a user actually experiences, and it is genuinely uncommon (no daily-driver OS ships a conversational agent as its primary interaction layer). Hardening (`notes/future-features.md`, Hardening Profiles) is real and worth doing — it mirrors exactly how Ubuntu differentiates from bare Debian — but it is a commodity, credibility-class signal, not an identity-class one: nearly every serious distro hardens its defaults, so hardening alone does not distinguish SynapseOS from the crowd. If forced to choose one item to get right before the "own distro" claim is defensible, it is the agentic session (D12/D13), not the hardening profile.

**Rejected:** treating the overlay/coexistence model as the thesis design — see D12's rejection of "true coexistence" for the study-validity reasoning. Leading with hardening as the primary "why this is a distro" argument — it is supporting evidence, not the load-bearing claim.

---

### D14 — Novice/power-user classification: self-report confirmed by a behavioral screener

**Status:** Refines D5. In SA3.1 Section 1.5.

Group assignment (novice vs. power user) is no longer decided by self-report alone. A three-task behavioral screener — performed unaided on the participant's primary OS, one task per system-task category used in the main study (file organization, process monitoring, application management) — confirms self-reported proficiency before assignment. Power user requires advanced self-report **and** passing all three tasks; novice requires basic self-report **and** failing the screener. Participants whose self-report and behavioral result disagree are excluded, same as participants who don't clearly satisfy either bucket under the original D5 criteria.

**Why:** Self-reported proficiency (basic/intermediate/advanced) has no operational anchor and is a known-weak classifier in HCI screening — two participants can pick the same label for different reasons (modesty, overconfidence, or genuine skill in a domain other than OS-native system tasks). The specific failure mode motivating this: a participant technically skilled in one area (e.g., software development) may not be fluent with their own OS's native file management, process monitoring, or system administration tools — the exact competency this study's group split measures — and would be misclassified by self-report alone. The behavioral screener is scoped to the same three categories as the main task suite so it verifies the construct actually being studied, not a general technical-skill proxy.

**Rejected:** Self-report only (original D5 criterion) — no behavioral check, vulnerable to self-calibration differences. A longer/more comprehensive skills test — adds session time and participant burden disproportionate to a screening step; three short pass/fail tasks are enough to confirm or contradict the self-report, not to produce a fine-grained skill score.

---

### D15 — Prior AI exposure: graded covariate, not a third grouping variable

**Status:** Refines D5. In SA3.1 Sections 1.5 and 3.2.

Prior conversational AI exposure — already a screened dimension under D5 — is upgraded from a binary "prior exposure: yes/no" item to a graded scale (never / occasionally / regularly / daily) plus which tools (chatbots, voice assistants, code-completion or agentic coding assistants, other LLM-based tools). It is recorded and analyzed as an exploratory covariate alongside primary OS, in the same section (3.2) and with the same "exploratory" framing already used for the per-OS subgroup analysis — not as a third grouping variable alongside novice/power user and condition.

**Why:** A participant fluent with LLM-based tools may have a better mental model of how to phrase a request to get a good result, which could inflate or explain SynapseOS-specific results (task completion time, SUS, trust in the confirmation gate) independent of their OS-native proficiency. That is a plausible confound worth capturing. A graded scale plus tool identity costs one more screening question — no new instrument, no added session time — and gives the analysis something more useful than a binary flag to correlate against.

**Rejected:** Treating AI exposure as a third primary grouping variable (e.g., low/high-exposure subgroups analyzed with their own hypothesis test) — with n = 20 already split 10/10 by novice/power user, a further split leaves cells too small to support inference; the existing per-OS subgroup analysis is exploratory for the same reason, and AI exposure is treated identically. Leaving the item as a binary yes/no — too coarse to support even exploratory correlation with any granularity.

---

### D16 — Citation style: numbered ACM-style with DOIs/URLs, thesis-wide

**Status:** Reflected in every chapter — SA1 (Ch.1), SA2 (Ch.2 RRL), SA3.1 (Ch.3).

All chapters use IEEE/ACM-style numbered in-text citations `[n]` against a shared master bibliography, with full DOIs/URLs on each reference entry, rather than the ITRD writing-guidelines handout's alphabetical author-year format.

**Why:** The proposal grading rubric explicitly grades citations as "ACM style," which the numbered `[n]` + venue + DOI/URL format matches. The literature this thesis draws on is arXiv/DOI-native (most sources are 2023–2026 preprints and proceedings); dropping URLs would materially degrade verifiability. Ch.1 was already submitted under this convention, so cross-chapter consistency requires all chapters follow it.

**Rejected:** The ITRD handout's author-year format with its "internet references should NOT be included" note — it predates the arXiv-heavy CS/HCI literature this thesis relies on, conflicts with the rubric's ACM directive, and is inconsistent with the already-submitted Ch.1. The handout note is treated as a general-writing default overridden by the discipline-specific rubric.

---

### D17 — Shared master bibliography; number [21] reserved/unused

**Status:** Bookkeeping across the Ch.1/Ch.2/Ch.3 reference lists. **Superseded 2026-09-09 (Session 31):** the vacancy was closed by renumbering, and the bibliography has since grown to `[1]`–`[44]`.

The chapters draw from one shared master bibliography; each chapter's reference list contains only the entries that chapter cites. Number `[21]` was previously left vacant. That was reversed: a deliberately empty slot in a numbered list reads as an error to every reader who notices it and cannot be distinguished from one, so entries `[22]`–`[25]` were renumbered down and the list made contiguous. It has since been extended with the agentic-OS landscape, the comparative-evaluation precedent, and the HCI foundations (D28, D29), and every entry's metadata was verified against its primary record rather than written from memory.

**Why:** Renumbering to close the gap would desync the already-submitted Ch.1 and Ch.3 reference lists — every in-text `[22]`–`[25]` citation would have to shift, across two submitted chapters, for a purely cosmetic gain. Leaving `[21]` reserved preserves numbering stability across chapters at zero risk. Citation integrity is verified per chapter (every cited number is defined and every defined number is cited); `[21]` is simply never cited. If a suitable source surfaces during final compilation it can occupy `[21]` without disturbing any existing number.

**Rejected:** Renumbering `[22]`–`[25]` down by one to close the hole — desyncs submitted chapters for no substantive benefit.

---

### D18 — Recruitment quota: minimum 2 participants per primary-OS background

**Status:** To be reflected in SA3.1 Section 1.2b and Section 1.5, and in the ethics application package's recruitment plan.

Recruitment for the sample adds a floor: at least 2 participants must have each of Windows, macOS, and Linux as their primary OS, with the remainder unconstrained by OS background. **Revised 2026-09-12 (Session 31), following D5's move to n = 40:** the floor scales to 4 per background. A minimum of 2 in a 40-participant sample is close to vacuous — it would admit 36 Windows users and still be satisfied. The binding constraint on this quota is the Linux-primary *novice* cell: a daily Linux user with no command-line familiarity is close to a null set on a university campus, and if it cannot be filled the Linux quota is met from command-line-fluent participants alone, with the resulting imbalance reported rather than concealed.

**Why:** Without a floor, the realized sample could end up all one OS (e.g., 18 Windows / 1 macOS / 1 Linux), leaving the per-OS exploratory subgroup analysis (D15, SA3.1 Section 3.2/3.4) unable to say anything about the underrepresented OS at all. A floor of 2 guarantees every OS background has at least a minimal, non-singleton presence without materially constraining recruitment — macOS users are expected to be the scarcest population reachable through Mapúa University – Makati's general recruitment channels, and 2 is judged achievable without delaying the timeline.

**Rejected:** No quota (risks a degenerate all-one-OS sample); increasing total n to ~50+ to properly power OS-specific subgroup claims (disproportionate to the core within-subjects hypothesis, which n = 20 already satisfies per the D5 power analysis — the per-OS breakdown is explicitly exploratory, not a primary hypothesis test); a higher floor (3 or 5 per OS) — judged to risk recruitment delay for macOS specifically without a clear analytical payoff at this sample size.

---

### D19 — CLI mode formalized as a third interface mode

**Status:** Refines D11. Reflected in SA3.1 Table 3.2 and the paragraph following it. **Execution model refined by D21** — CLI mode is still a single non-persistent invocation (no session survives between separate `synapse` calls), but a single invocation now runs a bounded multi-step loop internally rather than exactly one command; see D21.

SynapseOS ships three interface modes, not two: **CLI** (one-shot invocation — `synapse "<task>"` translates a single natural-language request into a proposed command and exits; no persistent session), **TUI** (persistent full-screen chat session, D11), and **GUI** (fullscreen conversational takeover, evaluated in the study, D11/D12). CLI mode is not new work — it is the M1 walking skeleton's existing behavior (`prototype/cmd/synapse/main.go`), promoted from a disposable stepping-stone toward M3 to a permanent, separately-named, shipped mode. It targets scripting, automation, and one-off remote invocations over SSH where a persistent interactive session is unnecessary overhead.

**Why:** CLI mode and TUI mode are genuinely different interaction shapes — one-shot request/response versus a persistent multi-turn conversation — not two maturity stages of the same feature. Collapsing CLI into "TUI without the chrome" would either force M3 to also support a non-interactive invocation path (extra branching in the TUI's own state machine) or quietly drop the one-shot use case once M3 lands, losing real utility (cron jobs, quick remote commands, shell pipelines) for no benefit. Naming it separately means M1's harness stays a permanent, useful artifact instead of throwaway scaffolding.

**Rejected:** Folding CLI into TUI as a single mode with two invocation styles — the interaction shapes are different enough (stateless vs. stateful) that conflating them in the same mode name obscures the actual distinction a user or a script author needs to reason about.

---

### D20 — GUI-mode fallback to XFCE: participant-accessible, logged, and excluded from primary analysis

**Status:** Amends D12. To be reflected in SA3.1 Table 3.1, Table 3.2, the paragraph following Table 3.2, and Section 3.5 (Threats to Validity). **Simplified by D27 (2026-09-12):** the fallback now returns the participant to a desktop that was running the whole time, rather than recovering a machine whose session had been displaced. The analysis treatment below — logged, task excluded from the primary comparison, invocation rate reported separately — is unchanged, and is the part that protects the study's causal claim.

GUI mode gains a fallback path: a participant can return to the underlying XFCE session — already running invisibly beneath SynapseOS's fullscreen window, per D12 — if SynapseOS becomes unresponsive or they want to stop using it mid-task. Unlike D12's original no-escape-hatch design, this fallback is participant-accessible, not facilitator-only. Every invocation is logged as a discrete telemetry event (participant ID, task ID, timestamp). Any task during which it is invoked is excluded from the primary SynapseOS-condition completion-time and error-rate comparison for that task — scored as "did not complete via SynapseOS" rather than silently counted as a success — and fallback-invocation rate is reported as its own secondary, exploratory metric (how often participants reached for it, and under which task categories).

**Why:** A participant-accessible fallback is what was asked for — a genuine safety net, not a hidden facilitator-only recovery path — but D12's original reasoning for having no escape hatch at all was sound: an accessible-and-unmeasured fallback would let a participant's own choice silently substitute for the interface being evaluated, contaminating the within-subjects comparison (D5) with no way to detect or correct for it afterward. The fix is not to restrict access but to instrument it: logging plus exclusion-from-primary-analysis means the core causal claim (task performance attributable to SynapseOS specifically) stays clean, while the fallback itself becomes an honest, reportable finding — a high fallback-invocation rate is informative data about the interface's reliability and trustworthiness, not something to hide.

**Rejected:** Facilitator-only hidden trigger, invisible to participants (the initially-recommended alternative) — cleanly preserves D12's original validity argument with zero instrumentation needed, but does not give participants direct access, which was the explicit requirement here. Leaving the fallback un-instrumented (accessible, but its use not logged or scored specially) — would silently reintroduce exactly the confound D12 was designed to prevent, with no way to detect after the fact which "SynapseOS-condition" data points were actually completed via the fallback instead.

---

### D21 — CLI-mode execution model: bounded, gated multi-step loop, not full autonomy

**Status:** Refines D19 (M1). Reflected in SA3.1 Table 3.2, footnote 8, and the paragraph following the table (2026-07-15).

A single `synapse "<task>"` invocation runs a bounded loop, not exactly one command: propose a command → classify its reversibility → confirm if irreversible → execute → feed the result (stdout, stderr, exit code) back to the model, which then either proposes the next command toward the same task or signals the task is complete. Every proposed command at every step passes through the same classifier and confirmation gate individually — there is no batch approval, and no step is granted trust carried over from a prior step's confirmation. The loop ends when the model signals completion or a fixed hard step cap is reached, whichever comes first; hitting the cap is reported as an explicit "step limit reached" failure, never silently treated as success. The 8-task sample suite stays propose-only and is unaffected.

**Why:** Single-command execution left a real capability gap: some tasks genuinely require multiple distinct actions (e.g., creating destination folders before sorting files into them) or a corrected retry after a failed attempt, neither of which a single proposed command can express. A bounded loop closes that gap without adopting full autonomy, which was considered and rejected below for reasons already established in this project's own literature review, not just an engineering preference.

**Rejected:** Full autonomy — the model deciding step count and task completion unsupervised, with the confirmation gate weakened, removed, or trusted-once-then-bypassed for later steps. Two independent reasons. First, capability: Qwen2.5-Coder-3B already produced a semantically wrong command at single-shot difficulty during live validation (`build-order.md` M1 status — the `dpkg-query`/`grep` mismatch); autonomous multi-step loops additionally require the model to judge its own task completion and avoid drifting from the original intent across turns, a harder capability that degrades faster at small parameter counts, and errors compound across unsupervised steps rather than self-correcting. Second, and more fundamentally for a thesis specifically: full autonomy would recategorize what SynapseOS is being evaluated as. SA2's own literature review (Section 2.9) explicitly distinguishes curated-benchmark evaluation — which "assess[es] the autonomous task completion of agents acting on a user's behalf" — from human-centered evaluation of "the performance of a human working through an interface," and identifies the latter as the underserved gap this study fills. Full autonomy moves SynapseOS toward the former category, undermining the comparison the study is designed to make. NaSh [3] and VoicePilot [5], both already cited as motivating the confirmation gate itself, reach the same conclusion from a safety and usability angle — NaSh because unguarded LLM output "may be unintended or unexplainable," VoicePilot because its own user study with motor-impaired participants derived preview-and-confirm as a design necessity, not an option.

**Also rejected:** Retry-only-on-failure (re-attempt the same failed command with its error appended, but never propose a genuinely different next command). Simpler to implement, but too narrow — it only helps when a single correct command exists and the model merely malformed it, not when a task inherently requires several distinct actions in sequence, which is the more common shape of the capability gap being addressed here.

---

### D22 — Classifier scope widened beyond filesystem reversibility: fetch/decode-and-execute and recursive permission changes

**Status:** Extends the reversibility classifier described under M1/D19. Reflected in `prototype/internal/classifier/classifier.go` and `prototype/testing-plan.md` Layer 2 (Session 23).

The reversibility classifier's job broadens from "will this destroy local file content with no undo" to also cover two related risk shapes surfaced while building the Layer 2 adversarial corpus (`testing-plan.md`): (1) fetch-and-execute / decode-and-execute — a command that pipes fetched remote content (`curl`/`wget`) or decoded content (`base64 -d`) directly into a shell interpreter (`sh`/`bash`/`zsh`) is now classified Irreversible, and (2) recursive permission/ownership changes (`chmod -R`, `chown -R`) are now classified Irreversible, while a single-file `chmod`/`chown` stays Reversible. A bare `eval` invocation is also now classified Irreversible, since the classifier cannot inspect a dynamically constructed string before it runs.

**Why:** Fetch/decode-and-execute is not filesystem-reversibility in the narrow sense the classifier was originally scoped to, but running arbitrary unreviewed remote or decoded code is at least as consequential as anything else the classifier already flags — the false-positive cost (one confirmation keypress on a legitimate installer script) is the same calculus already applied to `git reset --hard`, `git clean -f`, and the Session 22 content-mutation rules, none of which are pure "delete a file" either. Recursive permission/ownership changes were previously entirely unclassified — `chmod -R 000 /` auto-ran with no confirmation, and locking a whole tree out of access is functionally comparable to `rm -rf` in blast radius even though no bytes are deleted. Scoping the recursive-flag distinction to `-R`/`--recursive` (not every chmod/chown) keeps the common, low-stakes case (`chmod +x script.sh`) from costing an unnecessary confirmation.

**Rejected:** Leaving fetch-and-execute and permission/ownership changes as an accepted, documented gap (the same treatment given to the `cp`-onto-existing-destination gap and shell-variable indirection) — considered, but rejected here because unlike those two gaps, this class of risk is cheaply and precisely catchable with a narrow pattern (fetch/decode command AND a pipe into a shell interpreter; recursive flag on chmod/chown) without the same blanket-false-positive cost that makes `cp` and variable indirection genuinely hard to close. Blanket-flagging every `chmod`/`chown` regardless of scope — rejected as disproportionate: a single-file permission change is common, low-blast-radius, and would cost a confirmation keypress on one of the most frequent benign shell idioms. Full obfuscation defeat (detecting a dangerous inner command hidden behind arbitrary encoding or command substitution) — rejected as out of scope for a regex-based matcher; the decode-and-execute rule catches the specific base64-into-shell *shape*, not arbitrary obfuscation, and that boundary is documented in the code as an accepted gap alongside `cp` and variable indirection.

### D23 — `internal/undo` extended to back up file content before a confirmed content-mutating command runs

**Status:** Extends the undo safety net described under D19/D21. Reflected in `prototype/internal/undo/undo.go` (`ContentBackup`, `BackupContent`), `prototype/internal/classifier/classifier.go` (`ContentMutationTargets`), and wired into `cmd/synapse/main.go`'s `runLoop` (Session 24).

Until now, `internal/undo` only ever protected Reversible-classified commands — its directory-diff snapshot mechanism has no way to detect or reverse a file whose *content* changed in place, since the file never disappears from a directory listing. A command the classifier flags Irreversible for a content-mutation reason (`sed -i`, `awk -i inplace`, `truncate`, a truncating redirect, `tee` without `-a`) that the user explicitly confirms anyway had no safety net at all: a wrong "yes" was permanent. `classifier.ContentMutationTargets` now extracts the file(s) a matched content-mutating shape would overwrite; immediately before such a confirmed command executes, `undo.BackupContent` takes a full-content copy (with the original file's mode) and journals it as a new `ContentBackup` entry type, restorable through the existing `undo` CLI command via `undo.Apply`.

**Why:** this is the "guiltless" half of a UX principle set this session — the classifier should be accurate (minimize real mistakes) *and* guiltless (make even a wrongly confirmed "yes" as recoverable as possible), on the reasoning that a user who mostly just wants to press "yes" needs the cost of an occasional wrong yes to be low, not just the frequency of prompts to be low. Extending the *existing* directory-diff mechanism to cover this was considered and rejected: diffing can only detect that a file changed, not recover what it changed *from*, so protecting against in-place mutation structurally requires a backup taken *before* execution, not a diff taken after. Scoping the trigger to the classifier's own known content-mutating shapes (rather than backing up every file in the working directory before every irreversible command) keeps the cost proportional — a full-content copy only happens for the specific, relatively rare case of a confirmed content-mutating command, not on every step of the loop.

**Rejected:** Backing up every file in the working directory unconditionally before any Irreversible command — rejected as disproportionate; most Irreversible verdicts (`rm`, `pkill`, fetch-exec) have no single target file a pre-execution backup could meaningfully protect, and a whole-directory content backup on every gated command would impose a real, unbounded cost for no benefit in those cases. Reconstructing content after the fact from some external source (a filesystem snapshot, a version-control-style diff) — rejected as out of scope; this package deliberately does no bash parsing and no filesystem journaling beyond what it already does, and a true point-in-time content history is a different, much larger feature than a narrow pre-execution safety net. Extracting *every* file a multi-file `sed -i`/`tee` invocation targets, not just the last (`sed -i` case) or all (`tee` case) — deferred rather than rejected outright: closing it needs the same flag-arity knowledge (`ContentMutationTargets`' documented gap) that a full getopt parser would require, and the common single/explicit-multi-file cases this session tested are already covered.

### D24 — `internal/undo` gains a second, cheaper undo mechanism: hardlink-based trash for pure deletions

**Status:** Extends D23's "guiltless" undo work with a second mechanism. Reflected in `prototype/internal/undo/undo.go` (`TrashedItem`, `TrashPreserve`, `DefaultTrashDir`), `prototype/internal/classifier/classifier.go` (`TrashTargets`, `CpOverwriteTarget`), and wired into `cmd/synapse/main.go`'s `runLoop` (Session 25).

D23's content-backup mechanism protects a confirmed content-mutating command by copying the target file's bytes before execution — correct, but its cost scales with the file's size, which is why D23 explicitly kept `rm` out of scope ("a whole-directory content backup... would impose a real, unbounded cost"). This decision closes that gap with a mechanism suited to what `rm` actually does at the syscall level: removing a directory entry never touches the underlying data those bytes live in, so a hardlink taken into a holding directory (`~/.synapse/trash`) immediately before a confirmed `rm` runs keeps that data alive through the deletion — at a cost independent of the file's size, unlike a copy. A directory target is handled by recreating its structure in trash and hardlinking every regular file inside rather than copying data. `classifier.TrashTargets` extracts every path a confirmed `rm` would remove (deliberately excluding `shred`, whose entire purpose a trash copy would defeat, and `dd`/`mkfs`, which operate on raw block data no directory entry represents). Session 25 also verified empirically that `cp` overwrites an existing destination's *same inode* rather than replacing it — meaning a hardlink taken before a `cp` overwrite would share the very data being overwritten and protect nothing — so `cp`'s previously-flagged gap (D23 documented it as inconsistent but unclosed) is fixed by routing it onto the *content-backup* path instead, via a new `classifier.CpOverwriteTarget`, not onto this new trash path.

**Why:** the "minimize the undoable surface's exceptions" principle this session extended from "guiltless" — if a cheaper, correct mechanism exists for a whole class of commands (pure deletions), it should be used instead of leaving them out of scope by default. Preserving via hardlink rather than a rename-based "move to trash" was chosen specifically so the real `rm` command still executes unmodified afterward — a rename-based trash would require substituting SynapseOS's own move for the model's actual proposed command, which breaks down the moment `rm` is chained with anything else (`rm old.txt && echo done`); a hardlink coexists with the real command running normally, since both the original and the trash entry point at identical data until the original is unlinked.

**Rejected:** A rename/move-based trash (rather than hardlink) — rejected because it requires replacing the model's actual command with SynapseOS's own move, which only stays correct when the entire command is exactly `rm <args>` and nothing else; a hardlink needs no such substitution. Applying trash-preservation to `cp`'s overwrite case — rejected after empirical verification (Session 25) that `cp` mutates its destination's existing inode in place, which a hardlink cannot protect against; content-backup is the correct mechanism there instead. Extending trash to `git clean -f` and a `git reset --hard` SHA-capture in the same pass — deferred, not rejected: both are real, cheap wins in the same spirit (`git clean -f` is the identical directory-entry-removal shape as `rm`; `git reset --hard` doesn't even need a file-level backup, just the current commit SHA) but each needs its own small piece of design (a dry-run step for `git clean -f`'s target list; a `git`-subprocess call `internal/undo` doesn't currently make) — tracked as known gaps in `docs/safety-model.md` rather than bundled into this decision. A getopt-aware flag/end-of-options parser for `rm -- -oddly-named-file` — rejected for the same precision-for-auditability reasons as every other narrow extraction function in `classifier.go`.

### D25 — Four remaining known undo gaps closed: git reset --hard, git clean -f, recursive chmod/chown, and dd/mkfs onto a regular file

**Status:** Closes every known gap D23/D24 flagged as deferred. Reflected in `prototype/internal/executor/executor.go` (`RunIn`), `prototype/internal/undo/undo.go` (`Entry.GitReset`, `CaptureGitHead`, `MetadataBackup`, `BackupMetadata`), `prototype/internal/classifier/classifier.go` (`IsGitResetHard`, `IsGitCleanForce`, `GitCleanDryRunCommand`, `RecursivePermissionTargets`, `RawWriteOverwriteTarget`), and wired into `cmd/synapse/main.go`'s new `backupBeforeIrreversible` helper (Session 26).

Each of the four gaps needed a different mechanism, matched to what actually changes:

1. **`git reset --hard`** — `undo.CaptureGitHead` runs `git rev-parse HEAD` in the repository before a confirmed reset, and `Apply` restores via `git reset --hard <sha>`. No file-level backup at all — git already has its own object store, so a single commit hash is a complete, correct snapshot. This is the first time `internal/undo` invokes a subprocess, so `executor.Run` was split into a thin wrapper over a new `RunIn(ctx, dir, cmd)` (the working directory `Apply` needs to act in may not be the calling process's cwd, since undo can run long after and from anywhere).
2. **`git clean -f`** — reuses D24's trash mechanism exactly (removing an untracked file's directory entry is the identical safe-for-hardlink shape as `rm`), but needs a preliminary step `rm` doesn't: `classifier.GitCleanDryRunCommand` builds a `git clean -n -f` variant of the confirmed command, which is run first to learn the target list from its "Would remove `<path>`" output (verified empirically against a real repository, Session 26, including that `-n` overrides `-f` and nothing is actually removed) — `git clean -f` alone doesn't name its own targets the way `rm <files>` does.
3. **Recursive `chmod`/`chown`** — a third backup shape, `MetadataBackup` (mode + uid + gid, no content and no directory-entry change), recorded by walking the target tree before the confirmed change and restored via `os.Chmod`/`os.Chown`. Cheaper even than trash: a few bytes of metadata per file regardless of the file's own size.
4. **`dd`/`mkfs` onto a regular file** — `classifier.RawWriteOverwriteTarget` distinguishes this from `dd`/`mkfs`'s overwhelmingly common real target (a raw block device) by checking whether the resolved destination is an existing regular file; if so it's routed onto the same content-backup path as `cp`'s overwrite (verified reasoning: `dd`/`mkfs` writing to a regular file mutates its existing inode in place, the same shape as `cp`). A block device, a fresh path, or a directory all return "out of scope," left exactly as documented.

**Why:** each gap in `docs/safety-model.md`'s "known gaps" section had a concrete, low-cost mechanism already implied by the taxonomy — closing them was a matter of building the specific mechanism each shape calls for, not inventing new ones. Bundled into one decision (matching D22's precedent for several related, same-session classifier additions) since all four share the same motivation and none introduces a new architectural pattern beyond what D23/D24 already established.

**Rejected:** A generic "run an arbitrary shell command to capture/restore state" abstraction inside `internal/undo` (as opposed to the specific `CaptureGitHead`/`Apply`'s inline `git reset --hard` call) — rejected as premature generalization; git is the only external tool this package needs to shell out to today, and a generic abstraction for a single caller is speculative complexity. Treating `mkfs` the same as `dd` without distinguishing target type — considered leaving `mkfs` entirely out of scope (its overwhelmingly common use is a block device, arguably more so than `dd`), but the same regular-file check costs nothing extra to apply and correctly covers the real, if rare, loopback-image-file use case. Recursively backing up `chmod`/`chown` targets to a bounded depth rather than the full tree — rejected as inconsistent with the actual blast radius: `-R` changes the whole tree, so protecting less than the whole tree would leave part of the confirmed change unprotected for no real cost saving (metadata is cheap regardless of tree size).

---

### D26 — TUI mode drives the same execution loop, rather than reimplementing it

**Status:** Implemented in `prototype/internal/tui` and `cmd/synapse/main.go`'s `tui` subcommand (M5, Session 28).

`runLoop` is synchronous and blocks mid-task to ask a y/n question; bubbletea's `Update` must never block. The natural-looking resolution is to rebuild the propose → classify → confirm → execute cycle as a TUI state machine, with each stage an async `tea.Cmd`. Rejected. TUI mode instead receives the *identical* `runLoop` as an injected `tui.TaskRunner` and drives it on its own goroutine, bridged by two channels: the loop's `io.Writer` output arrives as messages, and its `confirmFn` publishes a confirmation request then blocks until `Update` — having rendered the prompt and taken a keypress — sends the verdict back.

**Why:** the reversibility gate is the thesis's central safety claim, and a reimplementation is a second copy of it that can drift from the first without anyone noticing. Injection makes drift structurally impossible: every verdict, gate, and undo-journal write in TUI mode is the same code CLI and REPL mode run. It also keeps `internal/tui` free of any Ollama or filesystem dependency, which is what makes the whole interface testable with no model and no real commands (96.3% coverage, no live backend). This is `interface-modes.md`'s "a mode's job is only ever: collect input, drive the core, render output" applied literally rather than approximately.

**Rejected:** A TUI-local state machine (idiomatic bubbletea, but duplicates safety-critical logic — the thing `interface-modes.md` explicitly warns against). Moving `runLoop` into an `internal/` package so `tui` could import it directly — a larger refactor touching every existing test call site, for no benefit over injection, and it would couple the UI package to the runtime's dependencies.

**Deliberate consequence, not a bug:** a keystroke arriving *before* a confirmation prompt renders is discarded, where CLI/REPL's line-buffered stdin would have queued it. Kept because the safer reading is the right one — a pre-typed `y` must never approve a destructive command the user hasn't seen described. Pinned by test so it can't be "fixed" back into type-ahead approval.

---

### D27 — SynapseOS is an agentic layer *over* the existing desktop session, not a replacement for it

**Status:** Decided 2026-09-12 (Session 31). Supersedes the framing carried by D19/D20 and by Chapter 1's scope, which described SynapseOS as replacing the desktop shell, session manager, and application launcher.

The product is an agent the user can direct in natural language, running on top of an ordinary XFCE desktop that stays exactly where it is. The user still has their windows, their file manager, their browser. SynapseOS is the layer they talk to when they want the machine to *do* something — the same shape as an agentic coding assistant, generalised from a code repository to the whole machine.

**Why:** the previous framing promised the destruction of a working desktop in exchange for an interface that cannot yet do visual tasks at all. That is a bad trade for a user and an unnecessary one for the research: the thesis question is whether *conversation is a better way to direct a computer*, and that is answerable with the agent layered on rather than substituted in. It also removes an enormous amount of unbuilt scope — session manager, application launcher, desktop shell — none of which was ever the contribution.

**What it changes:** M8 collapses from "build a GUI desktop environment" to "launch the existing TUI fullscreen as an XFCE session, with the participant-accessible fallback already specified." The confirmation gate, the undo journal, and the execution loop are untouched — they were always the substance.

**Rejected:** full session replacement (D19/D20's original reading). It was chosen when the project imagined the interface as the whole environment; once the wedge narrowed to shell-expressible operations (D7), replacing the graphical session meant removing capabilities the system cannot provide substitutes for.

---

### D28 — Linux/XFCE is a methodological necessity, not the contribution

**Status:** Decided 2026-09-12 (Session 31). Supersedes the second of Chapter 1's three research gaps ("no Linux desktop coverage").

The prototype targets Debian 13 with XFCE because an agent layer that observes and acts on a desktop session requires a substrate that permits it. Windows and macOS are proprietary and do not. The platform is where the experiment is *possible*, not what the experiment is *about*.

**Why:** "no one has done this on Linux" is a platform-coverage claim, and platform-coverage claims are the weakest kind of contribution and the fastest to expire — a single shipped product invalidates them, and several already have (see `drift.md`, agentic-OS landscape). Necessity is a permanent argument: it cannot be falsified by someone else shipping a Linux agent, because it was never a novelty claim. It also states the real constraint honestly rather than dressing a limitation as a finding.

**What it changes:** Gap 2 in Chapter 1 becomes a scope-and-feasibility justification rather than a gap. Generalisation to Windows and macOS stays a limitation in §3.5, which is where it belonged.

---

### D29 — The thesis's algorithmic contribution is recoverability analysis of generated shell commands

**Status:** Decided 2026-09-12 (Session 31), in response to adviser feedback that the project has no identified algorithmic contribution. **Revised 2026-09-14** — scope widened from one algorithm to four decision procedures over a shared formal object; see the revision note below. Specification now in `algorithms.md` (moved there 2026-09-13; `safety-model.md` keeps the taxonomy and is the baseline the work must beat).

The problem: *given an arbitrary shell command produced by a language model, decide whether its effects are recoverable, and where they are not, compute the minimal set of pre-images sufficient to restore the prior state.*

**Revision, 2026-09-14 — the formal model is now stated explicitly; the scope is unchanged.**

A second adviser review asked six questions of the proposal: what specific algorithm, what formal model supports it, how it differs from existing command filters, against which baselines, which measurable results would show it is better, and whether any contribution survives if the conversational interface is removed. The last was answered "probably no" for the manuscript as written, and that reading was correct — the proposal introduced this decision's algorithm as a *fourth* research question appended to three HCI ones. D32 records the restructure that answers it.

What this decision gains is one thing only: the **effect semantics** underlying the algorithm — a shell AST mapped to a set of filesystem effects that compose through the operator structure — is now written down as a named formal model in `algorithms.md` rather than left implicit inside the approach. That was a genuine gap, and "what formal model supports it" is a fair question that the entry previously could not answer crisply.

**What this decision does not gain: scope.** The contribution remains the verdict plus the minimal recovery plan, exactly as decided on 2026-09-12.

On 2026-09-14 this entry was briefly revised to widen the contribution to *four* decision procedures over the shared semantics, promoting risk-tiered gating and termination policy out of the rejection list below. That revision was withdrawn the same day. The stated argument — that neither is standalone once an effect set exists to define it over — is not wrong, but it was not what drove the change: four procedures answer "what specific algorithm are you proposing?" more impressively than one, and nothing the project had actually learned overturned the original judgements of "too thin alone" and "not a contribution on its own". Widening a contribution under review pressure, using reasoning assembled after the fact, is the failure this log exists to make visible rather than to hide. The rejections below stand unchanged.

**Rejected, with reasons.** *Pre-execution verification* (does the command do what was asked?) — strong, and supported by reference [27]'s finding that verification is what makes CLI agents outperform GUI agents, but nothing of it is built and its evaluation entangles with model quality. *Failure recovery and reformulation* — a real defect (the loop repeats an identical failing command to the step cap), but with a 3B model it is impossible to separate the algorithm's contribution from the model's ceiling, and a panel will ask. *Context compression*, *risk-tiered gating*, *termination policy* — each too thin to carry a thesis alone; the first two remain available as extensions.

**Narrowed 2026-09-20 — see D34.** The contribution is now composition through wrappers and resolution of run-time targets feeding a minimal capture plan, not verdicts for every command. Everything above stands as the record of how the contribution was first chosen.

---

### D30 — Generated commands stay visible to the user

**Status:** Decided 2026-09-12 (Session 31). Corrects a claim in Chapter 3's Conceptual Framework.

The paper currently states that "the shell command is an implementation detail invisible to the user." It is not, and it must not be.

**Why:** the confirmation gate is the thesis's central safety claim and RQ2's entire subject. A user cannot meaningfully approve what they cannot see — an invisible-command design degrades the gate to "may I do something?", which is not consent. Visibility is also what makes intent-parsing accuracy observable to the participant, and it matches the reference product class: agentic coding assistants show every command and block on the dangerous ones.

**What it changes:** the sentence comes out of the Conceptual Framework. Nothing in the runtime changes, because the runtime never implemented the claim.

---

### D31 — Results are reported in natural language, not dumped as raw output

**Status:** Decided 2026-09-12 (Session 31). Closes a gap between `vision.md`'s stated product and the prototype's behaviour.

After a command executes, the model turns its output into a sentence that answers what was asked — "there are four files in logs", "the largest is `big.bin` at 200 KB". Raw stdout and the exit code remain available and are still logged verbatim for the study's telemetry; the *reply* is the answer, not the transcript.

**Why:** `vision.md` defines the product as a dialogue and names "why is my laptop slow right now" as a complete instruction — a question expecting an answer. Chapter 3 already claims the user "receives results in natural language." Neither was true: the prototype printed `4` and `exit code: 0`. This is the single largest gap between what the project says it is and what it does, and it is why the interface reads as unresponsive on first contact even when it has executed correctly.

**Scope boundary, deliberate:** this makes the system answer *about the machine and about what it did*. It does not make it a general conversational assistant — the model is never asked to answer from its own knowledge instead of from command output. Capability questions ("what can you do") are answered locally, without invoking the model, for the same reason `context` and `clear` are.

**Amended 2026-09-15 — the local answers described above did not exist until now.** The sentence about capability questions being answered locally described an intention, not the code: no handler was written, so greetings and capability questions fell through to the translation loop. The model, asked to turn "hello" into a shell command, correctly concluded it could not and emitted `UNSUPPORTED` — and the user was told their greeting was a visual task like editing images. Found in live testing, and it is the first thing any user types.

`answerConversational` now handles greetings, capability questions, and thanks locally and deterministically. Matching is exact on normalised input rather than by prefix, because the dangerous direction is swallowing work that opened politely: "hi" is a greeting, "hi, delete the logs" is a task.

**Still out of scope, and now measured rather than asserted:** open-ended conversation ("are you human?", "tell me a joke") still falls through. It now reaches a message that does not claim to know why it failed, which is an improvement, but it is not conversation. Whether to support it is open — `open-problems.md` row 18 — and it interacts with the tool-calling protocol in row 17, where a model that has nothing to call simply replies in prose and the question resolves itself.

---

### D32 — The algorithm becomes RQ1; the two contributions are independent, not subordinate

**Status:** Decided 2026-09-14 (Session 33), after a second adviser review. Supersedes the research-question ordering the manuscript has carried since Session 16.

The recoverability algorithm (D29) becomes **RQ1**, ahead of the three interface questions, and its evaluation moves to §3.1 ahead of the user study. The algorithm and the user study are presented as **two independent contributions of different kinds** — one algorithmic, evaluated against a labelled corpus; one empirical, evaluated through participants — reported separately because they are established separately.

**Why.** The test the reviewer applied was: *if the conversational interface were removed, would a research contribution remain?* For the manuscript as written the answer was no, and that was a fair reading of it rather than a misreading. It was not, however, a fair reading of the project — `algorithms.md` and D29 had already made recoverability analysis a contribution. The paper had simply not caught up with its own documents, which is the failure mode `README.md`'s tier rule exists to prevent: when a chapter and a doc disagree, the doc is right and the chapter is stale by definition.

So this is not a change of research direction. Nothing in `algorithms.md`, `build-order.md`, or the runtime changes because of it. What changes is which claim the prose leads with.

**The scope limit, which is the point of this entry.** An earlier draft of this decision went further: it described the conversational interface as "the application domain and evaluation setting for the algorithm". That was withdrawn. The reviewer's question was whether *a* contribution survives without the interface — answering "yes, RQ1 does" never required subordinating everything else, and the interface is not a setting: it is three of the four research questions, the entire 40-participant study, and the substantial majority of Chapter 3 and the appendices. Demoting it in the prose would have misdescribed the thesis in order to sound more like a computer-science thesis, which is a worse failure than the one being corrected.

It is also not a concession that interaction-model research would be invalid. HCI theses are legitimate and this project's interface work is real. It is a judgement that the CS-department bar is the operative one here, and that the project clears it without giving up anything it wanted, because the analyser genuinely does not depend on the interface: it takes a command as a string, and its evaluation is corpus-based with no participants.

**What changes in the manuscript.**

- RQ order: the recoverability question becomes RQ1; the three interface questions follow as RQ2–RQ4.
- The abstract names the algorithm as the technical contribution while still opening on the problem the interface addresses.
- §3.5 (algorithm evaluation) becomes §3.1, ahead of the user-study design, and needs to grow: two pages is not proportionate to a primary contribution, and that thinness is a real outstanding problem rather than a formatting one.
- The two evaluations are stated as independent: the corpus study answers RQ1 without participants, the user study answers RQ2–RQ4 and waits on IRB.

**Cost, accepted knowingly.** This is the most expensive tier in the repo — re-export, hardcoded TOC page-number re-derivation, ITRD compliance recheck. It is justified only because the idea being rendered was settled in `algorithms.md` first, so the chapters present finished work rather than thinking on the page.

---

### D33 — File manipulation runs on typed operations; the shell handles the remainder

**Status:** Decided 2026-09-20. Resolves the decision half of F6 (`prototype/build-order.md`); the implementation half is still open.

File-manipulation tasks — find, move, delete, rename, copy, the five operations in `internal/typedops` — are dispatched as typed calls. Everything else continues through generated bash under the existing classifier and undo journal. Two tracks, chosen deliberately rather than inherited.

**Why.**

- **The measurement.** F4 put raw bash at 80% call validity and 60% task success on the 3B model, against 100% and 100% for typed operations on the same tasks.
- **Independent convergence.** Claude Code and Aider both split the work the same way: typed tools for file edits, with recovery attached, and a shell for the remainder, with none (`prior-art.md`, recovery-coverage entry; from published documentation, not source). Adopting a design that shipped tools already converged on is what the adopt-by-default rule (`vision.md`) asks for.
- **A cleaner scope for the algorithm.** A typed call names its targets on its face, so it needs no static analysis. What is left for D29's algorithm is the shell: chains, redirects, substitution, package and process commands. That is a narrower claim than "one algorithm covers everything", and a more defensible one.

**Rejected.** Staying on raw bash for everything: it leaves the reliability gap F4 measured in place and has no support from what comparable tools do. Adopting MCP wholesale: the protocol assumes a server process, and D8 fixes a single local binary; the pattern transfers without the plumbing, as F4's implementation already showed.

**What this obliges, so that it is not a silent split.**

- Chapter 3 must state plainly that file-manipulation tasks are dispatched through typed operations while the other categories use generated bash. F4's numbers make the headline results better for one category, and reporting them as the system's results without saying so would misdescribe what was measured.
- The confirmation gate and undo journal must apply to typed calls as they do to bash. That is a requirement on F6's implementation; this entry does not specify how.
- A1's corpus must be checked against the narrower role: the algorithm's evaluation takes a command as a string and does not depend on how the runtime dispatches, so the corpus stays valid, but it should still contain enough decidable, non-trivial shell commands once file-manipulation shapes are set aside (`prior-art.md`, typed-operations entry, "the honest test").

**Amended 2026-09-20.** D34 narrows the algorithm's claim further. Package, service, and process commands, which the paragraph above lists among what is left for the algorithm, are outside the effect model and always ask; what the algorithm claims is composition through wrappers and target resolution for filesystem effects. The typed-operation decision itself is unchanged and its implementation is still open.

---

### D34 — The algorithm is narrowed to composition and target resolution; the verdict alone is not the contribution

**Status:** Decided 2026-09-20. Narrows D29; supersedes the framing of D29's problem statement ("decide whether the effects of an arbitrary generated command are recoverable") and of `algorithms.md` Entry 1 as originally written.

The recoverability algorithm claims two abilities, and only these:

- **Composition through wrappers.** A generated line is analysed as the commands that actually mutate something, seen through `find -exec`, `xargs`, `for` loops, command and process substitution, pipelines, redirects, `sh -c`, and `sudo` and its relatives, with earlier parts of the line visible to later ones.
- **Resolution of run-time targets.** Where a target is computed while the command runs (`rm $(ls *.log)`, `find … -delete`, `xargs rm`, `git clean -f`, `tar -x`, `rsync --delete`), the analysis learns the concrete targets by running a form of that command that cannot change anything, so that a capture plan covering exactly those targets can be made before the command runs.

Single named commands with explicit targets stay with the list, which handled them in the pilot. A command the analysis does not model is **unknown, and unknown asks**: it fails closed, and it captures nothing.

**Why.** D29 was chosen in response to adviser feedback, and its premise — that a hand-kept list is structurally inadequate — was argued after the choice, not tested before it. The pilot in `algorithms.md` ("Pilot — is a list already enough?", rounds 1, 2, and 2b) tested it. Its result, stated as the design record states it: a list flipped to fail closed accounts for the whole *verdict* advantage over the current classifier on the pilot data, so the verdict is not the contribution. What a fail-closed list cannot do is protect anything: it asks about everything it does not know and captures nothing, and it cannot tell a rename or a new-file redirect from a deletion, so it asks more often. The effect analysis captured the targets it resolved and asked less. The measured advantage is capture coverage and lower friction, and both trace to the two abilities above. That evidence is a pilot: one annotator, few dangerous commands in the realistic sample, fixtures written after the round-2 results were seen. A held-out round (a fresh sample, labelled and given fixtures before the analyser is run, with the analyser frozen) is the result that would carry weight, and it is pending.

**What changes.**

- **RQ1** is reworded from determining recoverability "more accurately than an enumerated list" to whether composition-aware analysis with target resolution protects more of what a generated command destroys, and asks less often, than a list and than a list flipped to fail closed.
- **Algorithm 1** gains a RESOLVE step: before a wrapper's targets are declared unresolved, a read-only dry run is attempted, and it is run only if the same analysis proves it read-only. It also gains an overlay of what earlier parts of a line created or removed, and a MovedTo refinement so a rename or move needs no capture (not compression: the harness showed moving compressed bytes back is not an undo).
- **Primary metrics** become silent loss and capture coverage (the share of recoverable-with-capture commands whose plan actually restores the prior state, verified by executing in a sandbox and diffing). False-negative reporting is kept.
- **Baselines** become three: the current list (L0), a list flipped to fail closed (L1), and the effect analysis (ALG). Two primary comparisons follow, so the statistics gain a Holm correction.
- **The corpus** is stratified by command shape as well as by effect kind, and its ground truth is defined relative to a stated filesystem state per command (a fixture). The pilot showed that a state-aware analysis cannot be scored against state-free labels.

**Rejected.**

- *Keeping the broad claim.* The pilot did not support it, and a claim the evidence does not support is the failure D29's own revision note describes.
- *A language model as the analyser.* It is probabilistic and cannot give the one-sided soundness the design rests on: a wrong "safe" answer loses data. The project's own logs show this model stating confident falsehoods (`open-problems.md` rows 15 and 20).
- *Hardening the list only.* It cannot resolve run-time targets, so it cannot capture them, and the pilot's largest gap was exactly the deletions the list asks about and then protects nothing for. It also remains the baseline and the fallback, which is where it belongs.

**A required limit, stated wherever the algorithm is described.** The analyser's rule table is itself a list: a per-command table of effects and flags. The claim is what is built on top of it — composition, resolution, and the capture plan — and that unknown commands fail closed where a list fails open. It is not a claim to have eliminated enumeration.

**Still open, in `open-problems.md`.** The default gate policy (ask on every capturable command, or capture silently and ask only when unrecoverable), the ground-truth definition for the corpus, the second annotator, and whether resolution runs when no read-only sandbox is available.

### D35 — The gate is default-on and strict; package and service state is modelled; ground truth is automated; dependencies are hoarded

**Status:** Decided 2026-09-20. Resolves the eight owner decisions (C1–C8) left open by D34.

- **C1, gate policy.** The effect analysis is on by default in *strict* mode (ask unless recoverable with no capture). `SYNAPSE_ANALYSIS=capture` captures silently and asks only when unrecoverable; `off` restores the list alone. The list is still consulted, so the analysis can add a confirmation and never remove one.
- **C2, ground truth.** State-relative: every corpus item carries its fixture and the analysis is run against that same fixture.
- **C3, labelling.** No human labellers. Ground truth is produced by executing each command in a bubblewrap sandbox on its fixture and diffing the tree (`internal/oracle`, `cmd/corpusgen`). Commands that do not run cleanly are excluded and counted. Commands whose effect the filesystem cannot show form an external partition, never executed and labelled unrecoverable by construction. The generator shares no code with `internal/effects`.
- **C4, RQ1 wording.** Accepted as revised in the paper, with the narrowing disclosed to the adviser (D34).
- **C5, resolution without a sandbox.** Resolution stays on only where bubblewrap works; without it, anything needing resolution asks. Bubblewrap is a stated requirement of the evaluation and study machines.
- **C6, package and service state.** Not left as a limitation. The analysis asks the system's own tools (`apt-get -s`, `dpkg-query`, `apt-cache policy`, `systemctl is-active` and `is-enabled`) what a command would do and derives an inverse command; the undo journal stores and runs it (`undo.Entry.Inverses`, run after trash and before content restore, with privilege if the original had it, printing the command if it fails). Removing a package whose old version is no longer offered is unrecoverable; purging captures the configuration files as removals. Maintainer scripts and other package managers stay outside the model.
- **C7, editors and pagers.** Treated as read-only interactive tools, as before.
- **C8, NL2Bash.** Cited as Lin et al. (LREC 2018), reference [51]; the raw dataset is GPL-3.0 and git-ignored, only sampled commands are kept.

**Dependencies.** The distribution will be a bootable image, so every dependency is named in `distro/manifest.tsv`, checked by `distro/check.sh`, and collected by `distro/hoard.sh` (debs with their closure, Go modules, the Go toolchain, model blobs) into the git-ignored `distro/hoard/`. Existing tools are preferred over new code throughout.

**Process.** Agility over ceremony: `make ci` (format, vet, tests) runs in GitHub Actions; the corpus is generated from a seed and regenerated in seconds of human time. The one piece of rigour kept is validity: a development corpus is used to fix the analysis and a second corpus, from a fresh seed, is run once after the rule table is frozen and is the reported result.
