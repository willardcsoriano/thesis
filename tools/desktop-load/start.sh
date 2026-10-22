#!/usr/bin/env bash
# Starts the everyday desktop load the model timings run under (Chapter 3,
# printed p. 54-55 and 78): Chromium with three local, script-heavy pages
# open as tabs, and Mousepad holding a text file. Offline and the same every
# run. Returns once the load is up and settled, after printing status.sh's
# line. Run inside the desktop session (DISPLAY set); stop with stop.sh.
#
#   tools/desktop-load/start.sh
#
# Environment:
#   DESKTOP_LOAD_STATE    state directory (default $XDG_RUNTIME_DIR/synapse-desktop-load)
#   DESKTOP_LOAD_TIMEOUT  seconds to wait for every page to load (default 120)
#   DESKTOP_LOAD_SETTLE   seconds to wait after that before returning (default 20)
#
# Exit status: 0 up; 1 failed to come up (everything it started is stopped
# again); 3 a load is already recorded in the state directory (nothing new is
# started: run stop.sh first).
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=SCRIPTDIR/lib.sh
. "$here/lib.sh"

timeout=${DESKTOP_LOAD_TIMEOUT:-120}
settle=${DESKTOP_LOAD_SETTLE:-20}
case "$timeout$settle" in *[!0-9]*) dl_die "DESKTOP_LOAD_TIMEOUT and DESKTOP_LOAD_SETTLE must be whole seconds" ;; esac

[ -n "${DISPLAY:-}" ] || dl_die "DISPLAY is not set: start the load inside the desktop session"
for prog in chromium mousepad setsid; do
  command -v "$prog" >/dev/null 2>&1 ||
    dl_die "$prog is not installed (distro/manifest.tsv lists it; bash distro/check.sh prints the install line)"
done

# Creating the directory is the lock: mkdir is atomic, so of two starts at
# once exactly one proceeds, and a second start never doubles the load.
mkdir -p -- "$(dirname -- "$DL_STATE")"
if ! mkdir -m 700 -- "$DL_STATE" 2>/dev/null; then
  read -r up total < <(dl_leaders_up)
  DL_EXIT=3 dl_die "a load is already recorded in $DL_STATE ($up of $total apps alive); run stop.sh first"
fi
: >"$DL_MARKER"
: >"$DL_PIDS"
token=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
printf '%s\n' "$token" >"$DL_TOKEN_FILE"
mkdir -p "$DL_STATE/xdg/config" "$DL_STATE/xdg/cache" "$DL_STATE/xdg/data" "$DL_STATE/chromium" "$DL_STATE/log"

