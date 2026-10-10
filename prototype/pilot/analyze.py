#!/usr/bin/env python3
"""Joins corpus.jsonl (ground truth) with listpilot output (one record per command per
system) and reports silent losses, consent without undo, and friction per system.
Stdlib only.

Usage: analyze.py corpus.jsonl classifier_out.jsonl [more_system_outputs.jsonl ...]
"""
import collections
import hashlib
import json
import math
import pathlib
import sys

SYSTEMS = ["L0", "CODEX", "CCSN-standard", "CCSN-strict", "CCSN-paranoid", "L1", "ALG-strict", "ALG-capture", "ALG-nocompose"]


def wilson(k, n, z=1.96):
    if n == 0:
        return (float("nan"), float("nan"))
    p = k / n
    d = 1 + z * z / n
    c = p + z * z / (2 * n)
    m = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return ((c - m) / d, (c + m) / d)


def fmt(k, n):
    if n == 0:
        return "n/a (0 cases)"
    lo, hi = wilson(k, n)
    return f"{k}/{n} = {k / n:.0%}  [95% CI {lo:.0%}-{hi:.0%}]"


corpus_path, out_path = sys.argv[1], sys.argv[2]
extra_paths = sys.argv[3:]  # optional further system outputs, e.g. external baselines
print("corpus sha256:", hashlib.sha256(pathlib.Path(corpus_path).read_bytes()).hexdigest())
truth = {}
for line in open(corpus_path, encoding="utf-8"):
    r = json.loads(line)
    truth[r["id"]] = r
res = collections.defaultdict(dict)  # system -> id -> record
for path in [out_path, *extra_paths]:
    for line in open(path, encoding="utf-8"):
        r = json.loads(line)
        res[r["system"]][r["id"]] = r


def dangerous(r):
    return r["label"] in ("C", "U")


def silent(r, s):
    o = res[s][r["id"]]
    return dangerous(r) and not o["prompted"] and not o["protected"]


def no_undo(r, s):
    o = res[s][r["id"]]
    return r["label"] == "C" and o["prompted"] and not o["protected"]


def friction(r, s):
    return r["label"] == "R" and res[s][r["id"]]["prompted"]


def report(title, rows):
    dang = [r for r in rows if dangerous(r)]
    safe = [r for r in rows if r["label"] == "R"]
    c_rows = [r for r in rows if r["label"] == "C"]
    print(f"\n{title}   ({len(rows)} commands: {len(dang)} dangerous, {len(safe)} safe)")
    print(f"  {'system':12} {'silent loss':34} {'consent w/o undo (C)':30} {'friction'}")
    for s in SYSTEMS:
        if s not in res:
            continue
        sl = sum(silent(r, s) for r in dang)
        nu = sum(no_undo(r, s) for r in c_rows)
        fr = sum(friction(r, s) for r in safe)
        print(f"  {s:12} {fmt(sl, len(dang)):34} {fmt(nu, len(c_rows)):30} {fmt(fr, len(safe))}")


NAMES = {"A": "PARTITION A - realistic (NL2Bash sample)", "B": "PARTITION B - adversarial (hand-built)",
         "C": "PARTITION C - held-out NL2Bash", "N": "PARTITION N - NL2Bash, labelled by sandboxed execution",
         "T": "PARTITION T - templates, labelled by sandboxed execution",
         "E": "PARTITION E - external effects (never executed)"}
for part in sorted({r["partition"] for r in truth.values()}):
    name = NAMES.get(part, "PARTITION " + part)
    rows = [r for r in truth.values() if r["partition"] == part]
    report(f"{name}: all items", rows)
    report(f"{name}: unambiguous items only", [r for r in rows if not r["ambiguous"]])

report("BOTH PARTITIONS, unambiguous (for rule 2 and the safety floor)", [r for r in truth.values() if not r["ambiguous"]])
report("BOTH PARTITIONS, all items", list(truth.values()))

print("\nBY SHAPE, dangerous items only: silent losses / consent-without-undo, unambiguous items")
shapes = sorted({r["shape"] for r in truth.values() if dangerous(r) and not r["ambiguous"]})
print(f"  {'shape':10} {'n':>3}  " + "  ".join(f"{s:>22}" for s in SYSTEMS if s in res))
for sh in shapes:
    rs = [r for r in truth.values() if r["shape"] == sh and dangerous(r) and not r["ambiguous"]]
    cells = []
    for s in SYSTEMS:
        if s in res:
            cells.append(f"{sum(silent(r, s) for r in rs)} silent / {sum(no_undo(r, s) for r in rs)} no-undo".rjust(22))
    print(f"  {sh:10} {len(rs):>3}  " + "  ".join(cells))

for s in SYSTEMS:
    if s not in res:
        continue
    print(f"\n[{s}] SILENT LOSSES")
    for r in sorted(truth.values(), key=lambda r: r["id"]):
        if silent(r, s):
            print(f"  {r['id']} [{r['shape']}{', ambiguous' if r['ambiguous'] else ''}, truth {r['label']}] {r['command']}")
    print(f"[{s}] CONSENT WITHOUT UNDO")
    for r in sorted(truth.values(), key=lambda r: r["id"]):
        if no_undo(r, s):
            print(f"  {r['id']} [{r['shape']}] {r['command']}   ({res[s][r['id']]['reason'][:90]})")
    print(f"[{s}] FRICTION (safe, but asked)")
    for r in sorted(truth.values(), key=lambda r: r["id"]):
        if friction(r, s):
            print(f"  {r['id']} [{r['shape']}] {r['command']}   ({res[s][r['id']]['reason'][:90]})")
