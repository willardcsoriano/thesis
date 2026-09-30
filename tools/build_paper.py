#!/usr/bin/env python3
"""Builds the paper PDF: renders the HTML, settles the table-of-contents page numbers, and stamps
the page numbers the writing guidelines require.

Usage: SYNAPSE_CHROMIUM=/usr/bin/chromium tools/venv/bin/python tools/build_paper.py [paper.html] [paper.pdf]
"""
import subprocess
import sys
import tempfile
from pathlib import Path

TOOLS = Path(__file__).resolve().parent
sys.path.insert(0, str(TOOLS))
import export_pdf  # noqa: E402
from stamp_pages import stamp  # noqa: E402

html = Path(sys.argv[1] if len(sys.argv) > 1 else TOOLS.parent / "research-methods/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html").resolve()
pdf = Path(sys.argv[2] if len(sys.argv) > 2 else html.with_suffix(".pdf")).resolve()

with tempfile.TemporaryDirectory() as td:
    raw = Path(td) / "raw.pdf"
    for attempt in range(1, 6):
        export_pdf.export(html, raw)
        out = subprocess.run([sys.executable, str(TOOLS / "fix_toc.py"), str(html), str(raw)], capture_output=True, text=True, check=True).stdout
        print(f"pass {attempt}: {out.strip().splitlines()[-1]}")
        if out.strip().endswith("0 page numbers changed"):
            break
    else:
        raise SystemExit("table of contents did not settle")
    stamp(raw, pdf)
print("wrote", pdf)
