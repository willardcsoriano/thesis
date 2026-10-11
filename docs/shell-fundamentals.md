# Shell Fundamentals: From `ls` to the Algorithm

## Overview

A study guide that takes the team from knowing `ls`, `grep`, `rm`, and `mkdir` to a sound foundation in bash, aimed squarely at what a panel will ask about SynapseOS. The algorithm's whole job is reading a shell command and deciding what it will change and whether that can be undone, so defending it means understanding the shell as well as it does. Eleven short lessons go from reading a command to scripts, and lessons 3 to 5 (changing files, how bash reads a line, redirection) carry the most weight, because they are exactly the reasoning the algorithm automates. Every lesson has the same four parts: the idea in plain English, commands to try in a throwaway practice folder, what the analysis sees (with its real output), and panel-style questions with answers. A cheat sheet at the end gives every common command, what it can destroy, and how the analysis treats it.

## Table of Contents

- [Overview](#overview)
- [How to use this guide](#how-to-use-this-guide)
  - [Setup: the practice folder](#setup-the-practice-folder)
  - [Setup: asking the analysis](#setup-asking-the-analysis)
- [Lesson 0: How to read a command](#lesson-0-how-to-read-a-command)
  - [The idea](#the-idea)
  - [Try it](#try-it)
  - [What the analysis sees](#what-the-analysis-sees)
  - [Check yourself](#check-yourself)
- [Lesson 1: Where you are](#lesson-1-where-you-are)
  - [The idea](#the-idea-1)
  - [Try it](#try-it-1)
  - [What the analysis sees](#what-the-analysis-sees-1)
  - [Check yourself](#check-yourself-1)
- [Lesson 2: Looking without touching](#lesson-2-looking-without-touching)
  - [The idea](#the-idea-2)
  - [Try it](#try-it-2)
  - [What the analysis sees](#what-the-analysis-sees-2)
  - [Check yourself](#check-yourself-2)
- [Lesson 3: Changing things](#lesson-3-changing-things)
  - [The idea](#the-idea-3)
  - [Try it](#try-it-3)
  - [What the analysis sees](#what-the-analysis-sees-3)
  - [Check yourself](#check-yourself-3)
- [Lesson 4: How bash reads your line](#lesson-4-how-bash-reads-your-line)
  - [The idea](#the-idea-4)
  - [Try it](#try-it-4)
  - [What the analysis sees](#what-the-analysis-sees-4)
  - [Check yourself](#check-yourself-4)
- [Lesson 5: Redirection and pipes](#lesson-5-redirection-and-pipes)
  - [The idea](#the-idea-5)
  - [Try it](#try-it-5)
  - [What the analysis sees](#what-the-analysis-sees-5)
  - [Check yourself](#check-yourself-5)
- [Lesson 6: Chaining commands](#lesson-6-chaining-commands)
  - [The idea](#the-idea-6)
  - [Try it](#try-it-6)
  - [What the analysis sees](#what-the-analysis-sees-6)
  - [Check yourself](#check-yourself-6)
- [Lesson 7: Text tools](#lesson-7-text-tools)
  - [The idea](#the-idea-7)
  - [Try it](#try-it-7)
  - [What the analysis sees](#what-the-analysis-sees-7)
  - [Check yourself](#check-yourself-7)
- [Lesson 8: The system](#lesson-8-the-system)
  - [The idea](#the-idea-8)
  - [Try it](#try-it-8)
  - [What the analysis sees](#what-the-analysis-sees-8)
  - [Check yourself](#check-yourself-8)
- [Lesson 9: Downloads and archives](#lesson-9-downloads-and-archives)
  - [The idea](#the-idea-9)
  - [Try it](#try-it-9)
  - [What the analysis sees](#what-the-analysis-sees-9)
  - [Check yourself](#check-yourself-9)
- [Lesson 10: Scripts](#lesson-10-scripts)
  - [The idea](#the-idea-10)
  - [Try it](#try-it-10)
  - [What the analysis sees](#what-the-analysis-sees-10)
  - [Check yourself](#check-yourself-10)
- [Cheat sheet](#cheat-sheet)
- [Where to go next](#where-to-go-next)

## How to use this guide

- **Do, don't read.** Type every command in **Try it** yourself. Reading about the shell builds about as much skill as reading about swimming.
- **Stay in the practice folder.** Every exercise runs inside `~/shell-practice`, a folder made for breaking. Nothing outside it is touched.
- **Ask the analysis.** Each lesson ends by running the real algorithm on what you just did (setup below). When your prediction and its answer differ, one of you is wrong, and finding out which is the lesson.
- **Priority if time is short:** lessons 0 to 6, then the cheat sheet. Lessons 7 to 10 round you out.

### Setup: the practice folder

```bash
mkdir -p ~/shell-practice
cd ~/shell-practice
```

To start over at any time (this deletes only the practice folder):

```bash
cd ~
rm -rf ~/shell-practice
mkdir ~/shell-practice
cd ~/shell-practice
```

### Setup: asking the analysis

`effexplain` runs the algorithm on a command **without running the command**. It looks at the folder you are in, prints what the command would change, the verdict, and what would be captured for undo. It never changes anything. Build it once, from the repository's `prototype/` folder (Go must be installed):

```bash
cd ~/projects/thesis/prototype          # wherever your copy of the repo is
go build -o bin/effexplain ./cmd/effexplain
alias explain="$PWD/bin/effexplain -dir ."
cd ~/shell-practice
```

Then ask about any command by putting it in single quotes:

```bash
explain 'rm notes.txt'
```

The three verdicts you will see:

| Verdict | Meaning | What SynapseOS does (default policy) |
|---|---|---|
| `recoverable` | Only creates and moves. Undo removes or moves back. | Runs |
| `recoverable-with-capture` | Overwrites, removes, or permission changes, but it can save a copy first | Asks, then saves, runs, and can undo |
| `unrecoverable` | Cannot be undone, **or the analysis is not sure** | Asks, and warns there is no undo |

Remember that last row. "Not sure" counts as unrecoverable. That rule, called **fail closed**, is the most important property of the algorithm, and it shows up in every lesson.

## Lesson 0: How to read a command

### The idea

Every command line is a **program name** followed by **arguments**, separated by spaces.

```
rm   -r   old   photos
│    │    └─────┴── operands: what to act on
│    └── option (flag): how to act
└── program
```

- **Options** start with `-` (short, `-r`) or `--` (long, `--recursive`). Short ones combine: `-rf` is `-r -f`.
- **Operands** are the things acted on: files, folders, URLs.
- A program finishes with an **exit code**: `0` means success, anything else means failure. `$?` holds the last one.
- Some commands are not programs on disk but **builtins**, part of bash itself: `cd`, `echo`, `export`. `type` tells you which.

Finding help, in order of usefulness:

| Command | Gives |
|---|---|
| `COMMAND --help` | A quick summary of options |
| `man COMMAND` | The full manual. Arrows to scroll, `/word` to search, `q` to quit |
| `type COMMAND` | Whether it is a builtin, an alias, or a program, and where |

### Try it

```bash
ls --help | head -20
man rm                  # press q to quit
type cd
type ls
which rm
ls /nope
echo $?                 # 2: ls failed
ls
echo $?                 # 0: ls succeeded
```

### What the analysis sees

The analysis looks at the program name first, then the options, then the operands, just as you just did. Where a program lives matters: `/usr/bin/rm` is the real `rm`, but `./rm` or `~/bin/rm` could be anything someone put there, so the analysis treats any program run by a path outside the system folders as unknown.

```
$ explain './cleanup.sh'
class:      unrecoverable
  issue    opaque: runs ./cleanup.sh by path, which is not modelled
```

**Opaque** means "I cannot see inside this". A script's contents are not part of the command line, so the analysis does not guess. It asks.

### Check yourself

1. In `cp -r src backup`, which part is the program, the option, and the operands?
   *Program `cp`; option `-r` (recursive); operands `src` and `backup`.*
2. Why is `./cleanup.sh` unrecoverable even if the script only prints "hello"?
   *The analysis can only see the command line, not the file's contents. It cannot prove the script is harmless, and unknown means ask.*

## Lesson 1: Where you are

### The idea

The filesystem is one tree that starts at `/` (the **root**). Everything, including other disks, hangs off it.

| Path | What it is |
|---|---|
| `/` | The root of everything |
| `/home/you` or `~` | Your home folder |
| `/etc` | System settings |
| `/usr/bin` | Installed programs |
| `/tmp` | Temporary files, often cleared on reboot |
| `/dev` | Devices (disks, terminals) shown as files |

- You are always **in** some folder: the **working directory**. `pwd` prints it, `cd` changes it.
- An **absolute path** starts with `/` and means the same thing from anywhere: `/home/you/notes.txt`.
- A **relative path** is from where you are: `notes.txt`, `../other`. Its meaning **depends on the working directory**.
- `.` is "this folder", `..` is "the parent", `~` is your home, `cd -` goes back to the previous folder.
- Names starting with `.` are **hidden** (`ls -a` shows them).
- `ls -l` shows **permissions**: `-rw-r--r--` is type (`-` file, `d` folder, `l` link), then read/write/execute for the owner, the group, and everyone else.
- A **symbolic link** is a pointer to another path (`ln -s`). Deleting the link does not delete the target.

### Try it

```bash
pwd
mkdir -p a/b/c
cd a/b/c
pwd
cd ../..
pwd
cd -                    # back to a/b/c
cd ~/shell-practice
echo hi > .secret
ls
ls -a
ls -l
ln -s a/b shortcut
ls -l shortcut
```

### What the analysis sees

Relative paths are why the working directory matters so much. `rm notes.txt` means a different file in every folder. The analysis tracks `cd` along the line, but some forms make the folder impossible to know before running:

```
$ explain 'cd - && rm notes.txt'
class:      unrecoverable
  issue    unresolved: working directory is unknown
```

`cd -` goes to "wherever you were before", which depends on the history of the terminal, not on this line. So the analysis cannot know which `notes.txt` would be deleted, and it asks. Before a fix during testing, this case silently said "no effect": an example of why fail closed has to be checked, not assumed.

### Check yourself

1. You are in `/home/you/docs`. What does `../pics/a.png` refer to?
   *`/home/you/pics/a.png`.*
2. Why can't the analysis decide `cd - && rm notes.txt`?
   *`cd -` depends on the previous folder, which the command line does not contain, so the target of `rm` is unknown.*

## Lesson 2: Looking without touching

### The idea

Most commands only **read**. They are always safe, and knowing them means you never need to open a file in an editor just to look.

| Command | Shows |
|---|---|
| `cat FILE` | The whole file |
| `less FILE` | A file one screen at a time (`q` quits, `/` searches) |
| `head -n 5 FILE`, `tail -n 5 FILE` | The first or last lines |
| `tail -f FILE` | New lines as they are added (Ctrl+C stops) |
| `wc -l FILE` | How many lines |
| `stat FILE` | Size, permissions, owner, and times |
| `file FILE` | What kind of file it really is |
| `du -sh DIR`, `df -h` | How big a folder is; how full the disks are |
| `find DIR -name 'PATTERN'` | Files matching a name, searched through every subfolder |
| `tree` | A folder drawn as a tree (if installed) |

### Try it

```bash
seq 1 100 > numbers.txt
head -n 3 numbers.txt
tail -n 3 numbers.txt
wc -l numbers.txt
stat numbers.txt
file numbers.txt
du -sh .
find . -name '*.txt'
find . -type d
```

### What the analysis sees

The analysis knows 172 commands that are read-only in every form, and others that are read-only in some forms. Read-only commands produce no effects, so they are simply `recoverable`:

```
$ explain 'cat notes.txt | grep hi'
class:      recoverable
```

`find` is read-only **unless** it is given an action such as `-delete` or `-exec`. Lesson 6 shows how the analysis handles those.

### Check yourself

1. Which is safer for looking at a 2 GB log, `cat` or `less`?
   *`less`: it shows one screen at a time instead of flooding the terminal.*
2. Is `find . -name '*.log'` ever dangerous?
   *Not by itself. Only its actions (`-delete`, `-exec`, `-ok`) change anything.*

## Lesson 3: Changing things

### The idea

This is the lesson where data gets lost. The key fact: **most commands overwrite silently**. There is no "are you sure?" and no recycle bin.

| Command | What it does | What it can destroy |
|---|---|---|
| `touch F` | Creates `F` if missing, else just updates its time | Nothing |
| `mkdir D` | Creates a folder (`-p` also creates parents) | Nothing |
| `cp A B` | Copies `A` to `B` | **`B`'s old contents**, if `B` existed |
| `mv A B` | Moves or renames `A` to `B` | **`B`'s old contents**, if `B` existed |
| `rm F` | Deletes `F` | **`F`**. Gone, no trash |
| `rm -r D` | Deletes a folder and everything inside | **Everything under `D`** |
| `rmdir D` | Deletes a folder only if it is empty | Nothing that holds data |
| `ln -s T L` | Makes `L` point to `T` | With `-f`, whatever was at `L` |
| `chmod`, `chown` | Change permissions or owner | The old permissions or owner |

Useful safety options: `cp -i` and `mv -i` ask before overwriting, `mv -n` never overwrites, and `cp -b` keeps a backup. Nobody remembers to type them, which is part of the gap SynapseOS fills.

The distinction the algorithm is built on:

- A **move** onto a **new** name loses nothing: the same bytes exist under another name, and undo is just moving them back.
- A **move or copy** onto an **existing** name destroys the old file at that name.
- A **delete** destroys the file, unless something saved it first.

### Try it

```bash
echo "version 1" > a.txt
echo "precious" > b.txt
cp a.txt b.txt
cat b.txt               # "precious" is gone, silently
echo "precious" > b.txt
mv -n a.txt b.txt       # -n: refuses to overwrite
cat b.txt               # still "precious"
mkdir old
rm old                  # fails: rm will not remove a folder without -r
rm -r old
rm a.txt
ls
```

### What the analysis sees

The same command gets a different verdict depending on **what already exists**. This is the core idea: the analysis looks at the real folder, not just the words.

```
$ explain 'mv notes.txt new.txt'          # new.txt does not exist
class:      recoverable
  create   ./new.txt
  remove   ./notes.txt -> ./new.txt

$ explain 'mv notes.txt b.txt'            # b.txt exists
class:      recoverable-with-capture
  write    ./b.txt
  remove   ./notes.txt -> ./b.txt
plan:       what is captured before it runs
  content  ./b.txt
```

The arrow `->` means "these bytes survive over there", so undo moves them back. In the second case, `b.txt`'s old contents would be lost, so the plan copies them first.

```
$ explain 'rm notes.txt'
class:      recoverable-with-capture
  remove   ./notes.txt
plan:       what is captured before it runs
  trash    ./notes.txt
```

`trash` is a **hardlink**: a second name for the same data on disk, made before the delete. It costs nothing to make, whatever the file's size, and when `rm` removes the original name, the data survives under the second one.

```
$ explain 'rm old'                        # a folder, without -r
class:      recoverable
```

No effects at all. `rm` without `-r` refuses folders, so the analysis knows this command will fail and change nothing.

```
$ explain 'chmod 600 notes.txt'
class:      recoverable-with-capture
  metadata ./notes.txt
plan:       what is captured before it runs
  metadata ./notes.txt
```

### Check yourself

1. Why is `mv a.txt b.txt` sometimes `recoverable` and sometimes `recoverable-with-capture`?
   *If `b.txt` does not exist, nothing is lost; if it does, its old contents are overwritten and must be saved first.*
2. Why does capturing a deleted file cost nothing, even for a 10 GB video?
   *The capture is a hardlink, a second name for the same data, not a copy.*
3. What does `rm` do to a folder without `-r`?
   *Refuses with "Is a directory" and changes nothing.*

## Lesson 4: How bash reads your line

### The idea

**This is the most important lesson.** You type text; bash rewrites it before any program sees it. The program never sees `*.txt` or `$HOME`. It sees whatever bash turned those into.

Bash rewrites a line in this order:

| Step | Example you type | What the program receives |
|---|---|---|
| 1. Braces | `cp f.txt{,.bak}` | `cp f.txt f.txt.bak` |
| 2. Tilde | `ls ~/docs` | `ls /home/you/docs` |
| 3. Variables | `rm "$DIR/old"` with `DIR=tmp` | `rm tmp/old` |
| 4. Arithmetic | `echo $((2+3))` | `echo 5` |
| 5. Command substitution | `rm $(ls *.log)` | `rm` with whatever `ls` printed |
| 6. Word splitting | unquoted `$X` with `X="a b"` | **two** arguments, `a` and `b` |
| 7. Globs | `rm *.txt` | `rm a.txt b.txt notes.txt` |
| 8. Quote removal | `"hello"` | `hello` |

**Quoting** controls which steps happen:

| You write | Variables and `$( )` | Word splitting and globs |
|---|---|---|
| `'single quotes'` | No: taken literally | No |
| `"double quotes"` | Yes | No |
| no quotes | Yes | Yes |
| `\*` (backslash) | That one character is literal | |

Three traps that cause real data loss:

1. **Unquoted variables split.** With `FILE="my notes.txt"`, `rm $FILE` tries to delete `my` and `notes.txt`. Always write `"$FILE"`.
2. **An empty variable vanishes.** If `DIR` is unset, `rm -rf "$DIR/"*` becomes `rm -rf /*`, which tries to delete everything. This is a famous real-world disaster.
3. **A glob that matches nothing stays as text.** If no `.log` files exist, `rm *.log` receives the literal `*.log` and fails with "No such file".

### Try it

```bash
touch a.txt b.txt "my notes.txt"
echo *.txt              # what the glob becomes
echo '*.txt'            # quoted: literal
NAME="my notes.txt"
ls $NAME                # two arguments: fails twice
ls "$NAME"              # one argument: works
echo "home is $HOME"
echo 'home is $HOME'
echo f.txt{,.bak}
echo "there are $(ls | wc -l) files here"
echo *.log              # no match: stays as *.log
echo "[$UNSET_THING]"   # unset variable: empty
```

### What the analysis sees

The analysis expands the line **exactly as bash would**, against the real folder, using a real bash parser and its expansion rules. So it sees the actual files a glob will hit:

```
$ explain 'rm *.txt'
class:      recoverable-with-capture
  remove   ./b.txt
  remove   ./notes.txt
```

Quoting changes the meaning, and the analysis follows it:

```
$ explain 'rm "*.txt"'                    # a file literally named *.txt
class:      recoverable                   # none exists, so rm changes nothing
```

Variables set on the line are tracked:

```
$ explain 'X=old; rm -r "$X"'
class:      recoverable-with-capture
  remove   ./old
```

But an **unset** variable is not treated as empty. That is a deliberate difference from bash, and it is what prevents trap 2:

```
$ explain 'rm "$X/notes.txt"'
class:      unrecoverable
  issue    unresolved: arguments could not be determined: X: unbound variable
```

Bash would happily run `rm /notes.txt`. The analysis refuses to guess, and asks.

Command substitution `$( )` is resolved by **actually running the inner part**, but only after the analysis proves that inner part changes nothing (here, `ls` is read-only):

```
$ explain 'rm $(ls *.txt)'
class:      recoverable-with-capture
  remove   ./b.txt
  remove   ./notes.txt
```

That rule, never run anything the analysis has not proven read-only, is why the analysis is safe to run before the user says yes. The read-only commands also run inside a sandbox (bubblewrap) where the whole disk is read-only, as a second layer of protection.

### Check yourself

1. What does the program receive from `rm *.txt` in a folder with `a.txt` and `b.txt`?
   *`rm a.txt b.txt`. The program never sees the `*`.*
2. Why does the analysis treat an unset variable as an error when bash treats it as empty?
   *Treating it as empty can turn `"$X/notes.txt"` into `/notes.txt`, a different file. Not knowing must mean asking.*
3. The analysis runs `ls` inside `rm $(ls)`. Why is running anything safe?
   *It runs only commands it has first proven read-only, inside a read-only sandbox.*
4. What is the difference between `'$HOME'` and `"$HOME"`?
   *Single quotes keep it literal; double quotes expand it to your home folder.*

## Lesson 5: Redirection and pipes

### The idea

Every program has three streams: **input** (0, stdin), **output** (1, stdout), and **errors** (2, stderr). By default, input comes from the keyboard and both outputs go to the screen. Redirection sends them elsewhere.

| You write | Meaning | Danger |
|---|---|---|
| `cmd > file` | Output to `file`, **emptying it first** | **Destroys `file`'s contents** |
| `cmd >> file` | Output added to the end of `file` | Changes it, but keeps the old content |
| `cmd 2> file` | Errors to `file` | Same as `>` |
| `cmd &> file` | Both outputs to `file` | Same as `>` |
| `cmd 2>&1` | Errors go wherever output goes | None |
| `cmd < file` | Input from `file` | None (only reads) |
| `cmd > /dev/null` | Throw output away | None |
| `a \| b` | `a`'s output becomes `b`'s input | Depends on `a` and `b` |
| `cmd \| tee file` | Show output **and** write it to `file` | Same as `>` on `file` |

The trap: `>` empties the file **before** the command even starts. So `sort notes.txt > notes.txt` does not sort the file; it empties it, then sorts nothing.

### Try it

```bash
echo "line 1" > log.txt
echo "line 2" >> log.txt
cat log.txt             # both lines
echo "oops" > log.txt
cat log.txt             # only "oops": line 1 and 2 gone
ls /nope 2> errors.txt
cat errors.txt
seq 1 5 > n.txt
sort -r n.txt > n.txt
cat n.txt               # empty! > emptied it before sort read it
ls | wc -l
ls -l | grep txt | wc -l
echo hello | tee copy.txt
```

### What the analysis sees

Redirection is a hidden way to change files, since the program itself (`echo`, `sort`) only reads. The analysis treats every `>` target as a write or a create:

```
$ explain 'echo hello > notes.txt'        # notes.txt exists
class:      recoverable-with-capture
  write    ./notes.txt
plan:       what is captured before it runs
  content  ./notes.txt

$ explain 'cat notes.txt | grep hi > out.txt'   # out.txt does not exist
class:      recoverable
  create   ./out.txt

$ explain 'echo hi > /dev/null'
class:      recoverable
```

A write needs a **content** copy (not a free hardlink), because the bytes themselves change: a second name would point at the changed data. `>>` is treated the same as `>` because the file still changes, so saving it first is the safe choice.

### Check yourself

1. Why does `sort notes.txt > notes.txt` lose the file?
   *`>` empties `notes.txt` before `sort` starts, so `sort` reads an empty file.*
2. Why is `echo hi > notes.txt` a write, even though `echo` never touches files?
   *The shell, not `echo`, opens and empties the file. The analysis reads the whole line, redirections included.*
3. Why does a write capture a copy instead of a free hardlink?
   *A hardlink shares the same data, which is exactly what the write changes.*

## Lesson 6: Chaining commands

### The idea

| You write | Runs `b`… |
|---|---|
| `a ; b` | always, after `a` |
| `a && b` | only if `a` succeeded |
| `a \|\| b` | only if `a` failed |
| `( a; b )` | runs both in a separate copy of the shell (a `cd` inside does not leak out) |
| `for f in *.txt; do …; done` | once for each file |

Two commands run other commands, and both are common in generated commands:

- **`find … -exec CMD {} \;`** runs `CMD` on each match (`{}` is replaced by the file). `find … -delete` deletes each match.
- **`xargs CMD`** reads names from its input and runs `CMD` on them: `ls | xargs rm`. Names with spaces break it unless you use `find -print0 | xargs -0`.

### Try it

```bash
touch x.txt y.txt
mkdir out && cp x.txt out/ && ls out
ls /nope && echo "not printed"
ls /nope || echo "printed, because ls failed"
for f in *.txt; do echo "found $f"; done
for f in x.txt y.txt; do cp "$f" "$f.bak"; done
ls
find . -name '*.bak'
find . -name '*.bak' -exec echo "would delete" {} \;
find . -name '*.bak' -delete
ls
```

### What the analysis sees

This is where the algorithm goes beyond pattern lists. A pattern list sees `find … -delete` and can only say "dangerous, ask". The analysis works out **exactly which files** would go. It rewrites the `find` so that instead of deleting, it prints what it would delete, proves the rewrite contains no action, and runs that:

```
$ explain 'find . -name "*.txt" -delete'
class:      recoverable-with-capture
  remove   ./b.txt
  remove   ./notes.txt
plan:       what is captured before it runs
  trash    ./b.txt
  trash    ./notes.txt
```

`xargs` is handled the same way: the part feeding it (`ls`) is proven read-only, run, and split the way `xargs` would split it. Note that `old`, a folder, is not removed, because `rm` without `-r` skips folders:

```
$ explain 'ls | xargs rm'
class:      recoverable-with-capture
  remove   ./b.txt
  remove   ./notes.txt
```

Loops are unrolled, one pass per item:

```
$ explain 'for f in *.txt; do mv "$f" "$f.bak"; done'
class:      recoverable
  create   ./b.txt.bak
  remove   ./b.txt -> ./b.txt.bak
  create   ./notes.txt.bak
  remove   ./notes.txt -> ./notes.txt.bak
```

The analysis also remembers what earlier parts of the line do. A folder the line itself creates and then deletes needs no saving, because nothing that existed before is lost:

```
$ explain 'mkdir out && cp notes.txt out/ && rm -rf out'
class:      recoverable
  create   ./out
  create   ./out/notes.txt
```

For `a && b` and `a || b`, the analysis counts the effects of **both** sides, because it cannot know in advance whether `a` will succeed. Over-counting costs one extra question; under-counting could cost data.

### Check yourself

1. In `mkdir out && cd out && rm *`, when does `rm` run?
   *Only if `mkdir` and `cd` both succeeded.*
2. How can the analysis know what `find -delete` will delete without deleting anything?
   *It rewrites the command to print instead of delete, proves the rewrite changes nothing, and runs it.*
3. Why does the analysis count both sides of `a || b`?
   *It cannot know which side will run, so it plans for both.*

## Lesson 7: Text tools

### The idea

These read text and print transformed text. They are read-only, **except** for the options that edit files in place.

| Command | Does | Example |
|---|---|---|
| `grep PATTERN` | Lines that match | `grep -rin "todo" .` (recursive, case-insensitive, line numbers) |
| `grep -v` | Lines that do **not** match | `grep -v '^#' config` |
| `sort` | Sorts lines | `sort -n` numeric, `-r` reverse |
| `uniq` | Removes **adjacent** duplicates | `sort \| uniq -c` counts each line |
| `cut` | Picks columns | `cut -d: -f1 /etc/passwd` |
| `tr` | Replaces characters | `tr a-z A-Z` |
| `sed 's/a/b/'` | Replaces text in each line, prints the result | `sed 's/cat/dog/g' file` |
| `sed -i 's/a/b/'` | **Edits the file in place** | Changes the file |
| `awk '{print $2}'` | Picks fields; a small language | `ls -l \| awk '{print $5, $9}'` |

### Try it

```bash
printf "apple\nbanana\napple\ncherry\n" > fruit.txt
grep an fruit.txt
grep -v apple fruit.txt
sort fruit.txt | uniq -c
cut -c1-3 fruit.txt
tr a-z A-Z < fruit.txt
sed 's/apple/mango/' fruit.txt
cat fruit.txt           # unchanged: sed only printed
sed -i 's/apple/mango/' fruit.txt
cat fruit.txt           # changed in place
```

### What the analysis sees

The same program can be read-only or mutating depending on one option:

```
$ explain 'sed -i s/hi/bye/ notes.txt'
class:      recoverable-with-capture
  write    ./notes.txt
plan:       what is captured before it runs
  content  ./notes.txt
```

Without `-i`, the same `sed` is read-only. A `sed` or `awk` script that can write files or run commands from inside the script (`sed`'s `w` and `e`, `awk`'s `system()`) is opaque, so it asks.

### Check yourself

1. Why does `uniq` usually need `sort` before it?
   *`uniq` only removes duplicates that are next to each other.*
2. When is `sed` dangerous?
   *With `-i` (edits the file), or with a script that writes or runs commands.*

## Lesson 8: The system

### The idea

| Command | Does |
|---|---|
| `sudo CMD` | Runs `CMD` as the administrator (root). Asks for your password |
| `whoami`, `id` | Who you are and which groups you are in |
| `ps aux`, `top` | Running programs and their process IDs (PIDs) |
| `kill PID` | Asks a program to stop; `kill -9` forces it |
| `systemctl status NAME` | Whether a background service is running |
| `systemctl start`, `stop`, `enable`, `disable` | Controls a service now, or at boot |
| `apt list --installed` | Installed packages |
| `apt-get -s install NAME` | **Simulates** an install: prints what would happen, changes nothing |
| `apt install`, `apt remove` | Installs or removes packages (needs `sudo`) |

### Try it

```bash
whoami
id
ps aux | head -5
systemctl status cron   # q to quit; read-only
apt list --installed 2>/dev/null | wc -l
apt-get -s install cowsay | tail -3
sleep 300 &             # a harmless program in the background
ps aux | grep "sleep 300"
kill %1                 # stop it
```

### What the analysis sees

`sudo` is a **wrapper**: the analysis strips it and analyses the command inside. But it marks that command **privileged**, and refuses to resolve its targets as you, because root can see files you cannot. Looking as yourself might find nothing and wrongly report "no effect", so it asks.

Packages and services are not files, so they are recorded with **inverse commands**: installing a package records how to purge it; stopping a running service records how to start it again. The analysis finds out what would change with the tools' own simulation modes (`apt-get -s`, `systemctl is-active`).

Some things cannot be undone at all, and the analysis says so:

```
$ explain 'kill 1234'
class:      unrecoverable
  issue    unrecoverable: ends processes; process state cannot be captured
```

A stopped program's memory is gone. There is no file to save first.

### Check yourself

1. Why doesn't the analysis resolve targets under `sudo`?
   *Root can see files you cannot. Looking as you could miss them and wrongly report nothing.*
2. How is installing a package "undone"?
   *By recording and later running the inverse: purging the package.*
3. Why is `kill` unrecoverable?
   *A running program's state lives in memory, which cannot be saved and restored.*

## Lesson 9: Downloads and archives

### The idea

| Command | Does | Watch out |
|---|---|---|
| `wget URL` | Downloads to a file named after the URL | If the name exists, saves as `NAME.1` instead |
| `wget -O F URL` | Downloads to `F` | **Overwrites `F`** |
| `curl URL` | Prints the download to the screen | Writes nothing unless `-o` or `-O` |
| `curl -X POST …` | Sends data to a server | Cannot be undone: it happened on someone else's machine |
| `tar -czf out.tar.gz DIR` | Packs `DIR` into a compressed archive | Overwrites `out.tar.gz` |
| `tar -tf out.tar.gz` | Lists what is inside | Read-only |
| `tar -xzf out.tar.gz` | Unpacks into the current folder | **Overwrites files with the same names** |
| `unzip f.zip` | Unpacks a zip | Asks before overwriting, unless `-o` |
| `gzip F` | Compresses `F` into `F.gz` and **deletes `F`** | `-k` keeps the original |
| `rsync -a SRC/ DST/` | Copies changes only | `--delete` removes files in `DST` that are not in `SRC` |
| `git status`, `git diff` | Shows what changed | Read-only |
| `git reset --hard` | Throws away uncommitted changes | **Uncommitted work is gone** |

A subtle one: `rsync -a src/ dst/` copies the **contents** of `src`, while `rsync -a src dst/` copies `src` itself into `dst/src`. One slash changes the result.

### Try it

```bash
mkdir proj
echo "v1" > proj/file.txt
tar -czf proj.tar.gz proj
tar -tf proj.tar.gz
echo "v2" > proj/file.txt
tar -xzf proj.tar.gz    # silently puts v1 back over v2
cat proj/file.txt
cp proj/file.txt big.txt
gzip big.txt
ls                      # big.txt is gone, big.txt.gz is there
gunzip big.txt.gz
```

### What the analysis sees

Each of these has its own rule, and several are resolved by the tool's own **dry run** (a mode that only reports what it would do): `tar -tf` lists what `tar -x` would write, `unzip -Z1` does the same for zips, and `rsync --dry-run` lists what `rsync` would change. The analysis knows `wget`'s naming:

```
$ explain 'wget https://example.com/notes.txt'   # notes.txt already exists
class:      recoverable
  create   ./notes.txt.1
```

`wget` never replaces a file by default, so it creates `notes.txt.1`. An earlier version of the analysis got this wrong and would have had undo delete your original `notes.txt`. The tests caught it, and it is one of the four bugs the paper reports.

`git reset --hard` captures the current commit and every uncommitted file it would throw away. `curl` sending data, `git push --force`, and history rewrites are unrecoverable, because they change things outside this machine or erase history.

### Check yourself

1. Why does `wget` of an existing file name not overwrite it?
   *By default `wget` saves as `NAME.1` instead.*
2. Why is `tar -xzf` potentially destructive?
   *It overwrites any existing file with the same name as a member of the archive.*
3. Why is a `curl` POST unrecoverable?
   *Its effect happens on another machine, which nothing here can roll back.*

## Lesson 10: Scripts

### The idea

A script is a file of commands. Its first line, the **shebang** `#!/bin/bash`, says which program runs it. `chmod +x` makes it runnable.

```bash
#!/bin/bash
set -euo pipefail          # stop on errors, unset variables, and failed pipes

backup_dir="backup-$(date +%F)"
mkdir -p "$backup_dir"

for f in *.txt; do
  if [ -f "$f" ]; then
    cp "$f" "$backup_dir/"
    echo "saved $f"
  fi
done
```

`set -euo pipefail` is the professional default:

| Option | Effect |
|---|---|
| `-e` | Stop at the first command that fails |
| `-u` | Treat an unset variable as an error (the same rule the analysis uses) |
| `-o pipefail` | A pipeline fails if any part of it fails, not only the last |

### Try it

Save the script above as `backup.sh`, then:

```bash
chmod +x backup.sh
./backup.sh
ls backup-*
bash -n backup.sh       # checks the syntax without running it
```

### What the analysis sees

A script file is opaque (Lesson 0): its contents are not part of the command line. But `bash -c '…'` with the commands **in the line** is analysed like any other line:

```
$ explain 'python3 -c "print(1)"'
class:      unrecoverable
  issue    opaque: python3 runs arbitrary code; its effects are not modelled
```

Python, Perl, Node, and similar interpreters can do anything, and the analysis does not try to understand other languages, so they always ask. That is a stated limit of the thesis, not a bug.

### Check yourself

1. What does `set -u` have in common with the analysis?
   *Both treat an unset variable as an error instead of as empty.*
2. Why does running a script always ask, while the same commands typed directly might not?
   *The analysis only sees the command line, and a script's contents are not in it.*

## Cheat sheet

What each common command can destroy, and how the analysis treats it. The analysis's verdict always depends on what exists, so this is the usual case.

| Command | Can destroy | Usual verdict | Saved how |
|---|---|---|---|
| `ls`, `cat`, `less`, `grep`, `find` (no action), `head`, `wc`, `stat` | Nothing | recoverable | — |
| `touch`, `mkdir` | Nothing | recoverable | Undo removes what was created |
| `mv A NEW` | Nothing | recoverable | Undo moves it back |
| `mv A EXISTING`, `cp A EXISTING` | The old `EXISTING` | with capture | Content copy |
| `rm F`, `rm -r D`, `find -delete`, `xargs rm` | The files | with capture | Hardlink (free) |
| `rm -r /` or a top-level folder | Everything | unrecoverable | — |
| `> FILE`, `sed -i`, `truncate`, `tee FILE` | The file's contents | with capture | Content copy |
| `>> FILE` | (Changes it) | with capture | Content copy |
| `chmod`, `chown` | Old permissions or owner | with capture | Metadata record |
| `tar -x`, `unzip`, `rsync` | Same-named files | resolved by dry run | Content copy or hardlink |
| `wget` | Nothing by default | recoverable | Undo removes the download |
| `gzip F` | `F` (replaced by `F.gz`) | with capture | Hardlink |
| `git reset --hard`, `git checkout -- F` | Uncommitted changes | with capture | Commit id and content copies |
| `apt install`, `systemctl stop` | (System state) | with capture | Inverse command |
| `kill`, `shred`, `mkfs`, `mount`, `reboot`, `useradd` | Running state, data, disks | unrecoverable | — |
| `git push --force`, `curl -X POST` | Remote state | unrecoverable | — |
| `python`, `perl`, `node`, `./script.sh` | Anything | unrecoverable (opaque) | — |
| Unset variable, `cd -`, unknown program | Unknown | unrecoverable (unresolved or opaque) | — |

The full list of every rule, generated from the code, is in [recoverability-analysis.md, section 8](recoverability-analysis.md#8-the-rule-map).

## Where to go next

- **[recoverability-analysis.md](recoverability-analysis.md):** the algorithm in full, its guarantees, and its tests. After this guide, it should read clearly.
- **`man bash`:** the definitive reference. Long, but every answer is in it.
- **[The Bash Guide (Greg's Wiki)](https://mywiki.wooledge.org/BashGuide)** and **[Bash Pitfalls](https://mywiki.wooledge.org/BashPitfalls)**: the community references most experienced shell users point to. The pitfalls list is a catalogue of how commands go wrong, which is the problem SynapseOS exists to catch.
- **[ShellCheck](https://www.shellcheck.net):** paste in any command or script and it explains what is risky about it.
- **Practice daily:** the fastest way to fluency is doing ordinary tasks (renaming, finding, cleaning up) in the terminal instead of the file manager.
