## Overview

This file holds the LOW-priority findings from the per-chapter review of the proposal on 2026-10-12, kept so they are not lost while the HIGH and MEDIUM findings were fixed first (pull request #83). Each finding is minor: a phrase that reads carelessly, a small inconsistency, or a claim that would be stronger with a source. None changes what the paper argues or reports. The quotes are what to search for in `thesis 1/consolidated/text/`; page numbers moved when the HIGH and MEDIUM fixes were made, so find each finding by its quote. Fix them in the HTML, rebuild, and tick them off here, or delete this file once all are done.

## Table of Contents

- [Overview](#overview)
- [Chapter 1](#chapter-1)
- [Chapter 2](#chapter-2)
- [Chapter 3](#chapter-3)

## Chapter 1

- [ ] **"for fifty years it could not"** (§1.1, opening). The same page cites the Berkeley UNIX Consultant from the late 1980s. Say "for most of fifty years", or note that early systems advised and did not act.
- [ ] **Terminology drift.** §1.5 says "the minimal set of pre-images" where the objectives, RQ3, and Chapter 3 say "minimum-cost". Chapter 3 mixes "analyzer" and "analysis". Use "minimum-cost" and "analysis" throughout.
- [ ] **Repeated passage.** "A system that writes a shell command … acts on the user's own files" appears in nearly the same words at the end of §1.1, the start of §1.2, and in §1.3. Keep it once, in §1.3.

## Chapter 2

- [ ] **Undated benchmark claim** (§2.8): "leading agents now exceeding the original human baseline on the revised task set". Cite the leaderboard with an "as of" date.
- [ ] **Unsourced claim** (§2.3): "It is widely held that the command line's power is inaccessible to most people". Cite it or cut it.
- [x] **Stretched inference** (§2.9): [the reference on result verification] shows a benefit from verifying results; "precisely the role a reversibility gate and an undo record play" overstates it, since a gate acts before execution. Say "an analogous role".

## Chapter 3

- [ ] **Section references.** "the claims of RQ2 through RQ5 rest on the corpus evaluation in Section 3.1" should say Sections 3.1–3.2. "answered against the labeled command corpus described in Section 3.1": the corpus is described in Sections 1.1(a) and 1.2.
- [ ] **Repository paths cited as sources** (`docs/open-problems.md`, `pilot/snapshot_bench.txt`). Cite them as supplementary material or an appendix instead.
- [x] **"LLM-assisted functional equivalence assessment at 95% confidence"** (§2.3 Validation) is undefined. Name the judge, its prompt, and how agreement is checked.
- [ ] **Interface modes described twice** (System Environment Specifications): "SynapseOS additionally supports a TUI mode" follows a passage about TUI mode, and the CLI description repeats note 1. Trim to one description.
- [ ] **"a minimum-cost cover of the same set"** (§2.1(c)) has no antecedent in its subsection. Say "the composed effect set".
- [ ] **Heading prefix.** §2.1(d) is "Experimental Platform: Conversational Interface Layer" and (e) is "Execution Pipeline". Make the two consistent.
- [ ] **Spelling.** "analyser", "analysed", and "behaviour" appear beside "analyzer" and "behavior". Use American spelling throughout.
- [ ] **Data minimization** (§2.5 Ethics): "No identifying metadata … is retained beyond the consent confirmation step" conflicts with the ID-to-consent mapping kept for fourteen days. Qualify the sentence.

## Added by the pass/fail rounds 6 to 19

These LOW findings recurred across several fresh reviewers; none blocks a chapter.

- [ ] **"Nor does it ask less often…"** (Chapter 1, Limitations): "Nor" follows no negative. Start a new sentence.
- [ ] **Model-generated partition** (Chapter 1): add that the fail-closed list also let the mislabeled command through.
- [ ] **Objective 4** (Chapter 1): "ground truth obtained by sandboxed execution" needs "for every command that acts on files".
- [ ] **RESOLVE's timeout check** (Algorithm 1) sits before "run d"; move the time-limit and failure checks after the sandboxed run.
- [ ] **EXPAND(W)** (Algorithm 1) is undefined; say whether a loop list built from `$( )` goes through RESOLVE.
- [ ] **"loops" vs "for-loops"** (Chapter 3, 2.1(b) and 3.2): only `for` is marked [C]; say "for-loops" throughout, and drop "subshell" from 3.2's list or add a subshell line to Algorithm 1.
- [ ] **{UNKNOWN} vs "the unresolved verdict"** (Chapter 3, 3.2): use one sentinel name.
- [ ] **Risk difference** (Chapter 3, 3.1(c)): "the paired-data equivalent of Cohen's d" and "accuracy minus the baseline's" are loose; define the sign for silent loss.
- [ ] **Effect size for error rate** (Chapter 3, 3.6): error is modeled by logistic regression; report an odds ratio, not Cohen's d.
- [ ] **Shapiro-Wilk** (Chapter 3, 3.6): apply it to the paired differences.
- [ ] **VoicePilot's caregiver guideline** (Chapter 3, 2.1(e)): the sentence tying it to the gate reads as a non sequitur; rephrase.
- [ ] **Meta phrasing** (Chapter 3): "the review of this chapter", "raised directly by this study's adviser"; use neutral wording.
- [ ] **Recovery attempts vs retries** (Chapter 3, 1.1(d) vs 3.5(b)): one says an error category, the other attempts; unify.
- [ ] **Snapshot scope in use** (Chapter 3, 3.5(e)): tasks may act anywhere under the home folder, but the replay snapshot covers the task directory; state the scope.
- [ ] **Program analysis vs command safety** (Chapter 2, 2.11): guards and recovery mechanisms are filed under "program analysis"; use 2.1's wording.
- [ ] **Clark [7] and Luger [27]** (Chapter 2, 2.11) are cited as direct-manipulation critiques; they are conversational-agent studies.
- [ ] **Cousot [8]** (Chapter 2, 2.10) is cited for judging an analysis by its false negatives first; cite it for soundness only.
- [ ] **Table 2.2 stray line break** in "Demonstration, case studies".
- [ ] **Unchecked source details** (Chapter 2): ABash's taint tracking, Smoosh's test suites, OSWorld 2.0's 108 workflows and 20.6%, Whiteside's 76 users and seven systems; confirm against the sources.

