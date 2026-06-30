## Overview

This file is the holding area for ideas, decisions, and additions that have surfaced during research but are not yet committed to any chapter. Nothing here is final — entries graduate to the actual thesis files once the team aligns on them. The intent is to avoid losing good ideas mid-session while also avoiding premature edits to submitted or near-submitted documents. Each entry notes which chapter or file it would affect, and a brief rationale for why it was set aside rather than applied immediately.

## Table of Contents

- [Overview](#overview)
- [Pending Ideas](#pending-ideas)
  - [Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking](#cite-ndss-last-x-2026-paper-on-nl2bash-benchmarking)
  - [~~Name Qwen2.5-Coder-3B explicitly as the primary candidate in Section 2.1a~~ ✓ DONE](#name-qwen25-coder-3b-explicitly-as-the-primary-candidate-in-section-21a-done)
  - [~~LoRA fine-tuning as a system development step~~ ✓ DONE](#lora-fine-tuning-as-a-system-development-step-done)
  - [Tier 3 opt-in install — Moondream 2 as user-configured vision backend](#tier-3-opt-in-install-moondream-2-as-user-configured-vision-backend)
  - [Chapter 1 — SLM-first, model-agnostic architecture backport](#chapter-1-slm-first-model-agnostic-architecture-backport)

## Pending Ideas

### Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking

**Would affect:** SA3.1 Section 2.1a (LLM selection rationale), References list  
**What:** "Local LLMs for NL2Bash: A Large-Scale Open-Source Evaluation" (NDSS LAST-X 2026) evaluated 22 locally-deployable LLMs from 1B to 32B parameters. It is a strong empirical anchor for why Qwen2.5-Coder-3B is the primary candidate.  
**URL:** https://www.ndss-symposium.org/wp-content/uploads/lastx2026-49.pdf  
**Blocked on:** Retrieving full author list and citation metadata from the PDF (binary, not yet extracted). Full citation needed before adding to references.

---

### ~~Name Qwen2.5-Coder-3B explicitly as the primary candidate in Section 2.1a~~ ✓ DONE

Applied in SA3.1 Section 2.1a. Architecture shifted to local-SLM-first, model-agnostic. Qwen2.5-Coder-3B-Instruct named as intent parsing candidate; Tier 3 vision model left as "small VLM to be determined during development."

---

### ~~LoRA fine-tuning as a system development step~~ ✓ DONE

LoRA fine-tuning now appears in Section 2.1a as part of the SLM configuration description. May still be worth adding as a numbered step in Section 2.3 Validation — deferred to Thesis 1.

---

### Tier 3 opt-in install — Moondream 2 as user-configured vision backend

**Would affect:** Thesis 1 implementation chapter (system design), installer/setup script  
**What:** Tier 3 (vision-based GUI simulation) should be opt-in at installation time, not bundled by default. Installer prompts: "Enable vision fallback (Tier 3)? Requires an additional ~2 GB — downloads Moondream 2." Users on tight hardware skip it; full-coverage users opt in. Default install is Qwen2.5-Coder-3B only (~1.8 GB), fully functional for Tier 1 and Tier 2.

Model summary:
- Default: Qwen2.5-Coder-3B-Instruct — intent parsing, NL → bash (~1.8 GB Q4)
- Optional (Tier 3): Moondream 2 — screenshot → UI element grounding (~2 GB, ScreenSpot F1@0.5 = 80.4)
- Combined if both enabled: ~3.8 GB — feasible on any 8 GB RAM machine

**Why not Qwen2.5-VL-3B for both roles:** VL variant adds a vision encoder making it effectively ~3.7–4B parameters (~2.5 GB Q4) — larger than Coder-3B and weaker at bash generation (Coder was trained on 5.5T code tokens). Two specialized small models beats one larger generalist.

**Blocked on:** Thesis 1 implementation phase. Methodology chapter text already accommodates this — "small open-weights VLM evaluated during development" covers Moondream without naming it prematurely.

---

### Chapter 1 — SLM-first, model-agnostic architecture backport

**Would affect:** SA1 (Chapter 1, already submitted)  
**What:** The architecture decision has shifted significantly since Chapter 1 was written. Three changes needed when Chapter 1 is revised:
  1. **5th specific objective:** "To design and integrate a privacy-preserving, locally-hosted SLM backend with a model-agnostic interface, enabling SynapseOS to operate fully offline without transmitting user data to external services."
  2. **Remove limitation:** "LLM not yet committed" — this is now resolved. Replace with: accuracy ceiling of the 3B SLM relative to frontier alternatives at Tier 3 vision grounding.
  3. **Update scope:** Mention that cloud inference is opt-in, not a design dependency.
**Blocked on:** SA1 is already submitted. Apply when Chapter 1 is revised for Thesis 1.
