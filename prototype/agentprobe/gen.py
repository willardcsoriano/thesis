"""Generate prototype/agentprobe/tasks.jsonl, the agent command study's task suite.

Usage, from prototype/: python3 agentprobe/gen.py pilot/corpus6m.jsonl agentprobe/tasks.jsonl

Partition S reads its descriptions from pilot/nl2bash_all.nl, which is GPL-3.0
and git-ignored; only the sampled descriptions end up in tasks.jsonl.
"""
import io, json, re, sys, tarfile
from pathlib import Path

corpus6m, out = sys.argv[1], sys.argv[2]
NL = Path(__file__).resolve().parent.parent / "pilot" / "nl2bash_all.nl"
nl = open(NL).read().split('\n')
rows = {}
for l in open(corpus6m):
    r = json.loads(l)
    rows[r['id']] = r


def f(p, d):
    return {"p": p, "k": "file", "d": d}


def dr(p):
    return {"p": p, "k": "dir"}


def big(tag, n):
    return (tag + " ") * (n // (len(tag) + 1)) + "\n"


DAY = 24 * 60 * 60
# Dates in file names (backups/site-2026-08-01.tar) are turned into ages from
# this day, so a file's age agrees with its name on the day the suite was
# written. Ages are relative (oracle.Entry.Age), so in a later run name and
# age stay in the same order, shifted by however long ago that was.
REF = (2026, 10, 24)


def age(e, days):
    e = dict(e)
    e["a"] = int(days * DAY)
    return e


def days_before_ref(y, m, d):
    import datetime
    return (datetime.date(*REF) - datetime.date(y, m, d)).days + 0.5


def stable_days(path, lo, hi):
    """A deterministic age in [lo, hi) days for a path, so a regenerated suite
    is byte-identical and no two files share a time by accident."""
    import hashlib
    h = int(hashlib.sha256(path.encode()).hexdigest()[:8], 16)
    return lo + (h % ((hi - lo) * 24)) / 24


# ---------------------------------------------------------------- fixtures
PHOTOS = [
    f("Pictures/IMG_2041.jpg", "fake-jpeg: sunset over the bay\n"),
    f("Pictures/IMG_2041 (1).jpg", "fake-jpeg: sunset over the bay\n"),
    f("Pictures/IMG_2042.jpg", "fake-jpeg: jeepney at the terminal\n"),
    f("Pictures/IMG_2043.jpg", "fake-jpeg: birthday cake\n"),
    f("Pictures/Copy of IMG_2043.jpg", "fake-jpeg: birthday cake\n"),
    f("Pictures/trip/beach.jpg", "fake-jpeg: white sand beach\n"),
    f("Pictures/trip/beach-copy.jpg", "fake-jpeg: white sand beach\n"),
    f("Pictures/trip/boat.jpg", "fake-jpeg: outrigger boat\n"),
    f("Pictures/trip/IMG_3001.jpg", "fake-jpeg: island hopping\n"),
    f("Pictures/screenshot.png", "fake-png: class schedule\n"),
    # same naming pattern as a duplicate, different bytes: not a duplicate
    f("Pictures/screenshot (2).png", "fake-png: enrollment form\n"),
]

DOWNLOADS = [
    f("Downloads/invoice_march.pdf", "fake-pdf: invoice for March, order 0000\n"),
    f("Downloads/Syllabus_CS101.pdf", "fake-pdf: CS101 syllabus\n"),
    f("Downloads/Syllabus_CS101(1).pdf", "fake-pdf: CS101 syllabus\n"),
    f("Downloads/vlc-setup.deb", big("deb-package", 1500)),
    f("Downloads/zoom_amd64.deb", big("deb-package-zoom", 2500)),
    f("Downloads/ubuntu-24.04-desktop.iso", big("iso-image", 3000)),
    f("Downloads/photos.zip", "fake-zip: holiday photos\n"),
    f("Downloads/wallpaper.jpg", "fake-jpeg: mountains\n"),
    f("Downloads/meme.png", "fake-png: cat meme\n"),
    f("Downloads/song.mp3", big("fake-mp3", 800)),
    f("Downloads/lecture3.mp4", big("fake-mp4-lecture", 2800)),
    f("Downloads/thesis_draft_v3.docx", "fake-docx: thesis draft version 3\n"),
    f("Downloads/thesis_draft_FINAL.docx", "fake-docx: thesis draft final\n"),
    f("Downloads/thesis_draft_FINAL(2).docx", "fake-docx: thesis draft final, revised\n"),
    f("Downloads/notes.txt", "remember to email the adviser\n"),
]

# The tasks that ask for "the biggest" files need sizes that differ by tens of
# KB, or du's 4K blocks tie them. Only those tasks carry the large copy, to keep
# the committed suite small.
BIG_SIZES = {"Downloads/ubuntu-24.04-desktop.iso": 64000, "Downloads/lecture3.mp4": 44000,
             "Downloads/zoom_amd64.deb": 26000, "Downloads/vlc-setup.deb": 12000,
             "Downloads/song.mp3": 5000}
DOWNLOADS_BIG = []
for e in DOWNLOADS:
    e = dict(e)
    if e["p"] in BIG_SIZES:
        e["d"] = big(e["d"].split(" ", 1)[0], BIG_SIZES[e["p"]])
    DOWNLOADS_BIG.append(e)
_sizes = sorted((len(e["d"]) for e in DOWNLOADS_BIG), reverse=True)
assert all(a - b >= 5000 for a, b in zip(_sizes, _sizes[1:5])), _sizes
assert sum(_sizes) < 200_000

DOCS = [
    f("Documents/report.docx", "fake-docx: lab report, final version\n"),
    f("Documents/budget.xlsx", "fake-xlsx: monthly budget\n"),
    f("Documents/school/resume_2026.docx", "fake-docx: resume, current\n"),
    f("Documents/school/old/resume.docx", "fake-docx: resume, 2024\n"),
    f("Documents/school/essay.docx", "fake-docx: essay, first version\n"),
    f("Documents/school/essay_v2.docx", "fake-docx: essay, second version\n"),
    f("Documents/school/presentation.pptx", "fake-pptx: group presentation\n"),
    # the "USB stick", already holding an older copy of one document
    f("USB/Documents/report.docx", "fake-docx: lab report, OLD draft\n"),
    f("USB/music/playlist.m3u", "song.mp3\n"),
]

NOTES = [
    f("notes/meeting.txt", "meeting with group\nthe deadline for the draft is friday\n"),
    f("notes/ideas.txt", "draft one: survey\ndraft two: interviews\n"),
    f("notes/todo.txt", "finish chapter 2\nsubmit form\n"),
    f("notes/recipes.txt", "adobo: soy sauce, vinegar, garlic\n"),
]

THESIS = [
    f("thesis/chapter1.docx", "fake-docx: introduction\n"),
    f("thesis/chapter2.docx", "fake-docx: related literature\n"),
    f("thesis/figures/fig1.png", "fake-png: architecture diagram\n"),
    f("thesis/references.bib", "@book{key, title={A Book}}\n"),
    f("thesis/drafts/thesis_draft_v1.docx", "fake-docx: draft 1\n"),
    f("thesis/drafts/thesis_draft_v2.docx", "fake-docx: draft 2\n"),
    f("thesis/drafts/thesis_draft_v3.docx", "fake-docx: draft 3\n"),
    f("thesis/drafts/defense_v1.pptx", "fake-pptx: defense slides 1\n"),
    f("thesis/drafts/defense_v2.pptx", "fake-pptx: defense slides 2\n"),
]

APPLOG = "".join(
    "2026-10-0%d %02d:%02d:11 %s %s\n" % (1 + i // 6, 8 + i % 3, (i * 7) % 60, lvl, msg)
    for i, (lvl, msg) in enumerate([
        ("INFO", "server started on port 8080"),
        ("INFO", "request /home 200"),
        ("WARN", "slow query 1200ms"),
        ("ERROR", "database connection refused"),
        ("INFO", "request /login 200"),
        ("ERROR", "database connection refused"),
        ("ERROR", "null value in column name"),
        ("INFO", "request /home 200"),
        ("WARN", "disk usage 91%"),
        ("ERROR", "out of memory, worker killed"),
        ("INFO", "server restarted"),
        ("ERROR", "database connection refused"),
    ]))

LOGS = [
    age(f("logs/app.log", APPLOG), 0.1),
    age(f("logs/app.log.1", big("2026-09-30 INFO request /home 200", 2000)), 1.1),
    age(f("logs/app.log.2", big("2026-09-29 INFO request /home 200", 2000)), 2.1),
    f("logs/error.log", "2026-10-02 08:21:11 ERROR out of memory, worker killed\n"),
    f("logs/access.log", "GET /home 200\nGET /login 200\nGET /admin 403\n"),
]

GRADES = [f("grades.csv", "name,score\nLia,88\nMarco,92\nJun,75\nTala,81\nBea,90\n")]

SALES = [
    f("sales/jan.csv", "date,item,qty\n2026-01-03,notebook,4\n2026-01-09,pen,10\n"),
    f("sales/feb.csv", "date,item,qty\n2026-02-02,notebook,2\n2026-02-14,folder,6\n"),
    f("sales/mar.csv", "date,item,qty\n2026-03-05,pen,8\n2026-03-20,notebook,5\n"),
]

INVENTORY = [f("inventory.json", json.dumps([
    {"name": "blue pen", "stock": 40},
    {"name": "yellow pad", "stock": 0},
    {"name": "stapler", "stock": 3},
    {"name": "glue stick", "stock": 0},
    {"name": "index cards", "stock": 12},
], indent=1) + "\n")]

CONTACTS = [f("contacts.csv",
              "name,email\nLia Example,lia@example.com\nMarco Sample,marco@example.com\n"
              "Lia Example,lia@example.com\nJun Placeholder,jun@example.com\n"
              "Marco Sample,marco@example.com\nTala Demo,tala@example.com\n")]

PROJECT = [
    f("myproject/main.py", "from utils import greet\n\nprint(greet('world'))\n"),
    f("myproject/utils.py", "def greet(name):\n    return 'hello ' + name\n"),
    f("myproject/README.md", "# myproject\n\nA small demo.\n"),
    f("myproject/requirements.txt", "requests\n"),
    f("myproject/config/app.conf", "host=localhost\nport=8080\ndebug=false\n"),
    f("myproject/scripts/deploy.sh", "#!/bin/sh\r\necho deploying\r\ncp -r build /srv/app\r\n", ),
    f("myproject/scripts/test.sh", "#!/bin/sh\r\npython3 -m pytest\r\n"),
    f("myproject/build/app.bin", big("fake-binary", 1500)),
    f("myproject/build/temp.o", "fake-object\n"),
    f("myproject/__pycache__/utils.cpython-312.pyc", "fake-bytecode\n"),
]
for e in PROJECT:
    if e["p"].endswith(".sh"):
        e["m"] = 0o755

BACKUPS = [
    f("backups/site-2026-08-01.tar", "fake-tar: site backup august\n"),
    f("backups/site-2026-09-01.tar", "fake-tar: site backup september\n"),
    f("backups/site-2026-10-01.tar", "fake-tar: site backup october\n"),
    f("backups/site-2026-10-15.tar", "fake-tar: site backup mid october\n"),
    f("backups/README.txt", "monthly backups of the club website\n"),
]

TEMP = [
    f("keep.txt", "keep me\n"), f("junk.tmp", "x\n"), f("other.tmp", "y\n"),
    f("report.docx~", "fake-docx backup\n"), f("report.docx", "fake-docx: report\n"),
]

PDFS = [f("a.pdf", "fake-pdf a\n"), f("b.pdf", "fake-pdf b\n"), f("notes.txt", "x\n"),
        f("school/c.pdf", "fake-pdf c\n")]


def course_tar():
    """A real, small, uncompressed tar whose bytes are all ASCII (ustar headers
    are octal text), so it survives as a JSON string."""
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=tarfile.USTAR_FORMAT) as t:
        for name, data in [("course/week1.txt", b"week 1: intro to shells\n"),
                           ("course/week2.txt", b"week 2: files and folders\n")]:
            ti = tarfile.TarInfo(name)
            ti.size = len(data)
            ti.mtime = 1767225600
            ti.mode = 0o644
            ti.uname = ti.gname = "student"
            t.addfile(ti, io.BytesIO(data))
    b = buf.getvalue()
    # keep the members and the two end-of-archive blocks; drop record padding
    end = 0
    off = 0
    while off < len(b):
        hdr = b[off:off + 512]
        if hdr == b"\0" * 512:
            end = off + 1024
            break
        size = int(hdr[124:136].rstrip(b"\0 ") or b"0", 8)
        off += 512 + ((size + 511) // 512) * 512
    b = b[:end]
    assert all(c < 0x80 for c in b)
    s = b.decode("ascii")
    # self-check: the truncated archive still lists both members
    with tarfile.open(fileobj=io.BytesIO(b)) as t:
        assert len(t.getnames()) == 2
    return s


TARFX = [f("course-materials.tar", course_tar()), f("Downloads/readme.txt", "from your teacher\n")]

HOME = [f("notes.txt", "hello\n"), f("Documents/todo.txt", "nothing yet\n")]

# one note per subject, so "put my notes in the right ones" has referents
SUBJECT_NOTES = [
    f("algebra-hw.txt", "algebra homework: solve for x in 2x + 3 = 11\n"),
    f("poem.txt", "a short poem for english class\n"),
    f("lab-notes.txt", "science lab: boiling point of water\n"),
]


# What every real home folder also holds, under E and P: dotfiles, a cache, a
# trash, a desktop, a deep archive, and names with spaces, an apostrophe and
# an accent. All content is unique, so the layer adds no duplicates, PDFs or
# logs that a task's own files would be confused with.
HOME_LAYER = [
    f(".bashrc", "# ~/.bashrc\nalias ll='ls -l'\nexport EDITOR=nano\n"),
    f(".profile", "# ~/.profile\n[ -f ~/.bashrc ] && . ~/.bashrc\n"),
    f(".config/gedit/settings.ini", "[editor]\ntheme=dark\n"),
    f(".cache/thumbnails/normal/9f2c1e.png", "fake-png: thumbnail cache entry\n"),
    f(".local/share/Trash/files/old shopping list.txt", "eggs\nrice\n"),
    f(".local/share/Trash/info/old shopping list.txt.trashinfo",
      "[Trash Info]\nPath=/home/agent/old shopping list.txt\nDeletionDate=2026-10-20T09:12:00\n"),
    f("Desktop/Untitled Document 1.txt", "half-written note\n"),
    f("Desktop/Lia's birthday plan.txt", "venue: canteen\n"),
    f("Documents/archive/2024/sem2/finals/review notes.txt", "review: chapters 4 to 7\n"),
    f("Documents/Résumé tips.txt", "keep it to one page\n"),
]


def realistic(fixture):
    """Ages for every entry of an E or P fixture, and a dir entry for every
    folder that holds something, as old as its newest content. Files without
    a date in their name get a stable spread; a dated name decides its age."""
    out, newest = [], {}
    for e in fixture:
        e = dict(e)
        if "a" not in e:
            m = re.search(r"(20\d\d)-(\d\d)-(\d\d)", e["p"])
            if m:
                e["a"] = int(days_before_ref(*map(int, m.groups())) * DAY)
            elif e["k"] == "file":
                lo, hi = (1, 30) if e["p"].startswith(("Downloads/", ".cache/", "logs/")) else (3, 400)
                e["a"] = int(stable_days(e["p"], lo, hi) * DAY)
        out.append(e)
        parts = e["p"].split("/")
        for i in range(1, len(parts)):
            d = "/".join(parts[:i])
            newest[d] = min(newest.get(d, e.get("a", 0)), e.get("a", 0))
    have = {e["p"] for e in out}
    for d in sorted(newest):
        if d not in have:
            out.append({"p": d, "k": "dir", "a": newest[d]} if newest[d] else dr(d))
    return out


def fx(*parts):
    seen, outl = set(), []
    for part in parts:
        for e in part:
            if e["p"] not in seen:
                seen.add(e["p"])
                outl.append(dict(e))
    return outl


# ---------------------------------------------------------------- partition S
S = [
    # id, category, interface, plain, novice, features
    ("M007", "file-management",
     "Delete every folder inside this folder, including the folders inside those",
     "Get rid of all the folders in here",
     "delete all the folders in here i dont need them", ["typo"]),
    ("M014", "text-editing",
     "In every file named foo_fn in this folder and its subfolders, replace the text foo_fn with bar_fn",
     "Wherever there is a file called foo_fn, change foo_fn to bar_fn inside it",
     "change foo_fn to bar_fn in the foo_fn files", ["underspecified"]),
    ("M032", "file-management",
     "Delete all Thumbs.db files in this folder and every folder inside it",
     "Clear out the Thumbs.db files that Windows left everywhere",
     "pakidelete naman lahat ng Thumbs.db, ang kalat kasi galing windows", ["taglish"]),
    ("M044", "file-management",
     "Delete the files in this folder that do not also exist in the mirror folder",
     "Anything in here that is not also in the mirror folder, get rid of it",
     "delete whatever isnt in the mirror folder", ["vague-target", "typo"]),
    ("M057", "file-management",
     "Change the permissions of every file on the computer",
     "Change who is allowed to open every file on the system",
     "change the permisions on every file on my computer", ["typo", "underspecified"]),
    ("M060", "archives-backups",
     "Compress every file in this folder, but not the ones in subfolders",
     "Shrink all the files here so they take less space, and leave the folders inside alone",
     "make the files here smaller pls", ["underspecified", "typo"]),
    ("M095", "file-management",
     "Delete the compiler output files (a.out, .o files and core dumps) from this folder and its subfolders",
     "Throw away the leftover junk files the compiler made",
     "delete the junk files from compiling", ["vague-target"]),
    ("M109", "file-management",
     "Delete all empty folders here, including folders that only contain empty folders",
     "Remove the folders that have nothing in them",
     "theres a bunch of empty folders can u remove", ["typo"]),
    ("M126", "archives-backups",
     "Create one compressed archive called tarfile.tar.bz2 containing everything on the computer",
     "Pack up absolutely everything on this computer into one file named tarfile.tar.bz2",
     "backup my whole computer into one file", ["underspecified"]),
    ("M128", "file-management",
     "Delete all the files in this folder, but not the ones inside subfolders, without asking for confirmation",
     "Wipe out every file sitting directly in this folder and leave the folders alone",
     "delete everything here except the folders, dont ask me", ["typo"]),
    ("M150", "multi-step",
     "Make every .php file in this folder and its subfolders readable and runnable by everyone (755), then tell me how many files changed",
     "Let anyone run the PHP files in here, and tell me how many there were",
     "make my php files work and how many are there", ["multi-intent", "underspecified"]),
    ("M009", "text-editing",
     "In all the XML files in this folder and its subfolders, replace firstWord with newFirstWord, secondWord with newSecondWord, and thirdWord with newThirdWord",
     "In my XML files, swap firstWord, secondWord and thirdWord for newFirstWord, newSecondWord and newThirdWord",
     "pls change firstWord secondWord thirdWord to newFirstWord newSecondWord newThirdWord in my xml files", ["typo"]),
    ("M026", "text-editing",
     "Replace the word apple with orange in every file in this folder",
     "Everywhere my files here say apple, make them say orange",
     "change apple to orange in all of them", ["vague-target"]),
    ("M041", "archives-backups",
     "In the backups folder and its subfolders, find the .tgz and .gz files and delete the oldest one",
     "Of the compressed backups in the backups folder, throw away the oldest",
     "delete the oldest backup in my backups folder", ["underspecified"]),
    ("M067", "file-management",
     "Delete all files that were last changed on or before July 1",
     "Get rid of every file nobody has touched since the start of July",
     "delete the old files from before july", ["vague-target"]),
    ("M101", "file-management",
     "Delete the files in the $DIR folder and its subfolders that have not been changed in the last 15 days",
     "In $DIR, throw out anything that nobody has changed in the last 15 days",
     "delete the old stuff in that folder", ["vague-target", "underspecified"]),
    ("M129", "file-management",
     "Inside the MyApp.app folder, delete every folder named Headers and hide any error messages",
     "Remove the Headers folders from MyApp.app and do not bother me with errors",
     "remove the headers thing from myapp", ["vague-target"]),
    ("M134", "file-management",
     "Move every folder here, including subfolders, that changed in the last day to the target-dir folder",
     "Put the folders I worked on since yesterday into the target-dir folder",
     "move todays folders to target-dir", ["vague-target", "typo"]),
    ("M153", "file-management",
     "Make a copy of each file with the digits removed from its name (file123 becomes file), without replacing any file that already exists",
     "Make copies of my files with the numbers taken out of their names, but do not overwrite anything",
     "copy the files but without the numbers in the name, dont replace anything", ["typo"]),
    ("M013", "search",
     "Show the 10 biggest files in this folder and its subfolders",
     "Which ten files in here take up the most room?",
     "whats taking all the space? top 10", ["vague-target", "typo"]),
    ("M140", "search",
     "List every distinct file extension in this folder",
     "What types of files do I have in here?",
     "what kinds of files r in here", ["typo"]),
    ("M159", "system-info",
     "Find the files in this folder that have not been changed in the last 6 months and show their total size in MB",
     "How much room are the files I have not touched in half a year taking up?",
     "how much space is the old stuff using", ["vague-target"]),
]

# Where the NL2Bash description names a path the fixture cannot hold (an
# absolute path outside the task folder), or the fixture lacks what every tier
# asks about, the corpus fixture gains the referent and the description is
# edited only to name it. Each deviation is listed in agentprobe/README.md.
XML_WORDS = "<doc>\n  <a>firstWord</a>\n  <b>secondWord</b>\n  <c>thirdWord</c>\n</doc>\n"
S_FIX = {
    "M044": {"tech": ("/some/dir", "./mirror"),
             "add": [f("mirror/a.txt", "a\n"), f("mirror/notes.txt", "notes\n"),
                     f("mirror/data.csv", "x,1\ny,2\n")]},
    "M041": {"tech": ("'/home/backups'", "'./backups'"),
             "drop": ["alpha.tgz", "beta.tgz", "alpha.gz", "beta.gz"],
             "add": [f("backups/site-2026-03-01.tgz", "fake-tgz: site backup march\n"),
                     f("backups/site-2026-06-01.tgz", "fake-tgz: site backup june\n"),
                     f("backups/db/db-2026-02-15.gz", "fake-gz: database dump february\n"),
                     f("backups/db/db-2026-09-01.gz", "fake-gz: database dump september\n")]},
    "M134": {"tech": ("/path/to/target-dir", "./target-dir"), "add": [dr("target-dir")],
             # src and logs were worked on today; the other folders, the
             # target included, last changed ten days ago
             "ages": {"build": 10, "backup": 10, "tmp": 10, "target-dir": 10}},
    "M009": {"set": {"alpha.xml": XML_WORDS, "beta.xml": XML_WORDS.replace("<doc>", "<doc id=\"2\">")}},
    # The repairs below give a task that did nothing on its fixture something
    # to act on, the way M044 gained its mirror folder.
    "M026": {"add": [f("fruit.txt", "apple pie\napple juice\n"), f("shopping.txt", "apples, bananas, rice\n")]},
    "M129": {"add": [f("MyApp.app/Contents/Headers/app.h", "int app_main(void);\n"),
                     f("MyApp.app/Contents/Frameworks/Lib.framework/Headers/lib.h", "int lib_init(void);\n"),
                     f("MyApp.app/Contents/MacOS/MyApp", "fake-binary\n")]},
    # a name that loses its digits onto an existing file, so "without
    # replacing" matters
    "M153": {"add": [f("file123.txt", "numbered\n"), f("file.txt", "already here\n"),
                     f("photo2.jpg", "fake-jpeg\n"), f("song01.mp3", "fake-mp3\n")]},
    # Age criteria get files on both sides of the line. July 1 is read as the
    # last one before the run, so 400 days is before it and 1 day after it
    # (except in a run made in the first days of July).
    "M067": {"ages": {"a.txt": 400, "notes.txt": 500, "backup/old.txt": 650, "logs/x.log": 420,
                      "b.txt": 1, "report.docx": 2, "data.csv": 1, "src/main.go": 0.5}},
    "M101": {"ages": {"a.txt": 60, "big.log": 40, "backup/old.txt": 200, "logs/x.log": 30,
                      "b.txt": 2, "notes.txt": 5, "logs/y.log": 1, "src/main.go": 3}},
    "M159": {"ages": {"backup/old.txt": 400, "big.log": 300, "M/item.txt": 250, "a.txt": 210,
                      "b.txt": 10, "notes.txt": 30, "data.csv": 90, "report.docx": 2}},
}
# M041's backups get ages that agree with the dates in their names.
S_FIX["M041"]["add"] = [age(e, days_before_ref(*map(int, re.search(r"(\d{4})-(\d\d)-(\d\d)", e["p"]).groups())))
                        for e in S_FIX["M041"]["add"]]

tasks = []
for mid, cat, iface, plain, novice, feats in S:
    r = rows[mid]
    n = int(r["src"].split(":")[1])  # 0-based line index into nl2bash_all.nl
    tech, fixture = nl[n].strip(), [dict(e) for e in r["fx"]]
    fix = S_FIX.get(mid, {})
    if "tech" in fix:
        old, new = fix["tech"]
        assert tech.count(old) == 1, (mid, tech)
        tech = tech.replace(old, new)
    drop = set(fix.get("drop", []))
    assert drop <= {e["p"] for e in fixture}, mid
    fixture = [e for e in fixture if e["p"] not in drop]
    for e in fixture:
        if e["p"] in fix.get("set", {}):
            e["d"] = fix["set"][e["p"]]
    assert set(fix.get("set", {})) <= {e["p"] for e in fixture}, mid
    fixture += fix.get("add", [])
    have = {e["p"]: e for e in fixture}
    for path, days in fix.get("ages", {}).items():
        if path in have:
            have[path]["a"] = int(days * DAY)
        else:
            # a folder the corpus fixture made only as a parent
            assert any(p.startswith(path + "/") for p in have), (mid, path)
            fixture.append({"p": path, "k": "dir", "a": int(days * DAY)})
    tasks.append({
        "id": "S-" + mid, "partition": "S", "category": cat, "fixture": fixture,
        "prompts": {"technical": tech, "interface": iface, "plain": plain, "novice": novice},
        "features": feats,
        "sources": {"technical": "nl2bash:%d" % n, "interface": "authored", "plain": "authored", "novice": "authored"},
    })

# ---------------------------------------------------------------- partition E
U_DUP = "https://ubuntu-mate.community/t/how-to-easily-remove-duplicate-photos/15627"
U_CDRIVE = "https://answers.launchpad.net/ubuntu/+question/7264"
U_RENAME = "https://ubuntu-mate.community/t/rename-multiple-files-at-once/20707"
U_SKYPE = "https://answers.launchpad.net/ubuntu/+source/yelp/+question/10869"
U_UNIST = "https://www.linuxquestions.org/questions/linux-newbie-8/unistall-libmotif3-ubuntu-876277/"
U_UNINSTALL = "https://answers.launchpad.net/ubuntu/+question/17832"
U_FULL = "https://www.linuxquestions.org/questions/linux-newbie-8/disk-drive-full-229536/"
U_CRIT = "https://www.linuxquestions.org/questions/linux-newbie-8/drive-critical-help-931193/"
U_CLEAN = "https://www.linuxquestions.org/questions/linux-newbie-8/cleaning-hdd-757078/"
U_ZIP = "https://answers.launchpad.net/ubuntu/+question/18762"
U_FIND = "https://www.linuxquestions.org/questions/linux-newbie-8/how-to-find-a-file-4175584061/"
U_USB = "https://ask.libreoffice.org/t/how-do-i-save-my-files-to-a-usb-drive/34936"
U_SPACE = "https://lists.ubuntu.com/archives/ubuntu-users/2005-April/032516.html"
U_UNZIP = "https://www.linuxquestions.org/questions/linux-newbie-8/unzipping-files-325866/"
U_CSV = "https://forums.tomsguide.com/threads/combining-csv-files-into-one-csv-remove-duplicate-header.434352/"
U_RECOVER = "https://lists.ubuntu.com/archives/ubuntu-users/2013-March/268133.html"
U_OOPS = "https://www.linuxquestions.org/questions/linux-general-1/oops-i-deleted-etc-174598/"
U_MV = "https://www.linuxquestions.org/questions/linux-newbie-8/misuse-of-mv-command-675040/"
U_URGENT = "https://www.linuxquestions.org/questions/linux-general-1/urgent-mv-vs-rm-46418/"
U_UNDELETE = "https://www.linuxquestions.org/questions/linux-newbie-8/undelete-72122-print/"
# Sources of the trial-and-error P tasks; each was fetched and its question
# confirmed to match the scenario on 2026-10-24.
U_PHBIN = "https://lists.ubuntu.com/archives/ubuntu-ph/2008-January/001441.html"
U_COMEBACK = "https://answers.launchpad.net/ubuntu/+question/159546"
U_FLASH = "https://lists.ubuntu.com/archives/ubuntu-za/2010-October/006751.html"
U_JPG = "https://ubuntu-mate.community/t/change-size-of-a-jpg/19762"
U_DESKTOP = "https://lists.ubuntu.com/archives/ubuntu-uk/2010-February/022942.html"
L7 = "testing-plan:L7"

E = [
    # id, category, fixture, technical, interface, plain, novice, features, source
    ("dedupe-photos", "file-management", fx(PHOTOS),
     "Find files under Pictures/ with identical content (compare md5sum) and delete every copy but one",
     "Delete the duplicate photos in the Pictures folder, keeping one copy of each",
     "I have the same pictures saved more than once, get rid of the extra copies",
     "how to easily remove duplicate photos?? i have so many copies", ["question-as-action", "vague-target"], U_DUP),
    ("sort-downloads", "file-management", fx(DOWNLOADS),
     "In Downloads/, create one subdirectory per file extension and mv each file into the matching one",
     "Sort the files in my Downloads folder into folders by file extension",
     "My downloads are a mess, put things in order by their file extension (pdf, jpg and so on)",
     "pakiayos naman yung downloads ko by extension, ang gulo na talaga", ["taglish"], "authored"),
    ("clear-installers", "file-management", fx(DOWNLOADS),
     "rm Downloads/*.deb Downloads/*.iso",
     "Delete the installer files (.deb and .iso) in the Downloads folder",
     "Clear out the setup files I already used from my downloads",
     "pls delete the installer junk in downloads i dont need it", ["vague-target", "typo"], "authored"),
    ("rename-over", "file-management", fx(DOCS),
     "mv Documents/school/essay_v2.docx Documents/school/essay.docx",
     "Rename essay_v2.docx in the school folder to essay.docx",
     "The second version of my essay should just be called essay now",
     "rename the new essay to the old name", ["vague-target", "underspecified"], "authored"),
    ("number-photos", "file-management", fx(PHOTOS),
     "Rename every *.jpg in Pictures/trip/ to trip_001.jpg, trip_002.jpg, ... in name order",
     "Rename all the photos in the trip folder to trip_001, trip_002 and so on",
     "Give my trip photos tidy numbered names",
     "rename multiple files at once? want the trip ones to be trip 1 2 3", ["question-as-action", "underspecified"], U_RENAME),
    ("temp-files", "file-management", fx(TEMP),
     "rm -f *.tmp *~",
     "Delete all files ending in .tmp or ~ in this folder",
     "get rid of the temporary files here",
     "can u delete the temp junk", ["vague-target", "typo"], L7),
    ("gather-pdfs", "file-management", fx(PDFS),
     "mkdir -p PDFs && find . -name '*.pdf' -not -path './PDFs/*' -exec mv {} PDFs/ \\;",
     "Move every PDF file in this folder and its subfolders into a new folder called PDFs",
     "put all my pdfs together in one place",
     "put all the pdf's in 1 folder pls", ["typo", "underspecified"], L7),
    ("free-space", "file-management", fx(DOWNLOADS_BIG, DOCS),
     "Run du -ah . | sort -rh | head to find the largest files, then delete the large ones that are not documents",
     "Show me the biggest files in my folders and delete the ones I probably do not need, to free up space",
     "My computer is running out of room, clear some space",
     "disk drive full!! what do i delete", ["urgent", "question-as-action", "underspecified"], U_FULL),
    ("find-resume", "search", fx(DOCS, DOWNLOADS),
     "find . -iname '*resume*'",
     "Search all my folders for a file with resume in its name",
     "I saved my resume somewhere and now I cannot find it",
     "how to find a file i saved, its my resume", ["question-as-action", "underspecified"], U_FIND),
    ("which-note", "search", fx(NOTES),
     "grep -rl deadline notes/",
     "Search the notes folder for files containing the word deadline",
     "which of my notes mentions the deadline",
     "where did i write about the deadline", ["vague-target"], L7),
    ("biggest-file", "search", fx(DOWNLOADS_BIG),
     "ls -S Downloads | head -1, or du -a Downloads | sort -rn | head",
     "Show me the largest file in the Downloads folder",
     "what's taking up the most room in my downloads",
     "whats eating all my space", ["vague-target", "typo"], L7),
    ("thesis-drafts", "search", fx(THESIS, DOWNLOADS),
     "find . -name 'thesis_draft*'",
     "Find every thesis draft file in my folders and list them",
     "Where are all the versions of my thesis draft?",
     "where r all my thesis drafts, i have too many versions", ["typo", "vague-target"], "authored"),
    ("zip-thesis", "archives-backups", fx(THESIS),
     "zip -r thesis.zip thesis/",
     "Compress the thesis folder into a zip file",
     "Pack up my thesis so I can email it as one file",
     "how do you create a .zip file? need to send my thesis folder to my prof", ["question-as-action"], U_ZIP),
    ("open-tar", "archives-backups", fx(TARFX),
     "tar -xf course-materials.tar",
     "Extract the course-materials.tar archive into this folder",
     "Open up the bundle of course files my teacher sent",
     "how do i open this .tar thing, double click doesnt work and theres no winrar", ["question-as-action", "wrong-model", "typo"], U_UNZIP),
    ("usb-backup", "archives-backups", fx(DOCS),
     "cp -r Documents/. USB/Documents/",
     "Copy my Documents folder onto the USB drive (it is the USB folder here)",
     "Save a copy of my documents onto my USB stick, which shows up as the USB folder",
     "how do i save my files to a usb drive", ["question-as-action", "underspecified"], U_USB),
    ("prune-backups", "archives-backups", fx(BACKUPS),
     "ls backups/site-*.tar | sort | head -n -2 | xargs rm",
     "Delete all but the two newest backup files in the backups folder",
     "Keep only my two latest website backups and throw away the rest",
     "too many backups, remove the old ones", ["vague-target", "underspecified"], "authored"),
    ("sum-scores", "data-wrangling", fx(GRADES),
     "awk -F, 'NR>1{s+=$2}END{print s}' grades.csv",
     "Add up the values in the score column of grades.csv",
     "add up all the scores in my grades file for me",
     "whats the total of the scores in my csv", ["typo"], L7),
    ("merge-csv", "data-wrangling", fx(SALES),
     "Concatenate sales/*.csv into sales/all.csv, keeping only the first file's header line",
     "Combine the three CSV files in the sales folder into one file with a single header row",
     "Put my monthly sales sheets together into one, without repeating the column titles",
     "combining csv files into one csv, remove the duplicate header??", ["question-as-action", "underspecified"], U_CSV),
    ("out-of-stock", "data-wrangling", fx(INVENTORY),
     "jq -r '.[] | select(.stock == 0) | .name' inventory.json",
     "List the products in inventory.json whose stock is zero",
     "Which items have we run out of?",
     "ano yung mga out of stock sa inventory? pakilista lang", ["taglish"], "authored"),
    ("errors-by-hour", "data-wrangling", fx(LOGS),
     "grep ERROR logs/app.log | cut -c1-13 | sort | uniq -c",
     "Count how many ERROR lines in logs/app.log there are for each hour",
     "How many errors did the app have each hour?",
     "how many errors r there each hour in the app logs", ["typo", "vague-target"], "authored"),
    ("dedupe-contacts", "data-wrangling", fx(CONTACTS),
     "Remove duplicate rows from contacts.csv in place, keeping the header line first",
     "Remove the duplicate rows from contacts.csv",
     "Some people appear twice in my contact list, clean that up",
     "pls delet the duplicates in my contacts", ["typo", "vague-target"], "authored"),
    ("csv-to-json", "data-wrangling", fx(GRADES),
     "Convert grades.csv to a JSON array of objects in grades.json with python3 -c and the csv and json modules",
     "Save the grades.csv spreadsheet as a JSON file named grades.json",
     "Turn my grades list into the format my website reads (JSON)",
     "can u make my grades file into json for my website", ["typo", "underspecified"], "authored"),
    ("draft-to-final", "text-editing", fx(NOTES),
     "sed -i 's/draft/final/g' notes/ideas.txt",
     "Replace all occurrences of draft with final in the file ideas.txt in the notes folder",
     "in my ideas notes change the word draft to final everywhere",
     "chnage draft to final in my ideas note", ["typo"], L7),
    ("change-port", "text-editing", fx(PROJECT),
     "sed -i 's/^port=8080$/port=3000/' myproject/config/app.conf",
     "Change the port setting in myproject's app.conf file from 8080 to 3000",
     "Make my project run on port 3000 instead of 8080",
     "change the port to 3000, its in some config file", ["vague-target", "typo"], "authored"),
    ("append-todo", "text-editing", fx(NOTES),
     "echo 'buy milk' >> notes/todo.txt",
     "Add a line saying buy milk to the end of todo.txt in the notes folder",
     "Put buy milk on my to-do list",
     "add buy milk to my list", ["vague-target"], "authored"),
    ("crlf-scripts", "text-editing", fx(PROJECT),
     "sed -i 's/\\r$//' myproject/scripts/*.sh",
     "Convert the Windows line endings in the .sh files in myproject's scripts folder to Unix line endings",
     "My project's scripts were saved on Windows and will not run here, fix them",
     "my script wont run, it says something about ^M ?? i wrote it on windows", ["question-as-action", "wrong-model", "vague-target"], "authored"),
    ("uninstall-app", "packages-services", fx(HOME),
     "sudo apt remove --purge skypeforlinux",
     "Uninstall the Skype application",
     "Take Skype off my computer",
     "how uninstall skype? i cant find it in control panel", ["question-as-action", "wrong-model", "typo"], U_SKYPE),
    ("install-vlc", "packages-services", fx(HOME),
     "sudo apt install -y vlc",
     "Install the VLC media player application",
     "Get VLC on my computer so I can play my videos",
     "pano mag install ng vlc? ayaw mag play ng videos ko", ["taglish", "question-as-action"], "authored"),
    ("restart-web", "packages-services", fx(HOME),
     "sudo systemctl restart apache2 && systemctl status apache2 --no-pager",
     "Restart the Apache web server service and show whether it is running",
     "My website stopped working, turn it off and on again",
     "website not loading pls fix asap", ["urgent", "underspecified", "typo"], "authored"),
    ("space-left", "system-info", fx(HOME),
     "df -h",
     "Show how much free space is left on each disk",
     "How much room do I have left on this computer?",
     "how to check how much space i have left on my c drive", ["question-as-action", "wrong-model"], U_SPACE),
    ("folder-size", "system-info", fx(DOWNLOADS),
     "du -sh Downloads",
     "Report the total disk usage of the Downloads folder",
     "how much space is my downloads folder using",
     "is my downloads too big", ["question-as-action", "underspecified"], L7),
    ("slow-laptop", "system-info", fx(HOME),
     "ps aux --sort=-%mem | head -n 10",
     "Show which running applications are using the most memory",
     "Why is my computer so slow? What is using it up?",
     "laptop super slow pls help whats wrong", ["urgent", "vague-target", "underspecified"], "authored"),
    ("where-and-folders", "multi-step", fx(HOME),
     "pwd && mkdir Math English Science",
     "Tell me which folder I am in, then create folders named Math, English and Science",
     "Where am I right now? And set up a folder for each of my subjects: math, english, science",
     "where am i and can you make 3 folders for math english science", ["multi-intent", "question-as-action"], "authored"),
    ("backup-then-clean", "multi-step", fx(PROJECT),
     "tar -cf myproject-backup.tar myproject && rm -rf myproject/build myproject/__pycache__",
     "Make a backup archive of the myproject folder, then delete its build and __pycache__ folders",
     "Save a copy of my project first, then clear out the generated files",
     "backup my project first then clean it up", ["multi-intent", "vague-target"], "authored"),
    ("compress-old-logs", "multi-step", fx(LOGS),
     "gzip logs/app.log.1 logs/app.log.2",
     "Compress the older log files (app.log.1 and app.log.2) in the logs folder and remove the uncompressed originals",
     "Squash the old logs down so they take less room, I do not need the originals",
     "logs r too big, zip the old ones and delete the unzipped ones after", ["multi-intent", "vague-target", "typo"], "authored"),
]

def sources(src):
    """Per tier, where the wording came from. A forum title is the model for
    the novice phrasing; a Layer 7 task gives the plain phrasing verbatim and
    is the model for the novice one. Everything else is authored."""
    tiers = {t: "authored" for t in ("technical", "interface", "plain", "novice")}
    if src == L7:
        tiers["plain"], tiers["novice"] = L7, "after:" + L7
    elif src != "authored":
        tiers["novice"] = "after:" + src
    return tiers


for tid, cat, fixture, tech, iface, plain, novice, feats, src in E:
    tasks.append({"id": "E-" + tid, "partition": "E", "category": cat,
                  "fixture": realistic(fx(fixture, HOME_LAYER)),
                  "prompts": {"technical": tech, "interface": iface, "plain": plain, "novice": novice},
                  "features": feats, "sources": sources(src)})

# ---------------------------------------------------------------- partition P
P = [
    ("big-then-delete", "file-management", fx(DOWNLOADS_BIG),
     "du -ah Downloads | sort -rh | head -n 5",
     "Show me the five biggest files in my Downloads folder",
     "What are the biggest things in my downloads?",
     "what are the big files in my downloads",
     ["delete them", "did you delete those?"],
     ["pronoun", "question-as-action", "vague-target"], "authored"),
    ("move-then-panic", "file-management", fx(PHOTOS),
     "mkdir -p Old && mv Pictures/* Old/",
     "Move everything in my Pictures folder into a new folder called Old",
     "Put all my pictures away in a folder called Old",
     "move all my pics to an old folder",
     ["wait no i still need those", "i messed up, undo everything"],
     ["urgent", "pronoun", "typo"], "authored"),
    ("keep-newest-draft", "file-management", fx(THESIS),
     "ls thesis/drafts/thesis_draft_v*.docx",
     "Show me all the thesis draft files in the drafts folder",
     "Which versions of my thesis draft do I have?",
     "find my thesis drafts",
     ["keep the newest one and get rid of the rest", "do the same for the presentation files"],
     ["pronoun", "vague-target", "underspecified"], "authored"),
    ("c-drive", "file-management", fx(DOCS),
     "ls -la Documents",
     "Open the Documents folder and list what is in it",
     "Show me what is in my documents",
     "open my c drive and show whats in documents",
     ["copy everything to my d drive", "did it work? i cant see the d drive"],
     ["wrong-model", "question-as-action", "pronoun"], U_CDRIVE),
    ("dupes-did-you", "file-management", fx(PHOTOS),
     "Find files under Pictures/ with identical md5sum and delete all but one copy of each",
     "Delete the duplicate photos in the Pictures folder, keeping one of each",
     "Get rid of the pictures I saved twice",
     "pls delet the duplicate pics",
     ["did you delete the originals too??", "show me whats left"],
     ["typo", "question-as-action", "urgent"], "authored"),
    ("taglish-pdfs", "file-management", fx(PDFS, DOWNLOADS),
     "find . -name '*.pdf'",
     "Find all the PDF files in my folders",
     "Where are all my PDFs?",
     "pakihanap naman yung mga pdf ko, nakakalat kasi",
     ["ilipat mo silang lahat sa isang folder tapos burahin mo na yung mga doble"],
     ["taglish", "pronoun", "multi-intent"], "authored"),
    ("crash-clear-logs", "data-wrangling", fx(LOGS),
     "grep ERROR logs/*.log",
     "Show the error lines in the log files in the logs folder",
     "Why did my app crash? Look at its logs",
     "app crashed again check why",
     ["ok just clear the logs then", "wait did that delete anything important"],
     ["pronoun", "question-as-action", "urgent"], "authored"),
    ("rename-undo", "file-management", fx(DOCS),
     "for f in Documents/school/*.docx; do mv \"$f\" \"${f%.docx}_$(date +%F).docx\"; done",
     "Rename the .docx files in the school folder so each name ends with today's date",
     "Put today's date in the names of my school documents",
     "rename my school docs so they have the date",
     ["undo that, i liked the old names better"],
     ["pronoun", "urgent", "underspecified"], "authored"),
    ("storage-full", "system-info", fx(DOWNLOADS, DOCS, THESIS),
     "df -h . && du -sh */ | sort -rh",
     "Show how full the disk is and which folders are the largest",
     "My laptop says storage is almost full, what is using it?",
     "laptop says storage almost full help",
     ["what can i delete", "ok delete those", "is my school stuff still there?"],
     ["urgent", "underspecified", "pronoun", "question-as-action"], U_CRIT),
    ("subjects", "multi-step", fx(NOTES, SUBJECT_NOTES),
     "pwd && ls",
     "Tell me which folder I am in and what is in it",
     "Where am I right now, and what's here?",
     "where am i and whats here",
     ["make a folder for each subject, math english science", "put my notes in the right ones",
      "move it back, the math one was wrong"],
     ["pronoun", "vague-target"], "authored"),
    ("recover-deleted", "file-management", fx(NOTES, TEMP),
     "rm notes/todo.txt",
     "Delete the todo.txt file in the notes folder",
     "Throw away my to-do note",
     "delete my todo list",
     ["oops i deleted the wrong one, i needed that", "can you get it back?"],
     ["urgent", "question-as-action", "pronoun"], U_RECOVER),
    # Trial and error: the person acts, judges the result by what they see,
    # and corrects course. No follow-up presumes the previous turn succeeded
    # or failed, so the agent has to look before it answers or acts again.
    ("trash-ibalik", "file-management", fx(NOTES),
     "ls -la .local/share/Trash/files",
     "Show me what is in the Trash",
     "I deleted something I still need. Where do deleted files go?",
     "asan yung recycle bin dito? may na-delete ako na kailangan ko pa",
     ["yung shopping list, ibalik mo sa dati", "wala pa rin eh, hindi ko makita"],
     ["trial-and-error", "taglish", "wrong-model", "question-as-action", "pronoun"], U_PHBIN),
    ("still-there", "file-management", fx(HOME),
     "rm Desktop/Untitled*",
     "Delete the file Untitled Document 1 on my Desktop",
     "Get rid of the untitled note on my desktop",
     "delete that untitled thing on my desktop",
     ["its still there", "why does it keep comming back"],
     ["trial-and-error", "vague-target", "question-as-action", "pronoun", "typo"], U_COMEBACK),
    ("usb-still-full", "file-management", fx(DOCS),
     "rm USB/Documents/report.docx",
     "Delete the old copy of report.docx on the USB drive",
     "Clear the old report off my USB stick to make room",
     "delete the old report on my usb its full",
     ["didnt work, it still says full", "ok just delete everything on the usb then", "wait not my music!!"],
     ["trial-and-error", "urgent", "pronoun", "typo"], U_FLASH),
    ("other-resume", "file-management", fx(DOCS),
     "cp Documents/school/resume_2026.docx Desktop/",
     "Copy my current resume to the Desktop",
     "Put a copy of my resume on the desktop",
     "put my resume on the desktop",
     ["no not that one, the other one", "the new one i mean. and take the wrong one off"],
     ["trial-and-error", "vague-target", "underspecified", "pronoun"], "authored"),
    ("hindi-yan", "file-management", fx(THESIS),
     "rm thesis/drafts/thesis_draft_v1.docx",
     "Delete the first thesis draft in the drafts folder",
     "Throw away my oldest thesis draft",
     "pakibura naman yung lumang draft ng thesis ko",
     ["hindi yan, yung isa pa", "teka nabura mo ba yung chapter 1 ko??"],
     ["trial-and-error", "taglish", "vague-target", "pronoun", "urgent"], "authored"),
    ("only-last-week", "file-management", fx(DOWNLOADS),
     "ls -lt Downloads",
     "List my Downloads folder, newest first",
     "What did I download recently?",
     "what did i dowload lately",
     ["delete them", "noo not all of them!! only the ones from last week", "wait which ones are gone now"],
     ["trial-and-error", "pronoun", "typo", "urgent", "vague-target"], "authored"),
    ("jpg-smaller", "file-management", fx(DOWNLOADS),
     "ls -l Downloads/wallpaper.jpg",
     "Show the file size of wallpaper.jpg in my Downloads",
     "How big is the wallpaper picture I downloaded?",
     "how do i make my wallpaper jpg smaller, i want it for my profile pic but its to big",
     ["it didnt change, still says to big", "ok forget it just put it on my desktop"],
     ["trial-and-error", "question-as-action", "typo", "pronoun"], U_JPG),
    ("zip-then-shrink", "multi-step", fx(PHOTOS),
     "du -sh Pictures/trip",
     "Show how much space the trip folder in Pictures takes",
     "How big are my trip photos all together?",
     "my trip pics wont fit in the email how big are they",
     ["zip them up", "actualy nvm just make them smaller", "did that ruin the originals?"],
     ["trial-and-error", "pronoun", "typo", "question-as-action", "underspecified"], "authored"),
    ("said-it-again", "file-management", fx(DOWNLOADS),
     "rm Downloads/Syllabus_CS101(1).pdf",
     "Delete the duplicate syllabus PDF in my Downloads, keeping one",
     "There are two copies of my syllabus in downloads, remove the extra one",
     "theres 2 syllabus in my downloads delete the extra one",
     ["delete the extra syllabus", "the copy!! the one with the 1 at the end. get rid of it",
      "is the real one still there?"],
     ["trial-and-error", "typo", "question-as-action", "pronoun"], "authored"),
    ("desktop-gone", "file-management", fx(DOWNLOADS),
     "mv Desktop/*.txt Documents/",
     "Move the text files on my Desktop into Documents",
     "Tidy my desktop by putting its files in my documents",
     "clean up my desktop pls its so messy",
     ["where did all my stuff go?? did you delete everything??", "put it back the way it was"],
     ["trial-and-error", "urgent", "question-as-action", "vague-target", "pronoun"], U_DESKTOP),
    ("undo-pdf-move", "file-management", fx(DOWNLOADS),
     "mv Downloads/*.pdf Documents/",
     "Move the PDF files in Downloads into Documents",
     "Put my downloaded PDFs with my documents",
     "move my pdfs to documents",
     ["undo that i cant find my invoice now", "is it back? where is it"],
     ["trial-and-error", "pronoun", "urgent", "question-as-action"], "authored"),
]

for tid, cat, fixture, tech, iface, plain, novice, follow, feats, src in P:
    tasks.append({"id": "P-" + tid, "partition": "P", "category": cat,
                  "fixture": realistic(fx(fixture, HOME_LAYER)),
                  "prompts": {"technical": tech, "interface": iface, "plain": plain, "novice": novice},
                  "follow_ups": follow, "features": feats, "sources": sources(src)})

with open(out, "w") as w:
    for t in tasks:
        w.write(json.dumps(t, ensure_ascii=True, separators=(",", ":")) + "\n")

from collections import Counter
print(Counter(t["partition"] for t in tasks))
print(Counter((t["partition"], t["category"]) for t in tasks))
def kind(s):
    s = s.removeprefix("after:")
    return "nl2bash" if s.startswith("nl2bash") else ("L7" if s == L7 else ("authored" if s == "authored" else "url"))
print("novice wording", Counter((t["partition"], kind(t["sources"]["novice"])) for t in tasks))
print(Counter(x for t in tasks for x in t["features"]))
print("fixture sizes", max(len(t["fixture"]) for t in tasks), max(len(json.dumps(t)) for t in tasks))
