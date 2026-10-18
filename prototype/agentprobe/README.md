## Overview

`tasks.jsonl` is the task suite of the agent command study: 68 tasks, one JSON object per line, each loadable by `probe.LoadTasks`. Every task has a small fixture folder and the same request phrased four ways (technical, interface, plain, novice). The novice phrasing is tagged with the features it exercises. The tasks come in three partitions that are reported separately and never pooled: 22 NL2Bash descriptions behind round 6m (S), 35 everyday requests (E), and 11 scripted multi-turn persona conversations (P). Most novice wording is invented. 20 tasks base their novice wording on a real help-forum title or a Layer 7 plain phrasing, and the `source` field records where each task's wording came from. The study design, the tiers, and the reasons for each partition are in [`docs/agent-command-study.md`](../../docs/agent-command-study.md). This file covers only what is in the suite and how to extend it.

## Table of Contents

- [Overview](#overview)
- [Partitions](#partitions)
- [Feature vocabulary](#feature-vocabulary)
- [Provenance](#provenance)
- [Adding a task](#adding-a-task)

## Partitions

| Partition | Tasks | What it is |
|---|---|---|
| `S` | 22 | NL2Bash descriptions behind round 6m (`pilot/corpus6m.jsonl`). The technical prompt is the description verbatim. The other three tiers rewrite the same intent. The fixture is the corpus item's `fx`, so a model's command meets the same starting state as the 3B model's did. 11 of the 19 items labelled C (lossy) are included. The other 8 (M024, M086, M090, M103, M116, M120, M133, M155) describe shell-variable or stdout exercises, not file tasks. There are also 8 R items whose description asks for a destructive change, and 3 read-only ones. |
| `E` | 35 | Everyday requests across every category in the spec. Fixtures include a photo folder with duplicates, a messy Downloads folder, application logs, CSV and JSON files, notes, a small project, and dated backups. Several of them make the destructive path tempting: deduplicating, freeing space, pruning backups, renaming onto an existing name, copying onto an older copy on the "USB", and editing files in place. |
| `P` | 11 | Persona conversations. `prompts` is the first message in each tier. `follow_ups` are the later turns, sent unchanged whichever tier opened the conversation. They cover pronouns across turns, several requests in one message, verification questions after an action, panic and undo, and a Windows mental model. |

Categories are slugs of the spec's list: `file-management`, `search`, `archives-backups`, `data-wrangling`, `text-editing`, `packages-services`, `system-info`, `multi-step`.

S's fixtures were inferred from the 3B model's own command (`oracle.Infer`). Some therefore contain odd paths, such as a directory `s/foo_fn/bar_fn` or a file named `rm`. Others contain nothing for a different command to act on: M067, M101, M134 and M153 have no old or digit-named files. These fixtures are kept as they are for comparability with round 6m. They should not be read as a realistic home folder.

## Feature vocabulary

The `features` tags describe the novice phrasing, plus the follow-ups for P tasks. The vocabulary is fixed, and `internal/probe/suite_test.go` rejects any other tag:

| Tag | Meaning | Tasks |
|---|---|---|
| `vague-target` | "the old stuff", "those big files" | 33 |
| `underspecified` | no folder, no file type, no criterion | 26 |
| `typo` | typos and informal spelling ("pls delet", "r", "u") | 24 |
| `question-as-action` | a question that asks for an action, or a check after one ("did you delete those?") | 20 |
| `pronoun` | a reference across turns ("delete them", "move it back") | 10 |
| `urgent` | emotional or urgent framing ("i messed up, undo everything") | 9 |
| `wrong-model` | Windows or macOS vocabulary ("my c drive", "control panel") | 7 |
| `multi-intent` | several requests in one message | 6 |
| `taglish` | Filipino-English code-switching ("pakiayos naman yung downloads ko") | 5 |

## Provenance

`source` records where a task's wording came from:

| Source | S | E | P | Total |
|---|---|---|---|---|
| `nl2bash:N` (technical prompt verbatim; N is the 0-based line index into `nl2bash_all.nl`) | 22 | | | 22 |
| A forum-question URL (the novice phrasing is modelled on that question's title) | | 10 | 3 | 13 |
| `testing-plan:L7` (the plain phrasing comes from Layer 7, and the novice phrasing is derived from it) | | 7 | | 7 |
| `authored` | | 18 | 8 | 26 |

In S, `source` names the NL2Bash line, and the interface, plain and novice rewrites are authored. Counting by novice wording, then, 20 of 68 novice phrasings are grounded in real or prior text (13 forum titles and 7 Layer 7 phrasings) and 48 are invented. Forum titles are paraphrased in style, not copied. The URLs come from Launchpad Answers, LinuxQuestions, the Ubuntu MATE community, the ubuntu-users list, Ask LibreOffice, and Tom's Guide. Their titles were seen in search listings, and one was fetched and confirmed. All Taglish phrasings are authored.

Fixtures contain only invented content: placeholder names and `@example.com` addresses, with no real personal data. File contents are a line or a few kilobytes of filler, and the largest fixture has 33 entries.

## Adding a task

1. Append one JSON object on a single line with `id` (unique, with the prefix `S-`, `E-` or `P-`), `partition`, `category`, `fixture` (a list of `oracle.Entry`: `{"p":path,"k":"file"|"dir"|"link","d":data,"m":mode}`), `prompts` (all four tiers), `features`, and `source`. A P task also needs `follow_ups`.
2. Keep fixture paths relative, keep contents short and invented, and keep binary data out (fixture data is a JSON string). An archive must be one whose bytes are all ASCII, like the uncompressed tar in `E-open-tar`.
3. For S tasks, take the description from `nl2bash_all.nl` line N+1 (N is 0-based, matching the corpus's `model:N`), and use the corpus item's `fx` as the fixture.
4. Run `go test ./internal/probe/` from `prototype/`. It loads the suite, checks partitions, categories, feature tags and follow-ups, and materializes every fixture into a temporary folder.

The suite is frozen and hashed before the first reported run. After that, add tasks only in a new, separately reported version.
