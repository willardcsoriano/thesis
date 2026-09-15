## Overview

SynapseOS is a third way of operating a personal computer, alongside the command line and the graphical desktop rather than in place of either: the user states intent in natural language and an agent carries it out on a desktop that keeps working underneath. It is aimed at people fluent in **neither** existing interface — the CLI excludes by demanding memorised syntax, the GUI by offering only what its designers anticipated — which is why this is best understood as a new form of graphical interface rather than as the command line made easier. This document holds the *why*: the long-term bet that conversation becomes the primary human–computer interface, and the near-term thesis that produces the first credible evidence for it. It deliberately separates three layers so the ambition never contaminates the scope: the **north-star vision** (a decade-out world where you talk to your computer instead of operating it), the **thesis hypothesis** (a falsifiable claim the prototype can actually test), and the **wedge** (the smallest useful slice that proves it — the shell as the implementation substrate, because that is where the machine's full capability is reachable). Read this to understand what SynapseOS is *for*; read `scope.md` for what gets built, `decisions.md` for why it's built that way, `stack.md` for how, and `../prototype/README.md` for where things currently stand. `README.md` in this folder maps every document and the order to read them in. The guiding discipline: prove a small claim rigorously, and let the small claim point at the large one.

## Table of Contents

- [Overview](#overview)
- [North-Star Vision](#north-star-vision)
- [The Thesis Hypothesis](#the-thesis-hypothesis)
- [The Wedge — What the Prototype Actually Is](#the-wedge-what-the-prototype-actually-is)
- [Principles](#principles)
- [What Success Looks Like](#what-success-looks-like)
- [Non-Goals](#non-goals)
- [Horizons](#horizons)
  - [Where each horizon's detail lives](#where-each-horizons-detail-lives)

## North-Star Vision

The interface to a computer has been indirect for fifty years. Whether the user types `find . -name '*.pdf' -mtime -7` or clicks through four nested menus, they are translating an intention into the machine's vocabulary — learning where the system keeps its verbs. The GUI made that translation visual instead of textual, but it did not remove it. The user still adapts to the machine.

The bet behind SynapseOS is that this is now optional. Language models have made it possible for the machine to adapt to the user: to accept intent in the user's own words and resolve it into the system's actions. The north star is an operating environment where **conversation is the primary interface** — where "clean up my downloads folder" or "why is my laptop slow right now" is a complete and sufficient instruction, and the windows-icons-menus-pointer paradigm becomes one rendering option among several rather than the ground floor of interaction.

If that shift is real, it is disruptive in the strict sense: it changes what an operating system *is for*. The incumbents are architected around the GUI as the substrate. A conversation-first environment is not a better GUI — it is a different bet about where the interface lives. The ambition of this project is to make that bet legible and credible enough that it becomes a direction the field has to take seriously.

**This is the horizon, not the deliverable.** A thesis prototype does not dethrone Windows, and framing it that way would be a defense liability, not a strength. What the prototype can do is far more valuable: produce the first rigorous, measured evidence that the conversational interface *outperforms* the status quo on real tasks for real users. Evidence is the wedge that moves incumbents — not a competing product.

## The Thesis Hypothesis

Everything above collapses to one testable claim:

> For real operating-system tasks, a natural-language conversational interface lets users accomplish their intent **faster, with fewer errors, and lower cognitive load** than the interface they use every day — and the advantage is largest for users who are fluent in **neither** interface the machine currently offers.

The last clause matters and is easy to get wrong. The target is not "people who cannot use a terminal." Both existing interfaces exclude, in different ways: the command line demands memorised syntax, and the graphical desktop demands knowing where a capability lives and whether its designer thought to build a control for it at all. A person can be at home in neither — able to browse and type a document, unable to find the ten largest files on their disk by either route. That person is the one this is for.

This is falsifiable. It has a control (the participant's own primary OS — the expert baseline of D9), quantitative outcomes (task completion time, error rate, SUS, NASA-TLX), and a directional prediction (the novice/power-user split of D5). If the data comes back flat or negative, the hypothesis is wrong and the thesis says so. That is what makes it research rather than advocacy.

Note what the hypothesis does **not** claim: it does not claim SynapseOS replaces the GUI, handles every task, or beats a fluent power user at their own terminal. It claims that *for the tasks it covers*, conversation is a better interface than what people use now. Winning that narrow claim rigorously is what earns the right to gesture at the north star.

## The Wedge — What the Prototype Actually Is

The scoped artifact is an **agentic layer over an ordinary desktop**: the user's XFCE session stays exactly where it is — their windows, their file manager, their browser — and SynapseOS is the thing they talk to when they want the machine to *do* something. The user states intent in natural language, a local small language model turns it into a shell command, the system checks it for recoverability, executes it after any confirmation the check demands, and answers in natural language. The command is shown, never hidden: the user approves what they can see (D30).

The closest existing thing is an agentic coding assistant — Claude Code, Cursor, Aider — generalised from a code repository to the whole machine, and aimed at people who are fluent in neither of the interfaces a computer already offers rather than at developers already fluent in both. That comparison is the clearest statement of what this is, and `decisions.md` D27 is where the scope was narrowed to it: SynapseOS layers onto the desktop rather than replacing the desktop shell, session manager, and application launcher, because replacing a working desktop with an interface that cannot yet do visual tasks is a bad trade for the user and unnecessary for the research question.

**Debian and XFCE are the substrate, not the subject.** An agent layer that observes and acts on a desktop session needs a platform that permits it; Windows and macOS are proprietary and do not. The choice is what makes the experiment possible, not what the experiment is about (D28). Generalisation to other platforms is a limitation, not a finding.

**This is a new form of graphical interface, not a replacement for the terminal.** The framing to resist is that SynapseOS is "the CLI made easy." It is a third way of operating a computer, sitting alongside the two that exist, and it is defined by what both of those demand of the person:

| | What it can express | What it demands of you |
|---|---|---|
| **Command line** | nearly anything the system can do | the exact syntax, from memory |
| **Graphical desktop** | what its designers built a control for | knowing where that control lives |
| **Conversational** | anything the system can do *and* the model can express | that you can say what you want |

The command line is the *implementation* substrate for a deliberate reason — it is where the machine's full capability is reachable, so grounding intent there avoids inheriting the GUI's ceiling. But the command line is not the audience, and "dissolving memorised syntax" understates the claim: a graphical desktop excludes just as effectively by burying a capability four menus deep, or by never exposing it at all. A win here is the strongest evidence per unit of build effort precisely because it addresses both exclusions with one mechanism. (See D7 for why GUI automation is out of scope, and D11 for why the study runs in GUI-shell mode despite the CLI-only capability.)

## Principles

These are the non-negotiable commitments that define SynapseOS regardless of horizon. They are design constraints, not features. **The first two are listed first deliberately: when any principle here conflicts with another, the experience of the person using the system wins.** A design that is private, adopted, and reversible but opaque to its user has failed at the thing this project exists to test.

- **The user can know everything the system does, and chooses how much of it to see.** Every action is knowable to the person it acts for: the command that was generated, why it was classified as it was, what it changed, and how to reverse it. None of that is hidden and none of it is discarded.

  *Knowable* is not the same as *shown*, and the distinction is the whole design. The default surface is an answer to what was asked; the evidence sits one step beneath it, always reachable, never imposed. This is what reconciles two commitments that otherwise read as contradictory — that the generated command is displayed because a user cannot consent to what they cannot see (D30), and that results are reported in natural language rather than dumped as raw output (D31). The answer is the surface; the command, the output, the exit status, and the recovery record are underneath it; the user sets the depth. A system that buries what it did is not safe merely because it asked first, and a system that floods the user with everything it did has not informed them either.

- **UX is the constraint; UI is downstream of it.** What a person can know, decide, and undo is settled first, and the rendering follows. This is not a slogan — it is enforced structurally. Every decision that matters (classification, gating, recovery, what gets recorded) lives in the shared core, and an interface mode's job is only ever to collect input, drive that core, and render output (D26, `interface-modes.md`). A change to any interface therefore cannot change what the system does or what the user is told, only how it looks. That is also why the modes are cheap and the core is not: CLI, TUI, and the fullscreen study session are the same program wearing different frames, and the frame is the easy part.

  The corollary is a testable claim rather than a preference: if this is right, the study's workload and satisfaction measures should move with the interface paradigm, not with the polish of any particular rendering.

- **Local-first and private by default.** An OS-level agent observes everything the user does. Inference runs on-device on a local SLM; no command, file, or activity leaves the machine unless the user explicitly opts into a cloud model with their own key (D2). Privacy is not a setting — it is the default architecture.
- **Model-agnostic.** The system is not a wrapper around one vendor's API. Ollama decouples the runtime from the inference engine today (D8, though whether Ollama's specific packaging is worth keeping long-term vs. embedding the inference engine more directly is an open, unresolved reconsideration — see D8's Status line); the local model is swappable and the cloud path is opt-in, not load-bearing. The contribution is the *system*, not the model.
- **Reversible and consent-gated.** The system never runs an irreversible operation without explicit confirmation. Reversible operations are undoable (confirmation gate + undo log), and confirmed irreversible ones are too, wherever a bounded target exists to protect (content backup, trash, metadata backup, git-reset capture — see `safety-model.md`). Trust is the precondition for a conversational interface having any authority at all; the safety model is what earns it.
- **Intent over syntax.** The user expresses *what they want*, never *how the system encodes it*. Every design choice is measured against whether it moves work off the user and onto the machine.
- **Adopt by default; build only what cannot be adopted.** Reinvention is the default failure mode of an ambitious systems project, and it is expensive twice — once to build, and again to defend as a contribution when it is really a worse version of something that already exists. The kernel, the userland, the desktop, the window manager, the inference server, the model, the terminal rendering, the shell itself: all adopted, none reimplemented. Something is built here only when nothing existing does the job, and when it does get built it is named as such.

  **This is what defines the contribution.** If the standing rule is to adopt, then whatever remains after adopting everything possible *is* the original work, by construction rather than by assertion. It is a far stronger position in front of a panel than claiming novelty for an integration: *here is everything we took off the shelf; here is the short list of what did not exist; that list is the thesis.* The design record for everything on that list lives in `algorithms.md`, and the reason the list is short is this principle, working as intended.

## What Success Looks Like

Kept honestly separate, because they are different bars.

**Thesis success (the deliverable):** a working prototype, a clean within-subjects study (n=20), and a statistically defensible result on the hypothesis above — including an honest negative or null result if that is what the data shows. Success is *a credible answer*, not a favorable one.

**Vision success (the horizon):** the result is strong and clear enough to be worth building on past the thesis — enough signal that a conversation-first environment is a direction worth a product, a follow-on research program, or a response from the incumbents. This is out of scope to *deliver* and in scope to *point at*.

## Non-Goals

Stated explicitly so the north star cannot silently expand the build:

- **Not** a GUI-automation agent. No clicking, no accessibility-tree driving, no vision-based screen control (D4, D7).
- **Not** a desktop-environment replacement at all, for the thesis or after it. SynapseOS runs on top of XFCE, which keeps working (D27). The study runs it fullscreen as the session, with a participant-accessible fallback to the desktop beneath.
- **Not** a general conversational assistant. It answers about the machine and about what it did (D31); it does not answer from the model's own knowledge, and questions with no relationship to operating the computer are out of scope rather than badly served.
- **Not** a cloud service. No accounts, no telemetry-to-vendor, no network dependency for core function.
- **Not** a claim to replace the terminal for fluent power users, or the desktop for people who know their way around it. Both keep working and both stay available; this is a third option, not a substitution for either.
- **Not** aimed only at people who cannot use a command line. The audience is people fluent in neither interface — for whom the CLI is unreadable *and* the GUI only offers what someone else anticipated.

## Horizons

| Horizon | What exists | Interface |
|---|---|---|
| **H0 — Thesis prototype** | Agentic layer over bash; local SLM; recoverability gate; session memory; telemetry | Fullscreen TUI over a live XFCE desktop, with fallback (study, D20, D27) / TUI (server) / CLI (scripting, D19) |
| **H1 — Beyond thesis** | Persistent memory, richer task coverage, summoned rather than fullscreen | Conversation alongside the desktop |
| **H2 — North star** | Conversation as the primary OS interface; GUI as one rendering, not the substrate | Talk to the computer |

H1's near-term, buildable shape is the **Overlay** product mode (D13): the traditional desktop stays fully visible and usable, and SynapseOS is summoned via hotkey or systray icon rather than occupying the whole session. It is the lower-effort stepping stone toward the full wallpaper-layer active desktop, built from the same runtime as H0 (see "one slot, two or three sets of clothes" in `layers.md`).

The whole strategy in one line: **build H0 small and prove it rigorously; let the evidence, not the ambition, argue for H1 and H2.**

### Where each horizon's detail lives

H0 is the only horizon with detail worth tracking, and it is tracked in three places that each own a different question — what must exist (`scope.md`), in what order and when it counts as done (`prototype/build-order.md`), and what is true right now (`prototype/README.md`). The recoverability-analysis specification lives in `safety-model.md`. H1's ideas live in `notes/future-features.md`; H2 is this document.

*A fourth file, `roadmap.md`, previously restated H0's status as a "dashboard." It was retired 2026-09-12 — it had become the fourth copy of a status already held in three places, which is three opportunities for it to drift rather than one place to check. `scope.md`'s Critical Path owns the dependency graph; `prototype/README.md` owns live status.*
