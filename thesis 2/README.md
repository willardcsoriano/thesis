## Overview

This directory is for the **Thesis 2** course, which follows Thesis 1 (`thesis 1/`). Thesis 1 delivered the proposal: Chapters 1–3, the effect analysis as the contribution, and round 7's 777-command evaluation. Thesis 2 is where that proposal is carried out. SynapseOS is the platform for the user study, and the study validates the analysis in use. The paper stays in `thesis 1/consolidated/` until it moves here (see "Closing Thesis 1" in `thesis 1/README.md`). This README records what Thesis 2 starts from. `docs/scope.md` remains the checklist of deliverables, and `docs/open-problems.md` remains the register of what is broken or undecided. The critical path is ethics approval: nothing involving participants can start without it.

## Table of Contents

- [Overview](#overview)
- [Starting point](#starting-point)
- [Carried forward](#carried-forward)
- [Still to find out](#still-to-find-out)

## Starting point

- **Paper:** the defended proposal with its post-defense revisions (#100, #102), built from `thesis 1/consolidated/SynapseOS_Proposal_Chapters_1_to_3.html`.
- **Algorithm (Track C):** essentially complete. Round 7 restores 89.5% with zero silent loss, and the ablation is measured (`docs/recoverability-analysis.md` §10). One known limit is disclosed: row 37, the bidirectional `rsync` nesting.
- **Platform (Track A):** the CLI, REPL, and TUI modes ship, with the effect analysis as the default gate. The GUI-mode session launch is built but has not been tried through a real login (`docs/scope.md`, "GUI-mode session launch").

## Carried forward

In priority order. Row numbers refer to `docs/open-problems.md`.

1. **Ethics (row 7).** Compile the ethics application package from the drafted instruments and submit it. Approval turnaround is the one thing in the plan that is outside our control, so this starts first, and development continues in parallel (`docs/scope.md`, "Process — Ethics and Recruitment").
2. **Study-facing safety bugs.** These are what a novice participant would hit:
   - row 20: a verification question producing `rm`
   - row 21: pronoun scope over-broadening
   - row 42: undo reverting an entry from another folder
   - rows 15 and 39: answers that state things the output does not support
3. **Approval prompt wording (row 40).** It explains what the analysis found, but not what the command does.
4. **Loop behaviour (rows 16, 22, 26).** Completion detection, multi-intent requests, and the step-cap message.
5. **Human TTY passes (row 8)** and the plain-language gap (row 12b), which the pilot's real utterances will re-measure.

## Still to find out

- What Thesis 2 must deliver: the chapters, the defense format, and the deadlines. Ask the course professor once the Thesis 1 verdicts are in.
- Whether the Thesis 1 grade of C affects enrollment in Thesis 2, and whether the final Thesis 1 grade is P/F or numeric. Mapúa's grading system lists P/F for thesis courses, but it is not confirmed for this program.
