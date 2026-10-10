## Overview

This directory holds the **Thesis 1** course, the course following Research Methods (`research-methods/`). Thesis 1 has begun, and the consolidated proposal — Chapters 1 through 3, the sole living document for the thesis — now lives here under `consolidated/`, moved from `research-methods/consolidated/` where it was drafted during Research Methods. `research-methods/` itself is left in place as a historical record of that course's own coursework (module references and submissions) and is no longer edited. As Thesis 1 produces its own course-specific references or submissions, they belong alongside `consolidated/` in this directory, the same way `research-methods/` organized its own.

## Table of Contents

- [Overview](#overview)
- [Contents](#contents)

## Contents

- `consolidated/SynapseOS_Proposal_Chapters_1_to_3.html` — the sole living document for Chapters 1–3. Edit this, then rebuild the paired PDF with `tools/build_paper.py` (see `tools/TOOLING.md`); never edit the PDF directly.
- `consolidated/SynapseOS_Proposal_Chapters_1_to_3.pdf` — the exported, submission-ready PDF. Regenerated from the HTML above; not hand-edited.
- `consolidated/references.bib` — the proposal's references as BibTeX. Citations follow ACM style, as the proposal grading rubric requires: numbered, sorted, and compressed in the text ([3, 7], [34–36]; "Gyawali et al. [10]" when the author is named), with the reference list typeset by ACM's own `ACM-Reference-Format.bst` (TeX Live's `acmart`) and copied into the HTML. To add a reference, add it here, re-typeset with that style, and renumber the in-text citations: numbers follow alphabetical order, so a new entry shifts the ones after it.
- `archive/submitted-<date>/` — a frozen, dated snapshot of `consolidated/` taken each time a round goes to the adviser, so a specific round stays reachable by date rather than depending on git history alone. Never edited; see each one's own README for the git tag that corresponds to it.
- `correspondence/` — material prepared for a specific message to the adviser or course faculty (e.g., a defense follow-up), not part of the proposal itself. See its own README.
