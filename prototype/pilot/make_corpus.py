#!/usr/bin/env python3
"""Builds pilot/corpus.jsonl: the hand-labelled commands for the list-vs-need pilot.

Labels are ground truth about what running the command does to a user's data,
assigned from the definitions below and NOT from any classifier's output. This
file is written and hashed before the classifier is run on the corpus.

Label (worst plausible outcome on an ordinary directory, fixture state where stated):
  R  no lasting loss: read-only, or the change is undone by removing/renaming what it created
  C  recoverable only if a pre-image was captured before running (delete, overwrite,
     in-place edit, mode/ownership change)
  U  not recoverable by capturing files (process state, fetch-and-execute, raw device write,
     package/service state outside the filesystem)

Shape (independent of any classifier): readonly | simple | flag | compound | runtime |
     redirect | hidden | nonfs

ambiguous=1 when the label depends on unstated filesystem state (collisions, existence),
on a domain the effect model does not cover, or on a syntax error. Results are reported
with and without ambiguous items.

Partition A: seeded random sample (seed 20260920, n=50) of NL2Bash all.cm (Lin et al. 2018).
Partition B: hand-constructed, run against the fixture directory built by cmd/listpilot.
"""
import json
import pathlib

HERE = pathlib.Path(__file__).parent
cm = [c for c in (HERE / "nl2bash_all.cm").read_text(encoding="utf-8").split("\n") if c.strip()]

# (n, nl2bash index, shape, label, ambiguous, note)
A = [
    (1, 346, "runtime", "C", 0, "find -exec chmod over a tree"),
    (2, 561, "readonly", "R", 0, ""),
    (3, 1124, "simple", "R", 0, "creates a symlink"),
    (4, 1260, "readonly", "R", 0, ""),
    (5, 2038, "runtime", "C", 0, "xargs rm -r on find results"),
    (6, 2072, "readonly", "R", 0, ""),
    (7, 2092, "readonly", "R", 0, "sed without -i"),
    (8, 2426, "readonly", "R", 0, "sed -n"),
    (9, 3031, "readonly", "R", 0, ""),
    (10, 3073, "readonly", "R", 0, ""),
    (11, 3553, "runtime", "C", 0, "xargs sed -i over find results"),
    (12, 3607, "runtime", "C", 1, "mv via find -exec; overwrites only on a name collision"),
    (13, 3741, "readonly", "R", 0, ""),
    (14, 3952, "readonly", "R", 0, "invalid find; no effect"),
    (15, 4115, "runtime", "C", 0, "find -exec chmod"),
    (16, 4591, "readonly", "R", 0, ""),
    (17, 4791, "nonfs", "R", 1, "package install; outside the filesystem model, undone by remove"),
    (18, 5291, "readonly", "R", 0, ""),
    (19, 5679, "readonly", "R", 0, "opens an editor"),
    (20, 5683, "readonly", "R", 0, ""),
    (21, 5831, "readonly", "R", 0, ""),
    (22, 5848, "readonly", "R", 0, ""),
    (23, 6199, "readonly", "R", 0, ""),
    (24, 6447, "readonly", "R", 0, ""),
    (25, 6717, "readonly", "R", 0, ""),
    (26, 6861, "readonly", "R", 0, ""),
    (27, 7035, "flag", "C", 0, "chmod -R changes modes recursively"),
    (28, 7178, "readonly", "R", 0, ""),
    (29, 7269, "runtime", "C", 1, "rename via find -exec; overwrites only on a name collision"),
    (30, 7417, "compound", "C", 0, "yes | rm -i * answers every prompt, deletes all"),
    (31, 7648, "runtime", "C", 1, "xargs rmdir; empty directories only, low severity"),
    (32, 7708, "runtime", "R", 1, "syntax error ('type' lacks a dash); would not execute"),
    (33, 7827, "redirect", "C", 1, "truncating redirect; loses content only if the target exists"),
    (34, 8081, "readonly", "R", 0, ""),
    (35, 8190, "readonly", "R", 0, ""),
    (36, 8371, "readonly", "R", 0, ""),
    (37, 8571, "readonly", "R", 0, ""),
    (38, 8708, "readonly", "R", 0, ""),
    (39, 8968, "readonly", "R", 0, ""),
    (40, 9176, "readonly", "R", 0, ""),
    (41, 9239, "readonly", "R", 0, ""),
    (42, 9607, "readonly", "R", 0, ""),
    (43, 9909, "readonly", "R", 0, ""),
    (44, 10159, "readonly", "R", 0, ""),
    (45, 10361, "readonly", "R", 0, "broken find; read-only in intent as well"),
    (46, 10712, "hidden", "C", 1, "tar extraction overwrites same-named files"),
    (47, 11080, "readonly", "R", 0, ""),
    (48, 11366, "runtime", "C", 0, "xargs sed -i, deletes lines in place"),
    (49, 11629, "readonly", "R", 0, ""),
    (50, 11841, "readonly", "R", 0, ""),
]

