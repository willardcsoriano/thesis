## Overview

This file tracks the live state of the thesis project across working sessions — what has been completed, what is pending, and where the next session should pick up. It is not a deliverables checklist (that is `scope.md`) and not an architecture log (that is `decisions.md`). It is a lightweight orientation document: read this first when resuming work, update it at the end of each session. Each session section records the date, what changed, and what is unresolved. Pending items carry forward until resolved.

## Table of Contents

- [Overview](#overview)
- [Submission State](#submission-state)
- [Session Log](#session-log)
  - [Session 1 — prior to 2026-07-02](#session-1-prior-to-2026-07-02)
  - [Session 2 — 2026-07-02](#session-2-2026-07-02)
- [Pending Items](#pending-items)
- [Files to Know](#files-to-know)

## Submission State

| File | Chapter | Status | Last Changed |
|------|---------|--------|--------------|
| `research methods/module 2/submissions/summative-assessment-1.html` | Ch. 1 Introduction | Complete — formatted, footnoted | 2026-07-02 |
| `research methods/module 2/submissions/summative-assessment-1.pdf` | Ch. 1 Introduction | Exported clean (no headers/footers) | 2026-07-02 |
| `research methods/module 3/submissions/summative-assessment-3.1.html` | Ch. 2 Methodology | Complete — formatted, footnoted, GUI mode | 2026-07-02 |
| `research methods/module 3/submissions/summative-assessment-3.1.pdf` | Ch. 2 Methodology | Exported clean (no headers/footers) | 2026-07-02 |
| `research methods/module 2/submissions/summative-assessment-1.txt` | Ch. 1 plain text mirror | Present — may be deleted (see Pending) | — |
| `research methods/module 3/submissions/summative-assessment-3.1.txt` | Ch. 2 plain text mirror | Present — may be deleted (see Pending) | — |

---

## Session Log

### Session 1 — prior to 2026-07-02

- Established SynapseOS architecture and wrote Chapter 1 (SA1) and Chapter 2/Methodology (SA3.1)
- Made key architecture decisions D1–D9 (see `decisions.md`)
- Revised Table 2.1 OS column to "SynapseOS" (Debian 13 as substrate)
- Removed OS row from Table 2.2; folded Debian 13 into caption
- Added OSWorld "how it works" sentence to Section 1.1
- Applied footnote system to SA1 (7 footnotes) and SA3.1 (7 footnotes)
- Switched study interface from TUI to GUI mode (D11) — updated Table 2.1, paragraph after Table 2.2, Section 3.5
- Recorded D10 (conversation memory model) and D11 (GUI study mode) in `decisions.md`
- Updated `scope.md` with GUI mode hardware spec and Final Thesis Compilation section
- Copied writing guidelines PDF to `research methods/module 3/references/writing-guidelines.pdf`

### Session 2 — 2026-07-02

Applied Mapúa ITRD writing guidelines to both HTML submission files:

**CSS changes (both files):**
- Right margin corrected: 1.25 in → 1.0 in (left stays at 1.25 in)
- Line spacing: 1.5 → 2.0 (double space per guidelines)
- Paragraph indent: 1.27 cm → 1.50 cm
- Removed 0.8 em bottom paragraph margin (no extra space between paragraphs)
- First paragraphs after headings: now indented (guidelines indent all paragraphs)
- Chapter number label: bold, `text-transform: uppercase` removed
- Chapter title: `font-weight: bold` added (was missing)
- `h2` (second-level headings): title case instead of all-caps; `letter-spacing` removed; top margin 30 pt
- `h3` (third-level): top margin 30 pt
- Lists: 1.50 cm left indent, no inter-item gap
- References heading: left-aligned (was centered)
- Reference entries: 12 pt (was 11 pt)

**SA3.1 additionally:**
- `h4` (fourth-level): regular weight, no italic (guidelines: fourth-level = regular font style)
- Table/figure captions: non-italic, 12 pt
- **Table 2.1.**, **Table 2.2.**, **Figure 2.1.** label numbers wrapped in `<strong>`

**Commits:** `docs(decisions)` · `style(ch1)` · `style(methodology)`

---

## Pending Items

- **TXT files:** `summative-assessment-1.txt` and `summative-assessment-3.1.txt` are plain text mirrors of the HTML files. They're now out of date and the HTML is the source of truth. Decision pending: delete or keep. User asked about this but has not confirmed deletion.
- **NDSS LAST-X 2026 citation:** still blocked — the PDF is binary and the author list is not extractable. Noted in `brainstorm.md`.
- **Custom cross-platform task suite:** task list (individual prompts, expected outcomes, difficulty ratings) does not exist yet. Only categories are specified in the paper (Section 1.6).
- **Study hardware:** Table 2.1 specs say "to be confirmed prior to pilot study."
- **Chapter 1 alignment (final thesis):** SA1 still reflects pre-D7 and pre-D9 decisions — RQ2/Objective 2 reference the three-tier MCP/AT-SPI/vision arch (superseded), RQ3/Objective 4 reference "conventional Linux graphical desktop" (superseded by expert baseline design). Deferred to final compilation.
- **llm-selection-research.txt** — open in IDE as of session 2. May be the next thing to act on.

---

## Files to Know

| File | Purpose |
|------|---------|
| `decisions.md` | All architecture and study design decisions (D1–D11) |
| `scope.md` | Complete deliverables checklist — everything the paper has promised that must be built |
| `wordbank.md` | Terms to incorporate into the paper |
| `future-features.md` | Deferred ideas (not in scope for thesis) |
| `brainstorm.md` | Working notes, citations, open questions |
| `stack.md` | Implementation stack reference |
| `research methods/module 3/references/writing-guidelines.pdf` | Mapúa ITRD writing guidelines (applied in Session 2) |
| `research methods/module 3/references/llm-selection-research.txt` | Research notes on LLM selection (Qwen2.5-Coder-3B selection rationale) |
