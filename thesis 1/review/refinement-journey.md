## Overview

This is the record of how the Thesis 1 proposal was refined after the adviser's review of 2026-10-12, which asked that the recoverability-analysis algorithm become the research contribution and SynapseOS only the platform that deploys and validates it. It covers every round: what was asked, who reviewed and against what standard, what each review found (counted by severity), what was changed in response, and how each change was verified, with the pull requests that carry it. The stopping rule, set on 2026-10-12, is that a chapter is done when a fresh reviewer, checking it against a fixed pass/fail list of criteria, finds no HIGH or MEDIUM issue; LOW findings are kept in `2026-10-12-low-findings.md` for a later pass. The process converged by narrowing what reviewers were asked: open-ended reviews always find something, so the final rounds judged fixed criteria rather than hunting for problems.

## Table of Contents

- [Overview](#overview)
- [How a round works](#how-a-round-works)
- [Severity scale](#severity-scale)
- [The rounds](#the-rounds)
  - [Round 0: the adviser's reframe (2026-10-12)](#round-0-the-advisers-reframe-2026-10-12)
  - [Round 1: per-chapter review of all three chapters (2026-10-12)](#round-1-per-chapter-review-of-all-three-chapters-2026-10-12)
  - [Round 2: Chapter 1 spotlight review (2026-10-12)](#round-2-chapter-1-spotlight-review-2026-10-12)
  - [Round 3: fresh re-review of Chapter 1](#round-3-fresh-re-review-of-chapter-1)
  - [Round 4: second fresh re-review of Chapter 1](#round-4-second-fresh-re-review-of-chapter-1)
  - [Round 5: pass/fail check of all three chapters (2026-10-12)](#round-5-passfail-check-of-all-three-chapters-2026-10-12)
- [Facts the reviews surfaced](#facts-the-reviews-surfaced)
- [References added, and how each was checked](#references-added-and-how-each-was-checked)
- [Tools built along the way](#tools-built-along-the-way)
- [Pull requests](#pull-requests)

## How a round works

1. **Review.** A reviewer who has not seen the edits (a fresh agent each time) reads the chapter from the generated plain text in `thesis 1/consolidated/text/`, cross-checks it against the other chapters and the reference list, and reports findings with printed page numbers and quotes. Reviewers are read-only.
2. **Verify.** Each finding is checked against the text before it is acted on; a finding that is wrong is not applied (examples are noted per round).
3. **Fix.** Fixes are scripted as exact-match replacements in the HTML source, so an edit that does not match fails loudly instead of landing somewhere unexpected. Where a fix needs data, the number is computed from the round-7 results rather than estimated.
4. **Check.** The paper is rebuilt (PDF, plain text, Word); internal links, footnote order, and citation coverage (every reference cited) are checked; where references are added or removed, every existing citation is re-verified to resolve to the same source.
5. **Record.** The change is committed with the text diff, opened as a pull request, reviewed, and noted here.

## Severity scale

- **HIGH**: a panelist would likely catch it, or it is a factual or consistency error.
- **MEDIUM**: a real weakness a careful panelist could raise.
- **LOW**: polish; never blocks a chapter.

## The rounds

### Round 0: the adviser's reframe (2026-10-12)

**Trigger.** Dr. Rey: objectives 1–3 are strong Computer Science; objectives 4–5 (build SynapseOS, the user study) are system design and HCI. He proposed five algorithmic objectives and asked that SynapseOS be "the experimental platform used to deploy and demonstrate the algorithm".

**Decisions (by the team).** Adopt all five objectives, refining the fourth to measure silent loss, verified restoration, and needless confirmation rather than classification alone (on classification the analysis ties the fail-closed list); keep the user study as validation of the algorithm in use; move "SynapseOS" to the end of the title; run the runtime-resolution ablation the fifth objective requires.

**Changes.**
- Resolution ablation run on round 7: coverage 89.5% → 45.5% without resolution, 32.0% without composition or resolution; silent loss 0 throughout; the five existing systems' 3,885 decisions unchanged (#70).
- Paper reframed: title, five RQs and objectives, contributions, significance, scope, Chapter 3 introduction, validation-study framing, Table 3.5 (#71).
- Chapter 3 reorganized by objective inside the course template's headings (#75), cross-references updated (#76, #77).
- The SynapseOS-focused 2026-10-10 version archived in `thesis 1/archive/synapseos-focused-2026-10-10/` and `~/Documents/SynapseOS-Archive/`; Chapter 2 given the algorithm's theoretical framework and §2.5 on command safety and static analysis; Figure 3.1 redrawn algorithm-first (#78).
- Recovery-systems literature and the first comparison table added to §2.5, with five new references (#80).

### Round 1: per-chapter review of all three chapters (2026-10-12)

**Reviewers.** Three, one per chapter, open-ended: residue of the old framing, contradictions, overclaims, stale references, missing related work, careless writing.

**Findings: 52.**

| Chapter | HIGH | MEDIUM | LOW |
|---|---|---|---|
| 1 | 6 | 6 | 3 |
| 2 | 6 | 6 | 4 |
| 3 | 5 | 8 | 8 |

**Representative findings.** "Replaces the desktop" framing in Chapters 1–2 contradicting Chapter 3; "every system guards by permission… saves nothing" contradicted by ANOLISA's own rollback, which the paper mentioned uncited; "a domain neither tradition has targeted" contradicted by ABash and CoLiS a page later; uncited products; OSWorld still called "the primary evaluation standard"; the gate policies undefined and their figures mixed; Algorithm 1's read-only test failing on its own `read` effect kind; "minimum-cost" asserted but not argued; fine-tuning on NL2Bash contaminating the NL2Bash partition; results by partition promised but not reported.

**Changes (#83).** All HIGH and MEDIUM findings fixed: framing residue removed, claims scoped, ANOLISA, *try*, ToolEmu, NL2Bash, and AgentOS surveyed, policies defined, Algorithm 1 corrected, a minimality argument written, per-partition results reported, fine-tuning guarded, limitations completed; three references added. LOW findings recorded for later (#85). The plain-text export was built first so every later pull request shows readable wording diffs (#82).

**Verification.** All 171 existing citation groups re-verified to resolve to the same sources after renumbering; the 24 flagged phrases confirmed absent from the chapter text.

### Round 2: Chapter 1 spotlight review (2026-10-12)

**Question asked.** Is there residue of SynapseOS as the star, and is anything stale?

**Findings.** Page 1 ended "This study asks whether that is now optional", the interface question; §1.1 was mostly the interface story with a "deployable replacement" leftover; §1.2's accessibility paragraph and gaps 2–4 took most of the section; §1.7 led with platform scope and platform limitations; the same idea was stated three times across §1.1–§1.3.

**Changes.** §1.1 rewritten to end on the risk and reach the program-analysis problem; §1.2 tightened; §1.3 opened without repeating; §1.7 ordered analysis first.

### Round 3: fresh re-review of Chapter 1

**Findings.** HIGH 3, MEDIUM 5, LOW 3: page 1 still mostly interface history; Contribution 1 said a refusing list "reaches the same verdict" although the analysis asks far less; the headline result absent from Chapter 1; gaps 2–4 still reading as research gaps; interface-only significance items; Objective 4 naming checks RQ4 did not; repeated phrases.

**Standard added.** At the user's direction, Chapter 1 must be **self-contained**: a reader of Chapter 1 alone understands the problem, approach, headline result, contribution, and limits, with every term explained in plain words where first used.

**Changes.** Chapter 1 rewritten as one pass to that standard: a plain-word glossary in §1.2 (effect, pre-image, capture, composition, resolution, fail closed), the headline results in Contribution 1, RQ4 naming the agent checks, §1.6 merged to three items, Table 1.1's git row corrected (16 → 12 pages).

### Round 4: second fresh re-review of Chapter 1

**Findings.** HIGH 2, MEDIUM 6, LOW 5. Verdict: "Nearly solid. The reframing has held, and the numbers are correct." The HIGH findings: mixed denominators in the headline (41.2% of 405 beside 73–97% of 325) and an unscoped novelty claim (NaSh proposes undo without a mechanism). One finding was judged wrong and not applied: §1.6 item 1's "most file-destroying commands" referred to the shipped agent checks, which is accurate.

**Changes.** Denominators stated; the novelty claim scoped; verdict, policy, and capture coverage defined in plain words; the GUI-agent benchmark figure and the platform-coverage survey moved out of Chapter 1; ablation and McNemar's test glossed (12 → 13 pages).

### Round 5: pass/fail check of all three chapters (2026-10-12)

**The stopping rule.** Asked what "solid" means, the team defined it as passing a fixed list of criteria with zero HIGH or MEDIUM findings, judged by a fresh reviewer, because open-ended reviews never run out of polish to suggest. Chapter 1's criteria: algorithm-first, self-contained, accurate, defensible, consistent, honest. Chapter 2's: algorithm-first, accurate citations, defensible, consistent with Chapters 1 and 3, structure, writing. Chapter 3's: traceability, replicable algorithm method, numbers and tables, statistics, validation framing, threats and limits, template and references.

**Results.**

| Chapter | Criteria passed | HIGH | MEDIUM |
|---|---|---|---|
| 1 | 2 of 6 | 1 | 7 |
| 2 | 0 of 6 | 2 | 14 |
| 3 | 1 of 7 | 2 | 14 |

**The most consequential findings.**
- Chapter 3's RQ4 asks whether the analysis asks less often, but needless confirmation was never tested. Computed from round 7: far fewer than the fail-closed list (Holm-adjusted p = 2.3×10⁻¹⁸ under the capture policy), but **not fewer than the pattern list** (32 against 30 of 372, adjusted p = 0.89). The paper now says so in Chapters 1 and 3: RQ4's third part is answered yes against the fail-closed list and no against the pattern list.
- Algorithm 1's first branch caught every simple command, so the wrapper and run-time branches could never be reached; the overlay V was never updated; the directory-hardlink skip also skipped overwrites; package and service effects had no kind. Restructured, and fitted to its box.
- The minimality argument ignored the one shared capture (a git commit identifier covering many files). Extended to cover shared captures.
- Chapter 1 listed SynapseOS and the validation study under "contributes": now one research contribution and two supporting artifacts.
- Chapter 2 credited Jouvelot and Gifford with developing effect systems; [19] is their effect-inference paper. Reworded to what it is.

**Changes (#86).** Every HIGH and MEDIUM finding in the three chapters fixed in one commit, including the coding-agent checks reported with Wilson intervals in Chapter 3, per-command pre-state snapshots for the validation study's replay, the deployed policy named, task form added to the mixed model, and threats for the single corpus draw and for too few data-losing commands in the study.

## Facts the reviews surfaced

- Results hold within each partition: the analysis covered 133 of 150 losses among human-written NL2Bash commands (88.7%) and 158 of 175 among the team-written templates (90.3%), answering the concern that the same team wrote the templates and the rule table.
- The analysis asks about as often as the pattern list, not less; its gain on confirmations is against the fail-closed list.
- ANOLISA, a shipping Linux distribution for AI agents, already offers file-level workspace snapshots per conversation round: the nearest shipped system, and recovery by snapshot rather than by analysis.
- *try* (OSDI '26) previews a command's effects by running it in a semisolate; its authors cite an LLM-suggested `find -exec` deleting a user's files. It is the closest prior work to the analysis's dry runs.
- The model-generated partition was made with round 6's analyzer, before that round's five undo fixes.

## References added, and how each was checked

| Reference | Checked against | Free to read |
|---|---|---|
| Rodeh, Bacik, Mason 2013, BTRFS (ACM TOS) | Crossref | ACM Digital Library (open access since 2026-01-01) |
| Santry et al. 1999, Elephant (SOSP) | Crossref | ACM; confirmed open in a browser by the team |
| Porter et al. 2009, TxOS (SOSP) | Crossref | ACM; author copy at UT Austin |
| Goel et al. 2005, Taser (SOSP) | Crossref | ACM; author copy at Toronto |
| Brown and Patterson 2003, Operator Undo (USENIX ATC) | the paper's own PDF | usenix.org |
| Alibaba Cloud 2026, ANOLISA product overview | the documentation page | help.aliyun.com |
| Lamprou et al. 2026, *try* (OSDI '26) | the open-access PDF | usenix.org |
| Ruan et al. 2024, ToolEmu (ICLR) | arXiv and the ICLR listing | arXiv |

Each addition re-sorts the ACM numbered list, so every existing citation was renumbered by key and then independently verified to resolve to the same source.

## Tools built along the way

- `tools/export_text.py`, run by `tools/build_paper.py`: the per-chapter plain text with printed page numbers, used by every review and every page lookup since (#82).
- Scratch scripts (not committed): exact-match edit scripts per round; a citation renumberer that verifies the old list, maps numbers through keys, and resolves `{{cite:key}}` placeholders; a paragraph-level diff to prove moved text is intact; McNemar and Wilson calculators over the round-7 data.

## Pull requests

#70 resolution ablation · #71 algorithm-first reframe · #72 recap and explainer · #74 two-page recap · #75–#77 Chapter 3 by objective · #78 Chapters 1–3 algorithm-first, archive · #80 recovery literature · #82 plain-text export · #83 round-1 fixes · #84 recap pages · #85 LOW findings list · #86 rounds 2–5 (Chapter 1 rewrite and the pass/fail fixes).
