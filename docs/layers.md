## Overview

This document is a conceptual primer on how a Linux system is layered and exactly where SynapseOS fits within it. It exists because the project's boundary is easy to misjudge — SynapseOS is often imagined as a from-scratch operating system when it is in fact a new *interface layer* built on top of an unchanged Debian base. The guide walks the stack from firmware to applications, names the real components at each layer (kernel, userland, shell, display server, desktop environment), and pins SynapseOS to a single slot: the user-facing session. It also settles the recurring confusions — is this an OS, a distro, a userland replacement? — in a short FAQ, and defines the vocabulary in a glossary. Read it to place any part of the system correctly before reasoning about it. Companion to `stack.md` (the concrete technology) and `decisions.md` (why those choices were made).

## Table of Contents

- [Overview](#overview)
- [The Linux Stack, Bottom to Top](#the-linux-stack-bottom-to-top)
- [Layer by Layer](#layer-by-layer)
- [Where SynapseOS Sits](#where-synapseos-sits)
  - [The clean framing: SynapseOS is a shell](#the-clean-framing-synapseos-is-a-shell)
  - [One slot, two (or three) sets of clothes](#one-slot-two-or-three-sets-of-clothes)
- [What SynapseOS Is *Not*](#what-synapseos-is-not)
- [When It Becomes a "Distribution"](#when-it-becomes-a-distribution)
- [FAQ — Common Confusions](#faq-common-confusions)
- [Glossary](#glossary)
- [See Also](#see-also)

## The Linux Stack, Bottom to Top

A running Linux machine is a stack of layers. Each layer depends on the ones below it and is largely ignorant of the ones above. SynapseOS adds/occupies exactly **one** of them.

```
┌─────────────────────────────────────────────────────────┐
│  Applications            browsers, editors, media…       │
├─────────────────────────────────────────────────────────┤
│  SESSION / INTERFACE     shell (text)  OR  desktop env    │  ◄── SynapseOS lives here
│    graphical side needs a display server + a compositor   │
├─────────────────────────────────────────────────────────┤
│  Core userland           libc, coreutils, bash, grep, apt │  ◄── SynapseOS CALLS this (unchanged)
├─────────────────────────────────────────────────────────┤
│  Init & system services  systemd, daemons                 │
├─────────────────────────────────────────────────────────┤
│  Kernel                  Linux — hardware, processes, FS  │
├─────────────────────────────────────────────────────────┤
│  Bootloader / firmware   UEFI → GRUB                      │
└─────────────────────────────────────────────────────────┘
```

| Layer | What it does | Examples | SynapseOS touches it? |
|---|---|---|---|
| Applications | Programs the user runs | Firefox, VLC | No |
| **Session / interface** | **What greets the user at login and takes their input** | **bash prompt, GNOME, XFCE** | **Yes — occupies this slot** |
| Core userland | The command-line tools and libraries | glibc, coreutils, bash, `apt` | Calls them; leaves them unchanged |
| Init & services | Starts and supervises background services | systemd | No |
| Kernel | Talks to hardware; runs processes | Linux | No |
| Bootloader / firmware | Loads the kernel at power-on | UEFI, GRUB | No |

## Layer by Layer

**Firmware & bootloader.** UEFI firmware powers on and hands control to a bootloader (GRUB), which loads the kernel. Provided by Debian; nothing to build.

**Kernel (Linux).** The core: it drives hardware, schedules processes, manages memory, and exposes filesystems. Writing one is what "OS development" means in the hard sense — and it is explicitly *not* what this project does. Provided by Debian.

**Init & system services.** Once the kernel is up, `systemd` starts the services a running system needs (networking, logging, login). Provided by Debian.

**Core userland.** The libraries and command-line tools that make the system usable: `libc`, `coreutils` (`ls`, `cp`, `mv`), `bash`, `grep`, `findutils`, the `apt`/`dpkg` package tools. **This is the engine SynapseOS drives.** SynapseOS translates natural language into these commands and runs them (decision D7). It keeps the userland entirely — replacing it would delete the very tools the system depends on.

**Session / interface layer — the important one.** This is "what you talk to when you sit down," and it has two forms:

- *Text form:* a **login shell** (`bash`, `zsh`) — a prompt where you type commands.
- *Graphical form:* a **desktop environment** (GNOME, KDE, XFCE) — the windows, panels, file manager, and settings. A DE is itself a bundle that runs on top of a **display server / compositor** (an X11 server, or a Wayland compositor) that draws pixels and routes mouse/keyboard input.

Both the shell and the desktop environment fill the *same conceptual slot*: the user-facing session. **This is the slot SynapseOS takes.**

**Applications.** Everything the user launches on top of the session. Out of scope.

## Where SynapseOS Sits

SynapseOS is a **new interface layer at the session slot**. Two things are true about it at once, at different layers:

- **Additive to the machine.** It adds a layer *on top of* the userland — a natural-language front-end over the existing command-line tools. It touches nothing below it. This is why the project is tractable and why app-dev skills transfer: it is an application, not an OS rewrite.
- **Substitutive to the human.** The user no longer talks to a bash prompt or a desktop environment — they talk to SynapseOS. It *replaces the interface* the person used, even though it replaces nothing the machine runs.

```
        HUMAN
          │  natural language
   ┌──────┴───────┐
   │  SynapseOS   │   ADD this layer  →  it REPLACES the old prompt/DE at the surface
   └──────┬───────┘
   core userland (bash, coreutils…)   ← unchanged; SynapseOS calls it
   init · kernel · hardware           ← unchanged
```

### The clean framing: SynapseOS is a shell

Every shell in history has been an abstraction layer that *replaced the previous shell*: `bash` is a layer over the userland; `zsh` added a layer and replaced bash for those who switched; `fish` did it again. A new shell is always simultaneously additive (to the machine) and substitutive (of the old interface) — there is no contradiction, that is simply what a shell **is**. SynapseOS is **the next shell — a conversational one**. That single sentence is honest (no new kernel), additive (a layer), a replacement (of the interface), and novel (no daily-driver shell takes natural language).

### One slot, two (or three) sets of clothes

| Mode | Substrate underneath | SynapseOS puts there… | Target |
|---|---|---|---|
| **TUI** | bare Debian, no DE — a local **agentic shell**: NL → local SLM → proposed command → confirmation gate → execution, comparable in interaction model to Claude Code's CLI agent loop but fully offline (D2, D3, D12) | the SynapseOS terminal interface | server / remote (D11) |
| **GUI** | Debian + **XFCE** (X11 session), **invisible** — XFCE's Wayland session is still experimental, unsuitable for a reproducible study | the same SynapseOS app, launched **fullscreen within the XFCE session**, no escape hatch | user study (D11, D12) |
| **Overlay** *(post-thesis)* | Debian + XFCE, **fully visible and usable** — nothing hidden, no escape hatch to guard against because there is nothing to escape | the same SynapseOS app, summoned via hotkey or systray icon, floating over the desktop | commercial product (D13) |

All three are the **same program** — only the shell wrapped around it changes. In TUI and GUI mode, the substrate is invisible: XFCE supplies the display session and window management without ever being seen, exactly as Debian supplies the kernel and userland invisibly (D12). In Overlay mode the substrate is deliberately visible — the traditional desktop stays fully intact, and SynapseOS is one hotkey away rather than the whole session (D13). The GUI mode's lack of an escape hatch isn't a limitation carried into Overlay mode by accident — it is the whole point of GUI mode (study validity, D12) and deliberately absent from Overlay mode (product usability, D13).

## What SynapseOS Is *Not*

Stating the boundary explicitly, because overclaiming here is a defense liability:

- **Not a new operating system.** It contains no kernel. It runs on Debian's.
- **Not a userland replacement.** It keeps and calls the userland; it replaces the *shell/session* above it.
- **Not a desktop-environment reimplementation.** In GUI mode it runs *inside* a standard DE (XFCE) fullscreen — the DE keeps running underneath, invisible, not reimplemented or removed (see D12).
- **Not dishonest in claiming its own identity.** A reused, unadvertised substrate under a distinct branded identity is exactly how derivative distros work (Ubuntu/Debian, SteamOS/Arch, Pop!_OS/Ubuntu) — see D12 and "When It Becomes a Distribution" below.

## When It Becomes a "Distribution"

A Linux **distribution** is an integrated, bootable, installable whole. SynapseOS becomes one only if it is packaged as a **Debian derivative** — the same lineage as Ubuntu (from Debian) or Mint (from Ubuntu). The checklist:

| Ingredient | Source |
|---|---|
| Kernel, bootloader, init, userland, `apt` | Reused from Debian |
| **Default session = SynapseOS** | **The identity-defining contribution** |
| GUI host (X session, window management) | Reused from XFCE, unadvertised (D12) |
| Security/hardening defaults | The contribution — Ubuntu's exact playbook (`future-features.md`, Hardening Profiles) |
| Installable ISO | debian-installer or live-build + an installer |
| Branding / identity | Name, `/etc/os-release`, artwork |
| Update + security channel | Own package repo riding on Debian updates |

Note the pattern: identity is a top layer (name, default session, branding); everything below it is reused and unadvertised. That is not a shortcut around being "a real distro" — it is what every derivative distro does. Ubuntu doesn't tell a desktop user "this is Debian"; SteamOS doesn't say "this is Arch"; Pop!_OS doesn't say "this is Ubuntu." SynapseOS running on an invisible Debian+XFCE substrate (D12) is the same move, made explicit.

**Not all rows carry equal weight.** The default session is the *identity-defining* row — strip it and nothing distinguishes SynapseOS from a hardened Debian config; keep it alone and SynapseOS is still recognizably a different kind of system. Hardening is a *credibility* row — necessary for a serious, shippable product, and precedented (it's exactly how Ubuntu differentiates from bare Debian), but it is a commodity: most serious distros harden their defaults, so hardening alone does not make something feel like a different OS. If only one row can be gotten right first, it is the session, not the hardening profile (D13).

Until a bootable image exists, SynapseOS is an **application** you install on Debian — not yet a distro, because the installable-whole and update-channel ingredients are missing. This packaging is deferred to build milestone M9+ (see `prototype/build-order.md`). Even as a derivative, the honest claim is *"a Debian-based distribution,"* never *"a from-scratch OS."*

## FAQ — Common Confusions

**Is SynapseOS a new operating system?** No — it has no kernel of its own. It is an interface layer on top of Debian. The "OS" in the name is the product vision (see `vision.md`), not the engineering artifact.

**Am I replacing the userland?** No. You keep it and call it. You replace the *shell/session* that sits above it.

**What layer is XFCE?** A desktop environment, at the graphical side of the session layer. In GUI mode, XFCE (X11 session) is the invisible host SynapseOS runs fullscreen inside of — the participant never sees it (D12).

**Am I adding a layer or replacing one?** Both — additive to the machine, substitutive to the human. Exactly like any new shell.

**Can I call it a distro?** Only once you ship a bootable Debian derivative (default session = SynapseOS, plus branding, installer, and an update channel). Before that, it's an application.

**If it's Debian+XFCE underneath, is calling it "SynapseOS" as its own distro dishonest?** No — a reused, unadvertised base under a distinct branded identity is exactly how derivative distros work. Ubuntu, SteamOS, and Pop!_OS all do this; none surface their base to the end user either.

**Is there a version where I keep the traditional desktop and just summon the agent?** Yes — Overlay mode (D13), the post-thesis product target: XFCE stays fully visible and usable, SynapseOS opens via a hotkey or systray icon. It's a different mode from the study's GUI takeover (D12), built from the same runtime.

**Is hardening or the agentic layer the bigger reason this counts as a distro?** The agentic session, by a wide margin. Hardening is a credibility signal — expected of any serious distro, precedented by Ubuntu, but not distinguishing. The session is an identity signal — strip it and there's nothing left to call a distro; keep it and there still is. See D13.

**Do I need to write drivers, a kernel, or a bootloader?** No. Debian provides all of it. Your work lives entirely at the session layer and above.

**What does using SynapseOS actually feel like?** In TUI mode, closest reference point: Claude Code's agentic CLI loop — natural language in, the model reasons and proposes a shell command, a confirmation gate, then execution — except fully offline against an on-device SLM (D2, D3, D12).

## Glossary

- **Kernel** — the core program that runs hardware, processes, and memory. Linux is a kernel.
- **Userland** — everything that runs outside the kernel: libraries and command-line tools (`libc`, `coreutils`, `bash`, `grep`, `apt`).
- **Shell** — the program that takes user commands. Text shells: `bash`, `zsh`. SynapseOS is a conversational shell.
- **Display server / compositor** — draws pixels and routes input for graphical sessions. X11 uses a separate server + compositor; a Wayland **compositor** merges both roles. The thesis prototype uses XFCE's X11 session as the host (D12); a kiosk Wayland compositor (e.g. `cage`) or the `wlr-layer-shell` active-desktop mode are deferred post-thesis paths (`future-features.md`).
- **Desktop environment (DE)** — a bundle of graphical session software: window manager, panels, file manager, settings (GNOME, KDE, XFCE).
- **Session / interface layer** — the slot that greets the user at login; a shell (text) or a DE (graphical).
- **Distribution (distro)** — an integrated, bootable, installable Linux whole (kernel + userland + package manager + installer + default environment + release process).
- **Derivative distribution** — a distro built on another's base (Ubuntu ← Debian). SynapseOS's realistic path to being a distro.

## See Also

- `stack.md` — the concrete technology at each layer SynapseOS uses.
- `decisions.md` — D1 (Debian base), D7 (CLI-only scope), D8 (Go/Ollama stack), D11 (TUI vs GUI mode), D12 (distro identity vs. substrate: Debian+XFCE), D13 (Overlay product mode; hardening-vs-session weighting).
- `vision.md` — the product horizons (H0 thesis → H2 commercial distro).
- `prototype/build-order.md` — where the GUI/packaging work (M9+) sits in the build sequence.
