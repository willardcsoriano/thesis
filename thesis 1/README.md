## Overview

This directory holds the **Thesis 1** course, the course following Research Methods (`research-methods/`). Thesis 1 has begun, and the consolidated proposal — Chapters 1 through 3, the sole living document for the thesis — now lives here under `consolidated/`, moved from `research-methods/consolidated/` where it was drafted during Research Methods. `research-methods/` itself is left in place as a historical record of that course's own coursework (module references and submissions) and is no longer edited. As Thesis 1 produces its own course-specific references or submissions, they belong alongside `consolidated/` in this directory, the same way `research-methods/` organized its own.

## Table of Contents

- [Overview](#overview)
- [Contents](#contents)
- [Closing Thesis 1](#closing-thesis-1)

## Contents

- `consolidated/SynapseOS_Proposal_Chapters_1_to_3.html` — the sole living document for Chapters 1–3. Edit this, then rebuild the paired PDF with `tools/build_paper.py` (see `tools/TOOLING.md`); never edit the PDF directly.
- `consolidated/SynapseOS_Proposal_Chapters_1_to_3.pdf` — the exported, submission-ready PDF. Regenerated from the HTML above; not hand-edited.
- `consolidated/text/` — a generated plain-text copy of the built paper, one file per chapter, each page headed by its printed number. For reading, review, and readable diffs; never edited (change the HTML and rebuild).
- `consolidated/references.bib` — the proposal's references as BibTeX. Citations follow ACM style, as the proposal grading rubric requires: numbered, sorted, and compressed in the text ([3, 7], [34–36]; "Gyawali et al. [10]" when the author is named), with the reference list typeset by ACM's own `ACM-Reference-Format.bst` (TeX Live's `acmart`) and copied into the HTML. To add a reference, add it here, re-typeset with that style, and renumber the in-text citations: numbers follow alphabetical order, so a new entry shifts the ones after it.
- `archive/synapseos-focused-2026-10-10/` — the last SynapseOS-focused version of the proposal (before the adviser's 2026-10-12 algorithm-first reframe), kept for later use; see its README.
- `archive/submitted-<date>/` — a frozen, dated snapshot of `consolidated/` taken each time a round goes to the adviser, so a specific round stays reachable by date rather than depending on git history alone. Never edited; see each one's own README for the git tag that corresponds to it.
- `review/` — findings from reviews of the proposal that are kept for a later pass (for example `2026-10-12-low-findings.md`), each with the quote to search for in `consolidated/text/`.
- `correspondence/` — material prepared for a specific message to the adviser or course faculty (e.g., a defense follow-up), not part of the proposal itself. See its own README.

## Closing Thesis 1

Thesis 1 was defended, and the post-defense revisions were merged in #100 and #102. The course grade is **C (Continuing)** until the panel's verdicts come in. Under Mapúa's grading system, a C is replaced by the final grade once the course is completed, and it must be completed within one academic year. Development continues on `main` in the meantime. The steps below are the same whatever the verdict is.

1. **Verdicts.** Wait until all three panelists have sent theirs. Record what each one asks for under `review/`, one file per round, quoting the text each finding refers to in `consolidated/text/`, as `review/2026-10-12-low-findings.md` does.
2. **Revisions, if any.** Open an issue, branch, edit `consolidated/SynapseOS_Proposal_Chapters_1_to_3.html`, and rebuild with `SYNAPSE_CHROMIUM=/usr/bin/chromium tools/venv/bin/python tools/build_paper.py`. Commit the regenerated `consolidated/text/` with the HTML and merge through a PR.
3. **Freeze.** On the merged `main`, copy `consolidated/` to `archive/final-<date>/` with a README in the style of `archive/submitted-2026-10-04/README.md`, and tag the commit `thesis1-final`. `submitted-2026-10-04` remains the version sent before the defense.
4. **Submission copy.** Put the final PDF in `~/Documents/SynapseOS-Submissions/`, moving the copy it replaces to `superseded/`.
5. **Hand-over to Thesis 2.** When Thesis 2 extends the paper, move `consolidated/` to `thesis 2/consolidated/` and update every reference to the old path. `grep -rlI 'thesis 1/consolidated' --exclude-dir=.git --exclude-dir=archive .` lists them, including `CLAUDE.md`, `tools/build_paper.py`, and `tools/export_text.py`. Leave the archive snapshots where they are.
