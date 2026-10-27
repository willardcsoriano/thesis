## Overview

`synapse`'s command line is the system's contract, and every other interface sits on top of it. Scripts, agents, the study harnesses, the bootable image's boot test, and the TUI all drive the same loop, so this page states what the CLI accepts, what it writes where, and what each exit code means. Free text becomes a task only after `run` or `--`; anything else the parser does not recognise exits 2 without contacting the model (D48). Prompts and diagnostics go to stderr, results to stdout, and `run --json` turns the loop's events into JSON Lines that end with the task's outcome. The help text below is checked against the program by a test, and the guarantees are tested end to end against a scripted fake model.

## Table of Contents

- [Overview](#overview)
- [Help text](#help-text)
- [Guarantees](#guarantees)
- [The `--json` stream](#the---json-stream)
- [Known limits](#known-limits)

## Help text

This is `synapse help`, verbatim. `TestCLIDocMatchesHelp` fails when the two differ; after changing the help, paste the new output here.

<!-- help:begin -->
```
synapse turns a request in plain language into shell commands, runs the
ones it can undo, and asks before anything it cannot.

Usage:
  synapse run [--json] <task...>   run one task, e.g. synapse run "list the files here"
  synapse -- <task...>             same, for a task that starts with "-"
  synapse repl                     read tasks line by line, with memory of earlier ones
  synapse scratch                  read tasks line by line, without memory
  synapse tui [scratch]            the full-screen interface (scratch: without memory)
  synapse undo                     undo the most recent recorded command
  synapse demo                     propose commands for a built-in sample suite; runs nothing
  synapse help                     this text
  synapse version                  the build's version

run reads confirmations from stdin; answer y to approve. Anything else, or a
closed stdin, declines, so nothing unrecoverable runs unattended. Prompts and
diagnostics go to stderr; stdout carries only results.

--json writes one JSON object per line to stdout: command, result, answer,
notice, problem, note, and approval events, then a final
{"type":"end","outcome":...,"exit_code":...}.

Exit codes:
  0    done
  1    failed: a command could not run or kept failing, or undo failed
  2    the command line or environment was not accepted; nothing ran
  3    a confirmation was declined
  4    the request cannot be done with a shell command
  5    the step limit was reached before the task finished
  6    the model backend could not be reached or did not answer
  7    done, but a step could not be recorded for undo, so it cannot be undone
  129  stopped by a hang-up (SIGHUP)
  130  interrupted (Ctrl+C)
  143  stopped by SIGTERM, as timeout and supervisors send

Environment:
  SYNAPSE_MODEL         Ollama model to use (default qwen2.5-coder:3b)
  SYNAPSE_OLLAMA        Ollama endpoint (default http://localhost:11434)
  SYNAPSE_NUM_CTX       model context window, in tokens (at least 4096) (default 8192)
  SYNAPSE_KEEP_ALIVE    how long Ollama keeps the model loaded ("10m", seconds, or -1 for always) (default 30m)
  SYNAPSE_ANALYSIS      effect analysis: strict, capture, or off (default strict)
  SYNAPSE_RESOLVE       resolve run-time targets: auto (only in a sandbox), always, or never (default auto)
  SYNAPSE_SESSION_LOG   study telemetry log path; requires SYNAPSE_PARTICIPANT
  SYNAPSE_PARTICIPANT   participant ID written into the study log
  SYNAPSE_TASK_ID       study task ID for the events that follow
  SYNAPSE_AIDS          harness aids to use, for experiments: all, none, or a list of peek,tools,refs,lint,literal (default all)
```
<!-- help:end -->

## Guarantees

| Guarantee | Enforced by |
|---|---|
| A command line the parser does not accept never contacts the model, exits 2, and writes usage to stderr | `TestParseArgsRefuses`, `TestRefusedCommandLinesNeverReachTheModel` |
| A declined confirmation exits 3; only `y` or `yes` approves; a closed stdin declines | `TestExitCodeDeclinedWhenStdinIsClosed`, `TestOnlyAnExplicitYesApproves` |
| Each exit code comes from the task's recorded outcome, the same value the study log's `task_end` holds | `exitCodeFor` in `cmd/synapse/outcome.go`; the `TestExitCode*` tests |
| stdout carries only results; the model banner, step-time notice and prompts go to stderr | `TestStdoutCarriesOnlyResults` |
| Every `--json` line parses, and the last is `end` with the outcome and the same exit code the process returns | `TestJSONStreamEndsWithTheProcessOutcome`, `TestJSONWhenTheModelIsUnreachable` |
| Ctrl+C stops the running command, or ends a wait at a confirmation prompt at once, records the outcome, and exits 130; nothing waiting for approval runs | `TestInterruptStopsTheTaskAndExits130`, `TestInterruptAtAConfirmationPromptExitsAtOnce`, `TestDoubleInterruptEndsPromptly` |
| In `repl` and `scratch`, Ctrl+C at a prompt cancels only that task, and the next line typed reaches the session as a new task | `TestRunREPLInterruptAtAConfirmationPromptIsImmediate`, `TestLineSourceCancelledWaitDoesNotLoseTheLine` |
| A set but invalid `SYNAPSE_*` value exits 2 before the model is contacted; `undo` ignores model settings | `TestInvalidConfigurationIsRefusedBeforeTheModel`, `TestUndoIgnoresModelConfiguration` |
| Losing stdout (`synapse run … \| head -1`) stops the task at the next step boundary with exit 130, never between running a command and journaling its undo; the commands synapse runs still get the default SIGPIPE | `TestStdoutClosedEarlyNeverLeavesAStepUnjournaled`, `TestCommandsStillGetTheDefaultSIGPIPE` |
| A command's output costs bounded memory: synapse keeps the first and last 512 KiB of each stream and says how many bytes it left out | `TestHugeCommandOutputIsBounded`, `TestRunCappedBoundsOutput` |
| Text mode and the TUI show a command's terminal control sequences instead of obeying them; `--json` keeps them, escaped by the JSON encoder (bytes that are not valid UTF-8 become U+FFFD there, because JSON strings are UTF-8) | `TestTextModeNeutralisesTerminalEscapes`, `TestTUIShowsControlSequencesInsteadOfObeyingThem` |
| Concurrent synapse processes share the undo journal without losing, tearing, or double-applying an entry; an undo whose entry changed while it was being confirmed does nothing and says so | `TestConcurrentRunsShareTheJournalSafely`, `TestConcurrentUndosNeverApplyOrLoseAnEntryTwice`, `internal/undo/journal_test.go` |
| An unreadable journal line (an append cut off by a crash) is kept and skipped with a warning, so the rest of the journal still undoes | `TestCorruptJournalLineDoesNotDisableUndo` |
| Whatever the model replies (empty, not JSON, a dropped connection, an HTTP error, a huge or multi-line command), the run ends with a defined exit code and a valid `--json` stream, and nothing unapproved is deleted | `TestHostileModelReplies` |
| A panic inside the analysis fails closed: the command asks | `TestAnalysisPanicFailsClosed` |
| Undo never destroys what is there now: before it removes, overwrites or restores into a path, the current version goes to synapse's trash, and the plan shown before confirming says so in plain words; uncommitted git changes are stashed first | `undo_safety_test.go`, `internal/undo/safely.go` |
| A failed command's planned creations are not journaled, and a new journal entry after a crash-torn line starts on its own line | `TestAFailedCreationIsNotJournaledAsACreation`, `TestAppendAfterATornLineStartsANewLine` |
| SIGTERM and SIGHUP stop a run like Ctrl+C: what ran is journaled, the stream ends with `end`, and the exit code is 143 or 129 | `TestTermAndHangupJournalWhatRanAndExitCleanly` |
| stdin is read only when a prompt asks, a line is capped at 64 KiB, and endless input declines in bounded memory | `TestEndlessStdinIsBoundedAndDeclines`, `stdin_test.go` |
| Every confirmation says, in plain words, what the command does and whether undo can take it back, and the undo sentence follows the capture exactly; text mode, `--json` and the TUI show the same message | `TestConsentForADeletionIsPlainAndTrue`, `consent.go` |
| A command that uses administrator rights or reaches the network always asks, and says undo cannot take it back | `TestNetworkAndAdministratorCommandsAlwaysAsk`, `TestDetectRiskExtras` |
| A command that failed, followed by the model giving up, exits 1 (`gave_up`), not 4; a deleted working folder stops with a plain explanation | `TestGivingUpAfterAFailedCommandIsAFailure`, `TestDeletedWorkingFolderIsReported` |
| Session chrome (banner, `> `, prompts) goes to stderr; with piped input a confirmation declines without consuming the next task; "undo" typed in a session runs undo | `TestPipedREPLNeverSwallowsTheNextTask`, `TestUndoTypedInASessionRunsUndo` |
| A model reply cut off by the token cap is never parsed or run | `TestACutOffModelReplyIsNeverRun` |
| Results that cannot be written to stdout exit 1 with a diagnostic | `TestUnwritableStdoutIsAFailure` |
| A write through a symlink is undone in the file it reached, and the link is restored | `TestUndoThroughASymlinkRestoresTheTargetAndTheLink` |
| A command whose promised copy cannot be saved does not run (`capture_failed`); a command that changes synapse's own history or trash (`~/.synapse`) always asks and says undo cannot take it back | `TestACommandWhoseCopyFailsDoesNotRun`, `TestCommandsOnSynapsesOwnStateAskAndWarn` |
| After a yes, the command is checked again; if the folder changed while the person was deciding, it says so and asks again | `TestAChangeWhileDecidingIsAskedAgain` |
| When the undo history cannot be written, every command that changes files asks, saying undo is unavailable | `TestUnwritableJournalMakesChangesAsk` |
| Undo checks every saved copy exists before changing anything, stashes git edits before setting anything aside, and clears files added inside folders the command made; a step with nothing restorable is reported and dropped | `TestUndoWithAMissingCopyChangesNothing`, `TestUndoOfAGitResetStashesOnlyRealEdits`, `TestUndoOfACreatedFolderWithAddedFilesFinishes`, `TestUndoPlanSaysWhereAndWhetherAnythingCanComeBack` |
| A password in `SYNAPSE_OLLAMA` is never printed; numeric `SYNAPSE_KEEP_ALIVE` reaches Ollama as a number | `TestEndpointPasswordIsNeverPrinted`, `TestKeepAliveIsSentInTheTypeOllamaAccepts` |
| What a command was expected to create or move but did not is not journaled, so undo never reports a phantom "move back" as complete | `TestAFailedMoveLeavesNoPhantomUndo` |
| A command that empties a file it reads (`sort f > f`) asks with a plain warning, and a run that leaves a file empty says so and how to undo it | `TestSelfTruncationIsWarnedBeforeAndAfter` |
| Appends are described as adding, links are named with what they reach, and a URL or socket library inside an interpreter one-liner counts as network | `TestConsentNamesAppendsLinksAndEmbeddedNetwork` |
| A step that ran but could not be recorded for undo ends the task as `not_undoable` (exit 7: neither a failure nor a clean success), not `complete`; a study log that stops accepting events is reported once on stderr | `TestAStepThatCouldNotBeJournaledIsNotComplete`, `TestWriteFailureIsReportedOnce` |
| The help documents every variable and exit code the program has | `TestHelpDocumentsEveryVariableAndExitCode`; the environment section is printed from `envVars` |

The end-to-end tests run the real `main` (the test binary re-executes itself) against a fake Ollama, with the shipped default analysis (strict), and observe exit codes, both streams, and the files left behind.

## The `--json` stream

One object per line on stdout. `type` is always present; the other fields appear where that type sets them.

| `type` | Fields | Meaning |
|---|---|---|
| `command` | `step`, `command`, `tokens`, `latency_ms` | the model chose a command; it has not been checked or run yet |
| `approval` | `step`, `command`, `text` | the command needs confirmation; the prompt is read from stdin next |
| `result` | `step`, `command`, `stdout`, `stderr`, `exit_code`, `timed_out`, `not_run` | what the command did; `exit_code` is always present, including 0 |
| `answer` | `text` | the plain-language reply to the task |
| `notice` | `text` | something to read now |
| `problem` | `text`, `detail` | something went wrong: plain words, then the technical cause |
| `note` | `text` | bookkeeping, such as memory being condensed |
| `end` | `outcome`, `exit_code` | always last |

`outcome` is one of `complete`, `nothing_to_do`, `declined`, `unsupported`, `step_limit`, `propose_error`, `execution_error`, `repeated_failure`, `repeated_success`, `reversed_step`, `gave_up`, `capture_failed` (the copy undo needs could not be saved, so the command did not run), `not_undoable` (the task ran, but a step could not be recorded for undo), `no_working_directory`, `interrupted`, `model_unavailable` (the pre-flight check failed before a task started), or `incomplete` (a bug if ever seen). `repeated_success` exits 0 like `complete`: the model proposed the command it had just run instead of saying it was done, so it was not run again and the task was answered from what had run. It is kept apart so a study never counts it as a task the model finished. `reversed_step` is the same for a proposed command that would only move back what the step before it had moved. `gave_up` is a command that failed with no other to try (exit 1). The wire names of the event types live in `internal/loopevent` and do not depend on the order of its constants. The reply is generated as a stream, but `--json` writes only the `answer` record that carries all of it: the pieces (`answer_part` in `internal/loopevent`, which the TUI draws as they arrive) are left out, so a script never reads half a reply. A reply that breaks off writes no `answer` record, as when it failed before streaming.

## Known limits

- With `SYNAPSE_ANALYSIS=off`, the list alone decides, so a quoted `>` (as in `printf 'a>b'`) asks. With the analysis on, it runs (D49; `TestGateClearsOnlyTheListsTextScans`).
- A confirmation prompt waits on stdin. An agent that never answers waits, one that closes stdin gets a decline, and a final `y` without a newline counts as an answer.
