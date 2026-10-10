#!/usr/bin/env python3
"""Per-command audit of the recoverability evaluation: one row per corpus command, showing
what it really did (the sandbox label), what each system did with it, whether the
analysis's undo restored it, and what the analysis changed relative to the pattern list.

Usage: audit.py corpus.jsonl classifier_out.jsonl recovery_results.txt out_prefix
Writes out_prefix.csv and out_prefix.html. Stdlib only.

The totals it prints are the same quantities analyze.py and stats.py report (silent loss
over C and U items, capture coverage over C items with the analysis's coverage verified by
the recovery check). It checks them against each other and exits non-zero if they
disagree, so the table cannot drift from the reported result.
"""
import collections
import csv
import html
import json
import re
import sys

corpus_path, out_path, recovery_path, prefix = sys.argv[1:5]

truth = [json.loads(line) for line in open(corpus_path, encoding="utf-8")]
res = collections.defaultdict(dict)
for line in open(out_path, encoding="utf-8"):
    r = json.loads(line)
    res[r["system"]][r["id"]] = r

# Recovery check: case -> "yes" / "NO" for the production-path undo (V3), or "skip".
restored = {}
for line in open(recovery_path, encoding="utf-8"):
    m = re.match(r"^([A-Z]\d+)\s+(recoverable-with-capture|recoverable|unrecoverable)\s", line)
    if not m:
        continue
    tok = line.split()
    restored[m.group(1)] = "skip" if "skip" in tok else tok[-7]

SYSTEMS = [("L0", "Pattern list (today)"), ("L1", "Fail-closed list"),
           ("ALG-capture", "Algorithm"), ("ALG-nocompose", "Algorithm, composition off")]
PARTITION = {"N": "NL2Bash", "T": "Template", "E": "External"}
TRUTH = {"R": "Loses nothing", "C": "Loses data; recoverable if backed up first",
         "U": "Cannot be undone (external or irreversible)"}

SILENT, ASKED, BACKED_FAILED, BACKED = "Silent loss", "Asked, no backup", "Backed up, restore FAILED", "Backed up"
RAN, NEEDLESS = "Ran (correct)", "Asked needlessly"
RANK = {SILENT: 0, ASKED: 1, BACKED_FAILED: 1, BACKED: 2}


def outcome(system, r):
    o = res[system][r["id"]]
    if r["label"] == "R":
        return NEEDLESS if o["prompted"] else RAN
    if o["protected"]:
        if system.startswith("ALG"):
            got = restored.get(r["id"])
            if got == "yes":
                return BACKED + ", restore verified"
            if got == "NO":
                return BACKED_FAILED
        return BACKED + (" (not verified)" if not system.startswith("ALG") else "")
    return ASKED if o["prompted"] else SILENT


def rank(o):
    return RANK[BACKED] if o.startswith(BACKED + ",") or o.startswith(BACKED + " (") or o == BACKED else RANK[o]


def change(r, before, after):
    if r["label"] == "R":
        if before == NEEDLESS and after == RAN:
            return "Removed a needless ask"
        if before == RAN and after == NEEDLESS:
            return "Added a needless ask"
        return "No change"
    a, b = rank(before), rank(after)
    if r["label"] == "U":
        # Nothing restores an external or irreversible effect, so a "backup" of one is worth
        # no more than asking; comparing them would credit or blame a capture that cannot work.
        a, b = min(a, 1), min(b, 1)
    short = {0: "silent loss", 1: "asks only", 2: "backed up"}
    if a == b:
        return "No change"
    word = "Better" if b > a else "Worse"
    return f"{word}: {short[a]} → {short[b]}"


rows = []
for r in truth:
    outs = {s: outcome(s, r) for s, _ in SYSTEMS}
    rows.append({
        "id": r["id"],
        "partition": PARTITION.get(r["partition"], r["partition"]),
        "shape": r["shape"],
        "command": r["command"],
        "truth": TRUTH[r["label"]],
        "observed": r.get("note", ""),
        **{s: outs[s] for s, _ in SYSTEMS},
        "alg_reason": res["ALG-capture"][r["id"]].get("reason", ""),
        "restored": {"yes": "yes", "NO": "NO", "skip": "not executed"}.get(restored.get(r["id"]), "—"),
        "change": change(r, outs["L0"], outs["ALG-capture"]),
    })

