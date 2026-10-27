"""Merge the shard logs of one Layer 7 or Layer 9 run into one summary.

usage: python3 merge.py LAYER SHARD.log...

Layer 7 shards write one `RUN key=value ...` line per run, Layer 9 one `TURN`
line per turn (cmd/synapse/layer7_test.go, layer9_test.go); this sums them as
one unsplit run would have. A shard that did not finish its test is reported,
since its missing runs would otherwise just look like fewer runs.
"""
import collections
import re
import sys

LINE = re.compile(r"\b(RUN|TURN) (set=\S+ .*)")


def fields(rest):
    return dict(kv.split("=", 1) for kv in rest.split() if "=" in kv)


def main():
    layer, logs = sys.argv[1], sys.argv[2:]
    rows, unfinished = [], []
    for path in logs:
        text = open(path, encoding="utf-8", errors="replace").read()
        if not re.search(r"^(ok|PASS)\b", text, re.M):
            unfinished.append(path)
        for m in LINE.finditer(text):
            rows.append(fields(m.group(2)))
    for path in unfinished:
        print(f"  WARNING: {path} did not finish; its runs are missing or partial")
    if not rows:
        print("  no runs found")
        return
    outcomes = collections.Counter(r.get("outcome", "?") for r in rows)
    secs = sum(float(r.get("secs", 0)) for r in rows)
    if layer == "7":
        n, ok = collections.Counter(), collections.Counter()
        for r in rows:
            n[r["tier"]] += 1
            ok[r["tier"]] += r["ok"] == "true"
        for tier in ("plain", "interface", "technical"):
            if n[tier]:
                print(f"  {tier:10} {ok[tier]:3}/{n[tier]}  {100 * ok[tier] / n[tier]:5.1f}%")
        print(f"  runs {len(rows)}; mean {secs / len(rows):.1f} s a run")
    else:
        convs = collections.defaultdict(list)
        for r in rows:
            convs[(r["conv"], r["rep"])].append(r["ok"] == "true")
        good = sum(r["ok"] == "true" for r in rows)
        whole = sum(all(v) for v in convs.values())
        print(f"  turns satisfied          {good:3}/{len(rows)}  {100 * good / len(rows):5.1f}%")
        print(f"  conversations all right  {whole:3}/{len(convs)}  {100 * whole / len(convs):5.1f}%")
        print(f"  mean {secs / len(rows):.1f} s a turn")
    print("  outcomes: " + ", ".join(f"{k} {v}" for k, v in sorted(outcomes.items())))


if __name__ == "__main__":
    main()
