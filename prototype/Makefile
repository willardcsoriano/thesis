.PHONY: build test run task cli repl scratch tui undo ollama-serve ollama-serve-bg ollama-pull ollama-status pilot pilot3 clean ci fmt-check vet corpus4 round4 deps-check deps-hoard

# Falls back to the user-local toolchain (no sudo required) if `go` isn't
# already on PATH — see ../setup.md.
GO := $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)

# The system Ollama service listens on the default port. If something else holds 11434
# (a VS Code utility process once did), run `OLLAMA_PORT=11435 make ollama-serve-bg` and use the same
# OLLAMA_PORT for the other targets.
OLLAMA_PORT ?= 11434
export SYNAPSE_OLLAMA ?= http://127.0.0.1:$(OLLAMA_PORT)

build:
	$(GO) build -o bin/synapse ./cmd/synapse

test:
	$(GO) test ./...

# Built-in sample suite (8 tasks, propose-only — no filesystem changes).
# The agent is homed in the directory it is launched from: it tells the model that
# is the working directory and runs every command there. RUNDIR is where the
# session starts. It defaults to wherever make was invoked, and the root Makefile
# passes its own directory through, so `make tui` from the repo root starts in the
# repo root rather than in prototype/. Override with RUNDIR=/some/folder.
RUNDIR ?= $(CURDIR)
SYNAPSE = "$(CURDIR)/bin/synapse"

# Not a fourth interface mode — the built-in 8-task sample suite, propose-only
# (no filesystem changes, no classification, no execution). A quality smoke
# test, not something a user runs to get work done; see cli/repl/tui for that.
run: build
	cd "$(RUNDIR)" && $(SYNAPSE)

# One ad-hoc task, e.g. make task TASK="find the 10 largest files under /var/log"
# — this is CLI mode (D19); `cli` is the same target under that mode's own name,
# since a target named only `task` doesn't read as "the CLI mode" to someone
# going by the mode names in interface-modes.md.
task: build
	@if [ -z "$(TASK)" ]; then \
		echo "Usage: make task TASK=\"<what you want done>\" (or: make cli TASK=\"...\")"; \
		exit 1; \
	fi
	cd "$(RUNDIR)" && $(SYNAPSE) "$(TASK)"

cli: task

repl: build
	cd "$(RUNDIR)" && $(SYNAPSE) repl

# Scratch mode (D43): REPL's persistent, plain-text loop with no memory
# between tasks — each one starts fresh, disposable, nothing to compact or
# forget.
scratch: build
	cd "$(RUNDIR)" && $(SYNAPSE) scratch

tui: build
	cd "$(RUNDIR)" && $(SYNAPSE) tui

undo: build
	./bin/synapse undo

# Runs in the foreground on OLLAMA_PORT, like `ollama serve` normally would.
ollama-serve:
	OLLAMA_HOST=127.0.0.1:$(OLLAMA_PORT) ollama serve

# Same, but detached — logs to /tmp so it survives closing this terminal.
ollama-serve-bg:
	OLLAMA_HOST=127.0.0.1:$(OLLAMA_PORT) nohup ollama serve > /tmp/synapse-ollama.log 2>&1 &
	@echo "started in background, logging to /tmp/synapse-ollama.log"

ollama-pull:
	OLLAMA_HOST=127.0.0.1:$(OLLAMA_PORT) ollama pull qwen2.5-coder:3b

ollama-status:
	@curl -s -m 3 http://127.0.0.1:$(OLLAMA_PORT)/api/tags && echo || echo "not reachable on port $(OLLAMA_PORT)"

# Re-runs the list-vs-need pilot (docs/algorithms.md, "Pilot"). Verifies the frozen
# corpus hash first, so an edited corpus fails loudly instead of quietly changing results.
pilot:
	cd pilot && sha256sum -c corpus.sha256
	$(GO) run ./cmd/listpilot pilot/corpus.jsonl > pilot/classifier_out.jsonl
	python3 pilot/analyze.py pilot/corpus.jsonl pilot/classifier_out.jsonl | tee pilot/results.txt

# The held-out round: verifies the corpus hash and that the analyser is still the frozen one.
pilot3:
	cd pilot && sha256sum -c corpus3.sha256
	python3 -c "import hashlib,pathlib,sys; h=hashlib.sha256(); [h.update(f.read_bytes()) for f in sorted(pathlib.Path('internal/effects').glob('*.go')) if not f.name.endswith('_test.go')]; want=open('pilot/analyser_frozen.sha256').read().strip(); sys.exit(0 if h.hexdigest()==want else 'analyser changed since it was frozen')"
	$(GO) run ./cmd/listpilot pilot/corpus3.jsonl > pilot/classifier_out3.jsonl
	python3 pilot/analyze.py pilot/corpus3.jsonl pilot/classifier_out3.jsonl > pilot/results_round3.txt

clean:
	rm -rf bin

# Sandboxed execute-and-diff proof that the effects capture plan restores the prior state.
# Output is kept in pilot/recovery_results.txt.
.PHONY: recoverycheck recovery5
recoverycheck:
	$(GO) run ./cmd/recoverycheck pilot/corpus2.jsonl | tee pilot/recovery_results.txt

# One command for "is everything still green": formatting, vet, tests. CI runs this.
fmt-check:
	@out=$$($(GO)fmt -l cmd internal 2>/dev/null || gofmt -l cmd internal); if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

ci: fmt-check vet test

# Dependencies. deps-check prints the one install line for anything missing;
# deps-hoard collects the debs, Go modules, toolchain, and models for the image build.
deps-check:
	@bash ../distro/check.sh

deps-hoard:
	bash ../distro/hoard.sh --all

# The automated corpus: ground truth from sandboxed execution, no human labels.
# SEED picks the draw; the same seed gives the same candidates. Round 4 is the
# development corpus; a fresh SEED gives a held-out one.
SEED ?= 20260922
corpus4:
	$(GO) run ./cmd/corpusgen -seed $(SEED) -n 450 -min-c 150 -tn 250 -ext 60 -out pilot/corpus4.jsonl -report pilot/oracle_report4.txt
	cd pilot && sha256sum corpus4.jsonl > corpus4.sha256

round4:
	cd pilot && sha256sum -c corpus4.sha256
	$(GO) run ./cmd/listpilot pilot/corpus4.jsonl > pilot/classifier_out4.jsonl
	python3 pilot/analyze.py pilot/corpus4.jsonl pilot/classifier_out4.jsonl > pilot/results_round4.txt
	@head -60 pilot/results_round4.txt

# The same check on a generated corpus (held-out round 5): only its commands, not the hand-written cases.
recovery5:
	RC_NO_CURATED=1 $(GO) run ./cmd/recoverycheck pilot/corpus5.jsonl > pilot/recovery_results5.txt
	@sed -n "/^SUMMARY/,/^FAILURES/p" pilot/recovery_results5.txt
