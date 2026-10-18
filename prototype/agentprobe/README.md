## Overview

`tasks.jsonl` is the task suite of the agent command study: 68 tasks, one JSON object per line, each loadable by `probe.LoadTasks`. Every task has a small fixture folder and the same request phrased four ways (technical, interface, plain, novice). The novice phrasing is tagged with the features it exercises. The tasks come in three partitions that are reported separately and never pooled: 22 NL2Bash descriptions behind round 6m (S), 35 everyday requests (E), and 11 scripted multi-turn persona conversations (P). Most novice wording is invented. 20 tasks base their novice wording on a real help-forum title or a Layer 7 plain phrasing, and the `source` field records where each task's wording came from. The study design, the tiers, and the reasons for each partition are in [`docs/agent-command-study.md`](../../docs/agent-command-study.md). This file covers what is in the suite, where it deviates from its sources, what the sandbox cannot run, and how to extend it.

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
| `P` | 11 | Persona conversations. `prompts` is the first message in each tier. `follow_ups` are the later turns, sent unchanged whichever tier opened the conversation. They cover pronouns across turns, several requests in one message, verification questions after an action, panic and undo, and a Windows mental model. |

Categories are slugs of the spec's list: `file-management`, `search`, `archives-backups`, `data-wrangling`, `text-editing`, `packages-services`, `system-info`, `multi-step`.

S's fixtures were inferred from the 3B model's own command (`oracle.Infer`). Some therefore contain odd paths, such as a directory `s/foo_fn/bar_fn` (M014) or a file named `rm` (M041). These fixtures are kept for comparability with round 6m and should not be read as a realistic home folder.

**Where S departs from round 6m.** Where a description named a path no task folder can hold, or the fixture lacked what every tier asks about, the fixture gains the referent and the description is edited only to name it. A run of these tasks is not comparable to round 6m's on the same item.

| Task | Technical prompt change | Fixture change |
|---|---|---|
| M044 | `/some/dir` becomes `./mirror` | adds `mirror/` holding copies of `a.txt`, `notes.txt` and `data.csv` |
| M041 | `'/home/backups'` becomes `'./backups'` | the four root `.tgz` and `.gz` files become `backups/site-2026-03-01.tgz`, `backups/site-2026-06-01.tgz`, `backups/db/db-2026-02-15.gz` and `backups/db/db-2026-09-01.gz`. Their mtimes are equal (`oracle.Entry` has none), so "oldest" can only be read from the names. |
| M134 | `/path/to/target-dir` becomes `./target-dir` | adds the empty folder `target-dir/` |
| M009 | none | `alpha.xml` and `beta.xml` contain `firstWord`, `secondWord` and `thirdWord` |

M101's novice tier says "that folder" where the description says `$DIR`, because no novice writes a shell variable. The other tiers keep `$DIR`.

**Tasks that do nothing on their fixture.** These are kept, because "nothing to do" is itself an answer to score, but a run should not count them as exercising the command:
- M026: no file directly in the folder contains "apple". The word appears only in deeper paths and in the contents of files under `s/`.
- M067, M101 and M159: every file is materialized at run time, so none is older than July 1, 15 days or 6 months. M159 always sums to 0.
- M129: `Headers` is a file at the root, not a folder inside `MyApp.app`.
- M153: no file name contains a digit.

M134 has the opposite problem. Every folder changed "in the last day", so it acts on every folder, including `target-dir` itself.

## Feature vocabulary

The `features` tags describe the novice phrasing, plus the follow-ups for P tasks. The vocabulary is fixed, and `internal/probe/suite_test.go` rejects any other tag:

| Tag | Meaning | Tasks |
|---|---|---|
| `typo` | typos and informal spelling ("pls delet", "r", "u") | 28 |
| `vague-target` | "the old stuff", "those big files" | 28 |
| `underspecified` | no folder, no file type, no criterion | 22 |
| `question-as-action` | a question that asks for an action, or a check after one ("did you delete those?") | 20 |
| `pronoun` | a reference across turns ("delete them", "move it back") | 10 |
| `urgent` | emotional or urgent framing ("i messed up, undo everything") | 9 |
| `multi-intent` | several requests in one message | 5 |
| `taglish` | Filipino-English code-switching ("pakiayos naman yung downloads ko") | 5 |
| `wrong-model` | Windows or macOS vocabulary ("my c drive", "control panel", "winrar") | 5 |

