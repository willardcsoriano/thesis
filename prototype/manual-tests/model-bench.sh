#!/bin/bash
# Benchmarks a candidate Ollama model against two things synapse actually has
# to be good at: proposing correct commands for real tasks (the built-in
# sample suite, propose-only — see main.go's sampleSuite), and handling the
# off-topic, conversational, novelty-driven input a bored or curious human
# types at a chat-shaped tool instead of a real task (the list below, drawn
# from an actual live session — see manual-tests/README.md's convention on
# testing the realistic path, not a synthetic one).
#
# Usage: SYNAPSE_OLLAMA=http://127.0.0.1:11435 manual-tests/model-bench.sh <model-name>
#
# Writes manual-tests/bench-results/<model-name>.md (gitignored — regenerate
# per run, never a committed artifact, same convention as
# live_classifier_report.json). Run it once per candidate model and diff the
# reports by hand.
set -euo pipefail

MODEL="${1:?usage: model-bench.sh <model-name>}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTDIR="$ROOT/manual-tests/bench-results"
mkdir -p "$OUTDIR"
REPORT="$OUTDIR/$(echo "$MODEL" | tr '/:' '__').md"

cd "$ROOT"
go build -o bin/synapse ./cmd/synapse

bash playground/seed.sh >/dev/null
SANDBOX_HOME="$ROOT/playground/sandbox/home/user"

# Human-mimicking suite: what a real first-time user actually types when
# poking at a conversational tool out of curiosity or boredom, not working
# from a task list. Categories, in order: greeting, meta/identity question,
# capability question, novelty request, two off-topic emotional asks (the
# exact prompts that triggered the 34s UNSUPPORTED latency investigation),
# a philosophical/novelty probe, and one genuine task phrased the way a
# person actually talks (informal, contractions, no absolute path) rather
# than the clean imperative style of the built-in sample suite.
HUMAN_TASKS=(
  "hello"
  "are you a robot?"
  "what can you do?"
  "tell me a joke"
  "i feel sad. can i tell a story?"
  "me and my girlfriend broke up. can you comfort me?"
  "are you conscious?"
  "hey can u check how much space the downloads folder here is using"
)

{
  echo "# Model bench: $MODEL"
  echo
  echo "Run: $(date -Iseconds)"
  echo
  echo "## Product tasks (built-in sample suite, propose-only, no execution)"
  echo
  echo '```'
} > "$REPORT"

SYNAPSE_MODEL="$MODEL" ./bin/synapse >> "$REPORT" 2>&1

{
  echo '```'
  echo
  echo "## Human-mimicking suite (real execute path, isolated sandbox, no stdin — anything irreversible auto-declines)"
  echo
} >> "$REPORT"

for task in "${HUMAN_TASKS[@]}"; do
  {
    echo "### \"$task\""
    echo
    echo '```'
  } >> "$REPORT"
  start=$(date +%s.%N)
  ( cd "$SANDBOX_HOME" && SYNAPSE_MODEL="$MODEL" "$ROOT/bin/synapse" "$task" < /dev/null ) >> "$REPORT" 2>&1 || true
  end=$(date +%s.%N)
  {
    echo '```'
    printf 'wall time: %.1fs\n\n' "$(echo "$end - $start" | bc)"
  } >> "$REPORT"
  # Reseed between tasks: a prior task may have moved/created files the
  # next one shouldn't see, and each task should face the same starting
  # state a fresh user session would.
  bash "$ROOT/playground/seed.sh" >/dev/null
done

echo "report: $REPORT"
