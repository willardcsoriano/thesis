## Overview

This file tracks the live state of the thesis project across working sessions — what has been completed, what is pending, and where the next session should pick up. It is not a deliverables checklist (that is `scope.md`) and not an architecture log (that is `decisions.md`). It is a lightweight orientation document: read this first when resuming work, update it at the end of each session. Each session section records the date, what changed, and what is unresolved. Pending items carry forward until resolved.

## Table of Contents

- [Overview](#overview)
- [Submission State](#submission-state)
- [Session Log](#session-log)
  - [Session 1 — prior to 2026-07-02](#session-1-prior-to-2026-07-02)
  - [Session 2 — 2026-07-02](#session-2-2026-07-02)
  - [Session 3 — 2026-07-04 to 2026-07-05](#session-3-2026-07-04-to-2026-07-05)
  - [Session 4 — 2026-07-05](#session-4-2026-07-05)
- [Pending Items](#pending-items)
- [Files to Know](#files-to-know)

## Submission State

| File | Chapter | Status | Last Changed |
|------|---------|--------|--------------|
| `research methods/module 2/submissions/summative-assessment-1.html` | Ch. 1 Introduction | Complete — aligned with D3/D7/D9/D12, footnotes 1–5; TOC + anchor ids added | 2026-07-05 |
| `research methods/module 2/submissions/summative-assessment-1.pdf` | Ch. 1 Introduction | Re-exported via `tools/export_pdf.py` — TOC page numbers + PDF outline/bookmarks (15 pages) | 2026-07-05 |
| `research methods/module 3/submissions/summative-assessment-3.1.html` | Ch. 2 Methodology | Complete — aligned with D9(revised)/D10/D12/D14/D15, footnotes still 1–7; TOC/LOT/LOF + anchor ids added | 2026-07-05 |
| `research methods/module 3/submissions/summative-assessment-3.1.pdf` | Ch. 2 Methodology | Re-exported via `tools/export_pdf.py` — TOC/LOT/LOF page numbers + PDF outline/bookmarks (35 pages) | 2026-07-05 |

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

### Session 4 — 2026-07-05

**Planning docs:**
- Added a **Critical Path** section to `scope.md` — build track vs. ethics track dependency graph gating the pilot study; flagged the ethics application package as the actual bottleneck since IRB turnaround is the least controllable variable
- Created `roadmap.md` — thin cross-horizon (H0/H1/H2) status index, pointers only. Extracted the operational "Status" column out of `vision.md`'s Horizons table so `vision.md` stays purely about *why*, not *where things stand*

**Study design changes:**
- **D9 revised**: Windows and Linux now share one dual-boot machine (native performance both ways, no VM confound) instead of two separate machines; macOS stays dedicated (unreliable Linux support on Apple Silicon, and macOS's license blocks virtualizing macOS itself on non-Apple hardware). Study now needs **3 physical machines, not 4**. Updated `decisions.md`, `scope.md` Infrastructure section, and SA3.1 Table 2.1 + System Environment paragraph.
- **D14 added**: novice/power-user classification now requires a 3-task behavioral screener (file organization, process monitoring, application management) confirming self-reported proficiency, not self-report alone — motivated by the failure mode where general technical skill (e.g., software development) doesn't guarantee fluency with a given OS's native tools. Self-report/behavioral mismatches are excluded from the study.
- **D15 added**: prior AI/LLM exposure upgraded from a binary yes/no screening item to a graded frequency scale + tool identity; tracked as an exploratory covariate (same treatment as the OS-type subgroup), not a third grouping variable — n=20 is already split 10/10 by novice/power user, a further split would leave cells too small for inference.
- **Open, not yet decided**: whether to add an explicit *minimum* per-OS recruitment quota to D5/`scope.md`, to guarantee the realized sample can never end up all-Windows (or all one OS). Recommended against increasing n to properly *power* OS-specific subgroup claims (would need ~50+ participants, and isn't what the core within-subjects hypothesis needs) — recommended instead just a small quota floor per OS. **User was deciding when the session ended — pick this up first next time.**

**SLM choice reaudit (web search):**
- Confirmed Qwen2.5-Coder-3B-Instruct is still the best sub-4B choice as of 2026-07-05. Checked and rejected: Qwen3-Coder-Next (MoE, 80B total params — blows the CPU-only memory budget regardless of benchmark quality), Phi-4-mini (right size/license, but no NL2Bash-specific evidence), SmolLM3-3B (beats the wrong baseline — general Qwen2.5-3B, not the code-specialized Coder variant), LiteCoder-Terminal benchmark (doesn't test anything under 4B)
- **Resolved the long-blocked NDSS LAST-X 2026 citation**: authors are Jacobs, Lapon, Naessens (DistriNet, KU Leuven). The paper's own per-model accuracy table remains unextracted (binary/FlateDecode PDF, no available tool gets past it) — citable as corroborating literature only, not for its specific ranking
- Findings folded into `decisions.md` D3, `brainstorm.md`, `llm-selection-research.txt`, and SA3.1 Section 2.1a (added reference [25], added direct comparison numbers against Llama 3.2 variants, added a reconfirmation paragraph)

**SA3.1 chapter-quality fixes:**
- Section 2.1a tightened: was 7 dense paragraphs repeating the same "58%, best sub-7B" claim three times; cut to 6, the Westenfelder-methodology deep-dive reduced to one sentence pointing at Section 2.3
- Fixed a real inconsistency: Section 2.3's "Intent parsing accuracy" bullet described a *different* evaluation methodology (parsed-intent-vs-annotation matching) than 2.1a (execution-based scoring) — both now describe the same Westenfelder et al. methodology consistently
- Section 2 and Section 3 given orienting intro paragraphs matching Section 1's existing style (state the substance directly, no "this section covers..." framing) — fixes the unexplained jump from system-design content (2.1) to study-procedure content (2.2–2.5)
- Section 1.2a: added an explicit explanation of why only SynapseOS (not Condition B) is OSWorld-benchmarked — OSWorld evaluates autonomous agents, Condition B is human-operated

**TOC + PDF outline/bookmarks, both chapters:**
- SA3.1 got a Table of Contents, List of Tables, and List of Figures (Mapúa writing-guidelines Section F/G); SA1 got a Table of Contents only (no tables/figures in that chapter)
- Both have real page numbers baked into the HTML (two-pass: export → locate each heading's actual page via `pdftotext` → fill in → re-export) and a genuine PDF bookmarks sidebar, via Chrome DevTools Protocol's `generateDocumentOutline` (which Chromium's plain print-to-PDF CLI flag doesn't expose — needed the full tooling below to get at it)
- **Caveat, tracked in `scope.md`'s Final Thesis Compilation section:** the in-page TOC page numbers are hardcoded, not self-updating (Chromium doesn't support live `target-counter()` cross-references) — any future edit that shifts pagination needs the numbers re-derived by hand. The PDF outline itself *does* regenerate correctly on every export automatically.

**Built self-contained PDF export tooling (`tools/`),** replacing an ad-hoc bash/apt-get pipeline and a broken hand-rolled CDP/WebSocket client:
- `tools/venv/` — Python venv, `pip install playwright` (only real Python dependency)
- `tools/vendor/` — vendored Chromium binary + ~98 shared libs + `poppler-utils` + fonts, all resolved via `apt-get download`/`dpkg -x` (no root, no system installs), gitignored
- `tools/export_pdf.py` — drives the vendored Chromium via Playwright; calls `Page.printToPDF` directly over CDP for `generateDocumentOutline` (Playwright's stable API doesn't expose it yet); also has a `find_page()` helper for TOC page-number lookups
- `tools/requirements.txt`, `tools/TOOLING.md` (inventory of what's Python vs. vendored-native vs. genuinely irreducible — Python itself, the kernel/dynamic-linker/glibc, architecture/distro compatibility), `Makefile` (`make setup`, `make export FILE=... [OUT=...]`)

**Repo hygiene:**
- Ran `/gitignore` — regenerated `.gitignore` from the gitignore.io API for the detected stack (Go via `prototype/go.mod`, Python via `tools/requirements.txt`, plus macOS/Linux/Windows/VS Code baselines), merged into a managed block, all prior custom entries preserved above it. No already-tracked files conflicted.

**Consistency audit (background fork):** read all of SA3.1 against `scope.md` line by line. Found and fixed 8 gaps — most importantly, the novice/power-user subgroup + condition×group interaction (the study's central D5 design feature) was completely missing from the Statistical analysis script item, and the GUI-mode session manager (literally what Condition A is in the study) had no Go Runtime checklist line at all, only the TUI did. Also added: error-type categorization, the error rate formula, conversational-turns-per-task, task normalization, and a new "Safety mechanism validation" item for confirmation-gate/undo-log correctness checks — all previously described only in chapter prose, untracked as things to actually build/test. No drift found on any of today's earlier edits.

**Created `drift.md`** — tracks technical claims in this project that can go stale over time (SLM choice, NL2Bash benchmark citations, unwritten Go/Ollama/bubbletea APIs in `prototype/`, Playwright's experimental CDP features, the Debian/XFCE version target), with a "when to recheck" trigger list. Distinct from the consistency audit above — that was doc-vs-doc agreement *right now*; this is about facts becoming stale *later*.

---

## Pending Items

- **Recruitment quota decision (new, highest priority next session):** should `decisions.md` D5 / `scope.md`'s recruitment item get an explicit *minimum* quota per OS background (Windows/macOS/Linux), so the realized sample can't end up all one OS? Discussed and recommended, not yet drafted — pick this up first.
- **NDSS LAST-X 2026 citation:** author list and full citation metadata resolved 2026-07-05 (Jacobs, Lapon, Naessens — DistriNet, KU Leuven), citable now. The per-model accuracy table inside the PDF is still unextracted (binary/FlateDecode, no available tool gets past it) — only cite as corroborating literature, not for its specific ranking. Noted in `brainstorm.md`.
- **Custom cross-platform task suite:** task list (individual prompts, expected outcomes, difficulty ratings) does not exist yet. Only categories are specified in the paper (Section 1.6).
- **Study hardware:** now a 3-machine dual-boot setup (D9, revised 2026-07-05) — exact processor/RAM/storage specs still "to be confirmed prior to pilot study" per Table 2.1.
- **TOC page-number maintenance (both chapters):** hardcoded, not self-updating — re-derive via `tools/export_pdf.py`'s `find_page()` before any future submission if either chapter's content changes. Full detail in `scope.md`'s Final Thesis Compilation section.
- **Cross-chapter terminology consistency, footnote numbering unification, and the standing vision/business scope guard** — tracked in `scope.md`'s Final Thesis Compilation section; not one-time fixes, re-check on future revisions.
- **Prototype M2 walking skeleton** (`prototype/cmd/synapse/main.go`) has never been run against a live Ollama instance — open empirical risk, next natural step for the prototype track.
- **`tools/venv/` and `tools/vendor/` are gitignored, not committed** — on a fresh clone or if deleted, they need rebuilding (`make setup` handles the venv/pip part; `tools/vendor/` has no automated rebuild — see `tools/TOOLING.md`).

---

## Files to Know

| File | Purpose |
|------|---------|
| `decisions.md` | All architecture and study design decisions (D1–D15) |
| `layers.md` | OS-layer reference guide — where SynapseOS sits, TUI/GUI/Overlay modes, distro-identity criteria |
| `scope.md` | Complete deliverables checklist — everything the paper has promised that must be built; includes the Critical Path and Final Thesis Compilation sections |
| `wordbank.md` | Terms to incorporate into the paper |
| `future-features.md` | Deferred ideas (not in scope for thesis) |
| `brainstorm.md` | Working notes, citations, open questions |
| `stack.md` | Implementation stack reference |
| `vision.md` | Product vision — North Star, thesis hypothesis, wedge, horizons (why, not status) |
| `roadmap.md` | Cross-horizon status index — pointers only, no duplicated status |
| `drift.md` | Drift-prone technical claims (SLM choice, library APIs, benchmark citations) and when/how to recheck each |
| `prototype/` | M2 walking-skeleton Go scaffold (`build-order.md` has the M2–M9 milestone sequence) |
| `tools/` | Self-contained PDF export tooling — `export_pdf.py` (venv + Playwright), `TOOLING.md` (what's Python vs. vendored native vs. irreducible), `requirements.txt`; paired with the repo-root `Makefile` |
| `research methods/module 3/references/writing-guidelines.pdf` | Mapúa ITRD writing guidelines (applied in Session 2) |
| `research methods/module 3/references/llm-selection-research.txt` | Research notes on LLM selection, including the 2026-07-05 reaudit appendix |
