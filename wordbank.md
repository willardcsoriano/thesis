## Overview

This file collects terms, phrases, and concepts to be incorporated into the thesis paper at appropriate points. Entries are grouped by domain. Each entry includes a working definition and a note on where in the paper it is likely to land. Nothing here is final wording — these are seeds for the writing phase. Terms already used verbatim in the paper are noted as such.

## Table of Contents

- [Overview](#overview)
- [HCI and Research Design](#hci-and-research-design)
- [Architecture and System Design](#architecture-and-system-design)
- [GUI and Desktop Integration](#gui-and-desktop-integration)
- [Conversation and Memory Design](#conversation-and-memory-design)
- [Product and Vision](#product-and-vision)

## HCI and Research Design

**Expert baseline design**
Using the participant's most proficient, most familiar environment as the comparison condition — rather than a fixed alternative that may be unfamiliar to participants. Ensures that performance differences reflect interface quality, not OS familiarity. Contrast with a fixed baseline design, where all participants face the same comparison condition regardless of prior experience.
*Where it lands: Section 1.4 Why (already incorporated), Section 3.2 Evaluation Profile, and the Discussion/Conclusion when interpreting results.*

**Ecological validity**
The degree to which study conditions, tasks, and environments reflect real-world use. A study has high ecological validity when its findings are likely to hold outside the lab. Used to justify the expert baseline design (participants use their actual daily environment) and human-goal-framed task design (tasks reflect real computing goals, not artificial benchmarks).
*Where it lands: Section 1.4, Section 1.6 Task Design.*

**Within-subjects design**
An experimental design in which each participant completes all conditions, so each person serves as their own control. Reduces the sample size needed to detect effects and eliminates between-person variance as a confound. Counterbalancing (alternating condition order across participants) controls for order and carryover effects.
*Where it lands: Section 3.2 (already incorporated).*

**Counterbalancing**
The practice of alternating the order in which participants experience conditions to prevent order effects (performing better simply because a task is familiar the second time) from biasing results.
*Where it lands: Section 3.2.*

**Novelty effect**
The tendency for participants to perform differently with a new system simply because it is new — either better (excitement, curiosity) or worse (unfamiliarity). A threat to internal validity when one condition is familiar and the other is novel. Relevant because SynapseOS is new to all participants while their native OS is not; this should be acknowledged as a limitation.
*Where it lands: Section 3.4 Threats to Validity / Limitations.*

**Think-aloud protocol**
A data collection technique in which participants verbalize their thoughts while completing tasks. Surfaces reasoning, confusion, and intent that screen recording alone cannot capture. Used in the SynapseOS user study session procedure.
*Where it lands: Section 2.4 Study Session Procedure (already implied), qualitative analysis in Chapter 3/4.*

**Convergent mixed methods**
A research design that collects quantitative data (task time, error rate) and qualitative data (interviews, think-aloud) concurrently and integrates them at the interpretation stage. The SynapseOS study uses this structure.
*Where it lands: Chapter 2 framing, Discussion.*

---

## Architecture and System Design

**Session-scoped memory**
Conversation history retained only for the duration of a single login session; cleared on logout. The simplest memory model — no persistence layer required, no privacy exposure from stored history. The thesis prototype uses session-scoped memory.
*Where it lands: Section 2.1 System Design (when describing the conversation context model).*

**Context window**
The maximum token span a language model can attend to in a single inference call. Qwen2.5-Coder-3B-Instruct has an 8K token context window. Long sessions will eventually overflow it, requiring either a rolling window or summarization strategy.
*Where it lands: Section 2.1 System Design, limitations.*

**Rolling window**
A context management strategy in which the oldest messages are dropped from the active context when the context window limit is approached. Simple and stateless; loses early conversation detail but keeps inference within the model's capability.
*Where it lands: Section 2.1 (as a note on how session context is managed in the prototype).*

**Conversational summarization**
A context management strategy in which older conversation turns are compressed into a summary that replaces the raw history in the active context. Preserves more semantic content than a rolling window at the cost of an additional inference call.
*Where it lands: future-features.md (deferred), Discussion as a direction for future work.*

**Model-agnostic backend**
An inference backend design in which the application is decoupled from any specific model or provider. In SynapseOS, Ollama serves as the model-agnostic layer — swapping the local SLM for a cloud endpoint requires only a configuration change, not a code change.
*Where it lands: Section 2.1 (already in paper).*

**Confirmation gate**
A pre-execution intercept that classifies a pending bash command as reversible or irreversible. Irreversible commands (rm, mv to external, chmod) require explicit user confirmation before dispatch. Prevents accidental data loss from misinterpreted natural language.
*Where it lands: Section 2.1 (already in paper).*

---

## GUI and Desktop Integration

**Active desktop**
A computing pattern in which the desktop background is a live, interactive application rather than a static image. The foreground desktop layer (icons, windows) floats on top of it. Popularized by Windows Active Desktop (1998); revived in modern Linux via Wayland layer-shell protocols.
*Where it lands: Section 2.1 (GUI mode description, post-thesis product section, or future work).*

**Wallpaper-layer application**
An application rendered at the desktop background z-level, behind all other windows. Users see and interact with it whenever no foreground window is covering the screen. SynapseOS GUI mode targets this pattern.
*Where it lands: Section 2.1 system design description of the GUI mode.*

**wlr-layer-shell**
A Wayland protocol extension (part of the wlroots ecosystem) that allows applications to declare themselves as belonging to a specific desktop layer: background, bottom, top, or overlay. Used by wallpaper daemons (swww, swaybg) and SynapseOS's intended GUI mode. Supported by compositors including Sway and Hyprland.
*Where it lands: Section 2.1 (implementation detail of GUI mode).*

**xwinwrap**
An X11 utility that pins any application window as the root window (the desktop wallpaper layer). The X11 equivalent of the Wayland layer-shell approach. Serves as a fallback for non-Wayland systems.
*Where it lands: Section 2.1 (X11 fallback note).*

**WebKitGTK**
The GTK-integrated build of the WebKit browser engine. Provides the WebView rendering surface for Tauri applications on Linux. Enables web technologies (React, Tailwind, shadcn/ui) to run inside a native Linux desktop application.
*Where it lands: Section 2.1 (implementation stack note).*

---

## Conversation and Memory Design

**Session boundary**
The event or condition that delimits one conversation from the next. Candidates: system reboot, explicit user command ("new chat"), or a configurable inactivity timeout. Affects how history is segmented and retrieved in a persistent memory model.
*Where it lands: future-features.md (deferred); Discussion as future work direction.*

**Conversational context**
The accumulated history of the current session passed to the language model as part of each inference prompt. Gives the model memory of prior turns so it can resolve pronouns, follow up on previous commands, and maintain coherent multi-turn exchanges.
*Where it lands: Section 2.1 (already implicit in architecture description).*

---

## Product and Vision

**Conversational desktop**
A desktop computing paradigm in which the primary interaction surface is a persistent natural language chat interface rather than a graphical application launcher, taskbar, or file manager. SynapseOS instantiates this paradigm on Linux.
*Where it lands: Abstract, Introduction (Chapter 1 backport), Conclusion.*

**Ambient computing interface**
A computing interface that is always present in the environment but does not demand foreground attention. The SynapseOS active desktop mode is an ambient interface — it is always visible as the background layer, accessible instantly when foreground windows are dismissed.
*Where it lands: Discussion, related work comparison, product framing in Conclusion.*

**TUI mode / GUI mode**
The two deployment targets for SynapseOS. TUI mode (bubbletea): no display server required, SSH-friendly, targets servers and power users. GUI mode (Tauri + React + Tailwind, wallpaper-layer): requires Wayland/X11, targets personal machines and non-technical users. Both share the same Go backend, Ollama inference layer, and bash execution pipeline.
*Where it lands: Section 2.1 system design (deployment modes subsection).*
