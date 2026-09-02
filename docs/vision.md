## Overview

SynapseOS is a conversational operating environment: the user states intent in natural language and the system carries it out, replacing memorized commands and manual GUI navigation with a dialogue. This document holds the *why* — the long-term bet that conversation becomes the primary human–computer interface, and the near-term thesis that produces the first credible evidence for it. It deliberately separates three layers so the ambition never contaminates the scope: the **north-star vision** (a decade-out world where you talk to your computer instead of operating it), the **thesis hypothesis** (a falsifiable claim the prototype can actually test), and the **wedge** (the smallest useful slice that proves the hypothesis — natural-language control of the command line). Read this to understand what SynapseOS is *for*; read `scope.md` for what gets built, `decisions.md` for why it's built that way, `stack.md` for how, and `roadmap.md` for where things currently stand. The guiding discipline: prove a small claim rigorously, and let the small claim point at the large one.

## Table of Contents

- [Overview](#overview)
- [North-Star Vision](#north-star-vision)
- [The Thesis Hypothesis](#the-thesis-hypothesis)
- [The Wedge — What the Prototype Actually Is](#the-wedge-what-the-prototype-actually-is)
- [Principles](#principles)
- [What Success Looks Like](#what-success-looks-like)
- [Non-Goals](#non-goals)
- [Horizons](#horizons)

## North-Star Vision

The interface to a computer has been indirect for fifty years. Whether the user types `find . -name '*.pdf' -mtime -7` or clicks through four nested menus, they are translating an intention into the machine's vocabulary — learning where the system keeps its verbs. The GUI made that translation visual instead of textual, but it did not remove it. The user still adapts to the machine.

The bet behind SynapseOS is that this is now optional. Language models have made it possible for the machine to adapt to the user: to accept intent in the user's own words and resolve it into the system's actions. The north star is an operating environment where **conversation is the primary interface** — where "clean up my downloads folder" or "why is my laptop slow right now" is a complete and sufficient instruction, and the windows-icons-menus-pointer paradigm becomes one rendering option among several rather than the ground floor of interaction.

If that shift is real, it is disruptive in the strict sense: it changes what an operating system *is for*. The incumbents are architected around the GUI as the substrate. A conversation-first environment is not a better GUI — it is a different bet about where the interface lives. The ambition of this project is to make that bet legible and credible enough that it becomes a direction the field has to take seriously.

**This is the horizon, not the deliverable.** A thesis prototype does not dethrone Windows, and framing it that way would be a defense liability, not a strength. What the prototype can do is far more valuable: produce the first rigorous, measured evidence that the conversational interface *outperforms* the status quo on real tasks for real users. Evidence is the wedge that moves incumbents — not a competing product.

## The Thesis Hypothesis

Everything above collapses to one testable claim:

> For real operating-system tasks, a natural-language conversational interface lets users accomplish their intent **faster, with fewer errors, and lower cognitive load** than the interface they use every day — and the advantage is largest for users who lack command-line fluency.

This is falsifiable. It has a control (the participant's own primary OS — the expert baseline of D9), quantitative outcomes (task completion time, error rate, SUS, NASA-TLX), and a directional prediction (the novice/power-user split of D5). If the data comes back flat or negative, the hypothesis is wrong and the thesis says so. That is what makes it research rather than advocacy.

Note what the hypothesis does **not** claim: it does not claim SynapseOS replaces the GUI, handles every task, or beats a fluent power user at their own terminal. It claims that *for the tasks it covers*, conversation is a better interface than what people use now. Winning that narrow claim rigorously is what earns the right to gesture at the north star.

## The Wedge — What the Prototype Actually Is

The scoped artifact is a **conversational shell**: a fullscreen interface where the user types natural language, a local small language model translates it into a shell command, and the system executes it after a safety check — with the command output and the model's reasoning shown back in the conversation.

The wedge is the command line for a deliberate reason. The CLI is simultaneously the most *powerful* interface a computer has (anything expressible as a command) and the least *accessible* (you must know the command). That gap is the sharpest possible demonstration of the thesis: natural language dissolves exactly the barrier — memorized syntax — that keeps the most capable interface out of most people's hands. A win here is the strongest evidence per unit of build effort. (See D7 for why GUI automation is out of scope, and D11 for why the study runs in GUI-shell mode despite the CLI-only capability.)

## Principles

These are the non-negotiable commitments that define SynapseOS regardless of horizon. They are design constraints, not features.

- **Local-first and private by default.** An OS-level agent observes everything the user does. Inference runs on-device on a local SLM; no command, file, or activity leaves the machine unless the user explicitly opts into a cloud model with their own key (D2). Privacy is not a setting — it is the default architecture.
- **Model-agnostic.** The system is not a wrapper around one vendor's API. Ollama decouples the runtime from the inference engine today (D8, though whether Ollama's specific packaging is worth keeping long-term vs. embedding the inference engine more directly is an open, unresolved reconsideration — see D8's Status line); the local model is swappable and the cloud path is opt-in, not load-bearing. The contribution is the *system*, not the model.
- **Reversible and consent-gated.** The system never runs an irreversible operation without explicit confirmation. Reversible operations are undoable (confirmation gate + undo log), and confirmed irreversible ones are too, wherever a bounded target exists to protect (content backup, trash, metadata backup, git-reset capture — see `safety-model.md`). Trust is the precondition for a conversational interface having any authority at all; the safety model is what earns it.
- **Intent over syntax.** The user expresses *what they want*, never *how the system encodes it*. Every design choice is measured against whether it moves work off the user and onto the machine.

## What Success Looks Like

Kept honestly separate, because they are different bars.

**Thesis success (the deliverable):** a working prototype, a clean within-subjects study (n=20), and a statistically defensible result on the hypothesis above — including an honest negative or null result if that is what the data shows. Success is *a credible answer*, not a favorable one.

**Vision success (the horizon):** the result is strong and clear enough to be worth building on past the thesis — enough signal that a conversation-first environment is a direction worth a product, a follow-on research program, or a response from the incumbents. This is out of scope to *deliver* and in scope to *point at*.

## Non-Goals

Stated explicitly so the north star cannot silently expand the build:

- **Not** a GUI-automation agent. No clicking, no accessibility-tree driving, no vision-based screen control (D4, D7).
- **Not** a full desktop-environment replacement for the thesis. The study runs in a fullscreen conversational shell approximating an active desktop; the wallpaper-layer compositor integration is deferred (D11, `future-features.md`).
- **Not** a cloud service. No accounts, no telemetry-to-vendor, no network dependency for core function.
- **Not** a claim to replace the terminal for fluent power users. The target is the intent-to-syntax gap, most acute for everyone else.

## Horizons

| Horizon | What exists | Interface |
|---|---|---|
| **H0 — Thesis prototype** | Conversational shell over bash; local SLM; safety gate; session memory; telemetry | Fullscreen conversational GUI with XFCE fallback (study, D20) / TUI (server) / CLI (scripting, D19) |
| **H1 — Beyond thesis** | Persistent memory, richer task coverage, active-desktop compositor layer | Conversation as the desktop shell |
| **H2 — North star** | Conversation as the primary OS interface; GUI as one rendering, not the substrate | Talk to the computer |

H1's near-term, buildable shape is the **Overlay** product mode (D13): the traditional desktop stays fully visible and usable, and SynapseOS is summoned via hotkey or systray icon rather than occupying the whole session. It is the lower-effort stepping stone toward the full wallpaper-layer active desktop, built from the same runtime as H0 (see "one slot, two or three sets of clothes" in `layers.md`).

The whole strategy in one line: **build H0 small and prove it rigorously; let the evidence, not the ambition, argue for H1 and H2.**

For current status and where to look at each horizon, see `roadmap.md`.
