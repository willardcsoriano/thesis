## Overview

This file records architectural and research design decisions that have been made and are no longer under discussion. Each entry states what was decided, why, and what was explicitly rejected so the reasoning is not lost. Decisions that are already reflected in a submitted chapter are noted as such. Decisions not yet written into a chapter (implementation-phase) are noted so they can be pulled into Thesis 1 without revisiting the rationale.

## Table of Contents

- [Overview](#overview)
- [Decisions](#decisions)
  - [D1 — Deployment target: Debian 13 (Trixie)](#d1-deployment-target-debian-13-trixie)
  - [D2 — Local-SLM-first, model-agnostic inference backend](#d2-local-slm-first-model-agnostic-inference-backend)
  - [D3 — Intent parsing model: Qwen2.5-Coder-3B-Instruct](#d3-intent-parsing-model-qwen25-coder-3b-instruct)
  - [D4 — Tier 3 vision model: Moondream 2, opt-in at install time](#d4-tier-3-vision-model-moondream-2-opt-in-at-install-time)
  - [D5 — Within-subjects user study design, 20 participants](#d5-within-subjects-user-study-design-20-participants)
  - [D6 — Statistical analysis approach](#d6-statistical-analysis-approach)

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

### D4 — Tier 3 vision model: Moondream 2, opt-in at install time

**Status:** Decided. Not yet in any chapter — graduates to Thesis 1 system design chapter.

Tier 3 (vision-based GUI simulation — screenshot → UI element grounding) uses Moondream 2 (1.9B parameters, ~2 GB, ScreenSpot F1@0.5 = 80.4) as the vision model. Moondream 2 is the smallest model with verified, meaningful GUI grounding accuracy on the ScreenSpot benchmark.

Tier 3 is **opt-in at installation time.** The installer prompts:

> "Enable vision fallback (Tier 3)? Downloads Moondream 2, requires ~2 GB additional storage and memory."

Users who decline get a fully functional system on Tier 1 and Tier 2 only (~1.8 GB total). Users who opt in get full three-tier coverage (~3.8 GB total, feasible on any 8 GB RAM machine). Since Tier 3 is the last-resort fallback — fires only when both Tier 1 (MCP) and Tier 2 (AT-SPI) fail — most interactions are unaffected by whether it is installed.

**Rejected:** Bundling Moondream 2 by default. Adds ~2 GB for a component that may never be invoked in most sessions.

**Rejected:** Qwen2.5-VL-3B as a single unified model for both roles. See D3.

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
