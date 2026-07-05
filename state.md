## Overview

This file tracks the live state of the thesis project across working sessions — what has been completed, what is pending, and where the next session should pick up. It is not a deliverables checklist (that is `scope.md`) and not an architecture log (that is `decisions.md`). It is a lightweight orientation document: read this first when resuming work, update it at the end of each session. Each session section records the date, what changed, and what is unresolved. Pending items carry forward until resolved.

## Table of Contents

- [Overview](#overview)
- [Submission State](#submission-state)
- [Session Log](#session-log)
  - [Session 1 — prior to 2026-07-02](#session-1-prior-to-2026-07-02)
  - [Session 2 — 2026-07-02](#session-2-2026-07-02)
  - [Session 3 — 2026-07-04 to 2026-07-05](#session-3-2026-07-04-to-2026-07-05)
- [Pending Items](#pending-items)
- [Files to Know](#files-to-know)

## Submission State

| File | Chapter | Status | Last Changed |
|------|---------|--------|--------------|
| `research methods/module 2/submissions/summative-assessment-1.html` | Ch. 1 Introduction | Complete — aligned with D3/D7/D9/D12, footnotes renumbered 1–5 | 2026-07-04 |
| `research methods/module 2/submissions/summative-assessment-1.pdf` | Ch. 1 Introduction | Re-exported from corrected HTML (12 pages) | 2026-07-04 |
| `research methods/module 3/submissions/summative-assessment-3.1.html` | Ch. 2 Methodology | Complete — aligned with D10/D12, footnotes still 1–7 | 2026-07-04 |
| `research methods/module 3/submissions/summative-assessment-3.1.pdf` | Ch. 2 Methodology | Re-exported from corrected HTML (28 pages) | 2026-07-04 |

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

### Session 3 — 2026-07-04 to 2026-07-05

- Recorded **D12** (Debian 13 + XFCE/X11 as the invisible GUI-mode substrate; bare Debian for TUI mode) and **D13** (post-thesis "Overlay" product mode — hotkey/systray-summoned agent over a visible desktop) in `decisions.md`
- Created `layers.md` — OS-layer reference guide (stack diagram, "where SynapseOS sits," TUI/GUI/Overlay mode table, distro-identity checklist)
- Updated `vision.md`, `stack.md`, `future-features.md` to reflect D12/D13
- Created `prototype/` scaffold: `go.mod`, `cmd/synapse/main.go` (M2 walking-skeleton — sample task suite, Ollama call at temperature 0, markdown-fence stripping), `internal/ollama/client.go` (REST client: `Generate`, `Ping`), `README.md`, `setup.md`, `build-order.md` (M2–M9 milestones). **Not yet run against a live Ollama instance — open empirical risk.**
- Full drift audit of both submitted chapters against current decisions (D3, D7, D9, D10, D12) and correction of all found drift:
  - SA1 (Ch. 1): "userland" → "session layer" (11 places), footnote 3 rewritten, Android analogy replaced, RQ2/Objective 2 rewritten to the actual NL-to-bash pipeline, RQ3/Objective 4 rewritten to the expert-baseline design (D9), GUI-scope-creep language removed from RQ1/Objective 1/Significance, Limitations names Qwen2.5-Coder-3B-Instruct as committed, "Linux distribution" self-description removed, orphaned MCP/AT-SPI footnotes deleted and footnotes renumbered 1–5 (verified)
  - SA3.1 (Ch. 2/Methodology): "userland"/"Linux distribution" fixed, Table 2.1 + Platform-specificity limitation changed from "Wayland compositor" to "XFCE (X11 session)" per D12, D10's memory model added verbatim to Section 2.1a. Footnotes unchanged (still 1–7, verified)
  - Full detail in `scope.md`'s Final Thesis Compilation section
- Regenerated both PDFs from the corrected HTML via headless Chromium (Playwright), resolved fully in user space (no root): downloaded Chromium's ~32 shared libs + Liberation/DejaVu fonts via `apt-get download` + `dpkg -x` + `LD_LIBRARY_PATH`. Verified new page/char counts against git history of the originals (SA1: 11→12 pages, 18,545→18,551 chars; SA3.1: 24→28 pages, 37,685→38,661 chars — both consistent with the actual text added)
- Deleted stale `.txt` mirrors (`git rm`) — HTML is now sole source of truth for both chapters
- Updated `scope.md` Final Thesis Compilation checklist with `[x]` completions and dates
- Shipped as 5 concern-scoped commits (see `git log`): `docs(decisions)`, `fix(ch1)`, `fix(methodology)`, `chore(submissions)`, `docs(scope)`

---

## Pending Items

- **NDSS LAST-X 2026 citation:** still blocked — the PDF is binary and the author list is not extractable. Noted in `brainstorm.md`.
- **Custom cross-platform task suite:** task list (individual prompts, expected outcomes, difficulty ratings) does not exist yet. Only categories are specified in the paper (Section 1.6).
- **Study hardware:** Table 2.1 specs say "to be confirmed prior to pilot study."
- **llm-selection-research.txt** — open in IDE as of session 2. May be the next thing to act on.
- **Cross-chapter terminology consistency, footnote numbering unification, and the standing vision/business scope guard** — tracked in `scope.md`'s Final Thesis Compilation section; not one-time fixes, re-check on future revisions.
- **Prototype M2 walking skeleton** (`prototype/cmd/synapse/main.go`) has never been run against a live Ollama instance — open empirical risk, next natural step for the prototype track.

---

## Files to Know

| File | Purpose |
|------|---------|
| `decisions.md` | All architecture and study design decisions (D1–D13) |
| `layers.md` | OS-layer reference guide — where SynapseOS sits, TUI/GUI/Overlay modes, distro-identity criteria |
| `scope.md` | Complete deliverables checklist — everything the paper has promised that must be built |
| `wordbank.md` | Terms to incorporate into the paper |
| `future-features.md` | Deferred ideas (not in scope for thesis) |
| `brainstorm.md` | Working notes, citations, open questions |
| `stack.md` | Implementation stack reference |
| `vision.md` | Product vision — North Star, thesis hypothesis, wedge, horizons |
| `prototype/` | M2 walking-skeleton Go scaffold (`build-order.md` has the M2–M9 milestone sequence) |
| `research methods/module 3/references/writing-guidelines.pdf` | Mapúa ITRD writing guidelines (applied in Session 2) |
| `research methods/module 3/references/llm-selection-research.txt` | Research notes on LLM selection (Qwen2.5-Coder-3B selection rationale) |
