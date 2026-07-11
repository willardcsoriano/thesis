## Overview

This directory is a frozen snapshot of Chapters 1-3 exactly as they stood when this archive was made. **Correction (Session 13):** only SA1 (Chapter 1) and the consolidated Ch.1-3 monograph were ever actually submitted through the course portal — SA2 and SA3.1 here were standalone drafts that fed the consolidated document but were never themselves submitted individually. Session 13 deleted the live copies of SA2/SA3.1 entirely (they're preserved only here now) and retired `tools/consolidate_proposal.py`/`consolidate_proposal_paged.py`, since the consolidated document is now hand-maintained directly rather than generated from separate chapter files. This archive predates that: it exists because the live copies were about to be revised to add CLI mode and the GUI-mode XFCE fallback (decisions D19/D20) — content that postdates what was graded. Nothing in this folder should ever be edited; the exact commit this snapshot corresponds to is tagged `submitted-2026-07-10` in git.

## Table of Contents

- [Overview](#overview)
- [What's here](#whats-here)
- [Returning to this version](#returning-to-this-version)

## What's here

- `module 2/submissions/summative-assessment-1.{html,pdf}` — Chapter 1 (Introduction), as submitted
- `module 3/submissions/summative-assessment-2.{html,pdf}` — Chapter 2 draft (Review of Related Literature); **never submitted individually** — retained here as the last copy anywhere, since the live version was deleted in Session 13
- `module 3/submissions/summative-assessment-3.1.{html,pdf}` — Chapter 3 draft (Methodology), pre-D19/D20; **never submitted individually**, same as above
- `consolidated/SynapseOS_Proposal_Chapters_1_to_3*.{html,pdf}` — the consolidated Ch.1-3 monograph, as actually submitted, built at the time from the three files above (now hand-maintained directly instead)

## Returning to this version

```sh
git checkout submitted-2026-07-10 -- "research-methods/module 2" "research-methods/module 3" "research-methods/consolidated"
```

or browse this directory directly — it's a plain, unmodified copy, not a git-specific mechanism.
