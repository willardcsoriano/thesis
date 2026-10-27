"""Compare a layer-box run's SYNAPSE_AIDS settings against `none`.

usage: python3 compare_aids.py RESULTS_DIR
       python3 compare_aids.py --selftest

RESULTS_DIR is one layer-box output directory run with LAYER_BOX_AIDS, whose
shard logs are named l<layer>-<model>-aids-<setting>-shard<n>.log. For each
model and layer it prints every setting's score beside `none`, then, per task
(Layer 7: task and tier; Layer 9: conversation and turn), the ones a setting
made better or worse, with the successes out of the repeats. Totals alone hide
an aid that fixes one task and breaks another; this lists both.

Repeats are independent samples, so a task is compared by its success count
across them, never repeat 1 against repeat 1.
"""
import collections
import os
import re
import sys
import tempfile

NAME = re.compile(r"^l(7|9)-(.+)-aids-(.+)-shard\d+\.log$")
LINE = re.compile(r"\b(RUN|TURN) (set=\S+ .*)")


def fields(rest):
    return dict(kv.split("=", 1) for kv in rest.split() if "=" in kv)


def load(directory):
    """{(model, layer): {setting: {item: [ok, ...]}}} and a list of problems."""
    data = collections.defaultdict(lambda: collections.defaultdict(lambda: collections.defaultdict(list)))
    problems = []
    for name in sorted(os.listdir(directory)):
        m = NAME.match(name)
        if not m:
            continue
        layer, model, setting = m.groups()
        text = open(os.path.join(directory, name), encoding="utf-8", errors="replace").read()
        if not re.search(r"^(ok|PASS)\b", text, re.M):
            problems.append(f"{name} did not finish; its runs are missing or partial")
        for lm in LINE.finditer(text):
            f = fields(lm.group(2))
            if layer == "7":
                item = f"{f.get('task')} {f.get('tier')}"
            else:
                item = f"{f.get('conv')} turn {f.get('turn')}"
            data[(model, layer)][setting][item].append(f.get("ok") == "true")
    return data, problems


def report(data, problems, out=sys.stdout):
    for p in problems:
        print(f"WARNING: {p}", file=out)
    for (model, layer) in sorted(data):
        settings = data[(model, layer)]
        print(f"== {model} Layer {layer}", file=out)
        base = settings.get("none")
        for s in sorted(settings, key=lambda s: (s != "none", s)):
            items = settings[s]
            ok = sum(sum(v) for v in items.values())
            n = sum(len(v) for v in items.values())
            line = f"  {s:20} {ok:4}/{n:<4} {100 * ok / n if n else 0:5.1f}%"
            if base is not None and s != "none":
                bok = sum(sum(v) for v in base.values())
                line += f"   {ok - bok:+d} vs none"
            print(line, file=out)
        if base is None:
            print("  (no `none` run to compare against)", file=out)
            continue
        for s in sorted(settings):
            if s == "none":
                continue
            better, worse = [], []
            for item in sorted(set(base) | set(settings[s])):
                b, a = base.get(item, []), settings[s].get(item, [])
                d = sum(a) - sum(b)
                if d > 0:
                    better.append(f"{item} {sum(b)}/{len(b)}->{sum(a)}/{len(a)}")
                elif d < 0:
                    worse.append(f"{item} {sum(b)}/{len(b)}->{sum(a)}/{len(a)}")
            if better or worse:
                print(f"  {s}: better {len(better)}, worse {len(worse)}", file=out)
                for x in better:
                    print(f"    + {x}", file=out)
                for x in worse:
                    print(f"    - {x}", file=out)


def selftest():
    d = tempfile.mkdtemp()

    def log(name, lines, finished=True):
        with open(os.path.join(d, name), "w") as f:
            f.write("\n".join(lines) + ("\nok  synapseos/cmd/synapse 1s\n" if finished else "\n"))

    log("l7-m-aids-none-shard0.log", [
        "RUN set=development task=U1 tier=plain rep=1 ok=false",
        "RUN set=development task=U1 tier=plain rep=2 ok=false",
        "RUN set=development task=U2 tier=plain rep=1 ok=true",
    ])
    log("l7-m-aids-peek-shard0.log", [
        "RUN set=development task=U1 tier=plain rep=1 ok=true",
        "RUN set=development task=U1 tier=plain rep=2 ok=false",
        "RUN set=development task=U2 tier=plain rep=1 ok=false",
    ])
    log("l9-m-aids-none-shard0.log", ["TURN set=development conv=C1 turn=1 rep=1 ok=true"], finished=False)
    log("l9-n-aids-refs-shard0.log", ["TURN set=development conv=C1 turn=1 rep=1 ok=true"])
    data, problems = load(d)
    import io
    buf = io.StringIO()
    report(data, problems, buf)
    got = buf.getvalue()
    for want in [
        "  peek                    1/3     33.3%   +0 vs none",  # same total...
        "  peek: better 1, worse 1",                             # ...hides one fixed, one broken
        "    + U1 plain 0/2->1/2",
        "    - U2 plain 1/1->0/1",
        "WARNING: l9-m-aids-none-shard0.log did not finish",
        "== n Layer 9\n  refs                    1/1    100.0%\n  (no `none` run to compare against)",
    ]:
        assert want in got, f"missing {want!r} in:\n{got}"
    print("selftest ok")


if __name__ == "__main__":
    if sys.argv[1:] == ["--selftest"]:
        selftest()
    elif len(sys.argv) == 2:
        report(*load(sys.argv[1]))
    else:
        sys.exit(__doc__)
