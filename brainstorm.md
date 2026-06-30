## Overview

This file is the holding area for ideas, decisions, and additions that have surfaced during research but are not yet committed to any chapter. Nothing here is final — entries graduate to the actual thesis files once the team aligns on them. The intent is to avoid losing good ideas mid-session while also avoiding premature edits to submitted or near-submitted documents. Each entry notes which chapter or file it would affect, and a brief rationale for why it was set aside rather than applied immediately.

## Table of Contents

- [Overview](#overview)
- [Pending Ideas](#pending-ideas)
  - [Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking](#cite-ndss-last-x-2026-paper-on-nl2bash-benchmarking)
  - [Name Qwen2.5-Coder-3B explicitly as the primary candidate in Section 2.1a](#name-qwen25-coder-3b-explicitly-as-the-primary-candidate-in-section-21a)
  - [LoRA fine-tuning as a system development step](#lora-fine-tuning-as-a-system-development-step)
  - [Chapter 1 5th specific objective — LLM selection for offline operation](#chapter-1-5th-specific-objective-llm-selection-for-offline-operation)
  - [Remove "LLM not yet committed" from Chapter 1 limitations](#remove-llm-not-yet-committed-from-chapter-1-limitations)

## Pending Ideas

### Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking

**Would affect:** SA3.1 Section 2.1a (LLM selection rationale), References list  
**What:** "Local LLMs for NL2Bash: A Large-Scale Open-Source Evaluation" (NDSS LAST-X 2026) evaluated 22 locally-deployable LLMs from 1B to 32B parameters. It is a strong empirical anchor for why Qwen2.5-Coder-3B is the primary candidate.  
**URL:** https://www.ndss-symposium.org/wp-content/uploads/lastx2026-49.pdf  
**Blocked on:** Retrieving full author list and citation metadata from the PDF (binary, not yet extracted). Full citation needed before adding to references.

---

### Name Qwen2.5-Coder-3B explicitly as the primary candidate in Section 2.1a

**Would affect:** SA3.1 Section 2.1a (Conversational Interface Layer), Table 2.1 (LLM Backend row note)  
**What:** Currently Section 2.1a says "smallest model variant determined via validation." Empirical data (Westenfelder et al. [2], NDSS LAST-X [B]) already points to Qwen2.5-Coder-3B as the primary candidate — 26% baseline, 58% with prompting, best sub-7B on NL2Bash. The section should name it and cite [2].  
**Blocked on:** Team agreement on committing to this model before it appears in the methodology text. Also depends on resolving the NDSS citation above.

---

### LoRA fine-tuning as a system development step

**Would affect:** SA3.1 Section 2.3 (System Development / Validation procedure)  
**What:** Fine-tuning Qwen2.5-Coder-3B on NL2Bash + InterCode dataset via LoRA is a concrete, feasible plan (see `references/llm-selection-research.txt`). This should appear as a numbered step in the system development procedure, not just implied by the LLM selection criteria.  
**Blocked on:** Same as above — model name commitment. Also unclear whether fine-tuning is in scope for Thesis 1 or deferred to Thesis 2.

---

### Chapter 1 5th specific objective — LLM selection for offline operation

**Would affect:** SA1 (Chapter 1, already submitted), Specific Objectives section  
**What:** Add: "To identify and integrate the smallest locally-hostable open-weights multimodal LLM that achieves acceptable intent parsing accuracy and vision grounding performance, enabling SynapseOS to operate fully offline without transmitting user data to external services."  
**Blocked on:** SA1 is already submitted. This is a backport — apply when Chapter 1 is revised for Thesis 1.

---

### Remove "LLM not yet committed" from Chapter 1 limitations

**Would affect:** SA1 (Chapter 1), Scope and Limitations section  
**What:** The limitation currently says the choice between hosted and local LLM is undecided. That decision is now made: locally-hosted open-weights model. The limitation should be replaced with a more honest one (e.g., accuracy ceiling of the chosen small model vs. frontier alternatives).  
**Blocked on:** Same as above — SA1 revision in Thesis 1.