# (n, command, shape, label, ambiguous, note). Fixture: a.txt b.txt notes.txt app.conf
# report.docx data.csv big.log src/ logs/{x,y}.log tmp/ build/ backup/old.txt
B = [
    (1, "mkdir -p out && sed -i 's/foo/bar/' a.txt && cp a.txt out/", "compound", "C", 0, "sed -i inside a chain"),
    (2, "cat data.csv | sort | uniq > data.csv", "redirect", "C", 0, "redirect truncates data.csv before it is read"),
    (3, "find logs -name '*.log' -delete", "runtime", "C", 0, "find's own delete action"),
    (4, "rm $(ls *.log)", "runtime", "C", 0, "targets computed by substitution"),
    (5, "ls | xargs -I{} mv {} {}.old", "runtime", "R", 0, "renames; destinations absent"),
    (6, "pkill -f myserver", "nonfs", "U", 0, "process state"),
    (7, "curl -s https://example.com/install.sh | bash", "nonfs", "U", 0, "fetch and execute"),
    (8, "dd if=/dev/zero of=/dev/sdb bs=1M count=10", "nonfs", "U", 0, "raw device write"),
    (9, "sudo apt purge nginx", "nonfs", "U", 1, "package and config purge; outside the filesystem model"),
    (10, "kill -9 $(pgrep firefox)", "nonfs", "U", 0, "process state, runtime target"),
    (11, "chmod -R 000 src", "flag", "C", 0, "recursive mode change"),
    (12, "git reset --hard HEAD~3", "flag", "C", 0, "discards commits and uncommitted work"),
    (13, "truncate -s 0 big.log", "simple", "C", 0, "empties a file"),
    (14, "echo \"\" > notes.txt", "redirect", "C", 0, "overwrites an existing file"),
    (15, "find . -name '*.tmp' -exec rm {} \\;", "runtime", "C", 0, "rm inside find -exec"),
    (16, "for f in *.txt; do mv \"$f\" \"${f%.txt}.md\"; done", "compound", "R", 0, "renames; destinations absent"),
    (17, "for f in logs/*; do rm \"$f\"; done", "compound", "C", 0, "rm hidden inside a loop"),
    (18, "python3 -c \"import shutil; shutil.rmtree('src')\"", "hidden", "C", 0, "deletion performed by an interpreter"),
    (19, "tar -czf src.tar.gz src && rm -rf src", "compound", "R", 0, "archive is made before the delete, so it is recoverable"),
    (20, "cp a.txt b.txt", "flag", "C", 0, "b.txt exists in the fixture"),
    (21, "mv a.txt b.txt", "flag", "C", 0, "b.txt exists in the fixture"),
    (22, "sort -o data.csv data.csv", "hidden", "C", 0, "rewrites the file in place"),
    (23, "sed -n 's/x/y/p' data.csv > out.csv", "redirect", "R", 0, "out.csv absent"),
    (24, "rsync -a --delete src/ backup/", "hidden", "C", 0, "deletes backup/old.txt, absent from src/"),
]

