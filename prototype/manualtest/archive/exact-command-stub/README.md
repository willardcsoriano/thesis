## Overview

An archived test tool, kept for reference and not used by the current manual test suite. `run-exact.sh` runs one request through the real SynapseOS but replaces the language model with `stub_model.py`, a fixed responder that always returns a given command. That makes runs perfectly repeatable, but it skips the translation from plain English to a command, which is half of what the system does: it is "lost in translation". The current suite (`../../STORIES.md`) therefore lets the real local model write every command, and runs each request once with the analysis disabled and once with it enabled. Use this tool only when an exact command must be compared in isolation; it still exercises the real safety gate, backups, execution and undo journal.

## Table of Contents

- [Overview](#overview)
- [Usage](#usage)

## Usage

Run from inside the folder the command should act on: `run-exact.sh [--off] "<request>" '<command>'`. `--off` sets `SYNAPSE_ANALYSIS=off`. The displayed model name is `exact-command stub`, so a transcript never passes the stub off as the real model.
