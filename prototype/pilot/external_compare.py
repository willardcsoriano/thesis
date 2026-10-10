#!/usr/bin/env python3
"""Compares the external open-source baselines (pilot/external_baselines7.jsonl, from
cmd/extbaselines and pilot/ccsafetynet.mjs) with SynapseOS's pattern list, the
fail-closed list and the analysis, on file-destroying commands only (label C): the
80 external-effect commands (ssh, mount, kill, ...) are excluded because the external
tools do not claim to cover them. Stdlib only.

Usage: external_compare.py corpus.jsonl classifier_out.jsonl external_baselines.jsonl
"""
import collections
import json
import math
import sys

corpus, *outs = sys.argv[1:]
truth = {r["id"]: r for r in map(json.loads, open(corpus, encoding="utf-8"))}
res = collections.defaultdict(dict)
for p in outs:
    for r in map(json.loads, open(p, encoding="utf-8")):
        res[r["system"]][r["id"]] = r


def wilson(k, n, z=1.96):
    p = k / n
    d = 1 + z * z / n
    c = p + z * z / (2 * n)
    m = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return (c - m) / d, (c + m) / d


files = [r for r in truth.values() if r["label"] == "C"]
safe = [r for r in truth.values() if r["label"] == "R"]
print(f"file-destroying commands (label C): {len(files)}; harmless commands (label R): {len(safe)}\n")
print(f"{'system':15} {'silent loss (lower is better)':>34}   {'needless asks':>18}")
for s in ["L0", "CODEX", "CCSN-standard", "CCSN-strict", "CCSN-paranoid", "L1", "ALG-capture"]:
    k = sum(not res[s][r["id"]]["prompted"] and not res[s][r["id"]]["protected"] for r in files)
    f = sum(res[s][r["id"]]["prompted"] for r in safe)
    lo, hi = wilson(k, len(files))
    print(f"{s:15} {k:>4}/{len(files)} = {k / len(files):5.1%}  [95% CI {lo:.0%}-{hi:.0%}]   {f:>3}/{len(safe)} = {f / len(safe):4.1%}")
