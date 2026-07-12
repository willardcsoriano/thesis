## Overview

This is a frozen snapshot of the Paged.js-based same-page-footnote rendering pipeline, retired 2026-07-12 after a cost/benefit review. It successfully rendered genuine page-bottom footnotes (`float: footnote`, verified working across several sessions) but inflated the consolidated proposal from 42-43 pages to 62, and the vendored Paged.js polyfill turned out to have real layout-fidelity gaps (see below). The living consolidated document now uses endnotes-per-chapter again (plain Chromium print-to-PDF, `tools/export_pdf.py`), matching what was actually submitted. Nothing here is deleted — it's kept in case same-page footnotes are worth revisiting later, e.g. for a final thesis copy where page count matters less.

## Table of Contents

- [Overview](#overview)
- [What's here](#whats-here)
- [Why this was retired](#why-this-was-retired)
- [Reviving this approach](#reviving-this-approach)

## What's here

- `SynapseOS_Proposal_Chapters_1_to_3_paged.html` — the consolidated proposal, last live state before retirement (font bug fixed, duplicate ids namespaced, orphaned headings wrapped, TOC/LOT/LOF verified — see Session 15 in `docs/session.md`)
- `SynapseOS_Proposal_Chapters_1_to_3_paged.pdf` — its last rendered output, 62 pages
- `paged.polyfill.js` — the vendored Paged.js library (921 KB) that makes `float: footnote` work, since no browser implements it natively (confirmed via MDN, 2026-07-12 — still true as of this writing)
- `export_pdf_paged.py` — the export script that drives Paged.js via a headless, vendored Chromium and waits for `window.pagedFinished` before printing. Its internal paths assume it lives in `tools/`, so it will not run correctly from here without adjustment (see below)

## Why this was retired

Same-page footnotes require a JS polyfill today — no browser (Chrome, Firefox, Safari) implements CSS `float: footnote` natively. That's an unavoidable cost of the feature itself, and by itself would have been an acceptable tradeoff.

What tipped the decision was that the page-count cost turned out to be much larger than footnote-space overhead alone justifies. Measured directly (Session 15):

- Only 14 of 62 pages actually carry a footnote; genuine footnote-space overhead accounts for a modest fraction of the growth.
- Average body-text density: ~257 words/page in the Paged.js output vs. ~392-402 words/page in the plain-Chromium output — a ~35% density drop present even on completely footnote-free pages.
- Visual inspection confirmed why: some pages (e.g. p.18) pack edge-to-edge exactly like the plain-Chromium output; others (e.g. p.16) end with 1.5-2 inches of dead blank space and no footnote in sight. Paged.js's JS-driven page-break estimation is imprecise at some fragmentation points in a way native browser layout isn't.
- Separately, `break-after`/`page-break-after: avoid` — needed to stop headings from being stranded alone at the bottom of a page — silently does nothing in this vendored Paged.js build (traced through its source; "avoid" is only ever excluded from the small set of values it treats as *forcing* a break, never enforced as an actual constraint). The working fix was wrapping each affected heading with its next paragraph in a `break-inside: avoid` container, verified working — but finding that out took real debugging effort per heading, each of which triggered a full re-export and TOC re-verification cycle. That same `page-break-after: avoid` works correctly, no workaround needed, in the plain-Chromium pipeline now used instead.

Net: for the ~19-20 extra pages this pipeline costs, roughly half or more comes from the polyfill's own layout imprecision rather than the footnotes themselves, and the tooling is meaningfully more fragile to maintain. Endnotes-per-chapter are a normal, accepted academic convention — not a downgrade — so the tradeoff wasn't worth it for a proposal document.

## Reviving this approach

1. Copy `export_pdf_paged.py` back into `tools/` (its `TOOLS_DIR`/`VENDOR_DIR` path logic assumes that location).
2. Copy `paged.polyfill.js` back into `research-methods/consolidated/` (or wherever the target HTML lives) and update the `<script src="...">` reference in the HTML to match.
3. Treat `SynapseOS_Proposal_Chapters_1_to_3_paged.html` as a starting point, not a drop-in replacement — re-merge it against whatever the plain/flat document has become by then, since the two will have diverged.
4. Before trusting the output, re-check whether any major browser has shipped native `float: footnote` support in the meantime — that would remove the need for the polyfill (and likely its layout-fidelity issues) entirely.
