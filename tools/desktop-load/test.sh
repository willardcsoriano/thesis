#!/usr/bin/env bash
# shellcheck disable=SC2016 # checke's expressions are expanded by eval, later
# Tests start.sh, stop.sh and status.sh against a real Chromium and Mousepad
# on a headless X display (Xvfb), observing the processes they leave behind
# rather than what the scripts say about themselves.
#
#   bash tools/desktop-load/test.sh
#
# Without chromium, mousepad, setsid or xvfb-run it skips, saying which is
# missing, and exits 0; with SYNAPSE_REQUIRE_SANDBOX=1 (the repository's switch
# for "this host must have the tools", internal/testenv) a missing one fails.
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

missing=()
for prog in chromium mousepad setsid xvfb-run; do
  command -v "$prog" >/dev/null 2>&1 || missing+=("$prog")
done
if [ ${#missing[@]} -gt 0 ]; then
  msg="desktop-load test: ${missing[*]} not installed (distro/manifest.tsv; bash distro/check.sh prints the install line)"
  if [ "${SYNAPSE_REQUIRE_SANDBOX:-}" = 1 ]; then
    echo "FAIL: $msg, and SYNAPSE_REQUIRE_SANDBOX=1 says this host must have them"
    exit 1
  fi
  echo "SKIP: $msg"
  exit 0
fi

# Always on a private Xvfb display, never the desktop the test runs from.
if [ "${DL_TEST_INNER:-}" != 1 ]; then
  exec env DL_TEST_INNER=1 xvfb-run -a -s "-screen 0 1280x800x24" bash "${BASH_SOURCE[0]}"
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/desktop-load-test.XXXXXX")
export DESKTOP_LOAD_STATE=$work/state DESKTOP_LOAD_SETTLE=2 DESKTOP_LOAD_STOP_GRACE=10
# shellcheck source=SCRIPTDIR/lib.sh
. "$here/lib.sh"

failures=0
ok() { echo "ok   - $*"; }
no() { echo "FAIL - $*"; failures=$((failures + 1)); }
check() { local d=$1; shift; if "$@"; then ok "$d"; else no "$d"; fi; }
# checke DESCRIPTION EXPRESSION: as check, for a compound shell expression.
checke() { if eval "$2"; then ok "$1"; else no "$1"; fi; }

decoys=()
cleanup() {
  "$here/stop.sh" >/dev/null 2>&1
  [ ${#decoys[@]} -gt 0 ] && kill "${decoys[@]}" 2>/dev/null
  wait 2>/dev/null
  rm -rf -- "$work"
}
trap cleanup EXIT

# Decoys: an unrelated sleep, and an unrelated Mousepad, the same program the
# load starts. A stop that matched by name would take them down.
sleep 600 &
decoys+=($!)
printf 'decoy\n' >"$work/decoy.txt"
env XDG_CONFIG_HOME="$work/decoy-xdg" GSETTINGS_BACKEND=memory NO_AT_BRIDGE=1 \
  mousepad --disable-server "$work/decoy.txt" >/dev/null 2>&1 &
decoys+=($!)
sleep 2
decoys_alive() { local d; for d in "${decoys[@]}"; do dl_stat "$d" || return 1; done; }
check "decoy sleep and decoy mousepad are running before any start" decoys_alive

# browsers: how many Chromium browser processes use the load's profile (a
# renderer or helper carries --type=, the browser process does not). Chromium
# rewrites its command line into one space-separated string, so arguments are
# matched as substrings, not as NUL-separated words.
browsers() {
  local f c n=0
  for f in /proc/[0-9]*/cmdline; do
    c=$(tr '\0' ' ' 2>/dev/null <"$f") || continue
    [[ $c == *"--user-data-dir=$DL_STATE/chromium"* ]] || continue
    [[ $c == *"--type="* ]] && continue
    n=$((n + 1))
  done
  echo "$n"
}

# assert_stopped TOKEN SAVED_PIDS: nothing of the stopped load survives, by
# three independent looks, and the decoys do.
assert_stopped() {
  local token=$1 saved=$2 pid start _ alive=()
  while read -r pid start _; do
    dl_is "$pid" "$start" && alive+=("$pid")
  done <"$saved"
  check "no recorded PID is alive after stop (${alive[*]:-none})" [ ${#alive[@]} -eq 0 ]
  local tagged
  tagged=$(grep -lzx -- "DESKTOP_LOAD_TOKEN=$token" /proc/[0-9]*/environ 2>/dev/null | tr '\n' ' ')
  check "no process carries the run's token after stop (${tagged:-none})" [ -z "$tagged" ]
  check "no Chromium browser on the load's profile after stop" [ "$(browsers)" = 0 ]
  check "state directory removed" [ ! -e "$DL_STATE" ]
  check "decoy sleep and decoy mousepad survive stop" decoys_alive
}

# field KEY LINE prints KEY's value from a status line.
field() { local kv; for kv in $2; do [ "${kv%%=*}" = "$1" ] && echo "${kv#*=}"; done; }

# --- With nothing running.
out=$("$here/stop.sh" 2>&1); code=$?
checke "stop with no state exits 0 and says not running" '[ "$code" = 0 ] && [[ $out == *"not running"* ]]'
out=$("$here/status.sh" 2>&1); code=$?
checke "status with no state exits 1 and says state=down" '[ "$code" = 1 ] && [[ $out == *"state=down"* ]]'
out=$(env -u DISPLAY "$here/start.sh" 2>&1); code=$?
checke "start without DISPLAY fails and leaves no state" '[ "$code" = 1 ] && [ ! -e "$DL_STATE" ]'

mkdir -p "$work/foreign" && : >"$work/foreign/keep"
out=$(DESKTOP_LOAD_STATE=$work/foreign "$here/stop.sh" 2>&1); code=$?
checke "stop refuses a directory start.sh did not make, and leaves it" '[ "$code" = 2 ] && [ -e "$work/foreign/keep" ]'

out=$(DESKTOP_LOAD_SETTLE=soon "$here/start.sh" 2>&1); code=$?
checke "start refuses a settle time that is not whole seconds, and leaves no state" '[ "$code" = 1 ] && [ ! -e "$DL_STATE" ]'

# referrers: processes whose command line names the state directory (the
# browser's profile, the crash handler's database, Mousepad's file is not in
# it). Used where the token is gone with the state.
referrers() {
  local f c n=0
  for f in /proc/[0-9]*/cmdline; do
    c=$(tr '\0' ' ' 2>/dev/null <"$f") || continue
    [[ $c == *"$DL_STATE/"* ]] && n=$((n + 1))
  done
  echo "$n"
}

# --- Run 0: pages that cannot load in time. A zero timeout fails at the first
# look, while Chromium is still starting: start must stop what it started.
out=$(DESKTOP_LOAD_TIMEOUT=0 "$here/start.sh" 2>&1); code=$?
checke "start that times out exits 1 and says which pages did not load" '[ "$code" = 1 ] && [[ $out == *"pages not loaded"* ]]'
check "a start that timed out leaves no process naming its state directory" [ "$(referrers)" = 0 ]
check "a start that timed out removes its state directory" [ ! -e "$DL_STATE" ]
check "decoy sleep and decoy mousepad survive a failed start" decoys_alive

# --- Run 1: two starts at once, status, the pages' work, a third start, a clean stop.
"$here/start.sh" >"$work/s1.out" 2>&1 & p1=$!
"$here/start.sh" >"$work/s2.out" 2>&1 & p2=$!
wait $p1; c1=$?
wait $p2; c2=$?
codes=$(printf '%s\n' "$c1" "$c2" | sort | tr '\n' ' ')
check "of two concurrent starts one succeeds and one refuses with 3 (got $codes)" [ "$codes" = "0 3 " ]
[ "$codes" = "0 3 " ] || { cat "$work/s1.out" "$work/s2.out"; exit 1; }

st=$("$here/status.sh"); code=$?
echo "     status: $st"
checke "status exits 0 with state=up apps=2/2" '[ "$code" = 0 ] && [ "$(field state "$st")" = up ] && [ "$(field apps "$st")" = 2/2 ]'
check "status counts more than the two leaders (children included)" [ "$(field processes "$st")" -gt 2 ]
check "status reports nonzero rss_kb" [ "$(field rss_kb "$st")" -gt 0 ]
checke "status reports nonzero pss_kb, no more than rss_kb" '[ "$(field pss_kb "$st")" -gt 0 ] && [ "$(field pss_kb "$st")" -le "$(field rss_kb "$st")" ]'
check "exactly one Chromium browser on the load's profile" [ "$(browsers)" = 1 ]
check "while up, processes name the state directory (so run 0's zero is a real zero)" [ "$(referrers)" -gt 0 ]
for page in "${DL_PAGES[@]}"; do
  check "page $page ran its load handler" grep -q "\"DESKTOP_LOAD_READY $page\"" "$DL_STATE/log/chromium.log"
done
dead=()
while read -r pid start _ kind; do
  [ "$kind" = leader ] || continue
  dl_is "$pid" "$start" || dead+=("$pid")
done <"$DL_PIDS"
check "every recorded leader is alive (dead: ${dead[*]:-none})" [ ${#dead[@]} -eq 0 ]

# The pages keep working: the load's CPU time grows while nothing else is
# asked of it. 10 ticks (0.1 s of CPU) in 3 s is a floor any of the three
# pages' timers clears alone; an idle browser does not.
mapfile -t members < <(dl_members)
t0=$(dl_cpu_ticks "${members[@]}"); sleep 3
mapfile -t members < <(dl_members)
t1=$(dl_cpu_ticks "${members[@]}")
check "the load keeps doing work: $((t1 - t0)) CPU ticks in 3 s (want >= 10)" [ $((t1 - t0)) -ge 10 ]

cp "$DL_PIDS" "$work/pids.before"
# shellcheck disable=SC2034 # read by checke's eval
out=$("$here/start.sh" 2>&1); code=$?
checke "a second start refuses with 3 and says a load is already recorded" '[ "$code" = 3 ] && [[ $out == *"already recorded"* ]]'
check "a second start changes no recorded PID" cmp -s "$DL_PIDS" "$work/pids.before"
check "a second start starts no second browser" [ "$(browsers)" = 1 ]

token=$(dl_token); cp "$DL_PIDS" "$work/pids.run1"
"$here/stop.sh" >"$work/stop.run1.out" 2>&1; code=$?
check "stop exits 0" [ "$code" = 0 ]
assert_stopped "$token" "$work/pids.run1"

# crash_run APP: start, kill APP's leader with KILL as a crash would, then
# stop must still stop everything else.
crash_run() {
  local app=$1 pid start a kind leader="" lstart=""
  if ! "$here/start.sh" >"$work/start.$app.out" 2>&1; then
    no "start for the $app crash run"; cat "$work/start.$app.out"; return
  fi
  while read -r pid start a kind; do
    [ "$a" = "$app" ] && [ "$kind" = leader ] && { leader=$pid; lstart=$start; }
  done <"$DL_PIDS"
  kill -KILL "$leader"
  local i
  for ((i = 0; i < 50; i++)); do dl_is "$leader" "$lstart" || break; sleep 0.1; done
  local st code
  st=$("$here/status.sh"); code=$?
  echo "     status after $app crash: $st"
  checke "status after a $app crash exits 2 with state=degraded apps=1/2" '[ "$code" = 2 ] && [ "$(field state "$st")" = degraded ] && [ "$(field apps "$st")" = 1/2 ]'
  local token; token=$(dl_token); cp "$DL_PIDS" "$work/pids.$app"
  "$here/stop.sh" >"$work/stop.$app.out" 2>&1; code=$?
  check "stop after a $app crash exits 0" [ "$code" = 0 ]
  assert_stopped "$token" "$work/pids.$app"
}

# --- Run 2: Mousepad crashes; Chromium must still be stopped.
crash_run mousepad
# --- Run 3: the Chromium browser process crashes; its orphaned renderers,
# zygotes and crash handler must still be stopped, and Mousepad with them.
crash_run chromium

echo
if [ $failures -eq 0 ]; then echo "desktop-load test: all passed"; else echo "desktop-load test: $failures failed"; fi
[ $failures -eq 0 ]
