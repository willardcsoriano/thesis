## Overview

This is the log of every soundness defect found in the recoverability analysis (`prototype/internal/effects`) after the round-7 corpus was frozen on 2026-10-09: seven so far, each with how it was found, the command shape that triggered it, what it would have cost a user, the fix, and its effect on the reported results. Every one has been fixed with a regression test, and none changed a single decision on the round-7 corpus, so no number in the paper moved. The pattern matters more than any single entry. The per-command rules held up; every defect from the fifth on lies where parts of one command line interact (an earlier part changes what a later part sees, or may fail before it), a shape the corpus almost never contains. The log ends with why that happened and the test that would find the next one before a reviewer does: differential testing of generated compound lines against the sandbox.

## Table of Contents

- [Overview](#overview)
- [The defects](#the-defects)
  - [Defect 7 in detail](#defect-7-in-detail)
- [Effect on the reported results](#effect-on-the-reported-results)
- [Why there were seven](#why-there-were-seven)
- [What would find the next one first](#what-would-find-the-next-one-first)

## The defects

| # | Date | Found by | Shape | What it would have done | Fix |
|---|---|---|---|---|---|
| 1 | 2026-10-10 | Writing rule tests for the reference | `cd -; rm a.txt`, or a `cd` to a path that could not be expanded | Passed `rm` as harmless: every rule found no target at the unknown location | Commands at an unknown working directory are unresolved and ask (`ea1a85e`) |
| 2 | 2026-10-10 | Writing rule tests | `wget URL` onto an existing name | Recorded a creation of `NAME`, though wget saves `NAME.1`; undo would delete the user's original | The rule follows wget's naming (`0e696c1`) |
| 3 | 2026-10-10 | Writing rule tests | `rsync --dry-run --log-file=…` | The resolver's dry run wrote a file, so it was not read-only (bubblewrap blocked it in the running system) | Such options make `rsync` unresolved; `FuzzResolversAreReadOnly` checks every resolver (`ecfde3c`) |
| 4 | 2026-10-10 | Mutation testing, first sample | `curl -o x.tar URL && tar xf x.tar` | The archive did not exist yet, the listing came back empty, and that read as "extracts nothing", so extraction could overwrite files unprotected | Extraction asks when the archive does not exist yet or the line creates it (`1148a6d`) |
| 5 | 2026-10-11 | Mutation testing, second sample | `unset HOME; rm -r "$HOME/x"`, `read f; rm "$f"` | A variable changed by a builtin kept its environment value, so the analysis captured the wrong path | Variables named by `unset`, `read`, `mapfile` become unknown and ask (`10f9c24`) |
| 6 | 2026-10-12 | Chapter 3 review (round 4 pass/fail) | `mv a.txt logs/ && find logs -delete`, `mv a.txt logs/ && rm -r logs` | `find` listed the tree as it was before the line, missing the moved file; the move stayed "undo moves it back" though the file was deleted | `find` refuses a tree the line changed; the move becomes a captured removal (PR #87) |
| 7 | 2026-10-12 | Chapter 3 review (round 9 pass/fail), extended by round 10 | `cd logs \|\| rm a.txt`, `mv a.txt tmp/ \|\| echo x > a.txt`, `cd logs; rm *`, `x=a; if c; then x=b; fi; rm $x`, a `cd` inside a loop; then `rm a.txt && touch a.txt; echo x > a.txt` | The line was read as if every part succeeded and as if branches left no trace; each shape lost a file with nothing captured | Failure points and joins (below) (PR #88) |

### Defect 7 in detail

Bash runs the right side of `||` only when the left side failed, runs what follows `;` whether or not the line before it succeeded, and keeps an assignment or a `cd` made inside an `if` or a loop after it ends. The analysis assumed the opposite on all three counts. The fix models them:

- **Failure points.** After `;`, a newline, `||`, a pipe stage, `!`, an else branch, or a loop pass, a removal earlier on the line is only a possibility, so the path is read from disk again and a later write to it is captured; and a `cd` earlier on the line leaves the working directory unknown, which asks. `cd dir || exit` is recognized as certain.
- **Joins.** After an `if`, `case`, loop, or `&&`/`||`, a variable or directory on which the paths that may have run disagree is unknown. Each part of an `&&` chain still sees the state the parts before it leave.
- **Re-creations.** The round-10 review found the first version of the fix incomplete: a path removed and then made again (`rm a.txt && touch a.txt; echo x > a.txt`) counted as created by the line, so the last write was not captured, although if `rm` failed it overwrites the original. A creation over a path the line removed is now doubted at a failure point like the removal itself.

Probing found the shapes; the regression test `TestEarlierPartMayHaveFailed` holds 19 of them, and three curated cases in `cmd/recoverycheck` (S38 to S40) execute the new captures in the sandbox and check that undo restores them.

## Effect on the reported results

Each fix was checked by re-running the frozen round-7 corpus with the fixed analyzer and comparing every decision:

| After fix | Decisions compared | Changed |
|---|---|---|
| 1 to 4 | 3,885 (777 commands × 5 configurations) | 0 |
| 5 | 3,885 | 0 |
| 6 (#87) | 5,439 (777 × 7 systems) | 0 |
| 7 (#88) | 5,439 | 0 |

After #88 the sandboxed recovery check on the corpus is identical, row for row, to round 7 (630 of 631 restored). The paper reports these defects as found after the evaluation, with no decision changed (Chapter 3, Section 3.1(d)).

## Why there were seven

- **The testing pointed at the rules, and the rules held.** The corpus, the rule tests, the property test, and mutation testing all exercise what one command does. Four of the first five defects are single-command rule gaps, found when coverage was pushed into rules the corpus never failed.
- **The last two lie between commands.** An analysis of a command line is a small interpreter of the shell, and its hardest part is control flow: what a later part sees after an earlier one moved, failed, or branched. The round-7 corpus is drawn from NL2Bash and templates, which are mostly single commands and `&&` chains, so these shapes were nearly absent. That is also why fixing them changed no decision.
- **Precise pseudocode exposed them.** Both were found when a reviewer read Algorithm 1 against the prose and asked whether the code did what the pseudocode said. Writing the algorithm down exactly is itself a test.
- **The claim is bounded accordingly.** Zero silent loss is a result on the corpus, not a proof over all of bash; the paper words it that way, and this log is the evidence for keeping that wording.

## What would find the next one first

A differential test of composition: generate compound lines by joining corpus commands with `;`, `||`, `&&`, pipes, `if`, and loops, including parts made to fail (a `cd` to a missing directory, a `mv` into a read-only one), run each in the sandbox oracle, and flag any line where the oracle sees a loss the analysis neither captured nor asked about. It aims the existing ground truth at exactly the region where defects 6 and 7 lived, and it needs no human labels. It is recorded as the next step in section 12 of `recoverability-analysis.md`.

## Defect 8: undo, found by the differential test (2026-10-13)

The differential test proposed above was built (`prototype/pilot/make_compose.py`, `make composecheck`, run in parallel by `pilot/parallel_rc.py`). On its first 2,000 compound lines the analysis let 1,536 run, and undo failed to restore 24 of them. The analysis had captured what was needed; the fault was in undo, which assumed every recorded effect had happened. In several cases undo itself destroyed a file:

| Shape | What undo did |
|---|---|
| `test -f nosuch && mv a.txt b.txt` (the move never ran) | moved b.txt onto a.txt, destroying a.txt |
| `rm a.txt \|\| exit 1 ; cp b.txt a.txt` | restored a.txt, then deleted it as "created by the line" |
| `if rm -r logs; then mkdir logs; fi` | could not restore logs over the new directory |
| `mv a.txt b.txt ; mv b.txt tmp/` with the second move failing | wrote b's bytes into the restored a.txt through a shared inode |
| `mv b.txt tmp/ \| mv a.txt b.txt` (pipeline stages race) | removed a's file as a leftover |

Fix (branch `test/compose-differential`): moves are recorded with the moved file's identity and go back by identity, along chains and whatever order the parts ran; a directory made again is merged into; re-creations are removed before restoring, and a restored or pre-existing original is never removed; a content restore is separated from a file restored from trash. Eleven regression tests in `internal/undo/partial_test.go`, most confirmed to fail on the old code. Result: every compound line the analysis let run was restored exactly, 23,054 of 23,054 across six draws (`prototype/pilot/compose_results.txt`), with round 7 unchanged (0 of 5,439 decisions, recovery rows identical). The last draw was unseen until the fixes were done: its first run restored 6,150 of 6,151, and the one failure, a second name the line left for a restored file, was then fixed (twelve regression tests in all). Each fresh draw had surfaced rarer cases than the one before, so rare shapes may remain; the test is now one command, `make composecheck`.

