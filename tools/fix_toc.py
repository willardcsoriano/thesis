#!/usr/bin/env python3
"""Rewrites the hard-coded page numbers in the HTML table of contents to match a rendered PDF.

Usage: tools/fix_toc.py paper.html paper.pdf
Front matter is found by the page that starts with its heading. Chapters, references and
appendices are found by the page that starts with the heading. Tables and figures are found
by their caption label ("Table 3.3.") on a body page. Entries not found are reported and
left unchanged. Run, re-render, run again: the numbers keep their width, so it settles.
"""
import html
import re
import subprocess
import sys
from pathlib import Path

html_path, pdf_path = Path(sys.argv[1]), Path(sys.argv[2])
raw = subprocess.run(["pdftotext", "-layout", str(pdf_path), "-"], capture_output=True, text=True, check=True).stdout.split("\f")
norm = lambda s: re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", s))).strip().lower()
# Each page starts with its own number in the rendered footer/header text; drop it.
pages = [re.sub(r"^\d+ ", "", norm(p)) for p in raw]
src = html_path.read_text(encoding="utf-8")
pat = re.compile(r'(<p class="toc-entry"[^>]*><a href="#([^"]+)">(.*?)</a><span class="toc-page">)(\d+)(</span>)', re.S)

# Body starts on the page after the abstract begins.
abstract = next((i for i, p in enumerate(pages) if p.startswith("abstract")), 0)
body = abstract + 1
front = {"title page", "approval page", "acknowledgement", "table of contents", "list of tables", "list of figures", "abstract"}

def find(text):
    if text in front:
        if text == "title page":
            return 1
        return next((i + 1 for i, p in enumerate(pages[: body]) if p.startswith(text)), None)
    m = re.match(r"(table|figure) ([a-z0-9.]+):", text)
    if m:
        key = f"{m.group(1)} {m.group(2)}."
        return next((i + 1 for i in range(body, len(pages)) if key in pages[i]), None)
    head = text.split(":")[0]
    return next((i + 1 for i in range(body, len(pages)) if pages[i].startswith(head)), None)

changed = 0
def repl(m):
    global changed
    text = norm(m.group(3))
    page = find(text)
    if page is None:
        print(f"not found: {text[:60]!r}")
        return m.group(0)
    if str(page) != m.group(4):
        changed += 1
        print(f"{text[:55]!r}: {m.group(4)} -> {page}")
    return m.group(1) + str(page) + m.group(5)

html_path.write_text(pat.sub(repl, src), encoding="utf-8")
print(f"{changed} page numbers changed")
