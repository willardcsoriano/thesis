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

## Decisions

### D1 — Deployment target: Debian 13 (Trixie)

**Status:** In SA3.1 (Table 2.1, Section 1.2, throughout)

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

**Rejected:** Qwen2.5-VL-3B-Instruct as a unified model for both intent parsing and Tier 3 vision. The VL variant adds a vision encoder (~0.7–1B additional parameters), making it effectively ~3.7–4B total at ~2.5 GB Q4 — larger than Coder-3B and weaker at bash generation because Coder was trained on 5.5 trillion code tokens specifically.

---

### D4 — Vision model and GUI tiers: OUT OF SCOPE

**Status:** Superseded by D7. Not in any chapter.

Moondream 2 (Tier 3 vision fallback) and AT-SPI accessibility tree automation (Tier 2) were evaluated and explicitly removed from the architecture. The three-tier hybrid execution model (Tier 1 MCP, Tier 2 AT-SPI, Tier 3 Vision) has been replaced by a CLI-only execution pipeline. See D7.

Research data on Moondream 2 (1.9B params, ScreenSpot F1@0.5 = 80.4) and Qwen2.5-VL-3B is preserved in `research methods/module 3/references/llm-selection-research.txt` for future reference.

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

**Status:** In SA3.1 Table 2.1

**Go** owns the runtime — everything the user touches during the study: the TUI session manager (bubbletea + lipgloss), bash subprocess execution and stdout/stderr streaming, Ollama API client (HTTP streaming to localhost:11434), confirmation gate, and undo log.

**Python 3.12** owns the build pipeline — everything that runs offline before the study: LoRA fine-tuning (Unsloth / PEFT), dataset preparation (NL2Bash corpus + custom task suite), and statistical evaluation scripts (pandas, scipy).

**Ollama** serves the model at runtime — it manages model lifecycle, quantization, and exposes a REST API that both Go (at runtime) and Python (during evaluation) can call identically. This decouples the application from the inference engine and makes the cloud opt-in straightforward (swap the Ollama endpoint for an OpenAI-compatible API).

**Why this split and not pure Python:** Go produces a single binary with no dependency hell, goroutines are a natural fit for streaming model output token-by-token into the TUI, and memory overhead is ~15 MB vs ~80–100 MB for the Python interpreter + deps. Python is kept only where it has no peer — the ML fine-tuning ecosystem (Transformers, PEFT, Unsloth) is Python-only.

**Rejected:** Pure Python runtime. Textual is good but bubbletea is better for this use case, and asyncio subprocess streaming is more complex than goroutines. Rejected: Go for fine-tuning — no viable Go ML training ecosystem exists.

---

### D9 — Study baseline: expert baseline design (participant's primary OS)

**Status:** In SA3.1 Section 3.2

Condition B is each participant's primary OS — Windows 11, macOS, or Linux with GNOME — on a dedicated baseline machine in the lab. Study uses four physical machines: one SynapseOS (Debian 13), one Windows 11, one macOS, one Linux (GNOME). Each participant uses whichever baseline machine matches their daily driver.

**Why:** A fixed Linux desktop baseline (originally GNOME on Debian) would confound the result. Most participants have never used a Linux desktop; slow performance under that condition reflects OS unfamiliarity, not interface quality. The expert baseline design tests SynapseOS against what participants already know — a harder and more ecologically valid opponent. A positive result against a participant's home environment is a stronger claim than winning against a foreign system.

**Rejected:** Single fixed Linux baseline (GNOME/Debian) — confounds interface with OS familiarity. Three-OS between-subjects design — requires much larger n and loses within-subjects control. macOS-only or Windows-only baseline — excludes participants whose native platform differs.

---

### D10 — Conversation memory model: session-scoped, rolling window, output compression

**Status:** Reflected in SA3.1 Section 2.1; scope.md session context manager item

SynapseOS uses session-scoped in-memory conversation context. History lives in a Go slice of message structs for the duration of one login session and is cleared on logout or reboot. No persistence layer is required for the thesis prototype.

Context overflow is handled by a rolling window: when accumulated history approaches 75% of the 8K token limit (Qwen2.5-Coder-3B-Instruct hard ceiling), the oldest turns are dropped until the context fits within the budget. Bash command output is truncated before being appended to history — verbose raw output (full `ls -la`, `find` result dumps) is never stored at full length, only a compact result note. This prevents a single verbose command from consuming a disproportionate share of the token budget.

**Why:** Session-scoped memory eliminates the storage, privacy, and context-management complexity of persistent history entirely and keeps the evaluation environment clean and reproducible across participants. Rolling window is simpler than conversational summarization and sufficient at the 3B parameter scale. Output compression is necessary because a single `find /` result can run to thousands of tokens and would crowd out all prior context.

**Rejected:** Pure stateless (zero memory) — breaks pronoun resolution and multi-turn follow-up commands ("move it to Downloads" requires knowing what "it" was). Persistent memory across reboots — adds SQLite storage, session boundary logic, and history deletion UX; deferred to future-features.md. Conversational summarization — adds a second inference call per compression event; more complex than rolling window and not warranted for thesis scope.

---

### D11 — Study interface mode: GUI (fullscreen conversational interface)

**Status:** Reflected in SA3.1 Table 2.1, paragraph after Table 2.2, and Section 3.5

The user study evaluates SynapseOS in GUI mode — a fullscreen conversational interface running on a graphical desktop environment. This is the primary product target for general and non-technical users. The TUI mode (terminal-based, no display server required) is the server and remote deployment target and is not evaluated in the user study.

The thesis prototype GUI is a fullscreen borderless window approximating the active desktop aesthetic; the full wallpaper-layer active desktop (wlr-layer-shell integration) remains deferred to future-features.md.

**Why:** The study population includes novice users for whom a terminal interface would introduce a significant familiarity barrier independent of SynapseOS's core capability. Evaluating in GUI mode tests the interface as it would be experienced by its intended general-user audience. A TUI-mode study of novice users would conflate terminal unfamiliarity with interface quality, undermining the validity of the comparison.

**Rejected:** TUI mode for the study — the Interface mode threat to validity, previously listed in Section 3.5, identified exactly this problem: novice user results in TUI mode would not represent the experience those users would have under the intended GUI implementation.
