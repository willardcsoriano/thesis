## Overview

Project instructions for the SynapseOS thesis repository: a Go prototype under `prototype/` that turns natural language into shell commands, plus the thesis paper and its supporting documentation. The rules below set how work is done here. Development speed is the priority. Blockers are reported the moment they appear, with the exact command the owner can run, and everything not blocked keeps moving. The rest is the working philosophy, how the project's documents relate to the code and the paper, and the few repository facts that are not obvious from the code.

## Table of Contents

- [Overview](#overview)
- [Working philosophy](#working-philosophy)
- [Blockers](#blockers)
- [Tests are evidence, not targets](#tests-are-evidence-not-targets)
- [How to treat this project's docs](#how-to-treat-this-projects-docs)
- [Repository facts](#repository-facts)

## Working philosophy

Pro agility, anti rigidity, anti bureaucracy. Favor fast, efficient, effective progress: ship small, verify with automated tests, iterate. Skip ceremony (pre-registration, sign-off steps, extra documents, confirmation rounds) unless it protects something load-bearing: data safety, study validity, secrets, or an irreversible action. Prefer what already exists over writing new code, including inside dependencies. Make ordinary implementation decisions without asking. Ask only for product or research-design choices, batched, and keep working on everything else meanwhile.

## Blockers

Development speed is of utmost importance, so a blocker is reported at once, not discovered at the end.

- The moment something stops progress (a missing dependency, a command that needs a password or a terminal, a permission denial, a hook or classifier block, a stale package index), say so in one short message that names the blocker, then continue with every branch that does not depend on it.
- Hand the owner a command only when it cannot be run from the session (a password, an interactive login, a physical action, a permission block). Everything else is run, not handed over.
- Every step the owner must run goes into one consolidated block, copied once and pasted once: chained with `&&` in run order, with no placeholders and no prose between commands. End the reply with it as its only fenced code block so `/copy` gets exactly it. `/copy` takes only the text after the last tool call, so make no tool call after the block: the block is the last thing in the final message. Say whether to run it in a normal terminal (anything that prompts for input) or after `!`. After it runs, verify the result from the session.
- For a missing dependency the block is the one install line with sudo, for example `sudo apt install -y <packages>`. Do not work around its absence and do not wait on it.
- Report each blocker once, when it is hit, and again in the closing summary while it is still open. Do not retry variants of a blocked action and do not route around a block with another tool.
- Do not spend a turn on a step that only the owner can perform. Hand it over and move on.

## Tests are evidence, not targets

When a test fails or a blocker is hit, fix the system under test at its root cause, never the test to make it pass. Hitting a hundred honest blockers is better than one green run that hides a defect.

- **Find the wrong side first.** Before changing anything, establish with evidence whether the system or the test is wrong. Change a test only when the test itself is wrong, say why in the commit, and never because the result was unwelcome.
- **No shortcuts to green.** Never weaken an assertion, add a skip, special-case a test's inputs, loosen a threshold, mock away the failing part, or route around the check.
- **Development data is not evidence.** A fix derived from some data cannot be proved by that same data. Measure on held-out data the fix never saw, and every reported result says which data was development and which was held out. In this repository:
  - Corpora are drawn after the analyzer is frozen (`pilot/analyser_frozen*.sha256`).
  - A study batch that exposed a gap becomes development data once that gap is fixed.
  - The committed round-7 result files are the paper's numbers and are never regenerated over. A rerun is reported beside them.
- **Worst cases by default.** Every happy path gets its hostile neighbours: malformed and adversarial input, boundaries and empty states, concurrency and ordering, partial failure and interruption, and resource exhaustion.
- **Observe real behaviour.** Prefer tests that execute and then check the actual resulting state, as `internal/oracle`, `make composecheck`, and the sandboxed recovery check do, over tests that ask the code its own opinion of itself.

## How to treat this project's docs

`docs/README.md` is the map: every document, what it owns and what it does not, the reading order for a cold start, and where new material goes. Each file has one job, and the same content in two files is a bug. Read the map before adding to a doc or proposing a new one; a file whose job it is almost certainly exists.

The docs record why a call was made and what was tried and rejected. They are not a boundary future work must fit inside. Decisions in `docs/decisions.md` are revisable: mark them Revised, Reopened or Superseded in place, never delete or renumber them. Cite a past decision to explain the trade-off being weighed, never as a stopping argument on its own.

**Work moves through three tiers, in one direction, and each has its own standard of finish.**

| Tier | Where | Standard | Cost of a change |
|---|---|---|---|
| Dirty work | `prototype/` | Whatever it takes: iterate, break things, throw away | Cheapest: the compiler and the tests catch mistakes |
| Soft documentation | `docs/` | Honest and current, not polished; decisions superseded in place | Medium: prose, but no build |
| Finalized work | `thesis 1/consolidated/` | Publication standard: guideline formatting, verified ACM citations, a rebuilt PDF | Most expensive: every edit means a rebuild and a compliance re-check |

Settle a thing in the cheapest tier that can settle it, and only then render it upward. Never fix the paper before fixing the doc it should have come from, and never write a doc for something the prototype has not shown to be true. When the paper and a doc disagree, the doc is right and the paper is stale. The paper is a living document, and updating it to match what the system now does is ordinary maintenance, never a reason to hold the prototype back. Dated snapshots in `thesis 1/archive/` and `research-methods/` are frozen records and are never edited.

- **Design record before the code, amended after.** Anything the project builds rather than adopts gets an entry in `docs/algorithms.md` first: the problem, why nothing existing does it, the approach, and the evaluation design including its metrics and baseline, committed before any result exists. After implementation the entry is amended, not rewritten, with what the design got wrong.
- **Log a problem when it is hit, before it is fixed.** `docs/open-problems.md` holds only what is outstanding. Add a row the moment something blocks; when it closes, move the row to `docs/retrospective.md` with the resolution.
- **Adopt by default** (`docs/vision.md`, Principles). Build only what nothing existing does, and show that before building.

## Repository facts

- Go 1.25.0 lives at `~/.local/go/bin/go`. `prototype/Makefile` prefers whatever `go` is on PATH, and the dev VM's system `go` is 1.24, older than `go.mod` asks for, so put `PATH="$HOME/.local/go/bin:$PATH" GOTOOLCHAIN=local` in front of `make` and `go` commands.
- `make -C prototype ci` runs format, vet, and tests. Run it before calling anything done.
- The sandboxed recovery check runs one case at a time; run it on every CPU with `python3 pilot/parallel_rc.py CORPUS.jsonl OUT.txt` from `prototype/` (with `RC_NO_CURATED=1` for a corpus alone). `make composecheck` runs the differential test of composition (compound lines, undone and compared). While fixing, rerun only the failing cases; run the full draws once to confirm.
- The CLI is the system's contract, and the TUI and GUI are presentation over the same loop (D48). `docs/cli.md` states it and quotes `synapse help`, which a test keeps identical. A change to commands, exit codes, streams, `--json` or `SYNAPSE_*` variables updates the help, the doc and `cli_contract_test.go` together. Tasks are given only as `synapse run …` or `synapse -- …`. `internal/tui` may depend only on `internal/loopevent` and `internal/termsafe` (`boundary_test.go`).
- The analysis in `prototype/internal/effects` must fail closed: unknown or unresolved means unrecoverable, which asks. It never runs a mutating command; its resolvers are proven read-only and run under bubblewrap.
- Corpus ground truth comes from `prototype/internal/oracle` (sandboxed execution), never from the analysis, and never from a human label.
- `prototype/pilot/nl2bash_all.*` is GPL-3.0 and git-ignored. Only sampled commands are kept in the repository.
- Write text that mentions privileged commands with the Write tool, not in a Bash heredoc, so the guard hook cannot misread it.
- The paper's only source is `thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html`. `tools/build_paper.py` builds the PDF from it and writes a plain-text copy per chapter to `thesis 1/consolidated/text/` (`chapter-1.txt` and so on, each page headed by its printed page number). Read and review the paper from those text files, cite findings by printed page, and make every change in the HTML, then rebuild; never edit the text files or the PDF. Commit the regenerated text with the HTML so pull requests show the wording that changed.
