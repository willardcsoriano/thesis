#!/usr/bin/env bash
# Reports whether the everyday desktop load is up, and how much memory it
# holds, as one line of key=value fields a timing harness can log next to
# each timing:
#
#   desktop-load state=up apps=2/2 processes=14 rss_kb=812345 pss_kb=401234
#
# state is up (every app alive), degraded (an app has exited; stop.sh still
# stops the rest) or down (no load recorded). rss_kb and pss_kb are summed
# over every process of the load, children included (lib.sh, dl_memory, says
# why both).
#
# Exit status: 0 up, 1 down, 2 degraded.
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=SCRIPTDIR/lib.sh
. "$here/lib.sh"

if [ ! -e "$DL_MARKER" ]; then
  echo "desktop-load state=down apps=0/0 processes=0 rss_kb=0 pss_kb=0"
  exit 1
fi
read -r up total < <(dl_leaders_up)
mapfile -t members < <(dl_members)
read -r rss pss < <(dl_memory "${members[@]}")
if [ "$total" -gt 0 ] && [ "$up" = "$total" ]; then state=up; code=0; else state=degraded; code=2; fi
echo "desktop-load state=$state apps=$up/$total processes=${#members[@]} rss_kb=$rss pss_kb=$pss"
exit $code
