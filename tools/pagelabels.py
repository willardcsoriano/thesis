"""Page numbering rules of the university writing guidelines, worked out from a rendered PDF.

Front matter is numbered in lower-case roman numerals and the body in Arabic numerals starting
at 1 on the first page of Chapter 1. The title page and the appendices title page are counted but
show no number. A page that carries the title of a major section (approval page, acknowledgement,
table of contents, lists, abstract, a chapter, references) shows its number half an inch from the
bottom, centred. Every other page shows it half an inch from the top, one inch from the right.
"""
import re
import subprocess
from pathlib import Path

MAJOR = {"APPROVAL PAGE", "ACKNOWLEDGEMENT", "TABLE OF CONTENTS", "LIST OF TABLES", "LIST OF FIGURES", "ABSTRACT", "REFERENCES"}
UNNUMBERED = {"APPENDICES"}


def roman(n: int) -> str:
    out = ""
    for v, r in ((10, "x"), (9, "ix"), (5, "v"), (4, "iv"), (1, "i")):
        while n >= v:
            out += r
            n -= v
    return out


def page_lines(pdf: Path) -> list[list[str]]:
    txt = subprocess.run(["pdftotext", "-layout", str(pdf), "-"], capture_output=True, text=True, check=True).stdout
    return [[re.sub(r"\s+", " ", l).strip() for l in p.split("\n") if l.strip()] for p in txt.split("\f")[:-1] or txt.split("\f")]


def classify(pdf: Path) -> list[dict]:
    """One dict per physical page: label (str or ''), position ('bottom' | 'top' | ''), major (bool)."""
    pages = page_lines(pdf)
    first_body = next((i for i, ls in enumerate(pages) if i > 3 and any(re.fullmatch(r"Chapter 1", l) for l in ls[:4])), None)
    if first_body is None:
        raise SystemExit("could not find the first page of Chapter 1")
    out = []
    for i, ls in enumerate(pages):
        head = ls[:6]
        major = any(l in MAJOR or re.fullmatch(r"Chapter \d", l) for l in ls[:8]) or ("REFERENCES" in ls)
        unnumbered = any(l in UNNUMBERED for l in ls[:4]) or i == 0
        label = roman(i + 1) if i < first_body else str(i - first_body + 1)
        if unnumbered:
            out.append(dict(label=label, position="", major=False, printed=False))
        else:
            out.append(dict(label=label, position="bottom" if major else "top", major=major, printed=True))
    return out
