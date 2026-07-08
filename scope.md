## Overview

This file tracks every concrete deliverable the methodology chapter (SA3.1) has already committed to. If the paper describes it as existing, the study depends on it, or results are computed from it — it goes here. Nothing in this list is optional. Items are grouped by type and marked with their current status. The goal is to prevent arriving at study time and discovering the paper promised something that was never built. Companion to `future-features.md` (deferred ideas) and `stack.md` (implementation stack reference).

## Table of Contents

- [Overview](#overview)
- [Status Key](#status-key)
- [Critical Path](#critical-path)
- [Software — Go Runtime (SynapseOS)](#software-go-runtime-synapseos)
- [Software — Python Build Pipeline](#software-python-build-pipeline)
- [Software — Data Collection and Analysis](#software-data-collection-and-analysis)
- [Study Instruments — Documents](#study-instruments-documents)
- [Study Instruments — Task Suites](#study-instruments-task-suites)
- [Infrastructure — Hardware](#infrastructure-hardware)
- [Infrastructure — Third-Party Setup](#infrastructure-third-party-setup)
- [Process — Ethics and Recruitment](#process-ethics-and-recruitment)
- [Process — Study Execution](#process-study-execution)
- [Final Thesis Compilation](#final-thesis-compilation)

## Status Key

- `[ ]` Not started
- `[~]` In progress
- `[x]` Complete
- `[!]` Blocked — dependency or external gate

---

## Critical Path

Two independent tracks must both finish before the **Pilot study** can run. One of them sits behind a hard external gate.

- **Track A — Build** (paced by our own velocity, no external gate): LoRA fine-tuning script → Model export → Go Runtime complete (Session logger, Confirmation gate, Undo log in particular) → Ollama connectivity check passes → SynapseOS machine + other two machines provisioned → Screen capture software installed and tested.
- **Track B — Ethics** (long-lead-time track, currently blocked): Study Instruments — Documents drafted and reviewed → Ethics application package compiled → IRB / ethics approval (`[!]` blocked — application not yet submitted) → Participant recruitment.

**Convergence:** Track A ∧ Track B → Custom cross-platform task suite finalized + OSWorld benchmark setup verified → Pilot study → instrument/task revisions → Main study → Log parser / Metric computation / Anonymization / Statistical analysis → Qualitative coding → Final Thesis Compilation.

**Current bottleneck:** the Ethics application package has not been compiled yet. Since IRB turnaround is the least controllable variable in this whole plan, Track B document drafting should be prioritized now, in parallel with Track A build work — not sequenced after it.

---

## Software — Go Runtime (SynapseOS)

These are features of the SynapseOS binary that the paper explicitly describes. All live in the Go runtime.

- `[ ]` **Conversational TUI** — full-screen persistent chat interface (bubbletea + lipgloss); accepts NL input, renders responses and command output
- `[ ]` **GUI-mode session manager** — fullscreen conversational interface wrapping the same runtime as the TUI, launched in place of the conventional Linux graphical desktop (see D11, D12); this is Condition A as actually run in the study, not the TUI — see `prototype/build-order.md` M9
- `[ ]` **Intent parser client** — sends user utterance + session context to Ollama REST API; streams response tokens back to the TUI
- `[ ]` **Bash execution engine** — dispatches shell commands as subprocesses via `os/exec`; captures and streams stdout/stderr
- `[ ]` **Confirmation gate** — classifies pending commands as reversible/irreversible before dispatch; blocks irreversible commands pending explicit user approval
- `[ ]` **Undo log** — records reversible operations; exposes an undo command that restores prior system state
- `[ ]` **Session logger** — writes structured log entries for every event: task start, command issued, command output, undo events, confirmation gate triggers, task end. This is the source of all telemetry data the study depends on.
- `[ ]` **Telemetry fields logged per event:**
  - Timestamp (ms precision)
  - Event type (task_start / command_issued / command_result / confirmation_triggered / undo_invoked / task_end)
  - Command string (the bash command generated)
  - Execution latency (ms from dispatch to stdout/stderr capture)
  - Task ID (links events to the task prompt that caused them)
  - Participant ID (anonymized code, set at session start)
  - Condition (A = SynapseOS, B = native OS — always A for this logger)
  - Note: intent parsing accuracy is **not** its own logged field — it's a derived metric, computed offline by the Intent Parsing Accuracy Evaluator (Python Build Pipeline) by cross-referencing the logged command string + task ID against human-annotated ground truth. Section 1.1c's "system performance data... captured automatically" refers to this raw data, not a live accuracy score.
- `[ ]` **Session context manager** — maintains in-memory conversation history; passes prior turns as prompt context to Ollama; implements rolling window when context approaches the 8K token limit; truncates verbose bash output (long `find`/`ls` results) before appending to context — never stores raw verbose output at full length (see D10)
- `[ ]` **Ollama connectivity check** — verifies Ollama is running at `localhost:11434` on startup; fails gracefully with a user-readable error if not

---

## Software — Python Build Pipeline

Runs offline before the study. Never executes during the user study.

- `[ ]` **LoRA fine-tuning script** — fine-tunes Qwen2.5-Coder-3B-Instruct on NL2Bash corpus + custom SynapseOS task suite using Unsloth + PEFT
- `[ ]` **Training dataset preparation** — assembles and cleans the NL2Bash corpus, verified InterCode subset, and custom SynapseOS task examples into a fine-tuning format
- `[ ]` **Intent parsing accuracy evaluator** — compares the system's bash output against human-annotated ground truth per task utterance; computes accuracy as proportion of first-attempt matches
- `[ ]` **Model export** — exports the fine-tuned model in GGUF format for Ollama ingestion

---

## Software — Data Collection and Analysis

Scripts that process raw study data into results.

- `[ ]` **Task normalization** — standardizes task descriptions and expected outcomes across the OSWorld benchmark suite and the custom task suite into a consistent evaluation format (Section 2.2), so both suites can be scored the same way
- `[ ]` **Safety mechanism validation** — pre-study test pass verifying (a) every irreversible operation in the task set triggers the confirmation prompt and none execute without approval, and (b) a representative set of reversible operations can be undone with correct state restoration (Section 2.3); a build/test task, not just a chapter description
- `[ ]` **Log parser** — reads SynapseOS session logs; extracts per-task metrics: completion time (task_start → task_end delta), error events (typed: intent parsing error / execution error / recovery failure, per Section 3.3b), undo events, confirmation events, conversational turns per task (derived from command_issued event counts per task ID)
- `[ ]` **Metric computation script** — computes task completion time, error rate (Failed + Incorrect Outcomes / Total Task Attempts, per Section 3.3b), broken out by error type, SUS scores (standard formula: odd items: score−1; even items: 5−score; sum × 2.5), NASA-TLX composite (average of six subscales)
- `[ ]` **Anonymization step** — replaces all participant identifiers with participant ID codes before any analysis; runs before any output file is written
- `[ ]` **Statistical analysis script** — Shapiro-Wilk normality test; paired-samples t-test or Wilcoxon signed-rank (depending on normality result); Cohen's d effect size; descriptive statistics (mean, SD, 95% CI) per condition per group; **novice vs. power-user subgroup analysis and the condition × group interaction effect (primary analysis, per D5's dual-population design — not exploratory)**; subgroup analysis by OS type (Windows/macOS/Linux) as exploratory; correlation of prior AI exposure (frequency) with SynapseOS performance metrics, also exploratory (see D15)
- `[ ]` **Screen capture setup** — recording software configured on both the SynapseOS machine and the baseline machine before the study begins; output synced to participant session IDs

---

## Study Instruments — Documents

Physical or digital documents administered during the study. All need to be drafted, reviewed, and approved before participant recruitment opens.

- `[ ]` **Pre-study screening questionnaire** — captures: years of computer use, primary OS (Windows/macOS/Linux, ≥4 hrs/day), proficiency on primary OS (basic/intermediate/advanced), command-line familiarity (none/basic/intermediate/advanced), prior conversational AI exposure (frequency: never/occasionally/regularly/daily, and which tools), and a three-task behavioral screener (file organization, process monitoring, application management) that confirms self-reported proficiency against unaided performance (see D14). Used to classify novice vs. power user; self-report/behavioral mismatches are excluded. AI exposure is an exploratory covariate only (see D15), not a grouping variable.
- `[ ]` **Informed consent form** — describes study purpose, procedures, data collected, right to withdraw, anonymization policy. Signed before session begins. Must satisfy Mapúa IRB requirements.
- `[ ]` **Participant information sheet** — plain-language summary of the study given to participants before they consent
- `[ ]` **Task prompt sheet** — printed sheet listing all tasks in the assigned order, one task per page or clearly separated. Task wording in plain language, OS- and CLI-neutral.
- `[ ]` **SUS questionnaire form** — the standard 10-item System Usability Scale, administered immediately after each condition
- `[ ]` **NASA-TLX questionnaire form** — the standard 6-subscale form (Mental Demand, Physical Demand, Temporal Demand, Performance, Effort, Frustration), administered immediately after each condition
- `[ ]` **Semi-structured interview guide** — facilitator script covering: perceived ease of use, trust in the conversational interface, preference between conditions, open-ended feedback. 10–15 minutes post-study.
- `[ ]` **Debrief script** — full disclosure of study objectives; talking points for answering participant questions
- `[ ]` **Session facilitator checklist** — step-by-step checklist for running a session consistently across all 20 participants (machine setup — including booting the dual-boot machine to the participant's assigned OS beforehand, recording start, questionnaire handoff, break timing, session close)

---

## Study Instruments — Task Suites

- `[ ]` **Custom cross-platform task suite** — individual tasks with: unique ID, natural language prompt (as shown to participant), expected outcome, difficulty level (simple/moderate/complex), and per-platform equivalence note (how it is accomplished on SynapseOS bash / Windows / macOS / Linux GNOME). Covers four categories: file search and organization, system and process monitoring, application and package management, text and data processing.
- `[ ]` **OSWorld benchmark setup** — OSWorld evaluation environment configured on the SynapseOS machine; automated benchmark runs verified end-to-end before the main study

---

## Infrastructure — Hardware

Three physical machines required for the lab setup.

- `[ ]` **SynapseOS machine** — x86-64, 8 GB RAM recommended, 10 GB storage, XFCE desktop environment (X11 session) required as the invisible GUI host; runs SynapseOS in GUI mode (fullscreen conversational interface) on a Debian 13 (Trixie) base (see decision D12)
- `[ ]` **Windows 11 / Linux (GNOME) dual-boot machine** — x86-64 hardware chosen for mainstream Linux driver support; Windows 11 and a mainstream distro (Ubuntu 24.04 LTS recommended) installed on separate partitions with boot-menu OS selection; standard user account on each OS, no custom software beyond what a typical user would have. Facilitator boots to the OS matching the participant's assignment before the session begins (see D9) — no runtime switching.
- `[ ]` **macOS machine** — Apple Silicon or Intel Mac; macOS (current release); standard user account. Kept as a dedicated machine rather than dual-boot or virtualized: Linux hardware support on Apple Silicon is unreliable, and macOS's license prohibits virtualizing macOS itself on non-Apple hardware.

---

## Infrastructure — Third-Party Setup

- `[ ]` **Ollama installed and verified** — Ollama running at `localhost:11434` on the SynapseOS machine; Qwen2.5-Coder-3B-Instruct Q4_K_M pulled and loaded
- `[ ]` **Screen capture software** — installed and tested on all three machines; output format and storage location standardized

---

## Process — Ethics and Recruitment

- `[!]` **IRB / ethics approval** — Mapúa University – Makati ethics committee approval required before any participant recruitment begins. Blocked until application is submitted.
- `[ ]` **Ethics application package** — compiled from the instruments above: consent form, information sheet, screening questionnaire, session procedure, data handling policy
- `[ ]` **Participant recruitment** — 20 participants (10 novice, 10 power user) across Windows, macOS, and Linux primary OS backgrounds; recruited through university channels after ethics approval

---

## Process — Study Execution

- `[ ]` **Pilot study** — 5 participants before the main study; validates task difficulty calibration, questionnaire clarity, and session procedure; findings used to revise instruments
- `[ ]` **Main study** — 20 participants; sessions run in the lab; data collected, anonymized, and stored securely
- `[ ]` **Qualitative coding** — semi-structured interview transcripts coded using Braun & Clarke reflexive thematic analysis; two-coder agreement check

---

## Final Thesis Compilation

These items are not blocking for current submissions but must be resolved when all chapters are assembled into the final thesis document.

- `[x]` **Chapter 1 alignment** — SA1 (summative-assessment-1.html) reflected pre-D7 and pre-D9 decisions: RQ2/Objective 2 referenced the three-tier MCP/AT-SPI/vision architecture; RQ3/Objective 4 referenced "conventional Linux graphical desktop"; Limitations called the LLM "not yet committed" and "multimodal." **Fixed 2026-07-04:** RQ2/Objective 2 rewritten around the actual NL-to-bash intent parsing and execution pipeline (confirmation gate, undo mechanism); RQ3/Objective 4 rewritten to the expert baseline (participant's own primary OS); Limitations names the committed model (Qwen2.5-Coder-3B-Instruct) and drops "multimodal." RQ1/Objective 1/Significance also had the same GUI-scope creep ("graphical applications," "cross-application workflows") — fixed to CLI-scope language for internal consistency with the RQ2 fix.
- `[x]` **"Userland" terminology (SA1 and SA3.1).** **Fixed 2026-07-04:** all uses of "userland" meaning the desktop shell/session/launcher layer replaced with "session layer" throughout both chapters (SA1: 11 locations; SA3.1: 1). Footnote 3 (SA1) rewritten to precisely distinguish "session layer" (replaced) from "userland" (kept, coreutils/libc/bash/apt) — matching `layers.md`'s vocabulary.
- `[x]` **Android analogy overstated the comparison (SA1).** **Fixed 2026-07-04:** replaced with an accurate precedent — a new interactive shell (bash/zsh/fish) replacing the one before it, same kernel and userland, different interactive layer.
- `[x]` **D10 (conversation memory model) was unwritten in SA3.1.** **Fixed 2026-07-04:** added to Section 2.1a — session-scoped memory, the 75%-of-8K-token rolling window, and output compression, matching decisions.md D10 exactly.
- `[x]` **Table 3.1 GUI-mode substrate (SA3.1).** **Fixed 2026-07-04:** "Wayland compositor required" replaced with "XFCE (X11 session) desktop environment" in Table 3.1 and the Platform specificity limitation, per D12.
- `[x]` **"Linux distribution" self-description (SA1 and SA3.1).** Found while fixing the above: both chapters called SynapseOS "a Linux distribution" as the thesis artifact itself — an overclaim relative to what D12/D13 establish (SynapseOS is a distribution only once packaged as a bootable derivative, out of thesis scope). **Fixed 2026-07-04:** replaced with "a system" / "SynapseOS" throughout; the one remaining match ("Linux distributions," SA3.1 Limitations) is an unrelated, correct generalizability caveat.
- `[x]` **Convergent mixed-methods label (SA2 ↔ SA3.1).** **Fixed 2026-07-08:** SA2 §2.9 previewed the study as a "convergent mixed-methods" design, but SA3.1 described the quant+qual substance without ever naming it. Added a framing sentence to SA3.1 §1.4 naming it a convergent (parallel) mixed-methods design (quantitative measures + qualitative interview data collected concurrently, integrated at interpretation) — Ch.2 and Ch.3 now use the same label, and it strengthens Ch.3's "type of study" rubric row.
- `[ ]` **Cross-chapter terminology consistency** — re-verify wordbank.md terms (expert baseline design, ecological validity, session-scoped memory, etc.) are used consistently across both chapters now that the above fixes have landed. Ch.2 (SA2) adds: "four-layer interaction pipeline" / "theoretical framework" (§2.2) vs. Ch.3's "conceptual framework" (SynapseOS architecture) — distinct-by-design, not to be unified.
- `[ ]` **Footnote numbering continuity** — SA1 (Ch.1) runs 1–5, SA2 (Ch.2 RRL) runs 1–2, SA3.1 (Ch.3) runs 1–7. When compiled into a single document, footnote numbers must be unified into one sequence or converted to per-chapter endnotes.
- `[ ]` **TOC page numbers, all chapters (SA1 + SA2 + SA3.1)** — SA2 (Ch.2 RRL) added 2026-07-08 with the same mechanism (TOC + List of Tables + List of Figures — a List of Figures and Figure 2.1 were added 2026-07-08; page digits derived via `find_page()`): chapter-local Table of Contents in the HTML files (SA3.1 also has List of Tables/List of Figures, per writing-guidelines.pdf Section F/G — SA1 has neither tables nor figures so only needs a TOC), with real page numbers baked in (`#anchor` links + hardcoded page digits) and a PDF outline/bookmarks sidebar generated at export time (`tools/export_pdf.py`, via `Page.printToPDF`'s `generateDocumentOutline`). Chromium's print-to-PDF does not support live `target-counter()` page cross-references for the in-page TOC text (verified — Prince/WeasyPrint have it, Chromium doesn't), so the hardcoded `.toc-page` numbers are **not self-updating**: any edit that shifts pagination (added/removed paragraphs, tables/figures) requires re-deriving them (`tools/export_pdf.py` → its `find_page()` helper → update the `.toc-page` spans → re-export) before they can be trusted again. The PDF outline itself, by contrast, *is* regenerated correctly on every export automatically — only the in-page TOC text needs manual upkeep. Re-check before every future submission of either chapter, and again when both are merged into the full multi-chapter thesis (where page numbers reset relative to front matter anyway).
- `[x]` **PDF/TXT regeneration.** **Fixed 2026-07-04:** both PDFs re-exported from the corrected HTML via a headless Chromium render (Playwright + Liberation/DejaVu fonts, resolved entirely in user space — no root available in this environment). TXT mirrors deleted; HTML is now the sole source of truth (resolves the pending decision from `state.md`).
- `[ ]` **Scope guard — vision/business framing must not enter the thesis text.** D12/D13 and `vision.md` establish that SynapseOS presents its own distro identity over a reused Debian+XFCE substrate, with a deferred post-thesis "Overlay" product mode (hotkey/systray-summoned agent over a fully visible desktop). None of this — "distro," "own identity," hardening-as-differentiator, commercial/mall-PC framing — belongs in the academic chapters; it would overclaim relative to what the study evaluates. At most, D13's overlay idea earns one hedged sentence under a "Recommendations for Future Work" section. The thesis text should keep describing SynapseOS as a conversational shell for the CLI, evaluated per D5–D9, full stop. (Standing guard, not a one-time fix — re-check on every future revision.)
