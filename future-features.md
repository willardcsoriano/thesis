## Overview

This file holds product and research ideas that surfaced during thesis development but are explicitly out of scope for the thesis prototype. Nothing here is abandoned — these are the right next steps for SynapseOS after the thesis is defended. Items are grouped by concern. Each entry notes why it was deferred and what would be needed to implement it. The thesis prototype is evaluated only on what is in scope; these features should not appear in the methodology, results, or discussion unless cited as future work.

## Table of Contents

- [Overview](#overview)
- [GUI Automation (Deferred Architecture)](#gui-automation-deferred-architecture)
  - [Tier 1 — MCP-Exposed Application APIs](#tier-1-mcp-exposed-application-apis)
  - [Tier 2 — AT-SPI Accessibility Tree](#tier-2-at-spi-accessibility-tree)
  - [Tier 3 — Vision-Based GUI Simulation](#tier-3-vision-based-gui-simulation)
- [Model Options](#model-options)
  - [Llama 3.2-1B — Constrained Hardware Fallback](#llama-32-1b-constrained-hardware-fallback)
  - [Qwen2.5-VL-3B — Unified Vision-Language Model](#qwen25-vl-3b-unified-vision-language-model)
  - [T5-nl2bash — Ultra-Small Option](#t5-nl2bash-ultra-small-option)
  - [Model Hot-Swapping](#model-hot-swapping)
- [Installation and Hardening](#installation-and-hardening)
  - [Hardening Profiles](#hardening-profiles)
  - [Conversational Hardening Application](#conversational-hardening-application)
- [First-Run Experience](#first-run-experience)
  - [Conversational Setup Wizard](#conversational-setup-wizard)
- [Deployment Modes](#deployment-modes)
  - [VPS / SSH First-Class Support](#vps-ssh-first-class-support)

## GUI Automation (Deferred Architecture)

These are the three tiers of the original hybrid execution architecture, removed in D7. The research data and rationale are preserved in `decisions.md` (D4, D7) and `research methods/module 3/references/llm-selection-research.txt`.

### Tier 1 — MCP-Exposed Application APIs

Structured API calls to applications that expose a Model Context Protocol interface. Most reliable execution path — direct semantic control, no UI scraping. Requires MCP wrappers for each target application.

**Deferred because:** application-specific, adds implementation complexity without changing the core research question (NL → CLI). Worth revisiting once the CLI layer is proven.

### Tier 2 — AT-SPI Accessibility Tree

Linux accessibility tree automation via AT-SPI. Generalizes across most graphical Linux applications without application modification — locate UI elements, trigger actions programmatically.

**Deferred because:** deep integration with GUI toolkits that vary across applications and break with updates. Too fragile for a controlled user study with fixed evaluation conditions.

### Tier 3 — Vision-Based GUI Simulation

Screenshot grounding via a small vision-language model (Moondream 2, 1.9B params, ~2 GB, ScreenSpot F1@0.5 = 80.4). Identifies UI elements by visual position; generates mouse and keyboard simulation commands. Universal fallback — works on any application.

**Deferred because:** significantly increases hardware requirements, introduces non-determinism (visual grounding accuracy varies), and extends scope beyond what the thesis timeline allows. Moondream 2 remains the best candidate when this tier is revisited.

---

## Model Options

### Llama 3.2-1B — Constrained Hardware Fallback

37% NL2Bash accuracy after LoRA fine-tuning. ~650 MB at Q4. For machines below the 4 GB RAM minimum. Accuracy is borderline for simple commands; unreliable for complex operations. Not recommended as primary — only viable if hardware constraints are the binding concern.

### Qwen2.5-VL-3B — Unified Vision-Language Model

Handles both intent parsing and vision grounding in a single model. Rejected for the thesis because it is effectively ~3.7–4B parameters (vision encoder adds ~0.7–1B), heavier than Coder-3B, and weaker at bash generation. Relevant again if Tier 3 (vision) is reintroduced, since it would eliminate the need for a separate Moondream 2 instance.

### T5-nl2bash — Ultra-Small Option

Flan-T5-base fine-tuned on the NL2Bash corpus (~250M parameters). Extremely limited generalization — only reliable on task types present in the NL2Bash corpus. No multimodal capability. Consider only for highly constrained embedded environments where even 1.8 GB is too large.

### Model Hot-Swapping

Allow users to switch the inference model mid-session (e.g., switch from Qwen2.5-Coder-3B to a cloud model for a specific task). Requires the model-agnostic Ollama backend to expose a session-level model selection command. Architecturally straightforward given D2 (model-agnostic design) — just not needed for the thesis.

---

## Installation and Hardening

### Hardening Profiles

Named presets applied during SynapseOS installation based on intended use:

| Profile | Target use | Key additions over default |
|---|---|---|
| **Personal** (default) | Desktop / personal computing | Root locked, UFW default-deny, auto-updates |
| **Server / Prod** | Exposed production server | fail2ban, SSH key-only auth, audit logging, minimal open ports |
| **Dev** | Development machine | Docker group access, relaxed firewall for dev ports, no auto-reboot |
| **Syslog / Monitoring** | Log aggregation server | Log retention policies, specific inbound ports for log senders |

**Deferred because:** none of the research questions evaluate security posture. For the thesis, Personal (default) is applied uniformly to all evaluation machines. Building and testing multiple profiles is implementation work with no study payoff.

### Conversational Hardening Application

SynapseOS applying its own hardening through its own conversational interface — the system bootstraps its security configuration using the same NL → bash pipeline it uses for everything else. Strong design statement; demonstrates the system's own capability. Deferred — the thesis prototype needs to be stable before it self-configures.

---

## First-Run Experience

### Conversational Setup Wizard

On first launch (no user profile exists), SynapseOS runs a short setup conversation before opening the main interface:

1. "What should I call you?" — personalises the session
2. Model readiness check — if Ollama has not yet pulled the model, handles the download visibly and gracefully rather than freezing
3. One-sentence orientation — "You can ask me anything you'd normally type in a terminal."
4. Optional: apply hardening profile

**Deferred because:** the user study uses pre-configured machines. First-run setup is irrelevant in a controlled lab setting. High-value for a real product launch.

---

## Deployment Modes

### VPS / SSH First-Class Support

SynapseOS accessed remotely over SSH — the TUI session runs on a server, the user connects from any terminal. Architecturally already supported (TUI + CLI-only = no display server dependency). The 2 vCPU / 4 GB RAM minimum maps directly to a Hetzner CX21 (~€5.83/mo) or equivalent.

**Deferred because:** the thesis user study requires participants at a physical machine in a lab. Remote deployment is a product use case, not an evaluation condition. Worth one sentence in the thesis as a deployability claim.
