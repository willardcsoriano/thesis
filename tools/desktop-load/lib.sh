# shellcheck shell=bash
# Shared by start.sh, stop.sh and status.sh; sourced, never run.
#
# How the load's processes are known, without matching any process by name:
#   1. Every app is started with setsid, so it leads its own process group, and
#      its PID, start time and group are written to the state file at once.
#   2. Every app is started with DESKTOP_LOAD_TOKEN=<random, per run> in its
#      environment, which its children inherit. Chromium's crash handler
#      double-forks out of the group and is reparented away, but keeps the
#      token, so it is still found.
# A process belongs to the load when it is a recorded PID with the recorded
# start time, a member of a recorded group, or carries this run's token. A
# start time that differs means the PID was reused by an unrelated process,
# which is then left alone.

# Where the state lives. One load per user and machine; the test points it
# elsewhere.
DL_STATE=${DESKTOP_LOAD_STATE:-${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}/synapse-desktop-load}
DL_MARKER=$DL_STATE/.desktop-load
DL_PIDS=$DL_STATE/pids
DL_TOKEN_FILE=$DL_STATE/token
# shellcheck disable=SC2034 # used by start.sh and test.sh
DL_PAGES=(dashboard article mail)

dl_die() { echo "desktop-load: $*" >&2; exit "${DL_EXIT:-1}"; }

# dl_stat PID sets DL_PGID and DL_START from /proc/PID/stat. It fails
# when the process is gone or a zombie (a zombie holds no memory and cannot be
# signalled into exiting; its parent reaps it).
dl_stat() {
  local line rest
  IFS= read -r line 2>/dev/null <"/proc/$1/stat" || return 1
  # The command name (field 2) may hold spaces and parentheses: cut after the
  # last ") ". The rest starts at field 3, the state.
  rest=${line##*) }
  # shellcheck disable=SC2086 # split on purpose
  set -- $rest
  [ "$1" = Z ] && return 1
  DL_PGID=$3 DL_START=${20}
}

# dl_is PID START: the process is alive and is the one that was recorded.
dl_is() { dl_stat "$1" && [ "$DL_START" = "$2" ]; }

# dl_token prints the run's token, or nothing.
dl_token() {
  local t
  [ -r "$DL_TOKEN_FILE" ] && IFS= read -r t <"$DL_TOKEN_FILE" && printf '%s\n' "$t"
}

# dl_members prints, one per line and without repeats, every live process of
# the load: recorded PIDs still alive as recorded, the live members of every
# recorded group, and every process carrying the run's token.
dl_members() {
  local -A seen=() groups=()
  local pid start kind p tok f
  [ -r "$DL_PIDS" ] || return 0
  while read -r pid start _ kind; do
    [ -n "$pid" ] || continue
    if dl_is "$pid" "$start"; then
      seen[$pid]=1
      [ "$kind" = leader ] && groups[$pid]=1
    elif [ "$kind" = leader ] && ! dl_stat "$pid"; then
      # The leader is gone. While any member of its group lives, the kernel
      # keeps the group's number allocated, so a process in that group is
      # still one of ours.
      groups[$pid]=1
    fi
  done <"$DL_PIDS"
  if [ ${#groups[@]} -gt 0 ]; then
    for f in /proc/[0-9]*; do
      p=${f#/proc/}
      dl_stat "$p" || continue
      [ -n "${groups[$DL_PGID]:-}" ] && seen[$p]=1
    done
  fi
  tok=$(dl_token)
  if [ -n "$tok" ]; then
    # One grep over every environment this user can read; others' are
    # unreadable and skipped. The token is never in this script's own
    # environment, so grep does not find itself.
    while IFS= read -r f; do
      p=${f#/proc/}; p=${p%/environ}
      dl_stat "$p" && seen[$p]=1
    done < <(grep -lzx -- "DESKTOP_LOAD_TOKEN=$tok" /proc/[0-9]*/environ 2>/dev/null)
  fi
  for p in "${!seen[@]}"; do printf '%s\n' "$p"; done | sort -n
}

# dl_leaders_up prints "UP TOTAL": how many recorded app leaders are alive as
# recorded, out of how many were started.
dl_leaders_up() {
  local up=0 total=0 pid start kind
  if [ -r "$DL_PIDS" ]; then
    while read -r pid start _ kind; do
      [ "$kind" = leader ] || continue
      total=$((total + 1))
      dl_is "$pid" "$start" && up=$((up + 1))
    done <"$DL_PIDS"
  fi
  echo "$up $total"
}

# dl_memory PID... prints "RSS_KB PSS_KB" summed over the processes. RSS counts
# a page shared by several processes once per process, so it overstates a
# multi-process browser; PSS divides each shared page among its sharers and
# sums to the memory the load really holds. Both are reported.
dl_memory() {
  local rss=0 pss=0 p k v _
  for p in "$@"; do
    while read -r k v _; do
      [ "$k" = VmRSS: ] && rss=$((rss + v))
    done 2>/dev/null <"/proc/$p/status"
    while read -r k v _; do
      [ "$k" = Pss: ] && pss=$((pss + v))
    done 2>/dev/null <"/proc/$p/smaps_rollup"
  done
  echo "$rss $pss"
}

# dl_cpu_ticks PID... prints the user plus system CPU time of the processes,
# in clock ticks: two readings show whether the load is still doing work.
dl_cpu_ticks() {
  local t=0 p line rest
  for p in "$@"; do
    IFS= read -r line 2>/dev/null <"/proc/$p/stat" || continue
    rest=${line##*) }
    # shellcheck disable=SC2086
    set -- $rest
    t=$((t + ${12} + ${13}))
  done
  echo "$t"
}

# dl_stop_all signals every process of the load, TERM first, then KILL after a
# grace period, and removes the state directory once none is left. It returns
# 1, keeping the state, if any process survives KILL.
dl_stop_all() {
  local grace=${DESKTOP_LOAD_STOP_GRACE:-10} i
  local -a live
  mapfile -t live < <(dl_members)
  if [ ${#live[@]} -gt 0 ]; then
    kill -TERM "${live[@]}" 2>/dev/null
    for ((i = 0; i < grace * 10; i++)); do
      mapfile -t live < <(dl_members)
      [ ${#live[@]} -eq 0 ] && break
      sleep 0.1
    done
    if [ ${#live[@]} -gt 0 ]; then
      kill -KILL "${live[@]}" 2>/dev/null
      for ((i = 0; i < 50; i++)); do
        mapfile -t live < <(dl_members)
        [ ${#live[@]} -eq 0 ] && break
        sleep 0.1
      done
    fi
  fi
  if [ ${#live[@]} -gt 0 ]; then
    echo "desktop-load: still alive after KILL, state kept in $DL_STATE: ${live[*]}" >&2
    return 1
  fi
  # The marker proves the directory is ours before anything is removed.
  [ -e "$DL_MARKER" ] && rm -rf -- "$DL_STATE"
  return 0
}
