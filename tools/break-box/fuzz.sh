#!/usr/bin/env bash
# Runs every fuzz target in cmd/synapse and internal/termsafe for a fixed time each, one after
# another, each using every core (docs/cli-breaking.md, Part 1). Logs each
# target's result to ~/fuzz.log; a failing input lands in
# cmd/synapse/testdata/fuzz/<Target>/ and becomes a permanent seed.
#   bash fuzz.sh [time-per-target, default 8m] [Target...]
set -u
cd ~/thesis/prototype || exit 1
export PATH="$HOME/.local/go/bin:$PATH" GOTOOLCHAIN=local
t=${1:-8m}; shift || true
targets=("$@")
# Each target is package:Name; the default is every Fuzz function found.
[ ${#targets[@]} -gt 0 ] || mapfile -t targets < <(grep -o '^func Fuzz[A-Za-z]*' cmd/synapse/*_test.go internal/termsafe/*_test.go | sed -E 's#^(.*)/[^/]*_test.go:func #./\1:#')
log=~/fuzz.log
echo "start $(date -Is) commit $(git rev-parse --short HEAD) cores $(nproc) targets ${targets[*]}" >> "$log"
for target in "${targets[@]}"; do
  pkg=${target%%:*} f=${target##*:}
  start=$(date +%s)
  go test "$pkg" -timeout 0 -run '^$' -fuzz "^${f}\$" -fuzztime "$t" > ~/"fuzz-$f.out" 2>&1
  rc=$?
  echo "$f exit=$rc secs=$(( $(date +%s) - start )) $(grep -o 'execs: [0-9]*' ~/"fuzz-$f.out" | tail -1)" >> "$log"
done
echo "all done $(date -Is)" >> "$log"
