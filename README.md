# SynapseOS

## Table of Contents

- [Team](#team)
- [Directory Structure](#directory-structure)
- [Key References](#key-references)
- [HTML → PDF Export](#html-pdf-export)

> Designing a Conversational Interface Layer for Personal Computing

A Linux distribution that replaces the conventional graphical session layer — the desktop shell, session manager, and application launcher — with a conversational interface layer, leaving the underlying Linux kernel, core system utilities, and application ecosystem unchanged. Ships as three interface modes — CLI, TUI, and GUI (with a participant-accessible emergency fallback to the underlying desktop) — see `docs/decisions.md` D19/D20.

## Team

- **Alexandra Sulit**
- **Willard Soriano**
- **Steven Evian Lozano**

Department of Computer Science  
Mapúa University – Makati

## Directory Structure

```
├── thesis 1/                        # the current course — Thesis 1 (follows Research Methods)
│   └── consolidated/
│       ├── SynapseOS_Proposal_Chapters_1_to_3.html  # the sole living document for Chapters 1-3 — edit this
│       └── SynapseOS_Proposal_Chapters_1_to_3.pdf   # exported from the HTML above, never hand-edited
├── thesis 2/                        # reserved for the Thesis 2 course (follows Thesis 1)
└── research-methods/                # historical record of that course — no longer edited
    ├── module 2/
    │   ├── references/
    │   │   ├── chapter-1.pdf                   # Chapter 1 reference PDF
    │   │   ├── chapter-1-presentation.pptx     # Chapter 1 presentation slides
    │   │   ├── chapter-1-rubrics.docx          # Chapter 1 grading rubrics
    │   │   ├── methodology.pptx                # Methodology presentation
    │   │   └── reference-thesis-sonam.pdf      # Reference thesis (Sonam)
    │   └── submissions/
    │       ├── formative-assessment-2.1.txt    # Chapter 1 draft (source)
    │       ├── formative-assessment-2.1.html   # Chapter 1 draft (HTML, A4 thesis format)
    │       ├── formative-assessment-2.1.pdf    # Chapter 1 draft (PDF export)
    │       ├── summative-assessment-1.pdf      # Chapter 1 full, as submitted (HTML source retired, see thesis 1/consolidated/)
    │       └── receipt.txt                     # Submission receipt
    └── module 3/
        ├── MODULE-3-SPECIFICATIONS.txt         # Methodology chapter planning spec
        ├── references/
        │   ├── Methodology.pptx                # Methodology template (professor-provided)
        │   └── Revised_Thesis_Sonam.pdf        # Reference thesis (Sonam)
        └── submissions/                        # empty — Ch.2/Ch.3 drafts retired, see thesis 1/consolidated/
```

## Key References

| # | Paper | Link |
|---|-------|------|
| [1] | Wilensky et al. (1988) — Berkeley UNIX Consultant | [ACL Anthology](https://aclanthology.org/J88-4003/) |
| [2] | Westenfelder et al. (2025) — NL to Bash Translation | [arXiv](https://arxiv.org/abs/2502.06858) |
| [3] | Gyawali et al. (2025) — NaSh Shell Guardrails | [arXiv](https://arxiv.org/abs/2506.13028) |
| [5] | Padmanabha et al. (2024) — VoicePilot | [DOI](https://doi.org/10.1145/3654777.3676401) |
| [7] | Deng et al. (2023) — Mind2Web | [arXiv](https://arxiv.org/abs/2306.06070) |
| [8] | Zhou et al. (2024) — WebArena | [arXiv](https://arxiv.org/abs/2307.13854) |
| [9] | Zheng et al. (2024) — GPT-4V Web Agent | [arXiv](https://arxiv.org/abs/2401.01614) |
| [10] | Rawles et al. (2023) — Android in the Wild | [arXiv](https://arxiv.org/abs/2307.10088) |
| [11] | Zhang et al. (2023) — AppAgent | [arXiv](https://arxiv.org/abs/2312.13771) |
| [12] | Wang et al. (2024) — Mobile-Agent | [arXiv](https://arxiv.org/abs/2401.16158) |
| [15] | Hui et al. (2025) — WinClick | [arXiv](https://arxiv.org/abs/2503.04730) |
| [16] | Zhang et al. (2025) — UFO² Desktop AgentOS | [arXiv](https://arxiv.org/abs/2504.14603) |
| [17] | Xie et al. (2024) — OSWorld Benchmark | [arXiv](https://arxiv.org/abs/2404.07972) |
| [18] | Zhang et al. (2024) — LLM-Brained GUI Agents Survey | [arXiv](https://arxiv.org/abs/2411.18279) |
| [22] | Han et al. (2025) — GUIRoboTron-Speech | [arXiv](https://arxiv.org/abs/2506.11127) |
| [23] | Park et al. (2025) — R-VLM GUI Grounding | [DOI](https://doi.org/10.18653/v1/2025.findings-acl.501) |

## HTML → PDF Export

The proposal's `.pdf` is never hand-edited — it's generated from the paired `.html` via a self-contained toolchain (vendored Chromium, driven over the DevTools protocol by Playwright) that also settles the table-of-contents page numbers and stamps page numbers per the university writing guidelines (roman numerals for front matter, Arabic from Chapter 1). See `tools/TOOLING.md` for the full breakdown.

```bash
SYNAPSE_CHROMIUM=/usr/bin/chromium tools/venv/bin/python tools/build_paper.py \
  "thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html" \
  "thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.pdf"
```

Both arguments default to that same path, so `tools/build_paper.py` with no arguments rebuilds the live document in place. HTML files use A4 `@page` sizing, Times New Roman 12pt, 1-inch margins, and 1.5 line spacing — ready for thesis submission.