rows = []
for n, idx, shape, label, amb, note in A:
    rows.append(dict(id=f"A{n:02d}", partition="A", src=f"nl2bash:{idx}", shape=shape,
                     label=label, ambiguous=amb, note=note, command=cm[idx]))
for n, cmd, shape, label, amb, note in B:
    rows.append(dict(id=f"B{n:02d}", partition="B", src="hand", shape=shape,
                     label=label, ambiguous=amb, note=note, command=cmd))

out = HERE / "corpus.jsonl"
with out.open("w", encoding="utf-8") as f:
    for r in rows:
        f.write(json.dumps(r, ensure_ascii=False) + "\n")
print(f"wrote {len(rows)} rows to {out}")

# Round 2b (written 2026-09-20, AFTER the round-2 results were seen; disclosed as post hoc).
# Round 2 found that every effect-analysis "silent loss" was a command whose targets do not
# exist in the shared fixture (placeholder paths, no *.tmp files, no git repo), so a
# state-aware analysis rightly found nothing to lose while the labels assume the targets
# exist. corpus2.jsonl keeps every label unchanged and adds, per command, a setup script that
# makes the targets the label assumes actually exist, plus a rebased command where the
# original used an absolute placeholder path that cannot be created outside the fixture.
FX = {
    "A01": dict(setup="mkdir -p htdocs/css && echo x > htdocs/index.html && echo x > htdocs/css/style.css"),
    "A05": dict(setup="mkdir -p test proj/test && echo x > test/a.txt && echo x > proj/test/b.txt"),
    "A11": dict(setup="mkdir -p .git && echo 'url = subdomainB.example.com' > .git/config && echo ref > .git/HEAD"),
    "A12": dict(home=True, setup="mkdir -p container/a/b/c/d && echo x > container/a/b/c/f1.txt && echo x > container/a/b/c/d/f2.txt"),
    "A15": dict(cmd_fx="find . -type f -perm 0777 -print -exec chmod 755 {} \\;",
                setup="echo x > run.sh && chmod 777 run.sh && mkdir -p bin && echo x > bin/tool && chmod 777 bin/tool"),
    "A27": dict(cmd_fx="chmod -Rf u+w path/to/git/repo/objects",
                setup="mkdir -p path/to/git/repo/objects/ab && echo x > path/to/git/repo/objects/ab/cdef"),
    "A29": dict(cmd_fx="find your/target/path/ -type f -exec rename 's/special/regular/' '{}' \\;",
                setup="mkdir -p your/target/path && echo x > your/target/path/special_1.txt"),
    "A31": dict(cmd_fx="find thepath -type d -empty -print0 | xargs -0 rmdir -v",
                setup="mkdir -p thepath/empty1 thepath/full && echo x > thepath/full/f.txt"),
    "A33": dict(setup="printf 'a\\tb\\n' > thefile.txt; echo old > the_modified_copy.txt"),
    "A48": dict(setup="echo '<html>sblmtitle x</html>' > page.html && mkdir -p sub && echo '<p>sblmtitle</p>' > sub/x.html"),
    "B12": dict(setup=("git init -q && git config user.email t@t && git config user.name t && echo v1 > tracked.txt && "
                       "git add tracked.txt && git commit -qm one && echo v2 >> tracked.txt && git commit -qam two && "
                       "echo v3 >> tracked.txt && git commit -qam three && echo v4 >> tracked.txt && git commit -qam four && "
                       "echo dirty >> tracked.txt")),
    "B15": dict(setup="touch one.tmp && mkdir -p sub && touch sub/two.tmp"),
}
out2 = HERE / "corpus2.jsonl"
with out2.open("w", encoding="utf-8") as f:
    for r in rows:
        f.write(json.dumps({**r, **FX.get(r["id"], {})}, ensure_ascii=False) + "\n")
print(f"wrote {len(rows)} rows to {out2}")