# ---- totals, cross-checked against the definitions analyze.py and stats.py use ----
dangerous = [x for x in rows if not x["truth"].startswith("Loses nothing")]
losses = [x for x in rows if x["truth"].startswith("Loses data")]
safe = [x for x in rows if x["truth"].startswith("Loses nothing")]
totals = {}
for s, name in SYSTEMS:
    totals[s] = {
        "silent": sum(x[s] == SILENT for x in dangerous),
        # Same rule as stats.py: the analysis counts only where the undo restored the tree.
        "covered": sum((x[s] == BACKED + ", restore verified") if s.startswith("ALG") else x[s].startswith(BACKED)
                       for x in losses),
        "needless": sum(x[s] == NEEDLESS for x in safe),
    }
expected = {"L0": (167, 135, 30), "L1": (0, 0, 101), "ALG-capture": (0, 291, 32), "ALG-nocompose": (0, 115, None)}
bad = []
for s, (sl, cov, fr) in expected.items():
    t = totals[s]
    if t["silent"] != sl or t["covered"] != cov or (fr is not None and t["needless"] != fr):
        bad.append(f"{s}: got {t}, paper reports silent={sl} covered={cov} needless={fr}")
if bad:
    sys.exit("audit totals disagree with the reported result:\n  " + "\n  ".join(bad))

changes = collections.Counter(x["change"] for x in rows)

# ---- CSV ----
cols = ["id", "partition", "shape", "command", "truth", "observed"] + [s for s, _ in SYSTEMS] + ["restored", "change", "alg_reason"]
headers = ["ID", "Source", "Shape", "Command", "What it really does (sandbox)", "Observed in sandbox"] + \
          [n for _, n in SYSTEMS] + ["Algorithm's undo restored it?", "Change: pattern list → algorithm", "Algorithm's reason"]
with open(prefix + ".csv", "w", newline="", encoding="utf-8") as f:
    w = csv.writer(f)
    w.writerow(headers)
    for x in rows:
        w.writerow([x[c] for c in cols])

# ---- HTML ----
def cls(o):
    if o == SILENT or o == BACKED_FAILED or o.startswith("Worse"):
        return "bad"
    if o == NEEDLESS or o == "Added a needless ask":
        return "warn"
    if o.startswith(BACKED) or o.startswith("Better") or o == "Removed a needless ask":
        return "good"
    return ""


esc = html.escape
summary_rows = "".join(
    f"<tr><td>{esc(name)}</td><td class='num'>{totals[s]['silent']} / {len(dangerous)}</td>"
    f"<td class='num'>{totals[s]['covered']} / {len(losses)}</td><td class='num'>{totals[s]['needless']} / {len(safe)}</td></tr>"
    for s, name in SYSTEMS)
order = sorted(changes, key=lambda k: (not k.startswith("Better"), not k.startswith("Removed"), k == "No change", k))
chips = "".join(f"<button class='chip {cls(k)}' data-f='{esc(k)}'>{esc(k)} <span>{changes[k]}</span></button>" for k in order)
body = []
for x in rows:
    cells = [f"<td class='id'>{esc(x['id'])}</td>",
             f"<td><code>{esc(x['command'])}</code><div class='sub'>{esc(x['partition'])} &middot; {esc(x['shape'])}</div></td>",
             f"<td>{esc(x['truth'])}<div class='sub'>{esc(x['observed'])}</div></td>"]
    cells += [f"<td class='{cls(x[s])}'>{esc(x[s])}</td>" for s, _ in SYSTEMS]
    cells += [f"<td class='{'bad' if x['restored'] == 'NO' else ''}'>{esc(x['restored'])}</td>",
              f"<td class='{cls(x['change'])}'>{esc(x['change'])}</td>"]
    body.append(f"<tr data-c='{esc(x['change'])}'>{''.join(cells)}</tr>")

