# Forwards every target to prototype/Makefile, so `make tui` works from the repo root.
# Variables given on the command line pass through, e.g. make task TASK="list the files here".
.DEFAULT_GOAL := help

help:
	@echo "Forwarding to prototype/. Every synapse mode has its own target — none are hidden behind a curated subset:"
	@echo "  cli TASK=\"...\"   one-shot CLI mode (alias: task TASK=\"...\")"
	@echo "  repl              persistent, plain-text back-and-forth session"
	@echo "  tui               the bubbletea interface"
	@echo "  undo              undo the last recorded command"
	@echo "  run               propose-only demo suite (no filesystem changes)"
	@echo "Plus: build, test, ci, ollama-serve-bg, ollama-pull, ollama-status. See prototype/Makefile for the rest (pilot rounds, corpus generation, dependency hoarding)."

%:
	@$(MAKE) --no-print-directory -C prototype RUNDIR="$(CURDIR)" $@

.PHONY: help
