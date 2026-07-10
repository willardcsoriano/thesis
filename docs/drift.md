## Overview

This file tracks every place in the project where a stated fact, dependency version, or technical claim can go stale between when it was written and when it's next read — model choices superseded by new releases, library/API syntax that changes across versions, benchmark citations that newer papers supersede, and OS/package versions tied to a moving target. It exists because this project already had one incident of exactly this kind: the Qwen2.5-Coder-3B-Instruct selection (`decisions.md` D3) was reaudited on 2026-07-05 against models that didn't exist when it was first chosen, and came back confirmed — but easily could not have. Most of what's built here — the Go prototype, the Python export tooling, the SLM landscape the whole thesis depends on — sits in fast-moving ecosystems where a training-data snapshot lags behind reality. Read this before writing new code against an unfamiliar API, before citing a technical fact as current, and before resuming work after any gap.

## Table of Contents

- [Overview](#overview)
- [Principle](#principle)
- [Drift-Prone Surface Area](#drift-prone-surface-area)
- [When to Recheck](#when-to-recheck)

## Principle

Never rely on memorized/trained knowledge for anything time-sensitive — library versions, CLI flags, config keys, current model rankings, current release names, current best practices — without verifying against a live source first. Verification priority, in order: (1) Context7 MCP for library/API/framework documentation, (2) WebSearch/WebFetch for canonical docs, release notes, papers, and model cards, (3) training data — only ever as a last resort, and only when explicitly flagged to the reader as unverified. Never silently fall back to memory on a time-sensitive claim.

This mirrors the standing global rule this project's author already works under; this file's only job is to say *specifically what, in this repo* is drift-prone enough to need it, since "verify everything" without a concrete list is easy to forget to apply.

## Drift-Prone Surface Area

| What | Where it lives | Why it drifts | Recheck how |
|---|---|---|---|
| SLM choice (Qwen2.5-Coder-3B-Instruct) | `decisions.md` D3 | New small-model releases ship roughly monthly; any could plausibly beat it on NL2Bash-specific grounds | WebSearch for newer sub-4B models with shell/NL2Bash benchmark evidence (not just general reasoning scores) — see D3's 2026-07-05 reaudit as the template for what "checked" looks like |
| NL2Bash / shell-generation benchmark citations | SA3.1 Section 2.1a; references [2], [25] | Newer papers can supersede cited accuracy numbers or introduce a better standard evaluation methodology | Recheck before citing any specific accuracy percentage as current or as "state of the art" |
| Go / Ollama / bubbletea / lipgloss APIs | `prototype/` | Pre-1.0 libraries change API surface fast; the prototype hasn't been built past M2 yet, so most of this surface is still unwritten | Verify current API shape via Context7 or official docs before writing each new milestone (M3+) — don't assume a memorized function signature is still correct |
| Playwright API | `tools/export_pdf.py` | Frequent releases; the CDP-level features used here (`generateDocumentOutline`) are explicitly experimental | `tools/requirements.txt` pins the working version — if ever upgraded, re-verify the CDP call still behaves the same way before trusting a re-export |
| Debian / XFCE version specifics | `decisions.md` D12; `scope.md` hardware specs | A new Debian stable release could ship before the study's hardware is provisioned | Verify "Debian 13 (Trixie)" is still the correct target immediately before final hardware setup, not just at design time |
| Mapúa writing guidelines | `research-methods/module 3/references/writing-guidelines.pdf` | Static institutional document — low drift risk, listed for completeness | Only recheck if the department issues a revised version |

## When to Recheck

- Before any chapter submission (formative, summative, or final).
- Before writing new prototype code against a library not yet used elsewhere in this repo.
- After any gap in work longer than roughly a month — the SLM landscape specifically moves fast enough that a month-old assumption is worth re-verifying, not just resuming from.
- Whenever a reviewer, committee member, or panelist questions whether a technical claim is still current — treat that as a prompt to actually check, not just reassert the existing citation.
