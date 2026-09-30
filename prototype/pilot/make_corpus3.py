#!/usr/bin/env python3
"""Builds pilot/corpus3.jsonl: the HELD-OUT round. 100 NL2Bash commands, drawn with seed
20260921 from those not used in round 1 (indices in round3_indices.json), labelled and given
fixtures BEFORE the frozen analyser (pilot/analyser_frozen.sha256) was run on them. Anything not
listed in X below is read-only, label R, no fixture. Definitions are those of make_corpus.py.
Fixtures are written for the labels' assumptions (targets exist), never from analyser output.
"""
import json, pathlib
HERE = pathlib.Path(__file__).parent
cm = [c for c in (HERE / "nl2bash_all.cm").read_text(encoding="utf-8").split("\n") if c.strip()]
idx = json.load(open(HERE / "round3_indices.json"))

# n -> (shape, label, ambiguous, note, setup, cmd_fx, home)
X = {
 1:  ("runtime","C",0,"xargs sed -i over find results","echo x > a.py; mkdir -p sub; echo y > sub/b.py",None,False),
 2:  ("nonfs","U",1,"rsync to a remote host; remote state is outside the model",None,None,False),
 3:  ("runtime","C",1,"xargs chgrp; the first argument becomes the group so it likely fails","mkdir -p d1/d2 d3",None,False),
 4:  ("runtime","C",0,"find -exec rm over build artefacts","touch a.out x.o core; mkdir -p sub; touch sub/y.o",None,False),
 6:  ("runtime","C",0,"gzip -9 removes the originals",None,None,False),
 7:  ("nonfs","U",1,"remote command over ssh",None,None,False),
 8:  ("runtime","C",1,"cp into a directory overwrites on a name collision","mkdir -p foobar && echo x > a_FooBar.txt",None,True),
 9:  ("runtime","R",1,"malformed cp; would fail",None,None,False),
 12: ("compound","C",0,"tar --create overwrites an existing archive","mkdir -p data && echo x > data/a && echo old > archive.tar",None,False),
 13: ("runtime","R",0,"creates a file with an odd name",None,None,False),
 17: ("runtime","C",0,"xargs chmod over find results","echo x > a.cgi; mkdir -p cgi; echo y > cgi/b.CGI",None,False),
 22: ("runtime","C",0,"sudo find | xargs sudo chmod","mkdir -p path/to/Dir/sub","sudo find path/to/Dir -type d -print0 | xargs -0 sudo chmod 755",False),
 23: ("runtime","R",1,"creates one archive per directory","mkdir -p path/to/dir/d1","find path/to/dir -mindepth 1 -maxdepth 1 -type d -execdir sh -c 'd=${1##*/}; sudo tar -zcpvf \"$d\".tar.gz \"$d\"' - {} \;",False),
 24: ("runtime","R",1,"creates one archive per directory","mkdir -p path/to/dir/d1","find path/to/dir -mindepth 1 -maxdepth 1 -type d -execdir sudo tar -zcpvf {}.tar.gz {} \;",False),
 25: ("runtime","C",0,"find -exec sed -i with variable arguments",None,None,False),
 26: ("runtime","C",0,"find -exec chmod over a tree","mkdir -p home/username/public_html/themes/t1 && echo x > home/username/public_html/themes/t1/style.css","find home/username/public_html/themes -type f -exec chmod 640 {} +",False),
 37: ("compound","R",0,"creates a new archive","echo x > a.pl",None,False),
 39: ("hidden","R",0,"read-only, but pipes into perl",None,None,False),
 45: ("runtime","R",0,"creates directories named by a substitution",None,None,True),
 47: ("nonfs","U",1,"interactive remote session",None,None,False),
 60: ("nonfs","U",1,"rsync to a remote host",None,None,False),
 61: ("nonfs","U",0,"changes what is mounted",None,None,False),
 62: ("runtime","C",0,"xargs runs ksh -c with rm",None,None,False),
 63: ("runtime","C",1,"xargs rmdir; empty directories only","mkdir -p path/to/the/folder/e1 path/to/the/folder/e2/e3","find path/to/the/folder -depth -type d -print0 | xargs -0 rmdir",False),
 64: ("runtime","C",0,"ln -f replaces an existing link","echo x > lib-version.old; echo y > lib-version.new; ln -s lib-version.old SomeLibrary",None,False),
 65: ("hidden","R",0,"external time wrapping ls",None,None,False),
 66: ("hidden","R",0,"copies to the clipboard",None,None,False),
 73: ("flag","R",0,"-fprint creates a file","echo x > s.mp3","find . -name '*.mp3' -fprint nameoffiletoprintto",False),
 76: ("hidden","R",0,"split creates numbered files",None,None,False),
 77: ("hidden","R",0,"tmux environment; no filesystem effect",None,None,False),
 78: ("hidden","R",0,"pipes through pv",None,None,False),
 80: ("runtime","C",1,"chown needs root and would fail otherwise","mkdir -p mydir && echo x > mydir/a.txt","find mydir -type f -name \"*.txt\" -execdir chown root {} ';'",False),
 89: ("hidden","C",1,"runs an unknown script per link",None,None,False),
 92: ("redirect","R",0,"redirect creates a new file",None,None,False),
 95: ("runtime","C",1,"mv into TMP overwrites on a name collision","mkdir -p TMP && echo x > old1.txt && touch -a -d '3 days ago' old1.txt",None,False),
 96: ("nonfs","U",1,"runs a command as another user",None,None,False),
 100:("hidden","C",1,"runs an unknown script while defining an alias",None,None,False),
}
rows=[]
for n,i in enumerate(idx,1):
    shape,label,amb,note,setup,fx,home = X.get(n,("readonly","R",0,"",None,None,False))
    r=dict(id=f"C{n:03d}",partition="C",src=f"nl2bash:{i}",shape=shape,label=label,ambiguous=amb,note=note,command=cm[i])
    if setup: r["setup"]=setup
    if fx: r["cmd_fx"]=fx
    if home: r["home"]=True
    rows.append(r)
out=HERE/"corpus3.jsonl"
with out.open("w",encoding="utf-8") as f:
    for r in rows: f.write(json.dumps(r,ensure_ascii=False)+"\n")
import collections
print(len(rows),"rows;",collections.Counter(r["label"] for r in rows),"ambiguous",sum(r["ambiguous"] for r in rows))
