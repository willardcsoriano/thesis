#!/usr/bin/env bash
# Layers 7 and 9 on a rented box, split across parallel shards: each model's
# development tasks and conversations are divided between SHARDS copies of the
# test binary, all talking to one Ollama that serves that many requests at once,
# and the shards' lines are merged into one summary per model and layer. The
# laptop is never used for these runs (it has 4 shared threads; a two-model
# round took about 1.5 hours there).
#
#   bash tools/layer-box/run.sh MODEL...          # development sets, Layers 7 and 9
#   LAYER_BOX_SHARDS=4 LAYER_BOX_LAYERS="7" ...    # shard count, layers to run
#   LAYER_BOX_AIDS="none peek tools refs all" ...  # one run per SYNAPSE_AIDS setting
#
# With LAYER_BOX_AIDS, each model runs once per setting, on this one box,
# against this one Ollama and the same pulled model, so the aid is the only
# thing that varies between runs (docs/decisions.md, D52). Without it, runs use
# synapse's default (every aid on), as before.
#
# The held-out sets are not run unless LAYER_BOX_SET=heldout is given: they are
# for the model comparison, and a result seen while tuning is no longer held out.
# Splitting changes nothing a run sees: every (task, tier, repeat) still gets a
# fresh fixture, and only the order across processes differs.
#
# Set up the box with tools/verify-box/setup.sh first. Output goes to
# ~/layer-box/<UTC time>/: preflight.txt, one log per model, layer and shard,
# and summary.txt.
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel) || exit 1
export PATH="$HOME/.local/go/bin:$HOME/.local/ollama/bin:$PATH" GOTOOLCHAIN=local
url=${OLLAMA_URL:-http://127.0.0.1:11434}
shards=${LAYER_BOX_SHARDS:-4}
layers=${LAYER_BOX_LAYERS:-7 9}
aids_list=${LAYER_BOX_AIDS:-default}
set_name=${LAYER_BOX_SET:-development}
models=("$@")
[ ${#models[@]} -gt 0 ] || { echo "name at least one model" >&2; exit 2; }
out=~/layer-box/$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$out"

exec 9> ~/layer-box/lock
flock -n 9 || { echo "layer-box is already running" >&2; exit 1; }

# The code the results came from, with a fingerprint of uncommitted changes,
# as tools/model-box/run.sh records it.
commit=$(git -C "$repo" rev-parse HEAD)
dirty=$( { git -C "$repo" diff HEAD --binary
  git -C "$repo" ls-files -o --exclude-standard -z | (cd "$repo" && xargs -0r sha256sum); } | sha256sum | cut -c1-12)
[ -n "$(git -C "$repo" status --porcelain)" ] && commit="$commit+$dirty"
{
  echo "commit $commit"
  echo "date $(date -u +%FT%TZ)"
  echo "cpus $(nproc)"
  awk '/^MemTotal/ {printf "memory %d MB\n", $2/1024}' /proc/meminfo
  echo "shards $shards; layers $layers; set $set_name; aids $aids_list"
  if bwrap --ro-bind / / --unshare-all --die-with-parent true 2>/dev/null; then echo "bubblewrap works"; else echo "bubblewrap DOES NOT work: the analysis cannot resolve run-time targets here"; fi
} | tee "$out/preflight.txt"

# One server for every shard, answering as many requests at once as there are
# shards. Only a server started here gets that setting.
export OLLAMA_NUM_PARALLEL=$shards
# shellcheck source=tools/model-box/ollama.sh
. "$repo/tools/model-box/ollama.sh"
echo "ollama $(curl -sf "$url/api/version" | jq -r .version) at $url" | tee -a "$out/preflight.txt"

cd "$repo/prototype/cmd/synapse" || exit 1
bin=$out/synapse.test
go test -tags live -c -o "$bin" . || { echo "the test binary did not build" >&2; exit 1; }

# The ids come from the verifier checks, which list every task and conversation
# by set, so nothing here names one.
ids() { # layer
  local test=TestLayer7Verifiers
  [ "$1" = 9 ] && test=TestLayer9Verifiers
  "$bin" -test.run "^$test\$" -test.v 2>/dev/null | sed -n "s|.*--- PASS: $test/$set_name/\\([A-Za-z0-9]*\\).*|\\1|p"
}

for m in "${models[@]}"; do
  echo "== $m: pulling"
  curl -sf "$url/api/pull" -d "{\"model\":\"$m\",\"stream\":false}" >/dev/null || { echo "could not pull $m" >&2; continue; }
  # A tag can be moved to other weights upstream; the digest names what ran.
  echo "model $m digest $(curl -sf "$url/api/tags" | jq -r --arg m "$m" '.models[] | select(.name == $m) | .digest')" | tee -a "$out/preflight.txt"
  for aids in $aids_list; do
  # "default" leaves SYNAPSE_AIDS unset: synapse's own default, every aid on.
  aids_env=()
  [ "$aids" = default ] || aids_env=("SYNAPSE_AIDS=$aids")
  tag=${m//[:\/]/_}-aids-${aids//,/+}
  for layer in $layers; do
    mapfile -t all < <(ids "$layer")
    [ ${#all[@]} -gt 0 ] || { echo "no $set_name ids for Layer $layer" >&2; continue; }
    test=TestLayer7UtteranceTiers; only=SYNAPSE_LAYER7_ONLY; setvar=SYNAPSE_LAYER7_SET
    [ "$layer" = 9 ] && { test=TestLayer9Conversations; only=SYNAPSE_LAYER9_ONLY; setvar=SYNAPSE_LAYER9_SET; }
    started=$(date +%s)
    pids=()
    for ((s = 0; s < shards; s++)); do
      part=()
      for ((i = s; i < ${#all[@]}; i += shards)); do part+=("${all[i]}"); done
      [ ${#part[@]} -gt 0 ] || continue
      ids_csv=$(IFS=,; echo "${part[*]}")
      env SYNAPSE_MODEL="$m" SYNAPSE_OLLAMA="$url" "$setvar=$set_name" "$only=$ids_csv" "${aids_env[@]}" \
        "$bin" -test.run "^$test\$" -test.v -test.timeout 3h > "$out/l$layer-$tag-shard$s.log" 2>&1 &
      pids+=($!)
    done
    for p in "${pids[@]}"; do wait "$p"; done
    echo "== $m aids=$aids Layer $layer: $(( $(date +%s) - started )) s over ${#pids[@]} shards" | tee -a "$out/summary.txt"
    python3 "$here/merge.py" "$layer" "$out"/l"$layer"-"$tag"-shard*.log | tee -a "$out/summary.txt"
  done
  done
done
echo "all done" | tee -a "$out/summary.txt"