fail() {
  echo "desktop-load: $*; stopping what was started" >&2
  # The logs go with the state directory, so show their ends first.
  local log
  for log in "$DL_STATE"/log/*.log; do
    [ -s "$log" ] || continue
    echo "--- last lines of $(basename "$log")" >&2
    tail -n 15 -- "$log" >&2
  done
  dl_stop_all >&2
  exit 1
}
trap 'fail "interrupted"' INT TERM HUP

# launch APP CMD... starts CMD as the leader of a new session and process
# group, with the run's token and a private, empty set of XDG directories, so
# no setting or history from an earlier run (or the user's own) changes what
# the load does. Its PID is recorded before anything else happens, so a
# failure from here on still stops it.
launch() {
  local app=$1; shift
  # No job control in a script, so the background child is not a group leader
  # and setsid runs CMD in place: $! is the app's PID.
  env DESKTOP_LOAD_TOKEN="$token" \
    XDG_CONFIG_HOME="$DL_STATE/xdg/config" XDG_CACHE_HOME="$DL_STATE/xdg/cache" XDG_DATA_HOME="$DL_STATE/xdg/data" \
    setsid "$@" </dev/null >"$DL_STATE/log/$app.log" 2>&1 &
  local pid=$!
  # Until setsid has run, the child's group is still this script's; wait for
  # it to lead its own group, so the group recorded is the app's.
  local i
  for ((i = 0; i < 50; i++)); do
    dl_stat "$pid" || break
    [ "$DL_PGID" = "$pid" ] && break
    sleep 0.1
  done
  dl_stat "$pid" || fail "$app exited at once (log: $DL_STATE/log/$app.log)"
  echo "$pid $DL_START $app leader" >>"$DL_PIDS"
}

urls=()
for page in "${DL_PAGES[@]}"; do urls+=("file://$here/pages/$page.html"); done

# Chromium flags, each for repeatability:
#   --user-data-dir             a fresh profile every run, and its own browser
#                               process rather than a tab in a running Chromium
#   --no-first-run ... --disable-component-update
#                               no welcome tab, no default-browser bar, and no
#                               network traffic of the browser's own (updates,
#                               sync, safe browsing lists): the load is offline
#   --disable-background-timer-throttling --disable-renderer-backgrounding
#   --disable-backgrounding-occluded-windows
#                               a background tab or a covered window keeps its
#                               timers, so the same work runs whatever is in
#                               front, including the fullscreen SynapseOS
#                               terminal; without them the load would depend on
#                               the window stacking of the moment
#   --enable-logging=stderr --v=0
#                               the pages' console lines reach the log, which is
#                               how start.sh sees each page finish loading
#   --password-store=basic      no keyring prompt in a fresh profile
launch chromium chromium \
  --user-data-dir="$DL_STATE/chromium" \
  --no-first-run --no-default-browser-check --disable-default-apps --disable-sync \
  --disable-background-networking --disable-component-update \
  --disable-background-timer-throttling --disable-renderer-backgrounding \
  --disable-backgrounding-occluded-windows \
  --enable-logging=stderr --v=0 --password-store=basic \
  "${urls[@]}"

# --disable-server: Mousepad otherwise hands the file to a Mousepad already
# running in the session over D-Bus and exits, and the load would own no
# process. GSETTINGS_BACKEND=memory gives it default settings every run;
# NO_AT_BRIDGE=1 keeps it from starting an accessibility bus of its own.
launch mousepad env GSETTINGS_BACKEND=memory NO_AT_BRIDGE=1 mousepad --disable-server "$here/pages/notes.txt"

# Up means: every app's process alive, and every page's load handler has run
# (each page logs DESKTOP_LOAD_READY <name> from it, after building its
# content). Mousepad has no such signal; it must still be alive at the end of
# the settle period, which is longer than it takes to open a file.
deadline=$((SECONDS + timeout))
while :; do
  read -r up total < <(dl_leaders_up)
  [ "$up" = "$total" ] || fail "an app exited while starting (logs: $DL_STATE/log/)"
  missing=()
  for page in "${DL_PAGES[@]}"; do
    grep -q "\"DESKTOP_LOAD_READY $page\"" "$DL_STATE/log/chromium.log" 2>/dev/null || missing+=("$page")
  done
  [ ${#missing[@]} -eq 0 ] && break
  [ $SECONDS -lt $deadline ] || fail "pages not loaded after ${timeout}s: ${missing[*]} (log: $DL_STATE/log/chromium.log)"
  sleep 0.5
done

# Settle: a browser's first seconds are not its steady state. Starting the
# renderers, compiling the pages' scripts, laying out the long article and the
# first garbage collections all cost CPU that an everyday desktop, open for a
# while, no longer spends. Waiting a fixed period after the last page loads
# keeps that start-up burst out of the first timing, and keeps it the same on
# every tier: a fixed wait rather than "until CPU is quiet", because the pages
# never go quiet by design.
sleep "$settle"
read -r up total < <(dl_leaders_up)
[ "$up" = "$total" ] || fail "an app exited during the settle period (logs: $DL_STATE/log/)"

# Record every process the load now has, so stop.sh and status.sh name them
# even if a leader later dies.
mapfile -t members < <(dl_members)
for p in "${members[@]}"; do
  grep -q "^$p " "$DL_PIDS" && continue
  dl_stat "$p" && echo "$p $DL_START - member" >>"$DL_PIDS"
done
trap - INT TERM HUP
"$here/status.sh"
