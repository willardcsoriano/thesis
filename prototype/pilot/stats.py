#!/usr/bin/env python3
"""Confirmatory statistics for the recoverability evaluation (paper section 3.1(f)).

Usage: stats.py corpus.jsonl classifier_out.jsonl [recovery_results.txt]

Two primary measures, each compared for the analysis (ALG-strict) against the two lists
(L0, L1), which makes four tests in one family:
  silent loss       over items that lose something (label C or U): the system neither asked
                    nor captured. Lower is better.
  capture coverage  over items labelled C: the system planned a capture, and where a
                    recovery check was run the undo restored the tree exactly. Higher is better.
Each test is McNemar's exact test on the paired discordant items, with the risk difference
(Newcombe's paired interval, method 10) and the odds ratio b/c. Holm's correction is applied
across the four. Stdlib only.
"""
import collections
import json
import math
import re
import sys
from math import comb, sqrt


def wilson(k, n, z=1.96):
    if n == 0:
        return 0.0, 0.0
    p = k / n
    d = 1 + z * z / n
    c = p + z * z / (2 * n)
    m = z * sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    return (c - m) / d, (c + m) / d


def mcnemar_exact(b, c):
    """Two-sided exact p on discordant counts b, c (binomial, p = 0.5)."""
    n = b + c
    if n == 0:
        return 1.0
    k = min(b, c)
    tail = sum(comb(n, i) for i in range(0, k + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def newcombe_paired(a, b, c, d, z=1.96):
    """Newcombe (1998) method 10: interval for p1 - p2 from a paired 2x2 table.
    a: both yes, b: first yes / second no, c: first no / second yes, d: both no."""
    n = a + b + c + d
    p1, p2 = (a + b) / n, (a + c) / n
    l1, u1 = wilson(a + b, n, z)
    l2, u2 = wilson(a + c, n, z)
    denom = (a + b) * (c + d) * (a + c) * (b + d)
    phi = 0.0 if denom == 0 else (a * d - b * c) / sqrt(denom)
    phi = max(phi, 0.0)
    diff = p1 - p2
    lo = diff - sqrt((p1 - l1) ** 2 + (u2 - p2) ** 2 - 2 * phi * (p1 - l1) * (u2 - p2))
    hi = diff + sqrt((u1 - p1) ** 2 + (p2 - l2) ** 2 - 2 * phi * (u1 - p1) * (p2 - l2))
    return diff, lo, hi


def holm(ps):
    order = sorted(range(len(ps)), key=lambda i: ps[i])
    adj = [0.0] * len(ps)
    running = 0.0
    for rank, i in enumerate(order):
        running = max(running, min(1.0, (len(ps) - rank) * ps[i]))
        adj[i] = running
    return adj


corpus_path, out_path = sys.argv[1], sys.argv[2]
truth = {}
for line in open(corpus_path, encoding="utf-8"):
    r = json.loads(line)
    truth[r["id"]] = r
res = collections.defaultdict(dict)
for line in open(out_path, encoding="utf-8"):
    r = json.loads(line)
    res[r["system"]][r["id"]] = r

# Recovery check: item id -> whether the production-path undo (V3) restored the tree.
restored = {}
if len(sys.argv) > 3:
    for line in open(sys.argv[3], encoding="utf-8"):
        m = re.match(r"^([A-Z]\d+)\s+(recoverable-with-capture|recoverable)\s", line)
        if m and "skip" not in line.split():
            tok = line.split()
            restored[m.group(1)] = tok[-7] == "yes"


def silent(sysname, r):
    o = res[sysname][r["id"]]
    return not o["prompted"] and not o["protected"]


def covered(sysname, r):
    o = res[sysname][r["id"]]
    if not o["protected"]:
        return False
    if sysname.startswith("ALG") and restored:
        return restored.get(r["id"], False)
    return True


dangerous = [r for r in truth.values() if r["label"] in ("C", "U")]
losses = [r for r in truth.values() if r["label"] == "C"]
print(f"corpus: {len(truth)} items; {len(dangerous)} lose something or are external; {len(losses)} executable losses")
if restored:
    print(f"recovery check applied to ALG-strict coverage ({len(restored)} items with a restore result)")

tests = []
for measure, items, fn, better in (("silent loss", dangerous, silent, "lower"), ("capture coverage", losses, covered, "higher")):
    for base in ("L0", "L1"):
        a = b = c = d = 0
        for r in items:
            x, y = fn("ALG-strict", r), fn(base, r)  # x: ALG, y: baseline
            if x and y:
                a += 1
            elif x and not y:
                b += 1
            elif not x and y:
                c += 1
            else:
                d += 1
        # Orient so that a positive difference means ALG is better.
        if measure == "silent loss":
            n_alg, n_base = a + b, a + c
            diff, lo, hi = newcombe_paired(a, c, b, d)  # baseline silent minus ALG silent
            wins, losses_ = c, b  # baseline silent, ALG not = ALG wins
        else:
            diff, lo, hi = newcombe_paired(a, b, c, d)  # ALG covered minus baseline covered
            wins, losses_ = b, c
        p = mcnemar_exact(wins, losses_)
        orr = (wins / losses_) if losses_ else float("inf")
        tests.append((measure, base, len(items), a, b, c, d, wins, losses_, p, diff, lo, hi, orr))

adj = holm([t[9] for t in tests])
print()
print(f"{'measure':17} {'vs':3} {'n':>4} {'ALG wins':>8} {'ALG loses':>9} {'p (exact)':>10} {'p (Holm)':>9} {'risk diff':>10} {'95% CI (Newcombe)':>20} {'odds ratio':>10}")
for t, pa in zip(tests, adj):
    measure, base, n, a, b, c, d, wins, losses_, p, diff, lo, hi, orr = t
    print(f"{measure:17} {base:3} {n:>4} {wins:>8} {losses_:>9} {p:>10.2g} {pa:>9.2g} {diff:>+10.3f} {f'[{lo:+.3f}, {hi:+.3f}]':>20} {orr:>10.1f}")
print()
print("risk difference is positive when ALG-strict is better (less silent loss, or more coverage).")
print("ALG wins: items where ALG-strict is right and the baseline is wrong; ALG loses: the reverse.")

# Each system on its own, with Wilson intervals.
print()
for measure, items, fn in (("silent loss", dangerous, silent), ("capture coverage", losses, covered)):
    for s in ("L0", "L1", "ALG-strict", "ALG-capture"):
        if s not in res:
            continue
        k = sum(fn(s, r) for r in items)
        lo, hi = wilson(k, len(items))
        print(f"{measure:17} {s:12} {k:>4}/{len(items):<4} = {k / len(items):6.1%}  [Wilson {lo:.1%}, {hi:.1%}]")
