#!/usr/bin/env bash
# Timing on one hardware tier (D46): on a server the size of the tier, CPU
# only, runs tools/model-box/run.sh for the given models twice, first with the
# machine idle and then under the everyday desktop load (tools/desktop-load)
# in a headless X display, and keeps both. The paper times each model "under
# everyday desktop load" (Chapter 3, printed p. 54-55 and 78); the idle run
# shows what the load costs. model-box already skips a model that does not fit
# the memory available, so under the load a model that fits idle may not.
#
#   bash tools/tier-box/run.sh MODEL...
#
# Run it on the tier server, for example through tools/rent/rent.sh with
# RENT_TYPE set to the tier and RENT_FETCH=tier-box. It installs the
# desktop packages setup.sh leaves out (chromium, mousepad, xauth) with sudo.
#
# Output in ~/tier-box/<UTC time>/: idle/ and load/, each holding model-box's
# preflight.txt, results.tsv and per-model logs; load/desktop-load.txt holds
# status.sh's line from before and after the timed run, so a load that fell
# over during it shows.
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel) || exit 1

# The timed run under the load, inside the X display xvfb-run made.
if [ "${1:-}" = --inside-load ]; then
  dir=$2
  shift 2
  # The load's state (Chromium's profile among it) stays out of the results:
  # it is rewritten all through the run, so a copy taken meanwhile is torn.
  DESKTOP_LOAD_STATE=$(mktemp -d) || exit 1
  export DESKTOP_LOAD_STATE
  "$repo/tools/desktop-load/start.sh" > "$dir/desktop-load.txt" 2>&1 || {
    echo "the desktop load did not come up:" >&2
    cat "$dir/desktop-load.txt" >&2
    exit 1
  }
  rm -rf ~/model-box
  bash "$repo/tools/model-box/run.sh" --fresh "$@"
  rc=$?
  "$repo/tools/desktop-load/status.sh" >> "$dir/desktop-load.txt" 2>&1
  "$repo/tools/desktop-load/stop.sh" >> "$dir/desktop-load.txt" 2>&1
  rm -rf "$DESKTOP_LOAD_STATE"
  cp -a ~/model-box/. "$dir/"
  exit $rc
fi

models=("$@")
[ ${#models[@]} -gt 0 ] || { echo "name at least one model" >&2; exit 2; }
out=~/tier-box/$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$out/idle" "$out/load"

# xauth is what xvfb-run needs to set up the display; the first tier run
# (2026-10-27) failed its load half without it.
if ! command -v chromium >/dev/null || ! command -v mousepad >/dev/null || ! command -v xauth >/dev/null; then
  sudo -n apt-get install -y -qq chromium mousepad xauth >/dev/null ||
    { echo "could not install chromium, mousepad and xauth" >&2; exit 1; }
fi
command -v xvfb-run >/dev/null || { echo "xvfb-run is not installed (distro/manifest.tsv lists xvfb)" >&2; exit 1; }

echo "== idle"
rm -rf ~/model-box
bash "$repo/tools/model-box/run.sh" --fresh "${models[@]}"
idle_rc=$?
cp -a ~/model-box/. "$out/idle/"

echo "== under the desktop load"
xvfb-run -a -s "-screen 0 1920x1080x24" bash "$0" --inside-load "$out/load" "${models[@]}"
load_rc=$?

echo
for run in idle load; do
  echo "-- $run"
  column -t -s $'\t' "$out/$run/results.tsv" 2>/dev/null
done
[ $idle_rc -eq 0 ] && [ $load_rc -eq 0 ]