page = f"""<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Recoverability Audit</title><style>
:root {{ --bg:#F6F8FA; --card:#fff; --border:#D0D7DE; --text:#1A1F26; --dim:#57606A;
        --good:#1A7F37; --goodbg:#DAFBE1; --bad:#CF222E; --badbg:#FFEBE9; --warn:#9A6700; --warnbg:#FFF8C5; }}
* {{ box-sizing:border-box; }}
body {{ margin:0; background:var(--bg); color:var(--text); font:14px/1.5 -apple-system,"Segoe UI","DejaVu Sans",Arial,sans-serif; }}
.wrap {{ max-width:1500px; margin:0 auto; padding:32px 16px 48px; }}
h1 {{ font-size:24px; margin:0 0 6px; }} .lead {{ color:var(--dim); max-width:900px; margin:0 0 22px; }}
.card {{ background:var(--card); border:1px solid var(--border); border-radius:10px; padding:16px 18px; margin-bottom:18px; overflow-x:auto; }}
table {{ border-collapse:collapse; width:100%; }}
th, td {{ text-align:left; vertical-align:top; padding:7px 9px; border-bottom:1px solid var(--border); }}
th {{ font-size:12px; text-transform:uppercase; letter-spacing:.04em; color:var(--dim); background:var(--card); position:sticky; top:0; }}
.num {{ font-variant-numeric:tabular-nums; }}
code {{ font:12.5px/1.45 ui-monospace,"DejaVu Sans Mono",Consolas,monospace; word-break:break-all; }}
.sub {{ color:var(--dim); font-size:12px; margin-top:2px; }} .id {{ color:var(--dim); font-size:12px; white-space:nowrap; }}
td.good {{ color:var(--good); background:var(--goodbg); }} td.bad {{ color:var(--bad); background:var(--badbg); font-weight:600; }}
td.warn {{ color:var(--warn); background:var(--warnbg); }}
.tools {{ display:flex; flex-wrap:wrap; gap:8px; align-items:center; margin-bottom:12px; }}
.chip {{ font:inherit; font-size:13px; border:1px solid var(--border); background:var(--card); border-radius:999px; padding:4px 11px; cursor:pointer; color:var(--text); }}
.chip span {{ color:var(--dim); margin-left:4px; }} .chip.on {{ outline:2px solid var(--text); }}
.chip.good {{ border-color:var(--good); }} .chip.bad {{ border-color:var(--bad); }} .chip.warn {{ border-color:var(--warn); }}
input {{ font:inherit; padding:5px 10px; border:1px solid var(--border); border-radius:6px; min-width:240px; }}
#count {{ color:var(--dim); font-size:13px; }}
.big td:first-child {{ font-weight:600; }}
@media (max-width:700px) {{ input {{ min-width:0; width:100%; }} }}
</style></head><body><div class="wrap">
<h1>Recoverability audit &mdash; held-out corpus, round 7</h1>
<p class="lead">Every one of the {len(rows)} test commands, with what it really does (observed by running it in a sandbox), what each approach did with it, and whether the algorithm&rsquo;s undo actually restored the files. &ldquo;Pattern list&rdquo; is how tools work today; &ldquo;Algorithm&rdquo; is SynapseOS&rsquo;s analysis; the last column says what the algorithm changed for that command.</p>
<div class="card"><table class="big"><thead><tr><th>Approach</th><th>Silent losses (lower is better)</th><th>Losses backed up and restored (higher is better)</th><th>Needless asks on safe commands (lower is better)</th></tr></thead>
<tbody>{summary_rows}</tbody></table>
<p class="sub">These totals match Thesis 1 &sect;3.1(g)&ndash;(h) and Tables 3.4&ndash;3.5. The algorithm&rsquo;s backups count only where the sandbox undo restored the files exactly; the pattern list&rsquo;s are not verified.</p></div>
<div class="card"><div class="tools"><input id="q" placeholder="Search commands&hellip;"> {chips}
<button class="chip" data-f="">Show all</button> <span id="count"></span></div>
<table><thead><tr><th>ID</th><th>Command</th><th>What it really does</th>{''.join(f'<th>{esc(n)}</th>' for _, n in SYSTEMS)}<th>Undo restored?</th><th>Change</th></tr></thead>
<tbody id="rows">{''.join(body)}</tbody></table></div>
</div><script>
const rows=[...document.querySelectorAll('#rows tr')], q=document.getElementById('q'), count=document.getElementById('count');
let f='';
function apply(){{const t=q.value.toLowerCase();let n=0;for(const r of rows){{const ok=(!f||r.dataset.c===f)&&(!t||r.textContent.toLowerCase().includes(t));r.style.display=ok?'':'none';if(ok)n++;}}count.textContent=n+' of '+rows.length+' commands';}}
document.querySelectorAll('.chip').forEach(c=>c.addEventListener('click',()=>{{f=c.dataset.f;document.querySelectorAll('.chip').forEach(x=>x.classList.toggle('on',x===c&&f!==''));apply();}}));
q.addEventListener('input',apply);apply();
</script></body></html>"""
with open(prefix + ".html", "w", encoding="utf-8") as f:
    f.write(page)

print(f"{len(rows)} commands; totals match the reported result")
for s, name in SYSTEMS:
    t = totals[s]
    print(f"  {name:28} silent {t['silent']:>3}/{len(dangerous)}  covered {t['covered']:>3}/{len(losses)}  needless {t['needless']:>3}/{len(safe)}")
print("changes, pattern list -> algorithm:")
for k in order:
    print(f"  {changes[k]:>4}  {k}")
print("wrote", prefix + ".csv", "and", prefix + ".html")
