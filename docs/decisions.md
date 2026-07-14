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

**Status:** In SA3.1 Section 3.2

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

**Status:** In SA3.1 Table 3.1

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

**Status:** Reflected in SA3.1 Section 2.1; scope.md session context manager item

SynapseOS uses session-scoped in-memory conversation context. History lives in a Go slice of message structs for the duration of one login session and is cleared on logout or reboot. No persistence layer is required for the thesis prototype.

Context overflow is handled by a rolling window: when accumulated history approaches 75% of the 8K token limit (Qwen2.5-Coder-3B-Instruct hard ceiling), the oldest turns are dropped until the context fits within the budget. Bash command output is truncated before being appended to history — verbose raw output (full `ls -la`, `find` result dumps) is never stored at full length, only a compact result note. This prevents a single verbose command from consuming a disproportionate share of the token budget.

**Why:** Session-scoped memory eliminates the storage, privacy, and context-management complexity of persistent history entirely and keeps the evaluation environment clean and reproducible across participants. Rolling window is simpler than conversational summarization and sufficient at the 3B parameter scale. Output compression is necessary because a single `find /` result can run to thousands of tokens and would crowd out all prior context.

**Rejected:** Pure stateless (zero memory) — breaks pronoun resolution and multi-turn follow-up commands ("move it to Downloads" requires knowing what "it" was). Persistent memory across reboots — adds SQLite storage, session boundary logic, and history deletion UX; deferred to future-features.md. Conversational summarization — adds a second inference call per compression event; more complex than rolling window and not warranted for thesis scope.

---

### D11 — Study interface mode: GUI (fullscreen conversational interface)

**Status:** Reflected in SA3.1 Table 3.1, paragraph after Table 3.2, and Section 3.5

The user study evaluates SynapseOS in GUI mode — a fullscreen conversational interface running on a graphical desktop environment. This is the primary product target for general and non-technical users. The TUI mode (terminal-based, no display server required) is the server and remote deployment target and is not evaluated in the user study.

The thesis prototype GUI is a fullscreen borderless window approximating the active desktop aesthetic; the full wallpaper-layer active desktop (wlr-layer-shell integration) remains deferred to future-features.md.

**Why:** The study population includes novice users for whom a terminal interface would introduce a significant familiarity barrier independent of SynapseOS's core capability. Evaluating in GUI mode tests the interface as it would be experienced by its intended general-user audience. A TUI-mode study of novice users would conflate terminal unfamiliarity with interface quality, undermining the validity of the comparison.

**Rejected:** TUI mode for the study — the Interface mode threat to validity, previously listed in Section 3.5, identified exactly this problem: novice user results in TUI mode would not represent the experience those users would have under the intended GUI implementation.

---

### D12 — Distro identity vs. implementation substrate: Debian + XFCE (GUI), bare Debian (TUI)

**Status:** Refines D1 and D11. Not yet in a chapter — record for final compilation. GUI mode's "no escape hatch" claim below is refined by D20 (participant-accessible fallback, logged and excluded from primary analysis).

SynapseOS presents to the user as its own distribution — its own name and identity — while the substrate underneath is reused and never surfaced to the user. The substrate is mode-dependent:

- **GUI mode:** Debian 13 + **XFCE** as the host desktop environment, running its stable **X11** session (XFCE's Wayland session remains experimental as of 4.20 — unsuitable for a reproducible 20-participant study). XFCE supplies the display session and window management only; SynapseOS launches fullscreen within it, so the participant sees and interacts with nothing but the conversational interface. XFCE is chosen over a custom kiosk compositor because it is the lowest-effort, most standard traditional Linux DE — no new toolchain (no `cage`, no Wayland layer-shell work) is needed for the thesis prototype, while still being a genuine, vendor-neutral "traditional GUI environment" for D9's expert-baseline comparison.
- **TUI mode:** bare Debian 13, no desktop environment — unchanged from D1. Here SynapseOS is a local agentic shell: natural language in, a local SLM reasons over intent, proposes a shell command, and executes it under the confirmation gate. The interaction model is architecturally comparable to Claude Code's agentic CLI loop, but fully offline against an on-device model (D2, D3) rather than a cloud API — a useful reference point for describing the interaction model to a reader unfamiliar with OS-level agents.

**Why this is not dishonest:** distro identity has always been a branding and packaging layer over a reused base, invisible to the end user — Ubuntu never surfaces "Debian" to a desktop user, SteamOS never surfaces "Arch," Pop!_OS never surfaces "Ubuntu." SynapseOS qualifies as a distribution once packaged as a bootable Debian(+XFCE) derivative with its own default session, branding, and update channel (see `layers.md`, "When It Becomes a Distribution") — a real distro, with a reused and unadvertised substrate, exactly like its predecessors.

**Rejected:** a custom kiosk Wayland compositor (`cage`) as the GUI host for the thesis prototype — adds a new toolchain and packaging surface for no benefit over a fullscreen window in a standard DE session; the wallpaper-layer / `wlr-layer-shell` active-desktop mode remains the eventual post-thesis product target (`future-features.md`). **Amended by D20:** this decision originally also rejected any participant path back to XFCE during Condition A, on the grounds that it would confound the within-subjects comparison (D5, D9). D20 reopens this — a participant-accessible fallback now exists — but keeps the comparison clean by scoring and excluding fallback-invoked tasks rather than by denying access. See D20 for the full reasoning.

---

### D13 — Product mode (post-thesis): agentic overlay on a visible traditional desktop

**Status:** Post-thesis product direction. Does not affect the study design (D5, D9, D11) or D12. Not in any chapter — vision/roadmap only.

Beyond the thesis prototype, the commercial product target is a second mode where the traditional desktop (Debian + XFCE) stays fully visible and usable, and SynapseOS is summoned on demand — a global hotkey (via XFCE's `xfconf` keyboard-shortcut settings) or a systray icon opens a floating conversational window; the user can otherwise operate the desktop manually (drag-and-drop, the file manager, any traditional app) exactly as before. This is a lower-effort near-term instantiation of the wallpaper-layer active-desktop concept already recorded in `future-features.md` — the same idea, without requiring `wlr-layer-shell`/Tauri: XDG autostart plus a hotkey/systray launcher is sufficient.

This does not conflict with D12: D12 describes the *study* substrate (fullscreen takeover, no escape hatch, required for a clean within-subjects comparison); D13 describes the *product* substrate (full coexistence, by design). They are different modes of the same runtime — same Go binary, same Ollama backend, same confirmation gate and execution engine (M2–M8) — the only difference is the shell wrapped around it, matching the existing TUI/GUI "one slot, two sets of clothes" pattern (`layers.md`).

**Why coexistence is deferred from the thesis:** allowing the participant to fall back to the traditional GUI during Condition A would confound the within-subjects comparison (D5) — a result could no longer be attributed to the conversational interface specifically. The strict takeover (D12) is what makes the study's causal claim clean; the overlay is what makes the eventual product adoptable. Building both from one runtime lets the study protect its validity while the product still ships the friction-free experience users will actually want.

**On the distro claim — weighing D12/D13 against hardening:** the default session (D12's takeover in the study; D13's overlay in the product) is the identity-defining reason SynapseOS can be called its own distribution — it is what a user actually experiences, and it is genuinely uncommon (no daily-driver OS ships a conversational agent as its primary interaction layer). Hardening (`future-features.md`, Hardening Profiles) is real and worth doing — it mirrors exactly how Ubuntu differentiates from bare Debian — but it is a commodity, credibility-class signal, not an identity-class one: nearly every serious distro hardens its defaults, so hardening alone does not distinguish SynapseOS from the crowd. If forced to choose one item to get right before the "own distro" claim is defensible, it is the agentic session (D12/D13), not the hardening profile.

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

**Status:** Bookkeeping across the Ch.1/Ch.2/Ch.3 reference lists.

The chapters draw from one shared master bibliography numbered `[1]`–`[25]`; each chapter's reference list contains only the entries that chapter cites. Number `[21]` is currently unassigned and is intentionally left vacant.

**Why:** Renumbering to close the gap would desync the already-submitted Ch.1 and Ch.3 reference lists — every in-text `[22]`–`[25]` citation would have to shift, across two submitted chapters, for a purely cosmetic gain. Leaving `[21]` reserved preserves numbering stability across chapters at zero risk. Citation integrity is verified per chapter (every cited number is defined and every defined number is cited); `[21]` is simply never cited. If a suitable source surfaces during final compilation it can occupy `[21]` without disturbing any existing number.

**Rejected:** Renumbering `[22]`–`[25]` down by one to close the hole — desyncs submitted chapters for no substantive benefit.

---

### D18 — Recruitment quota: minimum 2 participants per primary-OS background

**Status:** To be reflected in SA3.1 Section 1.2b and Section 1.5, and in the ethics application package's recruitment plan.

Recruitment for the n = 20 sample (10 novice, 10 power user) adds a floor: at least 2 participants must have each of Windows, macOS, and Linux as their primary OS. The remaining 14 participants are unconstrained by OS background.

**Why:** Without a floor, the realized sample could end up all one OS (e.g., 18 Windows / 1 macOS / 1 Linux), leaving the per-OS exploratory subgroup analysis (D15, SA3.1 Section 3.2/3.4) unable to say anything about the underrepresented OS at all. A floor of 2 guarantees every OS background has at least a minimal, non-singleton presence without materially constraining recruitment — macOS users are expected to be the scarcest population reachable through Mapúa University – Makati's general recruitment channels, and 2 is judged achievable without delaying the timeline.

**Rejected:** No quota (risks a degenerate all-one-OS sample); increasing total n to ~50+ to properly power OS-specific subgroup claims (disproportionate to the core within-subjects hypothesis, which n = 20 already satisfies per the D5 power analysis — the per-OS breakdown is explicitly exploratory, not a primary hypothesis test); a higher floor (3 or 5 per OS) — judged to risk recruitment delay for macOS specifically without a clear analytical payoff at this sample size.

---

### D19 — CLI mode formalized as a third interface mode

**Status:** Refines D11. To be reflected in SA3.1 Table 3.2 and the paragraph following it. **Execution model refined by D21** — CLI mode is still a single non-persistent invocation (no session survives between separate `synapse` calls), but a single invocation now runs a bounded multi-step loop internally rather than exactly one command; see D21.

SynapseOS ships three interface modes, not two: **CLI** (one-shot invocation — `synapse "<task>"` translates a single natural-language request into a proposed command and exits; no persistent session), **TUI** (persistent full-screen chat session, D11), and **GUI** (fullscreen conversational takeover, evaluated in the study, D11/D12). CLI mode is not new work — it is the M2 walking skeleton's existing behavior (`prototype/cmd/synapse/main.go`), promoted from a disposable stepping-stone toward M3 to a permanent, separately-named, shipped mode. It targets scripting, automation, and one-off remote invocations over SSH where a persistent interactive session is unnecessary overhead.

**Why:** M2 and M3 are genuinely different interaction shapes — one-shot request/response versus a persistent multi-turn conversation — not two maturity stages of the same feature. Collapsing CLI into "TUI without the chrome" would either force M3 to also support a non-interactive invocation path (extra branching in the TUI's own state machine) or quietly drop the one-shot use case once M3 lands, losing real utility (cron jobs, quick remote commands, shell pipelines) for no benefit. Naming it separately means M2's harness stays a permanent, useful artifact instead of throwaway scaffolding.

**Rejected:** Folding CLI into TUI as a single mode with two invocation styles — the interaction shapes are different enough (stateless vs. stateful) that conflating them in the same mode name obscures the actual distinction a user or a script author needs to reason about.

---

### D20 — GUI-mode fallback to XFCE: participant-accessible, logged, and excluded from primary analysis

**Status:** Amends D12. To be reflected in SA3.1 Table 3.1, Table 3.2, the paragraph following Table 3.2, and Section 3.5 (Threats to Validity).

GUI mode gains a fallback path: a participant can return to the underlying XFCE session — already running invisibly beneath SynapseOS's fullscreen window, per D12 — if SynapseOS becomes unresponsive or they want to stop using it mid-task. Unlike D12's original no-escape-hatch design, this fallback is participant-accessible, not facilitator-only. Every invocation is logged as a discrete telemetry event (participant ID, task ID, timestamp). Any task during which it is invoked is excluded from the primary SynapseOS-condition completion-time and error-rate comparison for that task — scored as "did not complete via SynapseOS" rather than silently counted as a success — and fallback-invocation rate is reported as its own secondary, exploratory metric (how often participants reached for it, and under which task categories).

**Why:** A participant-accessible fallback is what was asked for — a genuine safety net, not a hidden facilitator-only recovery path — but D12's original reasoning for having no escape hatch at all was sound: an accessible-and-unmeasured fallback would let a participant's own choice silently substitute for the interface being evaluated, contaminating the within-subjects comparison (D5) with no way to detect or correct for it afterward. The fix is not to restrict access but to instrument it: logging plus exclusion-from-primary-analysis means the core causal claim (task performance attributable to SynapseOS specifically) stays clean, while the fallback itself becomes an honest, reportable finding — a high fallback-invocation rate is informative data about the interface's reliability and trustworthiness, not something to hide.

**Rejected:** Facilitator-only hidden trigger, invisible to participants (the initially-recommended alternative) — cleanly preserves D12's original validity argument with zero instrumentation needed, but does not give participants direct access, which was the explicit requirement here. Leaving the fallback un-instrumented (accessible, but its use not logged or scored specially) — would silently reintroduce exactly the confound D12 was designed to prevent, with no way to detect after the fact which "SynapseOS-condition" data points were actually completed via the fallback instead.

---

### D21 — CLI-mode execution model: bounded, gated multi-step loop, not full autonomy

**Status:** Refines D19 (M2). To be reflected in SA3.1 Table 3.2 and the paragraph following it.

A single `synapse "<task>"` invocation runs a bounded loop, not exactly one command: propose a command → classify its reversibility → confirm if irreversible → execute → feed the result (stdout, stderr, exit code) back to the model, which then either proposes the next command toward the same task or signals the task is complete. Every proposed command at every step passes through the same classifier and confirmation gate individually — there is no batch approval, and no step is granted trust carried over from a prior step's confirmation. The loop ends when the model signals completion or a fixed hard step cap is reached, whichever comes first; hitting the cap is reported as an explicit "step limit reached" failure, never silently treated as success. The 8-task sample suite stays propose-only and is unaffected.

**Why:** Single-command execution left a real capability gap: some tasks genuinely require multiple distinct actions (e.g., creating destination folders before sorting files into them) or a corrected retry after a failed attempt, neither of which a single proposed command can express. A bounded loop closes that gap without adopting full autonomy, which was considered and rejected below for reasons already established in this project's own literature review, not just an engineering preference.

**Rejected:** Full autonomy — the model deciding step count and task completion unsupervised, with the confirmation gate weakened, removed, or trusted-once-then-bypassed for later steps. Two independent reasons. First, capability: Qwen2.5-Coder-3B already produced a semantically wrong command at single-shot difficulty during live validation (`build-order.md` M2 status — the `dpkg-query`/`grep` mismatch); autonomous multi-step loops additionally require the model to judge its own task completion and avoid drifting from the original intent across turns, a harder capability that degrades faster at small parameter counts, and errors compound across unsupervised steps rather than self-correcting. Second, and more fundamentally for a thesis specifically: full autonomy would recategorize what SynapseOS is being evaluated as. SA2's own literature review (Section 2.9) explicitly distinguishes curated-benchmark evaluation — which "assess[es] the autonomous task completion of agents acting on a user's behalf" — from human-centered evaluation of "the performance of a human working through an interface," and identifies the latter as the underserved gap this study fills. Full autonomy moves SynapseOS toward the former category, undermining the comparison the study is designed to make. NaSh [3] and VoicePilot [5], both already cited as motivating the confirmation gate itself, reach the same conclusion from a safety and usability angle — NaSh because unguarded LLM output "may be unintended or unexplainable," VoicePilot because its own user study with motor-impaired participants derived preview-and-confirm as a design necessity, not an option.

**Also rejected:** Retry-only-on-failure (re-attempt the same failed command with its error appended, but never propose a genuinely different next command). Simpler to implement, but too narrow — it only helps when a single correct command exists and the model merely malformed it, not when a task inherently requires several distinct actions in sequence, which is the more common shape of the capability gap being addressed here.
