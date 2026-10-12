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
- [ ] **Stretched inference** (§2.9): [the reference on result verification] shows a benefit from verifying results; "precisely the role a reversibility gate and an undo record play" overstates it, since a gate acts before execution. Say "an analogous role".

## Chapter 3

- [ ] **Section references.** "the claims of RQ2 through RQ5 rest on the corpus evaluation in Section 3.1" should say Sections 3.1–3.2. "answered against the labeled command corpus described in Section 3.1": the corpus is described in Sections 1.1(a) and 1.2.
- [ ] **Repository paths cited as sources** (`docs/open-problems.md`, `pilot/snapshot_bench.txt`). Cite them as supplementary material or an appendix instead.
- [ ] **"LLM-assisted functional equivalence assessment at 95% confidence"** (§2.3 Validation) is undefined. Name the judge, its prompt, and how agreement is checked.
- [ ] **Interface modes described twice** (System Environment Specifications): "SynapseOS additionally supports a TUI mode" follows a passage about TUI mode, and the CLI description repeats note 1. Trim to one description.
- [ ] **"a minimum-cost cover of the same set"** (§2.1(c)) has no antecedent in its subsection. Say "the composed effect set".
- [ ] **Heading prefix.** §2.1(d) is "Experimental Platform: Conversational Interface Layer" and (e) is "Execution Pipeline". Make the two consistent.
- [ ] **Spelling.** "analyser", "analysed", and "behaviour" appear beside "analyzer" and "behavior". Use American spelling throughout.
- [ ] **Data minimization** (§2.5 Ethics): "No identifying metadata … is retained beyond the consent confirmation step" conflicts with the ID-to-consent mapping kept for fourteen days. Qualify the sentence.
