#!/usr/bin/env bash
# Runs the slow verification of the checked-out branch on the verification
# box, one step after another (each uses every core). It logs each step's exit
# status and time to ~/verify.log, keeps each step's output in ~/<step>.out,
# and samples used memory every 2 s. Started detached by
# docs/testing-environment.md, so a dropped connection does not stop it.
#   bash run.sh SEED...           # fresh seeds, written down before the run
#   bash run.sh --fresh SEED...   # forget earlier progress and run every step
#
# Rerunning it resumes: a step that passed on the same commit is recorded in
# ~/verify.done and skipped; one that failed or was cut off runs again. A new
# commit, or --fresh, starts from the first step. A second copy started while
# one is running refuses to start rather than overwrite its outputs.
set -u
cd ~/thesis/prototype || exit 1
export PATH="$HOME/.local/go/bin:$PATH" GOTOOLCHAIN=local
out=pilot/out
mkdir -p "$out"
log=~/verify.log
done_file=~/verify.done

exec 9> ~/verify.lock
if ! flock -n 9; then
  echo "run.sh is already running; see ~/verify.log" >&2
  exit 1
fi

commit=$(git rev-parse HEAD)
if [ "${1:-}" = --fresh ]; then
  shift
  : > "$done_file"
fi
touch "$done_file"
# Progress from another commit says nothing about this one.
if grep -qv "^$commit " "$done_file"; then
  : > "$done_file"
fi
if [ -s "$done_file" ]; then
  echo "== resuming $(date -u +%FT%TZ): $(wc -l < "$done_file") steps already passed on this commit" >> "$log"
else
  : > "$log"
  : > ~/mem.samples
fi

# The sampler must not hold the lock: its sleep would outlive this script.
( while sleep 2; do free -m | awk '/^Mem:/ {print $3}'; done ) >> ~/mem.samples 9>&- &
sampler=$!
trap 'kill $sampler 2>/dev/null' EXIT

{
  echo "commit $commit"
  echo "nproc $(nproc)"
  free -g | sed -n 2p
  echo "seeds: $(sed -n 's/^COMPOSE_SEED ?= //p' Makefile) (the Makefile's) $* (fresh)"
} >> "$log"

step() {
  local name=$1; shift
  if grep -qx "$commit $name" "$done_file"; then
    echo "== $name skipped: passed earlier on this commit" >> "$log"
    return
  fi
  echo "== $name start $(date -u +%FT%TZ)" >> "$log"
  local t0=$SECONDS rc
  "$@" > ~/"$name".out 2>&1
  rc=$?
  echo "== $name exit $rc after $((SECONDS - t0))s" >> "$log"
  [ "$rc" -eq 0 ] && echo "$commit $name" >> "$done_file"
}

step ci env SYNAPSE_REQUIRE_SANDBOX=1 make ci
step compose-recorded make composecheck
for s in "$@"; do
  step "compose-$s" make composecheck COMPOSE_SEED="$s"
done
step rc7 env RC_NO_CURATED=1 python3 pilot/parallel_rc.py pilot/corpus7.jsonl "$out/recovery7-rerun.txt"
step listpilot7 sh -c "go run ./cmd/listpilot pilot/corpus7.jsonl > $out/classifier_out7-rerun.jsonl"

echo "peak used MB $(sort -n ~/mem.samples | tail -1)" >> "$log"
echo "== all done $(date -u +%FT%TZ)" >> "$log"
