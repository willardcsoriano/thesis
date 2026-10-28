"""Keep a rented box's results in the repository, without the box's address.

usage: python3 keep.py SRC DEST
       python3 keep.py --selftest

SRC is one result directory that rent.sh fetched into tools/rent/out/, such as
out/<run>/layer-box/<time> or out/<run>/tier-box/<time>. tools/rent/out/ is
git-ignored, so a result that only lives there is lost with the laptop. This
copies what a result is read from (summaries, preflight, Layer 7 and 9 shard
logs, tier timings and their per-model logs) to DEST, normally under
prototype/pilot/live/, and leaves out the rest (Ollama's server log, lock and
marker files). Every IPv4 address other than loopback is replaced, since a
rented box's address is not ours to publish once the box is gone.

DEST must not exist: a kept result is evidence and is never written over. A
rerun is kept beside it under its own name.
"""
import os
import re
import shutil
import sys
import tempfile

KEEP = re.compile(r"^(summary\.txt|preflight\.txt|results\.tsv|desktop-load\.txt|l[79]-.+\.log|qwen.+\.log)$")
IPV4 = re.compile(r"\b(?!127\.)(?:\d{1,3}\.){3}\d{1,3}\b")


def redact(text):
    return IPV4.sub("<address>", text)


def keep(src, dest):
    """Copies the kept files from src to dest, keeping subfolders. Returns how many."""
    if os.path.exists(dest):
        raise SystemExit(f"keep: {dest} exists; a kept result is never written over")
    picked = []
    for root, _dirs, files in os.walk(src):
        for name in sorted(files):
            if KEEP.match(name):
                picked.append(os.path.relpath(os.path.join(root, name), src))
    if not picked:
        raise SystemExit(f"keep: nothing to keep in {src}")
    for rel in picked:
        out = os.path.join(dest, rel)
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with open(os.path.join(src, rel), encoding="utf-8", errors="replace") as f:
            text = f.read()
        with open(out, "w", encoding="utf-8") as f:
            f.write(redact(text))
    return len(picked)


def selftest():
    with tempfile.TemporaryDirectory() as tmp:
        src = os.path.join(tmp, "src")
        os.makedirs(os.path.join(src, "idle"))
        files = {
            "preflight.txt": "ollama 0.40.11 at http://203.0.113.7:11434\nlocal 127.0.0.1:11434\n",
            "l7-qwen2.5-coder_3b-aids-all-shard0.log": "RUN set=development task=U1 ok=true\n",
            "idle/results.tsv": "model\tgen_tok_s\nqwen2.5-coder:3b\t17.1\n",
            "ollama-serve.log": "listening on 203.0.113.7\n",
            "lock": "",
        }
        for rel, text in files.items():
            with open(os.path.join(src, rel), "w") as f:
                f.write(text)
        dest = os.path.join(tmp, "dest")
        assert keep(src, dest) == 3, "keeps preflight, the shard log and the timing table"
        pre = open(os.path.join(dest, "preflight.txt")).read()
        assert "203.0.113.7" not in pre and "<address>:11434" in pre, pre
        assert "127.0.0.1:11434" in pre, "loopback is not an address to hide"
        assert os.path.exists(os.path.join(dest, "idle", "results.tsv"))
        assert not os.path.exists(os.path.join(dest, "ollama-serve.log"))
        assert not os.path.exists(os.path.join(dest, "lock"))
        try:
            keep(src, dest)
        except SystemExit as e:
            assert "never written over" in str(e)
        else:
            raise AssertionError("an existing destination was written over")
        empty = os.path.join(tmp, "empty")
        os.makedirs(empty)
        try:
            keep(empty, os.path.join(tmp, "dest2"))
        except SystemExit as e:
            assert "nothing to keep" in str(e)
        else:
            raise AssertionError("an empty source was kept")
        assert not os.path.exists(os.path.join(tmp, "dest2"))
    print("selftest ok")


def main():
    if sys.argv[1:] == ["--selftest"]:
        selftest()
        return
    if len(sys.argv) != 3:
        raise SystemExit(__doc__.strip())
    n = keep(sys.argv[1], sys.argv[2])
    print(f"keep: {n} files to {sys.argv[2]}")


if __name__ == "__main__":
    main()
