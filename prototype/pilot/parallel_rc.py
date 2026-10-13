"""Run cmd/recoverycheck on a corpus in parallel shards and combine the results.

recoverycheck checks one case at a time, and sets HOME per capture, so it cannot run
cases concurrently in one process. This runs one process per CPU on interleaved
shards (RC_SHARD=i/n), then sums the recovery counts and gathers the failures.

Usage: parallel_rc.py CORPUS.jsonl OUT.txt [N]     (N defaults to the CPU count)
The Go toolchain is $GO if set, else go on PATH.
"""
import os
import re
import subprocess
import sys

corpus, out = sys.argv[1], sys.argv[2]
n = int(sys.argv[3]) if len(sys.argv) > 3 else os.cpu_count() or 1
subprocess.run([os.environ.get("GO", "go"), "build", "-o", "bin/recoverycheck", "./cmd/recoverycheck"], check=True)
procs = []
for i in range(n):
    env = dict(os.environ, RC_SHARD=f"{i}/{n}")
    procs.append(subprocess.Popen(["bin/recoverycheck", corpus], env=env,
                                  stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True))
texts = [p.communicate()[0] for p in procs]

totals = {}
failures = []
for t in texts:
    for key, pat in [("cases", r"cases: (\d+) total, (\d+) executed"),
                     ("V1", r"V1 \(.*?\):\s+(\d+)/(\d+)"),
                     ("V2", r"V2 \(.*?\):\s+(\d+)/(\d+)"),
                     ("V3", r"V3 \(.*?\):\s+(\d+)/(\d+)")]:
        m = re.search(pat, t)
        if m:
            a, b = totals.get(key, (0, 0))
            totals[key] = (a + int(m.group(1)), b + int(m.group(2)))
    if "FAILURES" in t:
        f = t[t.index("FAILURES"):]
        f = f[:f.index("SKIPPED")] if "SKIPPED" in f else f
        failures += [b.strip() for b in re.split(r"\n(?=\S+\s+class=)", f) if "class=" in b]

with open(out, "w") as w:
    c = totals.get("cases", (0, 0))
    w.write(f"SUMMARY ({n} shards)\n  cases: {c[0]} total, {c[1]} executed\n")
    for v in ("V1", "V2", "V3"):
        a, b = totals.get(v, (0, 0))
        w.write(f"  {v} restored exactly: {a}/{b}\n")
    w.write("\nFAILURES (V3 not exact)\n")
    for b in failures:
        if "V3: exact" not in b:
            w.write(b + "\n---\n")
print(open(out).read().split("FAILURES")[0].strip())
