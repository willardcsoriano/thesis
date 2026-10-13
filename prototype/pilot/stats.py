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
across the four. Ablations of the analysis (composition off, runtime-target resolution
off, both off), when present in classifier_out, are each compared with ALG-capture in a
separate block at nominal significance.

A secondary family scores needless confirmation over items labelled R (nothing is lost):
the system asked. The analysis under each policy (ALG-capture, ALG-strict) is compared with
each list, making four McNemar tests with Holm's correction across them. The deployed gate,
which asks when either the analysis or the pattern list (L0) does, is then scored on needless
confirmation and silent loss without a test. Stdlib only.
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
    for s in ("L0", "L1", "ALG-strict", "ALG-capture", "ALG-nocompose", "ALG-noresolve", "ALG-tableonly"):
        if s not in res:
            continue
        k = sum(fn(s, r) for r in items)
        lo, hi = wilson(k, len(items))
        print(f"{measure:17} {s:12} {k:>4}/{len(items):<4} = {k / len(items):6.1%}  [Wilson {lo:.1%}, {hi:.1%}]")

# Ablations (docs/open-problems.md row 38, and the adviser's request to quantify the
# contribution of composition and of runtime-target resolution): ALG-capture (the full
# analysis) against the same analysis with one or both parts switched off.
#   ALG-nocompose  wrapper, find -exec, xargs, eval, shell -c, and loop composition off
#   ALG-noresolve  no runner, so nothing is resolved by running a read-only command
#   ALG-tableonly  both off: the per-command rule table alone
# Each is a single comparison, not part of the four-test family above, so it is reported
# at nominal significance with no Holm correction; it answers a separate question (how
# much of ALG's result each part carries), not "is ALG better than a list."
# Coverage is scored for the ablations exactly as for the full analysis: protected AND,
# where the recovery check ran, the full analysis's restore result for that item. The
# check replayed the full analysis's capture plan, not the ablation's, so an ablation is
# credited no more than the full analysis earned on that item. This is sound while every
# item an ablation protects is one the full analysis also protects (on round 7 it is: no
# ablation wins on coverage); an ablation win would need its own recovery check.
ABLATIONS = (
    ("composition ablation", "ALG-nocompose", "nocompose", "docs/open-problems.md row 38"),
    ("resolution ablation", "ALG-noresolve", "noresolve", "runtime-target resolution off"),
    ("both off", "ALG-tableonly", "tableonly", "composition and resolution off: the rule table alone"),
)


def ablation(title, sysname, short, note):
    print()
    print(f"{title}: ALG-capture vs {sysname} ({note})")
    for measure, items, fn in (("silent loss", dangerous, silent), ("capture coverage", losses, covered)):
        a = b = c = d = 0
        for r in items:
            x, y = fn("ALG-capture", r), fn(sysname, r)
            if x and y:
                a += 1
            elif x and not y:
                b += 1
            elif not x and y:
                c += 1
            else:
                d += 1
        # Orient so that a positive difference and a "full win" mean the full analysis is
        # better: less silent loss, or more coverage.
        if measure == "silent loss":
            diff, lo, hi = newcombe_paired(a, c, b, d)  # ablated silent minus full silent
            wins, losses_ = c, b
        else:
            diff, lo, hi = newcombe_paired(a, b, c, d)  # full covered minus ablated covered
            wins, losses_ = b, c
        if wins + losses_ == 0:
            print(f"  {measure:17} n={len(items):<4} no discordant pairs (both systems agree on every item)")
            continue
        p = mcnemar_exact(wins, losses_)
        orr = (wins / losses_) if losses_ else float("inf")
        print(f"  {measure:17} n={len(items):<4} full wins={wins:>3} {short} wins={losses_:>3}  "
              f"p={p:.2g}  risk diff={diff:+.3f} [{lo:+.3f}, {hi:+.3f}]  odds ratio={orr:.1f}")


for title, sysname, short, note in ABLATIONS:
    if sysname in res:
        ablation(title, sysname, short, note)

# Needless confirmation (paper section 3.1(d)): how often each system asks about a command
# that loses nothing. Lower is better, so an ALG win is an item the list asks about and the
# analysis does not.
harmless = [r for r in truth.values() if r["label"] == "R"]


def asks(sysname, r):
    return res[sysname][r["id"]]["prompted"]


print()
print(f"needless confirmation over {len(harmless)} harmless items (label R)")
for s in ("L0", "L1", "ALG-strict", "ALG-capture"):
    k = sum(asks(s, r) for r in harmless)
    lo, hi = wilson(k, len(harmless))
    print(f"  {s:12} asks {k:>4}/{len(harmless):<4} = {k / len(harmless):6.1%}  [Wilson {lo:.1%}, {hi:.1%}]")
needless = []
for alg in ("ALG-capture", "ALG-strict"):
    for base in ("L0", "L1"):
        wins = sum(asks(base, r) and not asks(alg, r) for r in harmless)
        losses_ = sum(asks(alg, r) and not asks(base, r) for r in harmless)
        needless.append((alg, base, wins, losses_, mcnemar_exact(wins, losses_)))
print(f"  {'analysis':12} {'vs':3} {'ALG asks':>8} {'list asks':>9} {'p (exact)':>10} {'p (Holm)':>9}")
for (alg, base, wins, losses_, p), pa in zip(needless, holm([t[4] for t in needless])):
    print(f"  {alg:12} {base:3} {losses_:>8} {wins:>9} {p:>10.2g} {pa:>9.2g}")
print("  discordant counts: 'ALG asks' where only the analysis asks, 'list asks' where only the list does.")

# The deployed gate (paper section 2.1(e)) asks when the analysis or the pattern list asks.
print()
print("deployed gate: the analysis, plus the pattern list (L0) consulted alongside it")
for alg in ("ALG-capture", "ALG-strict"):
    def gate_asks(r):
        return asks(alg, r) or asks("L0", r)
    k = sum(gate_asks(r) for r in harmless)
    quiet = sum(not gate_asks(r) and not res[alg][r["id"]]["protected"] for r in dangerous)
    print(f"  {alg:12} needless {k:>4}/{len(harmless):<4} = {k / len(harmless):6.1%}   "
          f"silent loss {quiet}/{len(dangerous)}")
