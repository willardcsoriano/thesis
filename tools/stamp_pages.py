#!/usr/bin/env python3
"""Stamps page numbers onto a rendered PDF as the writing guidelines require (see pagelabels.py).

Usage: tools/venv/bin/python tools/stamp_pages.py in.pdf out.pdf
"""
import io
import sys
from pathlib import Path

from pypdf import PdfReader, PdfWriter
from reportlab.pdfgen import canvas

sys.path.insert(0, str(Path(__file__).resolve().parent))
from pagelabels import classify  # noqa: E402

FONT = ("Times-Roman", 12)


def stamp(src: Path, dst: Path) -> None:
    info = classify(src)
    # Clone the whole document, not page by page: copying pages into a fresh writer keeps
    # each page's link annotations but drops the catalog's named destinations, outline,
    # and structure tree, which leaves every table-of-contents link pointing nowhere.
    writer = PdfWriter(clone_from=str(src))
    for page, meta in zip(writer.pages, info):
        w, h = float(page.mediabox.width), float(page.mediabox.height)
        if meta["printed"]:
            buf = io.BytesIO()
            c = canvas.Canvas(buf, pagesize=(w, h))
            c.setFont(*FONT)
            if meta["position"] == "bottom":
                c.drawCentredString(w / 2, 36 - 4, meta["label"])      # half an inch from the bottom edge
            else:
                c.drawRightString(w - 72, h - 36 - 8, meta["label"])   # half an inch from the top, one inch from the right
            c.save()
            buf.seek(0)
            page.merge_page(PdfReader(buf).pages[0])
    writer.add_metadata({"/Title": "SynapseOS: Designing and Evaluating a Conversational Interface Layer for Personal Computing"})
    with open(dst, "wb") as f:
        writer.write(f)


if __name__ == "__main__":
    stamp(Path(sys.argv[1]), Path(sys.argv[2]))
    print("stamped", sys.argv[2])
