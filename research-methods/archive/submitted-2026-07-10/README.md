## Overview

This directory is a frozen snapshot of Chapters 1-3 exactly as they stood when this archive was made — the actually-submitted, graded coursework (SA1, SA2, SA3.1) plus the consolidated Ch.1-3 monograph built from them. It exists because the live copies under `research-methods/module 2/`, `research-methods/module 3/`, and `research-methods/consolidated/` were about to be revised to add CLI mode and the GUI-mode XFCE fallback (decisions D19/D20) — content that postdates what was graded. Nothing in this folder should ever be edited; if you need the current, evolving chapter text, look at the live paths instead. The exact commit this snapshot corresponds to is tagged `submitted-2026-07-10` in git.

## Table of Contents

- [Overview](#overview)
- [What's here](#whats-here)
- [Returning to this version](#returning-to-this-version)

## What's here

- `module 2/submissions/summative-assessment-1.{html,pdf}` — Chapter 1 (Introduction), as submitted
- `module 3/submissions/summative-assessment-2.{html,pdf}` — Chapter 2 (Review of Related Literature), as submitted
- `module 3/submissions/summative-assessment-3.1.{html,pdf}` — Chapter 3 (Methodology), as submitted
- `consolidated/SynapseOS_Proposal_Chapters_1_to_3*.{html,pdf}` — the consolidated Ch.1-3 monograph (flat and Paged.js variants), built from the three files above

## Returning to this version

```sh
git checkout submitted-2026-07-10 -- "research-methods/module 2" "research-methods/module 3" "research-methods/consolidated"
```

or browse this directory directly — it's a plain, unmodified copy, not a git-specific mechanism.