The test also requires every `taglish` task to use a common Filipino function word (`yung`, `ang`, `mga`, `sa`, `paki-` and so on) in its novice text or follow-ups.

`E-temp-files` is vague on purpose. "The temp junk" does not say which files count as temporary, and the technical tier's `*.tmp *~` is one reading of it, not the only correct one.

## Provenance

`source` records where a task's wording came from:

| Source | S | E | P | Total |
|---|---|---|---|---|
| `nl2bash:N` (technical prompt verbatim, but for the M041, M044 and M134 paths above; N is the 0-based line index into `nl2bash_all.nl`) | 22 | | | 22 |
| A forum-question URL (the novice phrasing is modelled on that question's title) | | 10 | 3 | 13 |
| `testing-plan:L7` (the plain phrasing comes from Layer 7, and the novice phrasing is derived from it) | | 7 | | 7 |
| `authored` | | 18 | 8 | 26 |

In S, `source` names the NL2Bash line, and the interface, plain and novice rewrites are authored. Counting by novice wording, then, 20 of 68 novice phrasings are grounded in real or prior text (13 forum titles and 7 Layer 7 phrasings) and 48 are invented. Forum titles are paraphrased in style, not copied. The URLs come from Launchpad Answers, LinuxQuestions, the Ubuntu MATE community, the ubuntu-users list, Ask LibreOffice, and Tom's Guide. Their titles were seen in search listings, and one was fetched and confirmed. All Taglish phrasings are authored.

Fixtures contain only invented content: placeholder names and `@example.com` addresses, with no real personal data. File contents are a line or a few kilobytes of filler. The exception is the three tasks that ask for the biggest files (`E-free-space`, `E-biggest-file`, `P-big-then-delete`): their Downloads files are 5 to 64 KB, so that `du` does not round them to the same 4K block count. Those fixtures hold about 150 KB each. The largest fixture has 33 entries.

## Sandbox limits

Some tasks cannot do in the sandbox what they ask, and a run should report them as such, not as agent failures:
- **Root, packages, network or services:** `E-uninstall-app` and `E-install-vlc` (apt, sudo), and `E-restart-web` (systemd, sudo). The sandbox has no root and no system service manager.
- **Processes:** `E-slow-laptop` runs in a private PID namespace, so it sees only the sandbox's own processes.
- **The whole filesystem:** `S-M057` (change the permissions of every file) and `S-M126` (archive the entire file system) walk everything visible in the sandbox. They are slow and likely to time out.
- **P follow-ups:** a persona's follow-ups are novice-style in every tier. Only turn 0 differs by tier, so a tier comparison on turn 1 or later is not a tier effect.

## Adding a task

1. Append one JSON object on a single line with `id` (unique, with the prefix `S-`, `E-` or `P-`), `partition`, `category`, `fixture` (a list of `oracle.Entry`: `{"p":path,"k":"file"|"dir"|"link","d":data,"m":mode}`), `prompts` (all four tiers), `features`, and `source`. A P task also needs `follow_ups`.
2. Keep fixture paths relative, keep contents short and invented, and keep binary data out (fixture data is a JSON string). An archive must be one whose bytes are all ASCII, like the uncompressed tar in `E-open-tar`.
3. For S tasks, take the description from `nl2bash_all.nl` line N+1 (N is 0-based, matching the corpus's `model:N`), and use the corpus item's `fx` as the fixture.
4. Run `go test ./internal/probe/` from `prototype/`. It loads the suite, checks partitions, categories, feature tags and follow-ups, and materializes every fixture into a temporary folder. It also checks that every path the technical prompt names exists in the fixture. A path the command creates (an output) goes in `notFixturePaths` in `suite_test.go`, with the reason.

The suite is frozen and hashed before the first reported run. After that, add tasks only in a new, separately reported version.
