#!/bin/bash
# Tests that model-bench.sh's execute path keeps a model's commands inside the
# playground sandbox (docs/retrospective.md, row 25: a 2026-09-17 bench run
# reached the operator's real ~/Downloads because HOME still named the real
# home).
#
# It observes real behaviour: bench_run_task (the exact function the bench
# runs each human-style task through) is handed a stand-in synapse that runs
# its argument the way internal/executor does (bash -o pipefail -c), so no
# Ollama is needed. Each hostile command tries to reach the operator's home
# through ~, $HOME, a bare cd, ~user, an absolute path and the XDG and TMPDIR
# variables; the test then looks at the filesystem for what landed.
#
# The test never touches the real home, even when the bench is broken: it
# re-runs itself inside a bubblewrap containment where the whole host is
# read-only and the operator's home, /tmp and the other user-writable trees
# are empty, throwaway tmpfs mounts at their usual paths. A probe that escapes
# the bench's sandbox lands in that tmpfs, where the test sees it, and is gone
# when the test exits.
#
# Usage: bash manual-tests/bench-sandbox_test.sh (from prototype/; `make
# bench-sandbox-check` and `make ci` run it). Without a working bubblewrap it
# skips, or fails when SYNAPSE_REQUIRE_SANDBOX=1, as the Go sandbox tests do
# (internal/testenv).
# The hostile commands are shell text for the sandboxed shell, so their $VARS
# are single-quoted on purpose.
# shellcheck disable=SC2016
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SELF="$HERE/$(basename "${BASH_SOURCE[0]}")"
OPERATOR="$(id -un)"
REALHOME="$(getent passwd "$(id -u)" | cut -d: -f6)"

if [ -z "${BENCH_SANDBOX_TEST_CONTAINED:-}" ]; then
  if ! command -v bwrap >/dev/null 2>&1 ||
    ! bwrap --ro-bind / / --unshare-all --die-with-parent true 2>/dev/null; then
    msg="bench-sandbox test: no working bubblewrap (install: sudo apt install -y bubblewrap)"
    if [ "${SYNAPSE_REQUIRE_SANDBOX:-}" = 1 ]; then
      echo "FAIL: $msg, and SYNAPSE_REQUIRE_SANDBOX=1 says this host must have it" >&2
      exit 1
    fi
    echo "SKIP: $msg"
    exit 0
  fi
  args=(--ro-bind / / --dev /dev --proc /proc)
  for d in /home /root /tmp /run /var/tmp /mnt /media /srv; do
    if [ -d "$d" ] && [ ! -L "$d" ]; then args+=(--tmpfs "$d"); fi
  done
  for h in "$REALHOME" "$HOME"; do
    if [ -n "$h" ] && [ "$h" != / ]; then args+=(--tmpfs "$h"); fi
  done
  # Only this folder comes back, read-only: the bench's helper and this test.
  args+=(--ro-bind "$HERE" "$HERE" --unshare-all --die-with-parent
    --setenv BENCH_SANDBOX_TEST_CONTAINED 1 --setenv HOME "$REALHOME")
  exec bwrap "${args[@]}" -- bash "$SELF"
fi

# --- inside the containment: $REALHOME is a throwaway tmpfs from here on ---

# shellcheck source=bench-sandbox.sh disable=SC1091
source "$HERE/bench-sandbox.sh"

fails=0
pass() { echo "PASS: $*"; }
fail() {
  echo "FAIL: $*"
  fails=$((fails + 1))
}

# The playground lives under the operator's home, as prototype/playground does.
SANDBOX_HOME="$REALHOME/bench-test/playground/sandbox/home/user"
mkdir -p "$SANDBOX_HOME" "$REALHOME/Downloads"
SECRET="host-secret-$$-$RANDOM"
echo "$SECRET" >"$REALHOME/Downloads/host-secret.txt"

BIN="$(mktemp -d)"
cat >"$BIN/synapse" <<'EOF'
#!/bin/bash
# Stand-in for bin/synapse: runs the task as the command, as executor.Run does.
exec bash -o pipefail -c "$1"
EOF
chmod +x "$BIN/synapse"

# What the operator's own shell would hand the bench: XDG and TMPDIR naming
# host folders.
export XDG_CONFIG_HOME="$REALHOME/.config" XDG_DATA_HOME="$REALHOME/.local/share"
export XDG_CACHE_HOME="$REALHOME/.cache" TMPDIR="/tmp/host-tmpdir"
mkdir -p "$TMPDIR"

run() { bench_run_task "$SANDBOX_HOME" "$BIN" fake-model "$1" 2>&1 || true; }

# Hostile neighbours: each tries to write where the operator's files live.
run 'touch ~/bench-escape-tilde' >/dev/null
run 'echo x > "$HOME/bench-escape-home"' >/dev/null
run 'cd && touch bench-escape-cd' >/dev/null
run 'mkdir -p ~/Downloads && touch ~/Downloads/bench-escape-dl' >/dev/null
run "touch ~$OPERATOR/bench-escape-user" >/dev/null
run "touch '$REALHOME/bench-escape-abs' '$REALHOME/Downloads/bench-escape-absdl'" >/dev/null
run 'mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" && touch "$XDG_CONFIG_HOME/bench-escape-xdg" "$XDG_DATA_HOME/bench-escape-xdg" "$XDG_CACHE_HOME/bench-escape-xdg"' >/dev/null
run 'touch "${TMPDIR:-/tmp}/bench-escape-tmp" /tmp/bench-escape-tmp' >/dev/null

for p in \
  "$REALHOME/bench-escape-tilde" "$REALHOME/bench-escape-home" \
  "$REALHOME/bench-escape-cd" "$REALHOME/Downloads/bench-escape-dl" \
  "$REALHOME/bench-escape-user" "$REALHOME/bench-escape-abs" \
  "$REALHOME/Downloads/bench-escape-absdl" \
  "$REALHOME/.config/bench-escape-xdg" "$REALHOME/.local/share/bench-escape-xdg" \
  "$REALHOME/.cache/bench-escape-xdg" \
  "/tmp/host-tmpdir/bench-escape-tmp" "/tmp/bench-escape-tmp"; do
  if [ -e "$p" ]; then fail "escaped the sandbox: ${p/#"$REALHOME"/<real home>}"; else pass "absent: ${p/#"$REALHOME"/<real home>}"; fi
done

# Reads count too: the bench report holds whatever a command printed.
out="$(run "cat ~$OPERATOR/Downloads/host-secret.txt '$REALHOME/Downloads/host-secret.txt' ~/Downloads/host-secret.txt")"
if [[ "$out" == *"$SECRET"* ]]; then fail "a command read a file from the real home"; else pass "real home unreadable"; fi

# Positive controls: the sandbox still works as a home, so the absences above
# are not just a bench that runs nothing.
out="$(run 'echo "model=$SYNAPSE_MODEL pwd=$PWD"; touch ~/inside-tilde inside-cwd && mkdir -p ~/Downloads && touch ~/Downloads/inside-dl && cd && touch inside-cd')"
if [[ "$out" == *"model=fake-model"* ]]; then pass "SYNAPSE_MODEL reaches synapse"; else fail "SYNAPSE_MODEL missing: $out"; fi
for f in inside-tilde inside-cwd Downloads/inside-dl inside-cd; do
  if [ -e "$SANDBOX_HOME/$f" ]; then pass "lands in the sandbox: ~/$f"; else fail "did not land in the sandbox: ~/$f"; fi
done

if [ "$fails" -gt 0 ]; then
  echo "bench-sandbox test: $fails failure(s)"
  exit 1
fi
echo "bench-sandbox test: ok"
