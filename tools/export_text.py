#!/usr/bin/env python3
"""Writes a plain-text copy of the built paper, one file per chapter, for reading, review, and
readable diffs. The HTML stays the only source: these files are generated, never edited.

Each page is headed by its printed page number, so a finding in the text can be cited as
"p. 47" and found in the PDF.

Usage: tools/venv/bin/python tools/export_text.py [paper.pdf] [out_dir]
"""
import re
import subprocess
import sys
from pathlib import Path

TOOLS = Path(__file__).resolve().parent
sys.path.insert(0, str(TOOLS))
from pagelabels import classify  # noqa: E402

pdf = Path(sys.argv[1] if len(sys.argv) > 1 else TOOLS.parent / "thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.pdf").resolve()
out = Path(sys.argv[2] if len(sys.argv) > 2 else pdf.parent / "text").resolve()

labels = [c["label"] or "–" for c in classify(pdf)]
pages = subprocess.run(["pdftotext", "-layout", str(pdf), "-"], capture_output=True, text=True, check=True).stdout.split("\f")
pages = pages[:len(labels)]


def tidy(text):
    """Drop the stamped page number line and trailing blank space; keep the layout."""
    lines = [ln.rstrip() for ln in text.splitlines()]
    while lines and not lines[-1]:
        lines.pop()
    return "\n".join(lines)


def first_page(pattern, start=0):
    """Index of the first page after the front matter whose text has a line matching pattern."""
    for i in range(start, len(pages)):
        if labels[i].isdigit() and re.search(pattern, pages[i], re.M):
            return i
    raise SystemExit(f"no page matches {pattern!r}")


ch1 = first_page(r"^\s*Introduction\s*$|^\s*INTRODUCTION\s*$")
ch2 = first_page(r"^\s*Review of Related Literature\s*$|^\s*REVIEW OF RELATED LITERATURE\s*$", ch1 + 1)
ch3 = first_page(r"^\s*Methodology\s*$|^\s*METHODOLOGY\s*$", ch2 + 1)
refs = first_page(r"^\s*REFERENCES\s*$", ch3 + 1)
parts = {
    "front-matter.txt": (0, ch1),
    "chapter-1.txt": (ch1, ch2),
    "chapter-2.txt": (ch2, ch3),
    "chapter-3.txt": (ch3, refs),
    "references-and-appendices.txt": (refs, len(pages)),
}

out.mkdir(parents=True, exist_ok=True)
header = f"Generated from {pdf.name} by tools/export_text.py. Do not edit: change the HTML and rebuild.\n"
for name, (a, b) in parts.items():
    body = "\n\n".join(f"──────── p. {labels[i]} ────────\n{tidy(pages[i])}" for i in range(a, b))
    (out / name).write_text(header + "\n" + body + "\n", encoding="utf-8")
    print(f"{name}: pages {labels[a]}–{labels[b - 1]}")
