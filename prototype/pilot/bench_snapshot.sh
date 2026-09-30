#!/usr/bin/env bash
# Measures the cost of the Timeshift-style alternative to the capture plan: a full,
# hardlink-based snapshot of the working directory taken before every command, the way
# `rsync -a --link-dest=<previous> src new` (what Timeshift and rsnapshot do on a
# filesystem with no native snapshot, such as this machine's ext4) would if invoked
# before each command. No root is needed: this reproduces the mechanism directly
# rather than driving Timeshift itself.
#
# The directory measured is a copy of distro/hoard/debs — 1000+ real files, several
# hundred MB, standing in for a realistic project or Downloads folder a command might
# run in.
#
# Usage: bash pilot/bench_snapshot.sh | tee pilot/snapshot_bench.txt
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
src="${1:-$here/../distro/hoard/debs}"
[ -d "$src" ] || { echo "no directory at $src (run distro/hoard.sh --debs first)" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
live="$work/live"
cp -a "$src" "$live"
nfiles=$(find "$live" -type f | wc -l)
size=$(du -sh "$live" | cut -f1)
echo "working directory: $nfiles files, $size (a copy of distro/hoard/debs)"
echo

echo "-- first snapshot (no previous to link against; the worst case, paid once) --"
t0=$(date +%s.%N)
cp -a "$live" "$work/snap0"
t1=$(date +%s.%N)
b0=$(du -sb "$work/snap0" | cut -f1)
printf "  time: %.3fs   bytes written: %s\n\n" "$(echo "$t1 - $t0" | bc)" "$b0"

echo "-- steady-state snapshot: nothing has changed since the last one --"
echo "   this is the tax paid before every command once the system is running: the cost a"
echo "   hardlink-based snapshot cannot avoid even when the command that follows touches nothing"
for i in 1 2 3; do
  rm -rf "$work/snap_u"
  t0=$(date +%s.%N)
  rsync -a --link-dest="$work/snap0" "$live/" "$work/snap_u/"
  t1=$(date +%s.%N)
  printf "  run %d — time: %.3fs\n" "$i" "$(echo "$t1 - $t0" | bc)"
done
echo

echo "-- one file changed out of $nfiles (the realistic case: a command writes or removes one path) --"
victim=$(find "$live" -type f -print -quit)
echo "modified" >> "$victim"
t0=$(date +%s.%N)
rsync -a --link-dest="$work/snap0" "$live/" "$work/snap_c/"
t1=$(date +%s.%N)
changed=$(diff -rq "$work/snap0" "$work/snap_c" 2>/dev/null | wc -l || true)
printf "  time: %.3fs   (rsync itself reports %d path(s) actually differing)\n\n" "$(echo "$t1 - $t0" | bc)" "$changed"

echo "-- for comparison: the analysis's capture plan for single-file cases --"
echo "   from pilot/recovery_results6.txt: internal/gate.Decision.Capture touches only the"
echo "   path a command names, so its cost does not depend on the size of the directory around it"
if [ -f "$here/pilot/recovery_results6.txt" ]; then
  # Column count varies: a Recoverable row has no "plan" token, so every field after it
  # shifts left by one. Index from the right instead, where the layout is fixed:
  # ... capturedB entries fixtureB plan+capture_s command_s exit.
  awk '
    NF < 8 { next }
    { fixtureB = $(NF-3); capB = $(NF-5); planS = $(NF-2) }
    fixtureB !~ /^[0-9]+$/ { next }
    (fixtureB+0) > 0 && (fixtureB+0) < 200 && !done[$1] {
      print "     " $1 ": " planS "s plan+capture, " capB " bytes captured, " fixtureB " bytes fixture"
      done[$1] = 1; n++
    }
    n >= 5 { exit }
  ' "$here/pilot/recovery_results6.txt"
else
  echo "     (pilot/recovery_results6.txt not found; run 'make recovery5' or the corpus6 recovery check first)"
fi
echo
echo "Reading it: a snapshot taken before every command pays the cost of walking and stat'ing"
echo "the whole tree, whether nothing changed or one file did, because deciding what to"
echo "hardlink still means visiting every entry. The capture plan's cost is set by what the"
echo "command touches, not by how large the directory around it is. This is the trade the"
echo "capture plan is built to avoid, on a filesystem (ext4) with no cheaper primitive to"
echo "lean on; a copy-on-write filesystem such as btrfs would make the snapshot itself"
echo "closer to free and change which mechanism is worth building."
