# Forwards every target to prototype/Makefile, so `make tui` works from the repo root.
# Variables given on the command line pass through, e.g. make task TASK="list the files here".
.DEFAULT_GOAL := help

help:
	@echo "Forwarding to prototype/. Common targets: tui, repl, task TASK=\"...\", build, test, ci, ollama-status"

%:
	@$(MAKE) --no-print-directory -C prototype RUNDIR="$(CURDIR)" $@

.PHONY: help
