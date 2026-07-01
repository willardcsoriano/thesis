## Overview

This file is the holding area for ideas and additions that have surfaced during research but are not yet actionable. Nothing here is final — entries either graduate to the thesis files directly or move to `decisions.md` once the team aligns on them. Settled architectural and design decisions live in `decisions.md`, not here.

## Table of Contents

- [Overview](#overview)
- [Pending Ideas](#pending-ideas)
  - [Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking](#cite-ndss-last-x-2026-paper-on-nl2bash-benchmarking)
  - [Chapter 1 — SLM-first, model-agnostic architecture backport](#chapter-1-slm-first-model-agnostic-architecture-backport)

## Pending Ideas

### Cite NDSS LAST-X 2026 paper on NL2Bash benchmarking

**Would affect:** SA3.1 Section 2.1a, References list  
**What:** "Local LLMs for NL2Bash: A Large-Scale Open-Source Evaluation" (NDSS LAST-X 2026) evaluated 22 locally-deployable LLMs from 1B to 32B parameters. Strong empirical anchor for Qwen2.5-Coder-3B selection. Full data in `research methods/module 3/references/llm-selection-research.txt`.  
**URL:** https://www.ndss-symposium.org/wp-content/uploads/lastx2026-49.pdf  
**Blocked on:** Full author list and citation metadata not extractable from binary PDF. Needed before adding to references.

---

### Chapter 1 — SLM-first, model-agnostic architecture backport

**Would affect:** SA1 (Chapter 1, already submitted)  
**What:** Three changes needed when Chapter 1 is revised for Thesis 1:
  1. **5th specific objective:** "To design and integrate a privacy-preserving, locally-hosted SLM backend with a model-agnostic interface, enabling SynapseOS to operate fully offline without transmitting user data to external services."
  2. **Remove limitation:** "LLM not yet committed" is now resolved. Replace with the honest limitation: accuracy ceiling of the 3B SLM on complex or ambiguous natural language commands relative to frontier alternatives.
  3. **Update scope:** Cloud inference is opt-in, not a design dependency.  
**Blocked on:** SA1 already submitted. Apply during Thesis 1 revision.
