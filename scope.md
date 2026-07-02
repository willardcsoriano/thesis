## Overview

This file tracks every concrete deliverable the methodology chapter (SA3.1) has already committed to. If the paper describes it as existing, the study depends on it, or results are computed from it — it goes here. Nothing in this list is optional. Items are grouped by type and marked with their current status. The goal is to prevent arriving at study time and discovering the paper promised something that was never built. Companion to `future-features.md` (deferred ideas) and `stack.md` (implementation stack reference).

## Table of Contents

- [Overview](#overview)
- [Status Key](#status-key)
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

## Software — Go Runtime (SynapseOS)

These are features of the SynapseOS binary that the paper explicitly describes. All live in the Go runtime.

- `[ ]` **Conversational TUI** — full-screen persistent chat interface (bubbletea + lipgloss); accepts NL input, renders responses and command output
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

- `[ ]` **Log parser** — reads SynapseOS session logs; extracts per-task metrics: completion time (task_start → task_end delta), error events, undo events, confirmation events
- `[ ]` **Metric computation script** — computes task completion time, error rate, SUS scores (standard formula: odd items: score−1; even items: 5−score; sum × 2.5), NASA-TLX composite (average of six subscales)
- `[ ]` **Anonymization step** — replaces all participant identifiers with participant ID codes before any analysis; runs before any output file is written
- `[ ]` **Statistical analysis script** — Shapiro-Wilk normality test; paired-samples t-test or Wilcoxon signed-rank (depending on normality result); Cohen's d effect size; descriptive statistics (mean, SD, 95% CI) per condition per group; subgroup analysis by OS type (Windows/macOS/Linux) as exploratory
- `[ ]` **Screen capture setup** — recording software configured on both the SynapseOS machine and the baseline machine before the study begins; output synced to participant session IDs

---

## Study Instruments — Documents

Physical or digital documents administered during the study. All need to be drafted, reviewed, and approved before participant recruitment opens.

- `[ ]` **Pre-study screening questionnaire** — captures: years of computer use, primary OS (Windows/macOS/Linux, ≥4 hrs/day), proficiency on primary OS (basic/intermediate/advanced), command-line familiarity (none/basic/intermediate/advanced), prior conversational AI exposure. Used to classify novice vs. power user.
- `[ ]` **Informed consent form** — describes study purpose, procedures, data collected, right to withdraw, anonymization policy. Signed before session begins. Must satisfy Mapúa IRB requirements.
- `[ ]` **Participant information sheet** — plain-language summary of the study given to participants before they consent
- `[ ]` **Task prompt sheet** — printed sheet listing all tasks in the assigned order, one task per page or clearly separated. Task wording in plain language, OS- and CLI-neutral.
- `[ ]` **SUS questionnaire form** — the standard 10-item System Usability Scale, administered immediately after each condition
- `[ ]` **NASA-TLX questionnaire form** — the standard 6-subscale form (Mental Demand, Physical Demand, Temporal Demand, Performance, Effort, Frustration), administered immediately after each condition
- `[ ]` **Semi-structured interview guide** — facilitator script covering: perceived ease of use, trust in the conversational interface, preference between conditions, open-ended feedback. 10–15 minutes post-study.
- `[ ]` **Debrief script** — full disclosure of study objectives; talking points for answering participant questions
- `[ ]` **Session facilitator checklist** — step-by-step checklist for running a session consistently across all 20 participants (machine setup, recording start, questionnaire handoff, break timing, session close)

---

## Study Instruments — Task Suites

- `[ ]` **Custom cross-platform task suite** — individual tasks with: unique ID, natural language prompt (as shown to participant), expected outcome, difficulty level (simple/moderate/complex), and per-platform equivalence note (how it is accomplished on SynapseOS bash / Windows / macOS / Linux GNOME). Covers four categories: file search and organization, system and process monitoring, application and package management, text and data processing.
- `[ ]` **OSWorld benchmark setup** — OSWorld evaluation environment configured on the SynapseOS machine; automated benchmark runs verified end-to-end before the main study

---

## Infrastructure — Hardware

Four physical machines required for the lab setup.

- `[ ]` **SynapseOS machine** — x86-64, 8 GB RAM recommended, 10 GB storage, Wayland compositor required; runs SynapseOS in GUI mode (fullscreen conversational interface) on a Debian 13 (Trixie) base
- `[ ]` **Windows 11 machine** — any x86-64 hardware; Windows 11 installed and configured with standard user account, no custom software beyond what a typical user would have
- `[ ]` **macOS machine** — Apple Silicon or Intel Mac; macOS (current release); standard user account
- `[ ]` **Linux / GNOME machine** — x86-64; GNOME desktop on a mainstream distro (Ubuntu 24.04 LTS recommended for familiarity); standard user account

---

## Infrastructure — Third-Party Setup

- `[ ]` **Ollama installed and verified** — Ollama running at `localhost:11434` on the SynapseOS machine; Qwen2.5-Coder-3B-Instruct Q4_K_M pulled and loaded
- `[ ]` **Screen capture software** — installed and tested on all four machines; output format and storage location standardized

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

- `[ ]` **Chapter 1 alignment** — SA1 (summative-assessment-1.html) reflects pre-D7 and pre-D9 decisions: RQ2 and Specific Objective 2 reference the three-tier MCP/AT-SPI/vision architecture (superseded by D7 — CLI-only scope); RQ3 and Objective 4 reference "conventional Linux graphical desktop" as the comparison condition (superseded by D9 — expert baseline design); Scope and Limitations states the LLM is "not yet committed" (resolved by D3 — Qwen2.5-Coder-3B-Instruct). All four items need updating when Chapter 1 is revised for final submission.
- `[ ]` **Cross-chapter terminology consistency** — wordbank.md terms incorporated into SA3.1 (expert baseline design, ecological validity, session-scoped memory, etc.) should be verified for consistent use across Chapter 1 and Chapter 2 in the compiled document.
- `[ ]` **Footnote numbering continuity** — SA1 and SA3.1 each have their own footnote sequences (1–7 each). When compiled into a single document, footnote numbers must be unified into a single sequence or converted to per-chapter endnotes.
