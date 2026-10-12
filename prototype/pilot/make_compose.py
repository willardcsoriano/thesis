"""Generate compound command lines for the differential test of composition.

Each line joins two or three short commands with an operator or a control construct
(;, &&, ||, a pipe, !, if/else, for, while, a block, a subshell), on the base fixture of
cmd/recoverycheck, sometimes with a directory made read-only first so that a part of
the line fails. The point is the region the round-7 corpus barely samples: what a later
part of a line does after an earlier part changed, moved, or failed to change something.

recoverycheck then runs every line the analysis does not refuse, applies the undo the
runtime would apply, and compares the tree; a line it let run whose undo does not
restore the tree is a loss the analysis neither asked about nor captured.

Usage: make_compose.py SEED N > compose.jsonl
"""
import json
import random
import sys

# Commands on the base fixture (a.txt, b.txt, notes.txt, app.conf, report.docx,
# data.csv, big.log, src/main.go, logs/x.log, logs/y.log, build/out.o, backup/old.txt,
# and an empty tmp/). Each changes, moves, or destroys something that existed.
DESTROY = [
    "rm a.txt", "rm -f b.txt", "rm -r logs", "rm -rf build", "rm logs/x.log",
    "mv a.txt b.txt", "mv a.txt tmp/", "mv notes.txt renamed.txt", "mv logs tmp/",
    "echo x > a.txt", "echo x > logs/x.log", "echo more >> notes.txt", "cp b.txt a.txt",
    "sed -i 's/a/z/' a.txt", "truncate -s 0 big.log", "chmod 600 app.conf",
    "gzip big.log", "find logs -name '*.log' -delete", "rm -f logs/*.log",
    "tee data.csv < b.txt", "sort -o data.csv data.csv",
]
# Commands that change nothing that existed: create, read, or fail outright.
BENIGN = [
    "touch new.txt", "mkdir newd", "echo hi > new.txt", "cat a.txt", "ls logs",
    "true", "false", "test -f a.txt", "test -f nosuch", "grep -q a a.txt",
    "mkdir logs", "cp nosuch a.txt", "mv nosuch a.txt", "rm nosuch",
]
# Changes to the shell's own state that later parts depend on.
STATE = ["cd logs", "cd nosuch", "cd tmp", "cd src", "x=a.txt", "x=b.txt", "x=logs"]
VARUSE = ["rm $x", "echo y > $x", "rm -r $x", "mv $x tmp/"]
# Commands that fail when tmp/ or logs/ is read-only.
FAILABLE = ["mv a.txt tmp/", "mv b.txt tmp/", "rm logs/x.log", "echo x > logs/new.log"]
SETUPS = ["", "", "", "chmod 555 tmp", "chmod 555 logs"]


def part(rng, allow_state=True):
    pools = [DESTROY, BENIGN, FAILABLE] + ([STATE, VARUSE] if allow_state else [])
    weights = [5, 3, 2] + ([2, 1] if allow_state else [])
    return rng.choice(rng.choices(pools, weights)[0])


def line(rng):
    a, b, c = part(rng), part(rng), part(rng)
    shape = rng.choice([
        "{a} ; {b}", "{a} && {b}", "{a} || {b}", "{a} | {b}", "! {a} && {b}",
        "{a} ; {b} ; {c}", "{a} && {b} ; {c}", "{a} || {b} ; {c}", "{a} ; {b} && {c}",
        "if {a}; then {b}; fi", "if {a}; then {b}; else {c}; fi",
        "if {a}; then {b}; fi ; {c}", "if {a}; then {b}; fi && {c}",
        "for f in a.txt b.txt; do {b}; done ; {c}", "while false; do {b}; done ; {c}",
        "{{ {a}; {b}; }} ; {c}", "( {a}; {b} ) ; {c}", "{a} || exit 1 ; {b}",
    ])
    return shape.format(a=a, b=b, c=c)


def main():
    seed, n = int(sys.argv[1]), int(sys.argv[2])
    rng = random.Random(seed)
    seen = set()
    while len(seen) < n:
        cmd, setup = line(rng), rng.choice(SETUPS)
        if (cmd, setup) in seen:
            continue
        seen.add((cmd, setup))
        print(json.dumps({"id": f"K{len(seen):04d}", "partition": "B", "label": "C",
                          "command": cmd, "setup": setup}))


if __name__ == "__main__":
    main()
