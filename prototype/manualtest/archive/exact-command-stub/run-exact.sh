#!/usr/bin/env bash
# Runs one request through the real synapse, with the model's answer fixed to an exact
# command, so the same command can be compared with the analysis off and on.
# Run it from inside the folder the command should act on.
#
#   run-exact.sh [--off] "<request>" '<command>'
#
# --off sets SYNAPSE_ANALYSIS=off (the pattern list alone). Everything except the model's
# answer is the real product: the safety gate, backups, execution, and the undo journal.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
analysis=""
if [ "${1:-}" = "--off" ]; then analysis=off; shift; fi
[ $# -eq 2 ] || { echo "usage: run-exact.sh [--off] \"<request>\" '<command>'" >&2; exit 2; }
portfile=$(mktemp)
python3 "$here/stub_model.py" "$portfile" "$2" &
stub=$!
trap 'kill $stub 2>/dev/null; rm -f "$portfile"' EXIT
for _ in $(seq 50); do [ -s "$portfile" ] && break; sleep 0.1; done
SYNAPSE_OLLAMA="http://127.0.0.1:$(cat "$portfile")" SYNAPSE_ANALYSIS="$analysis" SYNAPSE_MODEL="exact-command stub" \
  "$here/../../../bin/synapse" "$1"
