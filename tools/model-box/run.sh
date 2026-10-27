#!/usr/bin/env bash
# Model feasibility run: is CPU (or whatever this machine has) fast enough to
# run the model-size comparison (docs/experiments.md, D46) in about a day?
# For each model it pulls the model, measures Ollama's own token rates, times
# a small sample of Layer 7's development tasks, and extrapolates the full
# comparison's time. The held-out set is never run here: its results must stay
# unseen until the comparison.
#
# Provider-agnostic: a Hetzner box set up by tools/verify-box/setup.sh, or any
# Debian or Ubuntu machine with the repository checked out, Go on PATH or in
# ~/.local/go, and the manifest's packages. It also reports whether bubblewrap
# works here, which decides whether the machine can run the sandboxed parts.
#
#   bash tools/model-box/run.sh                 # the D46 candidates
#   bash tools/model-box/run.sh MODEL...        # these models only
#   bash tools/model-box/run.sh --fresh ...     # forget earlier progress
#
# OLLAMA_URL names a running Ollama to use (default http://127.0.0.1:11434).
# If none answers there, Ollama's official archive is fetched (hoard.sh
# --ollama), unpacked under ~/.local/ollama, and served on that port for the
# run, then stopped by its PID. MODEL_BOX_SAMPLE names the development tasks
# timed (default U1,U4,U7,U10: a move, a count, a search, a count).
#
# Output in ~/model-box: preflight.txt, results.tsv (one row per model), and
# <model>.log with the raw output. Rerunning resumes: a model finished on the
# same commit is skipped. A second copy started while one runs refuses.
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel) || exit 1
out=~/model-box
mkdir -p "$out"
export PATH="$HOME/.local/go/bin:$HOME/.local/ollama/bin:$PATH" GOTOOLCHAIN=local
url=${OLLAMA_URL:-http://127.0.0.1:11434}
sample=${MODEL_BOX_SAMPLE:-U1,U4,U7,U10}
# The full comparison runs every (task, tier) pair this many times (Layer 7's
# default, D46).
full_repeats=3

exec 9> "$out/lock"
if ! flock -n 9; then
  echo "model-box is already running; see $out" >&2
  exit 1
fi

fresh=0
if [ "${1:-}" = --fresh ]; then fresh=1; shift; fi
models=("$@")
if [ ${#models[@]} -eq 0 ]; then
  # D46, docs/decisions.md. Each tag was checked against registry.ollama.ai on
  # 2026-10-23.
  models=(qwen2.5-coder:0.5b qwen2.5-coder:1.5b qwen2.5-coder:3b qwen2.5-coder:7b
    qwen2.5-coder:14b qwen2.5-coder:32b qwen3:1.7b qwen3:4b qwen3:8b qwen3-coder:30b)
fi

# The code a result came from: the commit, plus a fingerprint of any
# uncommitted changes (a checkout copied to the box before shipping), so a
# result is never filed under a commit whose code it did not run.
commit=$(git -C "$repo" rev-parse HEAD)
dirty=$( { git -C "$repo" diff HEAD --binary
  git -C "$repo" ls-files -o --exclude-standard -z | (cd "$repo" && xargs -0r sha256sum); } | sha256sum | cut -c1-12)
[ -n "$(git -C "$repo" status --porcelain)" ] && commit="$commit+$dirty"
done_file=$out/done
[ $fresh -eq 1 ] && : > "$done_file"
touch "$done_file"
if grep -qv "^$commit " "$done_file"; then : > "$done_file"; fi

results=$out/results.tsv
if [ ! -s "$results" ] || [ ! -s "$done_file" ]; then
  printf 'model\tsize_gb\tpull_s\tload_s\tprompt_tok_s\tgen_tok_s\tsample_runs\tsample_ok\tsample_capped\tmean_run_s\tfull_runs\test_full_h\tnote\n' > "$results"
fi

# --- Preflight: what this machine is, and whether the sandbox works here.
{
  echo "commit $commit"
  echo "date $(date -u +%FT%TZ)"
  echo "cpus $(nproc)"
  awk '/^MemTotal|^MemAvailable/ {printf "%s %d MB\n", $1, $2/1024}' /proc/meminfo
  echo "disk free $(df -BG --output=avail "$HOME" | tail -1 | tr -d ' ') in \$HOME"
  if command -v nvidia-smi >/dev/null; then echo "gpu $(nvidia-smi -L 2>&1 | head -3 | tr '\n' ';')"; else echo "gpu none (no nvidia-smi)"; fi
  if command -v bwrap >/dev/null && bwrap --ro-bind / / --unshare-all --die-with-parent true 2>/dev/null; then
    echo "bubblewrap works"
  else
    echo "bubblewrap DOES NOT work here: the sandboxed tests and model-bench.sh cannot run on this machine"
  fi
} > "$out/preflight.txt"
cat "$out/preflight.txt"

# --- Ollama: use one that answers, or start our own.
# shellcheck source=tools/model-box/ollama.sh
. "$here/ollama.sh"
echo "ollama $(curl -sf "$url/api/version" | jq -r .version) at $url"

# The full comparison's run count, from the corpus itself: every task in both
# sets, three tiers, full_repeats each.
cd "$repo/prototype" || exit 1
tasks=$(go test -count=1 -run 'TestLayer7Verifiers' -v ./cmd/synapse/ 2>/dev/null | grep -c -- '--- PASS: TestLayer7Verifiers/')
[ "$tasks" -gt 0 ] || { echo "could not count Layer 7's tasks (TestLayer7Verifiers did not pass)" >&2; exit 1; }
full_runs=$((tasks * 3 * full_repeats))
echo "full comparison: $tasks tasks x 3 tiers x $full_repeats repeats = $full_runs runs per model"

# Ollama keeps each model loaded for its keep_alive after the last request, so
# an earlier model would still hold memory when the next one's fit is checked.
unload_all() {
  local n
  for n in $(curl -sf "$url/api/ps" | jq -r '.models[].name'); do
    curl -sf "$url/api/generate" -d "{\"model\":\"$n\",\"keep_alive\":0}" >/dev/null
  done
  for _ in $(seq 30); do
    [ "$(curl -sf "$url/api/ps" | jq '.models | length')" = 0 ] && return 0
    sleep 1
  done
  echo "Ollama did not unload its models" >&2; return 1
}
mem_kb() { echo "${MODEL_BOX_MEM_AVAILABLE_KB:-$(awk '/^MemAvailable/ {print $2}' /proc/meminfo)}"; }
row() { printf '%s\n' "$(IFS=$'\t'; echo "$*")" >> "$results"; }

for m in "${models[@]}"; do
  if grep -qx "$commit $m" "$done_file"; then echo "== $m: done earlier on this commit"; continue; fi
  log="$out/$(echo "$m" | tr '/:' '__').log"
  : > "$log"
  echo "== $m"

  t0=$SECONDS
  if ! { curl -sf "$url/api/pull" -d "{\"model\":\"$m\",\"stream\":false}" && echo; } >> "$log" 2>&1; then
    row "$m" - - - - - - - - - - - "pull failed"; echo "$commit $m" >> "$done_file"; continue
  fi
  pull_s=$((SECONDS - t0))
  size_b=$(curl -sf "$url/api/tags" | jq -r --arg m "$m" '.models[] | select(.name == $m) | .size')
  size_gb=$(awk -v b="${size_b:-0}" 'BEGIN {printf "%.2f", b/1e9}')

  # Ollama needs the weights in memory, plus the context cache and runtime.
  # A model that cannot fit would swap or be killed, and time nothing real.
  need_kb=$(awk -v b="${size_b:-0}" 'BEGIN {printf "%d", b*1.2/1024}')
  unload_all || exit 1
  if [ "$(mem_kb)" -lt "$need_kb" ]; then
    row "$m" "$size_gb" "$pull_s" - - - - - - - - - "does not fit: needs about $((need_kb/1024)) MB, $(( $(mem_kb)/1024 )) MB available"
    echo "$commit $m" >> "$done_file"; continue
  fi

  # Ollama's own rates on a fixed request, with the options synapse uses
  # (temperature 0, an 8192-token context). The first call also loads the model.
  bench='{"model":"'"$m"'","prompt":"List every file in the current directory sorted by size, largest first, and explain the command in one sentence.","stream":false,"keep_alive":"15m","options":{"temperature":0,"num_ctx":8192,"num_predict":128}}'
  load_s=-; prompt_tps=-; gen_tps=-
  for i in 1 2 3; do
    if r=$(curl -sf "$url/api/generate" -d "$bench"); then
      echo "$r" | jq -c 'del(.context, .response)' >> "$log"
      [ "$i" -eq 1 ] && load_s=$(echo "$r" | jq -r '.load_duration/1e9 | . * 10 | round / 10')
      prompt_tps=$(echo "$r" | jq -r 'if .prompt_eval_duration > 0 then .prompt_eval_count / (.prompt_eval_duration/1e9) | round else "-" end')
      gen_tps=$(echo "$r" | jq -r 'if .eval_duration > 0 then .eval_count / (.eval_duration/1e9) * 10 | round / 10 else "-" end')
    fi
  done

  # Time the sample on the development set, one repeat.
  SYNAPSE_OLLAMA=$url SYNAPSE_MODEL=$m SYNAPSE_LAYER7_SET=development SYNAPSE_LAYER7_ONLY=$sample SYNAPSE_LAYER7_REPEATS=1 \
    go test -count=1 -tags live -timeout 6h -run 'TestLayer7UtteranceTiers' -v ./cmd/synapse/ >> "$log" 2>&1
  runs=$(grep -c 'RUN set=' "$log")
  if [ "$runs" -eq 0 ]; then
    row "$m" "$size_gb" "$pull_s" "$load_s" "$prompt_tps" "$gen_tps" 0 - - - - - "no Layer 7 run completed; see the log"
    echo "$commit $m" >> "$done_file"; continue
  fi
  read -r ok capped mean < <(grep 'RUN set=' "$log" | awk '
    { for (i = 1; i <= NF; i++) { split($i, kv, "="); v[kv[1]] = kv[2] }
      n++; s += v["secs"]; if (v["ok"] == "true") ok++; if (v["capped"] == "true") cap++ }
    END { printf "%d %d %.1f\n", ok, cap, s / n }')
  est_h=$(awk -v m="$mean" -v n="$full_runs" 'BEGIN {printf "%.1f", m * n / 3600}')
  note="estimate from $runs development runs"
  [ "$capped" -gt 0 ] && note="LOWER BOUND: $capped of $runs runs hit the per-run cap"
  row "$m" "$size_gb" "$pull_s" "$load_s" "$prompt_tps" "$gen_tps" "$runs" "$ok" "$capped" "$mean" "$full_runs" "$est_h" "$note"
  echo "$commit $m" >> "$done_file"
  echo "   $mean s a run; full comparison about $est_h h ($note)"
done

echo
column -t -s $'\t' "$results"
