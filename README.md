# SynapseOS

## Table of Contents

- [Team](#team)
- [Directory Structure](#directory-structure)
- [Key References](#key-references)
- [HTML → PDF Export](#html-pdf-export)

> Designing a Conversational Interface Layer for Personal Computing

A Linux distribution that replaces the conventional graphical userland — the desktop shell, session manager, and application launcher — with a conversational interface layer, leaving the underlying Linux kernel and application ecosystem unchanged.

## Team

- **Alexandra Sulit**
- **Willard Soriano**
- **Allyson Vivar**

Department of Information Technology  
Mapúa University – Makati

## Directory Structure

```
├── thesis 1/                        # (pending)
├── thesis 2/                        # (pending)
└── research methods/
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
    │       ├── summative-assessment-1.txt      # Chapter 1 full (source)
    │       ├── summative-assessment-1.html     # Chapter 1 full (HTML, A4 thesis format)
    │       ├── summative-assessment-1.pdf      # Chapter 1 full (PDF export)
    │       └── receipt.txt                     # Submission receipt
    └── module 3/
        ├── MODULE-3-SPECIFICATIONS.txt         # Methodology chapter planning spec
        ├── references/
        │   ├── Methodology.pptx                # Methodology template (professor-provided)
        │   └── Revised_Thesis_Sonam.pdf        # Reference thesis (Sonam)
        └── submissions/
            └── summative-assessment-3.1.txt    # Chapter 2 methodology (source)
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

```bash
chromium --headless --disable-gpu --no-pdf-header-footer \
  --print-to-pdf=output.pdf input.html
```

HTML files use A4 `@page` sizing, Times New Roman 12pt, 1-inch margins, and 1.5 line spacing — ready for thesis submission.
