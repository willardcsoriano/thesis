#!/usr/bin/env python3
"""Rewrites the page numbers in the HTML table of contents (and lists of tables and figures) to the
labels the guidelines give the pages of a rendered PDF: roman numerals for the front matter, Arabic
from Chapter 1.

Usage: tools/fix_toc.py paper.html paper.pdf
Front matter is found by the page that starts with its heading, chapters and references by the
page that starts with the heading, and tables and figures by their caption label. Entries not
found are reported and left unchanged. Run, re-render, run again until nothing changes.
"""
import html
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from pagelabels import classify, page_lines  # noqa: E402

html_path, pdf_path = Path(sys.argv[1]), Path(sys.argv[2])
lines = page_lines(pdf_path)
labels = classify(pdf_path)
norm = lambda s: re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", s))).strip().lower()
pages = [norm(" ".join(ls)) for ls in lines]
body_start = next(i for i, l in enumerate(labels) if l["label"] == "1")
front = {"title page", "approval page", "acknowledgement", "table of contents", "list of tables", "list of figures", "abstract"}
src = html_path.read_text(encoding="utf-8")
pat = re.compile(r'(<p class="toc-entry[^"]*"[^>]*><a href="#([^"]+)">(.*?)</a><span class="toc-page">)([ivxlc\d]+)(</span>)', re.S)


cursor = {"head": body_start, "table": body_start, "figure": body_start}


def find(text):
    """Physical page index of the entry, or None. Entries are in document order, so each search
    starts at the page where the previous entry of the same kind was found."""
    if text in front:
        if text == "title page":
            return 0
        return next((i for i in range(body_start) if lines[i] and lines[i][0].lower() == text), None)
    m = re.match(r"(table|figure) ([a-z0-9.]+):", text)
    if m:
        key = f"{m.group(1)} {m.group(2)}."
        hit = next((i for i in range(cursor[m.group(1)], len(pages)) if key in pages[i]), None)
        if hit is not None:
            cursor[m.group(1)] = hit
        return hit
    head = text.split(":")[0]
    top = text.startswith("chapter ") or text in ("references", "appendices")
    def on_page(i):
        ls = lines[i][:8] if top else lines[i]
        key = head if top else text
        return any(l.lower() == key or (len(key) > 12 and l.lower().startswith(key[:40])) for l in ls)
    hit = next((i for i in range(cursor["head"], len(pages)) if on_page(i)), None)
    if hit is not None:
        cursor["head"] = hit
    return hit


changed = 0


def repl(m):
    global changed
    text = norm(m.group(3))
    i = find(text)
    if i is None:
        print(f"not found: {text[:60]!r}")
        return m.group(0)
    label = labels[i]["label"]
    if label != m.group(4):
        changed += 1
        print(f"{text[:55]!r}: {m.group(4)} -> {label}")
    return m.group(1) + label + m.group(5)


html_path.write_text(pat.sub(repl, src), encoding="utf-8")
print(f"{changed} page numbers changed")
