#!/usr/bin/env bash
# Stops the everyday desktop load start.sh started, and only that: the
# processes recorded in the state directory (checked against their recorded
# start time, so a reused PID is left alone), the members of their process
# groups, and processes carrying the run's token (lib.sh says why). Never by
# name, so another Chromium or Mousepad on the machine is not touched.
# Works when an app has already crashed: the rest are still stopped and the
# state directory removed.
#
#   tools/desktop-load/stop.sh
#
# Exit status: 0 stopped, or nothing was running; 1 a process survived KILL
# (state kept); 2 the state directory is not one start.sh made (left alone).
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=SCRIPTDIR/lib.sh
. "$here/lib.sh"

if [ ! -e "$DL_STATE" ]; then
  echo "desktop-load: not running (no state in $DL_STATE)"
  exit 0
fi
[ -e "$DL_MARKER" ] || DL_EXIT=2 dl_die "$DL_STATE exists but is not a desktop-load state directory; not touching it"
read -r up total < <(dl_leaders_up)
dl_stop_all || exit 1
echo "desktop-load: stopped ($up of $total apps were alive)"
