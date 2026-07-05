## Overview

This file tracks which parts of the PDF-export toolchain have been moved into Python (`tools/export_pdf.py`, running inside `tools/venv/`) versus which parts are native, compiled binaries that can never become Python code — they're vendored into `tools/vendor/` instead, so they live inside the repo rather than depending on anything installed system-wide. The short version: all *orchestration logic* (page loading, PDF generation, outline/bookmark generation, page-number lookup) is now Python; the actual browser engine, its shared libraries, the PDF-text-extraction tool, and the fonts are native code and data that Python can only call, never replace. A handful of things remain genuinely irreducible — present on the host machine no matter what — and those are listed separately below.

## Table of Contents

- [Overview](#overview)
- [Exported to Python](#exported-to-python)
- [Vendored, not Python (native binaries/data — `tools/vendor/`)](#vendored-not-python-native-binariesdata-toolsvendor)
- [Genuinely irreducible (can't be vendored away)](#genuinely-irreducible-cant-be-vendored-away)

## Exported to Python

- **`tools/export_pdf.py`** — the single entrypoint. Drives the browser, generates the PDF (including the outline/bookmarks sidebar via a direct `Page.printToPDF` CDP call), and provides `find_page()` for locating a heading's page number in an already-exported PDF.
- **`playwright`** (pip package, inside `tools/venv/`) — the only real Python dependency. Talks to the vendored Chromium over the DevTools Protocol; replaces the hand-rolled WebSocket/CDP client and the old ad-hoc bash invocation.
- **Page-load waiting, retries, CDP session handling** — previously hand-rolled (and buggy, as of the first hand-rolled attempt this session); now Playwright's job.

## Vendored, not Python (native binaries/data — `tools/vendor/`)

These can't be "exported to Python" because they aren't logic, they're compiled machine code or binary data. Python calls them; it doesn't replace them. Vendoring means the *files* live in the repo instead of the host system, but they still run as native code:

- **`tools/vendor/chromium/`** — the actual Chromium browser binary (`chrome`, ~280 MB) and its bundled resource files. A real web-rendering engine; there is no pure-Python substitute that renders this document's CSS/print layout correctly.
- **`tools/vendor/libs/`** — ~98 shared libraries (`.so` files: NSS, Cairo, Pango, X11, fontconfig, etc.) that the Chromium binary links against at runtime. Resolved once via `apt-get download` + `dpkg -x` (no root, no system install) rather than assuming they're present on the host.
- **`tools/vendor/pdftotext` / `pdfinfo`** — poppler's PDF-text-extraction tools, used by `find_page()` to read page-indexed text back out of a generated PDF. Same vendoring approach as the libs.
- **`tools/vendor/fonts/`** — Liberation and DejaVu font files (Times New Roman substitutes), since no system font config exists in this environment.

## Genuinely irreducible (can't be vendored away)

- **A Python 3 interpreter on the host** — `tools/venv/` is created *from* a system Python; a venv cannot bootstrap itself. This is the one unavoidable host dependency for any of this to run at all.
- **The OS kernel + dynamic linker (`ld.so`) + glibc** — every vendored `.so` file and the Chromium binary still get loaded and executed by the host's own kernel and linker. Vendoring the libraries doesn't vendor the kernel underneath them.
- **Architecture/distro compatibility** — everything in `tools/vendor/` was resolved for this specific machine (Debian 13 Trixie, x86-64). It is not portable to a different CPU architecture or a sufficiently different Linux distribution without re-resolving.
