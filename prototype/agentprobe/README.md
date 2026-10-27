## Overview

`tasks.jsonl` is the task suite of the agent command study: 79 tasks, one JSON object per line, each loadable by `probe.LoadTasks`. Every task has a small fixture folder and the same request phrased four ways (technical, interface, plain, novice). The novice phrasing is tagged with the features it exercises. The tasks come in three partitions that are reported separately and never pooled: 22 NL2Bash descriptions behind round 6m (S), 35 everyday requests (E), and 22 scripted multi-turn persona conversations (P), half of them trial-and-error conversations in which the person judges each result by what they see and corrects course. Most novice wording is invented. 25 tasks base their novice wording on a real help-forum title or a Layer 7 plain phrasing, and the `sources` field records, per tier, where each phrasing came from. Fixtures carry ages, so a task about old files has old files, and the everyday and persona fixtures sit in a realistic home folder. The study design, the tiers, and the reasons for each partition are in [`docs/agent-command-study.md`](../../docs/agent-command-study.md). This file covers what is in the suite, where it deviates from its sources, what the sandbox cannot run, and how to extend it.

## Table of Contents

- [Overview](#overview)
- [Partitions](#partitions)
- [Feature vocabulary](#feature-vocabulary)
- [Provenance](#provenance)
- [Sandbox limits](#sandbox-limits)
- [Adding a task](#adding-a-task)

## Partitions

| Partition | Tasks | What it is |
|---|---|---|
| `S` | 22 | NL2Bash descriptions behind round 6m (`pilot/corpus6m.jsonl`). The technical prompt is the description verbatim, except for the three placeholder paths listed below. The other three tiers rewrite the same intent. The fixture is the corpus item's `fx` (with the additions listed below), so a model's command meets the same starting state as the 3B model's did. 11 of the 19 items labelled C (lossy) are included. The other 8 (M024, M086, M090, M103, M116, M120, M133, M155) describe shell-variable or stdout exercises, not file tasks. There are also 8 R items whose description asks for a destructive change, and 3 read-only ones. |
| `E` | 35 | Everyday requests across every category in the spec. Fixtures include a photo folder with duplicates, a messy Downloads folder, application logs, CSV and JSON files, notes, a small project, and dated backups. Several of them make the destructive path tempting: deduplicating, freeing space, pruning backups, renaming onto an existing name, copying onto an older copy on the "USB", and editing files in place. |
| `P` | 22 | Persona conversations. `prompts` is the first message in each tier. `follow_ups` are the later turns, sent unchanged whichever tier opened the conversation. They cover pronouns across turns, several requests in one message, verification questions after an action, panic and undo, and a Windows mental model. The 11 `trial-and-error` tasks follow how the least technical user works: "it's still there", "no, the other one", "undo that", narrowing an over-broad action, changing the goal midway, repeating a request in other words, and "did you delete everything??". Every follow-up makes sense whatever the agent did on the turn before, so the agent has to check the state before it answers or acts again. |

Every fixture is the agent's home folder and its starting directory (`probe.LayoutHome`), so a prompt's "my downloads" is `~/Downloads`. E and P fixtures also hold a home layer (`HOME_LAYER` in `gen.py`), which therefore sits at `~`: dotfiles, a thumbnail cache, a Trash with one file in it, a Desktop, a deep archive folder, and names with spaces, an apostrophe and an accent. Its content is unique, so it adds no duplicate, PDF or log that a task's own files could be confused with. Every E and P entry has an age (`oracle.Entry.Age`, seconds before the fixture is built): a file with a date in its name is as old as that date was on 2026-10-24 (`REF`), Downloads, cache and log files are 1 to 30 days old, other files 3 to 400, spread by a hash of the path so regeneration is byte-identical, and each folder is as old as its newest content. Ages are relative, so a task means the same thing in every run and every replay.

Categories are slugs of the spec's list: `file-management`, `search`, `archives-backups`, `data-wrangling`, `text-editing`, `packages-services`, `system-info`, `multi-step`.

S's fixtures were inferred from the 3B model's own command (`oracle.Infer`). Some therefore contain odd paths, such as a directory `s/foo_fn/bar_fn` (M014) or a file named `rm` (M041). These fixtures are kept for comparability with round 6m and should not be read as a realistic home folder.

**Where S departs from round 6m.** Where a description named a path no task folder can hold, or the fixture lacked what every tier asks about, the fixture gains the referent and the description is edited only to name it. A run of these tasks is not comparable to round 6m's on the same item.

| Task | Technical prompt change | Fixture change |
|---|---|---|
| M044 | `/some/dir` becomes `./mirror` | adds `mirror/` holding copies of `a.txt`, `notes.txt` and `data.csv` |
| M041 | `'/home/backups'` becomes `'./backups'` | the four root `.tgz` and `.gz` files become `backups/site-2026-03-01.tgz`, `backups/site-2026-06-01.tgz`, `backups/db/db-2026-02-15.gz` and `backups/db/db-2026-09-01.gz`. Their ages agree with the dates in their names, so the oldest by name is also the oldest by modification time. |
| M134 | `/path/to/target-dir` becomes `./target-dir` | adds the empty folder `target-dir/` |
| M009 | none | `alpha.xml` and `beta.xml` contain `firstWord`, `secondWord` and `thirdWord` |
| M026 | none | adds `fruit.txt` and `shopping.txt`, which contain "apple", directly in the folder |
| M129 | none | adds `MyApp.app/Contents/Headers/`, a `Headers` folder inside a framework, and the app binary; the root file `Headers` stays |
| M153 | none | adds `file123.txt`, `photo2.jpg` and `song01.mp3`, and `file.txt`, which `file123.txt` would overwrite once its digits are gone |
| M067, M101, M159 | none | ages on both sides of the line: July 1 (400 to 650 days against 0.5 to 2), 15 days (30 to 200 against 1 to 5), 6 months (210 to 400 against 2 to 90) |
| M134 | none | `build`, `backup`, `tmp` and `target-dir` last changed ten days ago; `src` and `logs` changed today |

M101's novice tier says "that folder" where the description says `$DIR`, because no novice writes a shell variable. The other tiers keep `$DIR`.

**Tasks that used to do nothing on their fixture.** M026, M067, M101, M129, M153 and M159 had nothing to act on, and M134 acted on every folder, the target included, because every file was as new as the run. The rows above give each one something to act on. `TestSuiteAgeTasksSplitTheirFixture` checks with find(1) that each age criterion matches some of its task's fixture and not all of it. M159's total is still small: the files are a few bytes, so a total "in MB" rounds to 0.

## Feature vocabulary

The `features` tags describe the novice phrasing, plus the follow-ups for P tasks. The vocabulary is fixed, and `internal/probe/suite_test.go` rejects any other tag:

| Tag | Meaning | Tasks |
|---|---|---|
| `typo` | typos and informal spelling ("pls delet", "r", "u") | 34 |
| `vague-target` | "the old stuff", "those big files" | 33 |
| `underspecified` | no folder, no file type, no criterion | 24 |
| `question-as-action` | a question that asks for an action, or a check after one ("did you delete those?") | 27 |
| `pronoun` | a reference across turns ("delete them", "move it back") | 21 |
| `urgent` | emotional or urgent framing ("i messed up, undo everything") | 14 |
| `multi-intent` | several requests in one message | 5 |
| `taglish` | Filipino-English code-switching ("pakiayos naman yung downloads ko") | 7 |
| `wrong-model` | Windows or macOS vocabulary ("my c drive", "control panel", "winrar") | 6 |
| `trial-and-error` | follow-ups that react to a result the person judged by eye: "its still there", "no not that one, the other one", "undo that", "actualy nvm just make them smaller" | 11 |

The test also requires every `taglish` task to use a common Filipino function word (`yung`, `ang`, `mga`, `sa`, `paki-` and so on) in its novice text or follow-ups.

`E-temp-files` is vague on purpose. "The temp junk" does not say which files count as temporary, and the technical tier's `*.tmp *~` is one reading of it, not the only correct one.

## Provenance

`sources` records, for each tier, where that phrasing came from. The vocabulary is fixed, and the loader refuses anything else (`probe.CheckSource`):

| Value | Meaning |
|---|---|
| `nl2bash:N` | NL2Bash line N (0-based into `nl2bash_all.nl`), verbatim but for the M041, M044 and M134 paths above |
| `testing-plan:L7` | a Layer 7 plain phrasing, verbatim |
| a URL | a real question's title, verbatim |
| `after:` one of those | wording modelled on that source, not copied from it |
| `authored` | written for this study |

| Novice wording | S | E | P | Total |
|---|---|---|---|---|
| `after:` a forum URL | | 10 | 8 | 18 |
| `after:testing-plan:L7` | | 7 | | 7 |
| `authored` | 22 | 18 | 14 | 54 |

S's technical tier is `nl2bash:N` and its other three tiers are `authored`. An E task from Layer 7 has a `testing-plan:L7` plain tier. So 25 of 79 novice phrasings are grounded in real or prior text and 54 are invented. Forum titles are paraphrased in style, not copied. The URLs come from Launchpad Answers, LinuxQuestions, the Ubuntu MATE community, the ubuntu-users, ubuntu-ph, ubuntu-za and ubuntu-uk lists, Ask LibreOffice, and Tom's Guide. Their titles were seen in search listings, and one was fetched and confirmed. The five sources of the trial-and-error tasks (`U_PHBIN`, `U_COMEBACK`, `U_FLASH`, `U_JPG`, `U_DESKTOP` in `gen.py`) were each fetched on 2026-10-24 and confirmed to ask the question the task models. Taglish wording is authored: `P-trash-ibalik` is modelled on an English-language post to the Philippine Ubuntu list, and its Taglish phrasing was written for this study.

Fixtures contain only invented content: placeholder names and `@example.com` addresses, with no real personal data. File contents are a line or a few kilobytes of filler. The exception is the three tasks that ask for the biggest files (`E-free-space`, `E-biggest-file`, `P-big-then-delete`): their Downloads files are 5 to 64 KB, so that `du` does not round them to the same 4K block count. Those fixtures hold about 150 KB each. The largest fixture, with the home layer and its folders, has 68 entries.

## Sandbox limits

Some tasks cannot do in the sandbox what they ask, and a run should report them as such, not as agent failures:
- **Root, packages, network or services:** `E-uninstall-app` and `E-install-vlc` (apt, sudo), and `E-restart-web` (systemd, sudo). The sandbox has no root and no system service manager.
- **Processes:** `E-slow-laptop` runs in a private PID namespace, so it sees only the sandbox's own processes.
- **The whole filesystem:** `S-M057` (change the permissions of every file) and `S-M126` (archive the entire file system) walk everything visible in the sandbox. They are slow and likely to time out.
- **P follow-ups:** a persona's follow-ups are novice-style in every tier. Only turn 0 differs by tier, so a tier comparison on turn 1 or later is not a tier effect.

## Adding a task

1. Add the task to `gen.py` and regenerate: from `prototype/`, `python3 agentprobe/gen.py pilot/corpus6m.jsonl agentprobe/tasks.jsonl` (S needs the git-ignored `pilot/nl2bash_all.nl`). Never edit `tasks.jsonl` by hand. A task has `id` (unique, with the prefix `S-`, `E-` or `P-`), `partition`, `category`, `fixture` (a list of `oracle.Entry`: `{"p":path,"k":"file"|"dir"|"link","d":data,"m":mode,"a":age_seconds}`), `prompts` (all four tiers), `features`, and `sources` (all four tiers). A P task also needs `follow_ups`. The loader refuses an unknown field.
2. Keep fixture paths relative, keep contents short and invented, and keep binary data out (fixture data is a JSON string). An archive must be one whose bytes are all ASCII, like the uncompressed tar in `E-open-tar`.
3. For S tasks, take the description from `nl2bash_all.nl` line N+1 (N is 0-based, matching the corpus's `model:N`), and use the corpus item's `fx` as the fixture.
4. Run `go test ./internal/probe/` from `prototype/`. It loads the suite, checks partitions, categories, feature tags and follow-ups, and materializes every fixture into a temporary folder. It also checks that every path the technical prompt names exists in the fixture. A path the command creates (an output) goes in `notFixturePaths` in `suite_test.go`, with the reason.

The suite is frozen and hashed before the first reported run. After that, add tasks only in a new, separately reported version.
