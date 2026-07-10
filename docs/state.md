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
  - [Session 5 — 2026-07-07](#session-5-2026-07-07)
  - [Session 6 — 2026-07-08](#session-6-2026-07-08)
  - [Session 7 — 2026-07-08 (Ch.2 alignment audit + patch)](#session-7-2026-07-08-ch2-alignment-audit-patch)
  - [Session 8 — 2026-07-08 (shipped to `main`)](#session-8-2026-07-08-shipped-to-main)
  - [Session 9 — 2026-07-08 (cross-chapter/vision/rubric audit + patch)](#session-9-2026-07-08-cross-chaptervisionrubric-audit-patch)
  - [Session 10 — 2026-07-08 (consolidated proposal audit + patch)](#session-10-2026-07-08-consolidated-proposal-audit-patch)
- [Pending Items](#pending-items)
- [Files to Know](#files-to-know)

## Submission State

| File | Chapter | Status | Last Changed |
|------|---------|--------|--------------|
| `research methods/module 2/submissions/summative-assessment-1.html` | Ch. 1 Introduction | Complete — aligned with D3/D7/D9/D12, footnotes 1–5; TOC + anchor ids added | 2026-07-05 |
| `research methods/module 2/submissions/summative-assessment-1.pdf` | Ch. 1 Introduction | Re-exported via `tools/export_pdf.py` — TOC page numbers + PDF outline/bookmarks (15 pages) | 2026-07-05 |
| `research methods/module 3/submissions/summative-assessment-2.html` | Ch. 2 Review of Related Literature | **Merged to `main` (PR #4, 2026-07-08); content read confirmed by user (Session 9).** 24-source RRL (refs [1]–[20],[22]–[25]): §2.1 intro, §2.2 Theoretical Framework (four-layer pipeline + Figure 2.1), §2.3–2.7 thematic literature, §2.8 synthesis (Table 2.1 matrix + six themed `h3` headings + threefold gap), §2.9 methodological literature, §2.10 summary. Footnotes 1–2; TOC/LOT/LOF + anchor ids. Every rubric row maps to a section. Figure-caption font-size fixed to 12pt (Session 9, matches Ch.3's convention) | 2026-07-08 |
| `research methods/module 3/submissions/summative-assessment-2.pdf` | Ch. 2 Review of Related Literature | Re-exported via `tools/export_pdf.py` (caption-font fix) — TOC/LOT/LOF page numbers unchanged and reverified (29 pages) | 2026-07-08 |
| `research methods/module 3/submissions/summative-assessment-3.1.html` | Ch. 3 Methodology | Complete — aligned with D9(revised)/D10/D12/D14/D15/D18, footnotes 1–7; TOC/LOT/LOF + anchor ids. Renumbered Ch.2→Ch.3 (Session 5); §1.4 names the **convergent mixed-methods** design (PR #5). **Session 9:** fixed broken citation integrity ([3]/[5]/[16] now actually cited in §2.1b, [22]/[23] removed as dead entries — they belong to Ch.2), added an RQ1–RQ3 restatement to the chapter intro, added an Appendices section (Appendix A: SUS, Appendix B: NASA-TLX — placeholder pending finalized instruments), and added the D18 recruitment-quota-floor sentence to §1.2b | 2026-07-08 |
| `research methods/module 3/submissions/summative-assessment-3.1.pdf` | Ch. 3 Methodology | Re-exported via `tools/export_pdf.py` — TOC/LOT/LOF page numbers re-derived and verified against the new content (35 pages, up from 29) | 2026-07-08 |
| `research methods/SynapseOS_Proposal_Chapters_1_to_3.html` | Consolidated Ch.1–3 monograph | Built by `tools/consolidate_proposal.py` from the three chapter files above plus hand-authored front matter (Approval Page, Acknowledgement, Abstract). **Session 10:** fixed a stale/corrupted build — front matter had been hand-patched into the HTML without rerunning the generator, leaving the TOC off by 3–5 pages everywhere past the front matter, Appendices ordered before References, and 3 orphaned `<div class="references">` tags (each chapter's fallback strip left the wrapper unclosed). Folded front-matter generation into the script and fixed both bugs at the source; every TOC/LOT/LOF entry now verified against actual PDF page content | 2026-07-08 |
| `research methods/SynapseOS_Proposal_Chapters_1_to_3.pdf` | Consolidated Ch.1–3 monograph | Re-exported via `tools/consolidate_proposal.py`'s two-pass build — 41 pages, all TOC/LOT/LOF entries spot-verified against `pdftotext` output | 2026-07-08 |

---

## Session Log

### Session 1 — prior to 2026-07-02

- Established SynapseOS architecture and wrote Chapter 1 (SA1) and Chapter 3/Methodology (SA3.1) — *labeled Chapter 2 at the time; renumbered to Chapter 3 in Session 5*
- Made key architecture decisions D1–D9 (see `decisions.md`)
- Revised Table 3.1 OS column to "SynapseOS" (Debian 13 as substrate)
- Removed OS row from Table 3.2; folded Debian 13 into caption
- Added OSWorld "how it works" sentence to Section 1.1
- Applied footnote system to SA1 (7 footnotes) and SA3.1 (7 footnotes)
- Switched study interface from TUI to GUI mode (D11) — updated Table 3.1, paragraph after Table 3.2, Section 3.5
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
- **Table 3.1.**, **Table 3.2.**, **Figure 3.1.** label numbers wrapped in `<strong>`

**Commits:** `docs(decisions)` · `style(ch1)` · `style(methodology)`

### Session 3 — 2026-07-04 to 2026-07-05

- Recorded **D12** (Debian 13 + XFCE/X11 as the invisible GUI-mode substrate; bare Debian for TUI mode) and **D13** (post-thesis "Overlay" product mode — hotkey/systray-summoned agent over a visible desktop) in `decisions.md`
- Created `layers.md` — OS-layer reference guide (stack diagram, "where SynapseOS sits," TUI/GUI/Overlay mode table, distro-identity checklist)
- Updated `vision.md`, `stack.md`, `future-features.md` to reflect D12/D13
- Created `prototype/` scaffold: `go.mod`, `cmd/synapse/main.go` (M2 walking-skeleton — sample task suite, Ollama call at temperature 0, markdown-fence stripping), `internal/ollama/client.go` (REST client: `Generate`, `Ping`), `README.md`, `setup.md`, `build-order.md` (M2–M9 milestones). **Not yet run against a live Ollama instance — open empirical risk.**
- Full drift audit of both submitted chapters against current decisions (D3, D7, D9, D10, D12) and correction of all found drift:
  - SA1 (Ch. 1): "userland" → "session layer" (11 places), footnote 3 rewritten, Android analogy replaced, RQ2/Objective 2 rewritten to the actual NL-to-bash pipeline, RQ3/Objective 4 rewritten to the expert-baseline design (D9), GUI-scope-creep language removed from RQ1/Objective 1/Significance, Limitations names Qwen2.5-Coder-3B-Instruct as committed, "Linux distribution" self-description removed, orphaned MCP/AT-SPI footnotes deleted and footnotes renumbered 1–5 (verified)
  - SA3.1 (Ch. 3/Methodology): "userland"/"Linux distribution" fixed, Table 3.1 + Platform-specificity limitation changed from "Wayland compositor" to "XFCE (X11 session)" per D12, D10's memory model added verbatim to Section 2.1a. Footnotes unchanged (still 1–7, verified)
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
- **D9 revised**: Windows and Linux now share one dual-boot machine (native performance both ways, no VM confound) instead of two separate machines; macOS stays dedicated (unreliable Linux support on Apple Silicon, and macOS's license blocks virtualizing macOS itself on non-Apple hardware). Study now needs **3 physical machines, not 4**. Updated `decisions.md`, `scope.md` Infrastructure section, and SA3.1 Table 3.1 + System Environment paragraph.
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

### Session 5 — 2026-07-07

- **Corrected a chapter-numbering error: the Methodology submission (SA3.1) was mislabeled "Chapter 2" and is now "Chapter 3."** Surfaced by the Blackboard title of the module 3 reference deck ("Chapter 3"). Verified against `module 3/references/writing-guidelines.pdf` (Mapúa TOC: Ch.1 Introduction, **Ch.2 Review of Related Literature**, **Ch.3 = major aspect / journal-format methodology**, Ch.4 Conclusion, Ch.5 Recommendation) and the Sonam reference thesis, whose methodology chapter uses `Table 3.1` / `Figure 3.1`-`3.2` numbering — confirming the methodology chapter is Chapter 3.
- **Scope of the edit (deliberately narrow):** changed only the chapter label (title, heading) and the *chapter-prefixed* table/figure numbers + anchors — `Table 2.1→3.1`, `Table 2.2→3.2`, `Figure 2.1→3.1`, `table-2-*`/`figure-2-1` ids. **Left untouched:** the in-article Section structure (Section 1 Dataset / Section 2 Procedure / Section 3 Testing) and its `1.x`/`2.x`/`3.x` subsection numbers, `sec2-*` anchors, and the `Section 2.3` / `Section 2.1a` cross-references — these are section numbers, *not* chapter numbers, and a blind 2→3 replace would have collided the Section 2 subsections with the existing Section 3 ones. The `Chapter 1` back-reference (Introduction) was correctly preserved. Re-exported the PDF.

### Session 6 — 2026-07-08

- **Added the official `proposal-grading-rubrics.pdf`** to `module 3/references/` (transferred from local VM); it confirms the chapter structure (Ch.2 = RRL, Ch.3 = Methodology) and supplied the RRL grading criteria. Also transferred `GROUP5_SA2.1_ResearchMethods.odt` (RRL context) and a `module 1` course folder; **tidied** the latter out of `module 3/references/` into a new sibling `research methods/module 1/` split into `references/` (course examples/samples) and `submissions/` (own work).
- **Drafted Chapter 2 (Review of Related Literature)** — new `module 3/submissions/summative-assessment-2.html` + PDF (26 pp.). Reviewed `module 1/submissions/Soriano_SA1.1_ResearchMethods.pdf` first (a 20-paper survey, the reference-dense source). **Key finding:** SA1.1's bibliography aligns 1:1 with the current thesis numbering, and the "gap" numbers [4],[6],[13],[14],[19],[20] are exactly the six survey papers SA1 didn't cite (van Dam, DexAssist, CogAgent, ScreenAgent, PixelHelp, ReAct) — so Ch.2 reuses them at their reserved numbers with **zero renumbering**. Chapter cites 24 real papers ([1]–[20],[22]–[25]); citation integrity verified (every cite defined, every ref cited). Deliberately excluded SA1.1's **outdated** proposed architecture (multimodal MCP + AT-SPI + vision three-tier); reframed the gap/positioning to the current CLI/NL-to-bash + Qwen2.5-Coder-3B design, and honored the vision/business scope guard. Structure follows the rubric: §2.1 intro → §2.2–2.6 thematic research literature → §2.7 synthesis + Table 2.1 matrix + threefold gap → §2.8 methodological literature → §2.9 summary/transition to Ch.3.

### Session 7 — 2026-07-08 (Ch.2 alignment audit + patch)

- **Audited the Ch.2 draft against Ch.1, Ch.3, the rubric, and the writing guidelines** (not just SA1.1). Confirmed strong substantive alignment — identical threefold gap, consistent system framing, reference numbering [1]–[20],[22]–[25] consistent across all three chapters, and the §2.8→Ch.3 transition promises methods (within-subjects, expert-baseline, counterbalancing, think-aloud, thematic analysis [24], confirmation-gate/undo) that all actually appear in Ch.3 — so **not** a blind survey copy. Surfaced five gaps and patched all of them.
- **Added §2.2 Theoretical Framework** (the four-layer Input→Reasoning→Action→OS-Integration pipeline, adapted from [18]) as a distinct numbered section — the rubric grades a "Theoretical Framework" component under Ch.2 (weight 5) that the draft was missing. Sections **renumbered §2.2–2.9 → §2.3–2.10** (headings, ids, TOC, and 5 in-text `§2.x` cross-refs remapped; citation integrity re-verified, TOC count == heading count == 10). Reconciled with Ch.3: Ch.3's "Conceptual Framework" (Figure 3.1) is the concrete **SynapseOS system architecture**, so the pipeline is Ch.2's complementary *theoretical* lens, explicitly bridged ("Chapter 3 instantiates this pipeline").
- **Added Figure 2.1** — a self-contained inline-SVG diagram of the four-layer pipeline (resolves the previously-deferred optional figure); added a **List of Figures** front-matter block. Renders as vector in the PDF; PDF outline confirmed §2.1–§2.10 + the six new synthesis subheadings + Notes/References.
- **Promoted the six §2.8 synthesis patterns to `h3` subheadings** (rubric's exemplary band wants "themes evident in headings").
- **Reconciled the "convergent mixed-methods" label** — §2.9 (Ch.2) asserted it but Ch.3 never named the design; added one framing sentence to **Ch.3 §1.4** naming it a convergent (parallel) mixed-methods design (also strengthens Ch.3's "type of study" rubric row). Both PDFs re-exported (Ch.2 now **29 pp.**; page numbers re-derived via `find_page()`).
- **Documented two standing choices in `decisions.md`:** D16 (numbered ACM-style citations with DOIs/URLs thesis-wide — a deliberate override of the ITRD handout's "no internet references" note, per the rubric's ACM directive) and D17 (shared master bibliography; `[21]` intentionally reserved/unused to avoid desyncing submitted chapters).

### Session 8 — 2026-07-08 (shipped to `main`)

- **Landed the entire Session 5–7 body of work on `main` via four squash-merged PRs** (`/ship`), partitioned by concern: **#3** `docs(refs)` course reference materials (Module 1 folder, GROUP5 SA2.1, grading rubric) · **#5** `fix(ch3)` methodology-chapter alignment (Ch.2→3 reference correction + the convergent-mixed-methods sentence) · **#6** `docs` decisions D16–D17 + tracking refresh · **#4** `feat(ch2)` the Chapter 2 RRL chapter (tip of `main`). Working tree clean, local `main` fast-forwarded to `2fc7f9c`.
- **Process note:** `Table 2.x`→`3.x` renumber and the mixed-methods sentence both live in `summative-assessment-3.1.html`, so PR #5 bundles them (file-granularity, can't hunk-split). Auto-merge enqueue was blocked by a review guardrail; you merged the PRs manually.

### Session 9 — 2026-07-08 (cross-chapter/vision/rubric audit + patch)

- **You confirmed Ch.2's content read** — §2.2 and the Ch.3 §1.4 mixed-methods sentence read as yours. No further action on that item.
- **Ran three parallel audits** (per your request to check Ch.2↔Ch.1/Ch.3 alignment and thesis-wide vision/guideline alignment) covering cross-chapter consistency, vision/scope alignment, and formal guidelines/rubric compliance (the last one closed a real coverage gap — Ch.1 and Ch.3 had never been checked against the official rubric PDF, only Ch.2 had). Vision alignment came back fully clean. The other two surfaced real, verified gaps, all patched:
  - **Ch.3 citation integrity was broken**, contradicting D17: `[3]` NaSh, `[5]` VoicePilot, `[16]` UFO², `[22]` GUIRoboTron-Speech, `[23]` R-VLM were listed in SA3.1's reference list but never cited in its body. Investigated each: `[3]`/`[5]` are genuinely relevant to the confirmation-gate/undo-log design (Ch.2 already names them as its motivation) and are now cited in §2.1b; `[16]` (UFO²) is now cited in the same section's GUI-scope-boundary sentence as a deliberate contrast; `[22]`/`[23]` have no legitimate connection to Ch.3's CLI-only methodology and were removed from its reference list — both remain correctly cited in Ch.2, where the underlying GUI/speech-grounding discussion actually lives.
  - **Ch.3's introduction never restated the three RQs from Ch.1** — the rubric's "Introduction" row requires problem *and* question. Added a paragraph restating RQ1–RQ3 and mapping each to the section that answers it.
  - **Ch.3 had no Appendices section** for the SUS/NASA-TLX instrument copies the rubric grades. Added one with Appendix A (SUS) / Appendix B (NASA-TLX) placeholders, pending the finalized instruments from the Study Instruments track.
  - **Minor pre-existing formatting deviations** (h3 heading case, page-number position, Ch.1's heading-based Introduction structure) were reviewed and deliberately left as-is — logged in `scope.md`'s Final Thesis Compilation section as a documented judgment call, not pursued further.
  - **Ch.2's figure-caption font-size** (11pt) never matched Ch.3's established 12pt convention (Session 4) — fixed for consistency.
- **Resolved the recruitment quota decision** (open since Session 4): added **D18** — minimum 2 participants per primary-OS background (Windows/macOS/Linux) within the n=20 sample. Reflected in `decisions.md`, `scope.md`'s recruitment item, and a new sentence in SA3.1 §1.2b.
- **Both PDFs re-exported**; Ch.3's TOC/LOT/LOF page numbers fully re-derived via `find_page()` against the new content (now 35 pages, up from 29) and spot-verified stable across the final two re-exports; Ch.2's page numbers reverified unchanged (29 pages) after the caption-font fix.

### Session 10 — 2026-07-08 (consolidated proposal audit + patch)

- **You asked whether the consolidated Chapters 1–3 monograph (`SynapseOS_Proposal_Chapters_1_to_3.html/pdf`) was submission-ready.** It was not — the file predates this session's state.md history (never logged, apparently built and then hand-edited outside of a tracked session) and had drifted out of sync with its own generator, `tools/consolidate_proposal.py`.
- **Root cause:** the HTML had Approval Page / Acknowledgement / Abstract front matter hand-inserted directly into the output, but the script that regenerates this file has no knowledge of that content and was never updated or rerun afterward. Three concrete defects followed:
  1. **Stale TOC/LOT/LOF page numbers** — every entry from Chapter 1 onward pointed 3–5 pages too early because the front-matter insertion was never accounted for.
  2. **Appendices ordered before References** — Chapter 3's own Appendices section (added Session 9) landed in the document body ahead of the consolidated master References block, contradicting both the guideline's sample TOC order and the TOC's own listed order.
  3. **Three orphaned `<div class="references">` wrapper tags** (one per chapter) — `strip_references_section()`'s no-next-heading fallback truncated at the `<h2>` start instead of the wrapper's own closing tag, leaving Ch.2 and Ch.3's entire remaining content nested inside an unclosed div from the previous chapter.
- **Fixed all three at the source**, not by hand-patching the output: folded front-matter generation into `consolidate_proposal.py` (front matter is now templated, so `Approval → Acknowledgement → TOC → LOT → LOF → Abstract → Chapter 1` order matches the writing-guidelines Section F sample); added `extract_appendices()` to pull Chapter 3's Appendices block out before reference-stripping and re-attach it after the master References section; rewrote `strip_references_section()` to consume the full `<div class="references">…</div>` wrapper (including its nested `ref-entry` divs) as one balanced match instead of truncating mid-structure.
- **Extended the two-pass page-detection** (`detect_page_numbers()`) to cover the new front-matter headings (APR/ACK/ABS) in addition to the existing TOC/LOT/LOF/CH1/CH2/CH3/REF/APP targets, so all of these stay self-correcting on rebuild.
- **Verified exhaustively against the rebuilt PDF**: every top-level and sub-level TOC/LOT/LOF entry checked against actual `pdftotext` page locations (found and fixed two more pre-existing off-by-one errors unrelated to the front-matter bug: `2.8 Synthesis`/`Table 2.1` and `Section 3: System Testing` were each one page early — these use hardcoded chapter-relative offsets, not per-heading detection, and had drifted from earlier content edits); confirmed all 24 references ([1]–[20],[22]–[25]) are cited-and-listed with no orphans on either side; confirmed div-tag balance (was 62 open / 59 close, now 59/59) with byte-identical rendered text before and after the structural fix.
- **Known limitation, not fixed:** front matter (Title/Approval/Acknowledgement/TOC/LOT/LOF/Abstract) uses plain continuous Arabic page numbers, not the roman-numeral-then-restart-at-1 scheme the guideline's illustrative sample shows — Chromium's print engine (used by `tools/export_pdf.py`) has no native support for resetting its page counter mid-document, and the already-graded individual chapter submissions use continuous Arabic too, so this was kept consistent with existing project precedent rather than engineering a workaround. Flagged for you to weigh in on if it matters for grading.
- **Result:** consolidated proposal PDF is now internally consistent — every TOC/LOT/LOF page number matches actual content, section order matches the guideline sample, and the HTML is structurally valid. Not committed (per project convention, awaiting explicit instruction).

---

## Pending Items

- **NDSS LAST-X 2026 citation:** author list and full citation metadata resolved 2026-07-05 (Jacobs, Lapon, Naessens — DistriNet, KU Leuven), citable now. The per-model accuracy table inside the PDF is still unextracted (binary/FlateDecode, no available tool gets past it) — only cite as corroborating literature, not for its specific ranking. Noted in `brainstorm.md`.
- **Custom cross-platform task suite:** task list (individual prompts, expected outcomes, difficulty ratings) does not exist yet. Only categories are specified in the paper (Section 1.6).
- **Study hardware:** now a 3-machine dual-boot setup (D9, revised 2026-07-05) — exact processor/RAM/storage specs still "to be confirmed prior to pilot study" per Table 3.1.
- **TOC page-number maintenance (both chapters):** hardcoded, not self-updating — re-derive via `tools/export_pdf.py`'s `find_page()` before any future submission if either chapter's content changes. Full detail in `scope.md`'s Final Thesis Compilation section.
- **Consolidated proposal (`tools/consolidate_proposal.py`) has a hybrid page-numbering scheme:** the chapter/front-matter *starts* (APR/ACK/TOC/LOT/LOF/ABS/CH1/CH2/CH3/REF/APP) are auto-detected via the two-pass build and self-correct on any content change; the *sub-entries within* Chapter 2 and Chapter 3 (e.g. `2.8 Synthesis`, `Table 2.1`, `Section 3: System Testing`) are still hardcoded chapter-relative offsets (`p_ch2 + N`, `p_ch3 + N`) and will silently drift if ch2/ch3 content changes again — re-verify with `pdftotext -layout` against the rebuilt PDF (see Session 10 method) before any future submission of the consolidated file. Also unresolved: front matter uses continuous Arabic page numbers rather than the roman-numeral scheme the writing guideline's sample TOC illustrates (see Session 10 for why).
- **Cross-chapter terminology consistency, footnote numbering unification, and the standing vision/business scope guard** — tracked in `scope.md`'s Final Thesis Compilation section; not one-time fixes, re-check on future revisions.
- **Prototype M2 walking skeleton** (`prototype/cmd/synapse/main.go`) has never been run against a live Ollama instance — open empirical risk, next natural step for the prototype track.
- **`tools/venv/` and `tools/vendor/` are gitignored, not committed** — on a fresh clone or if deleted, they need rebuilding (`make setup` handles the venv/pip part; `tools/vendor/` has no automated rebuild — see `tools/TOOLING.md`).

---

## Files to Know

| File | Purpose |
|------|---------|
| `decisions.md` | All architecture and study design decisions (D1–D17) |
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
| `tools/` | Self-contained PDF export tooling — `export_pdf.py` (venv + Playwright), `consolidate_proposal.py` (builds `research methods/SynapseOS_Proposal_Chapters_1_to_3.html/pdf` from the three chapter files + templated front matter), `TOOLING.md` (what's Python vs. vendored native vs. irreducible), `requirements.txt`; paired with the repo-root `Makefile` |
| `research methods/module 3/references/writing-guidelines.pdf` | Mapúa ITRD writing guidelines (applied in Session 2) |
| `research methods/module 3/references/proposal-grading-rubrics.pdf` | Official proposal grading rubric (added 2026-07-07) — grades by chapter (Ch.1 Introduction, Ch.2 Review of Related Literature, Ch.3 Methodology); authoritative source for the RRL drafting criteria |
| `research methods/module 3/references/llm-selection-research.txt` | Research notes on LLM selection, including the 2026-07-05 reaudit appendix |
