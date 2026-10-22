# shellcheck shell=bash
# The sandbox model-bench.sh runs each human-style task in. Sourced, not run;
# manual-tests/bench-sandbox_test.sh tests it.
#
# Why bubblewrap and not just HOME=: the bench once cd'd into the playground
# and left HOME naming the operator's home, so a model's `~/Downloads` reached
# the real one (docs/retrospective.md, row 25). Pointing HOME (and the XDG and
# TMPDIR variables) at the playground closes `~`, `$HOME` and a bare `cd`, but
# not `~user`, which the shell resolves through /etc/passwd, nor an absolute
# path such as /home/<operator>/Downloads that a model can learn from any
# command's output. So synapse runs under bubblewrap, in the same layout as the
# oracle's HideHost sandbox (internal/oracle, HiddenDirs): the host read-only,
# and an empty tmpfs over every home, temp and runtime folder, so the operator's
# files are neither writable nor readable. Only the playground home is bound
# back, read-write, at /home/user; synapse itself is bound read-only.
#
# What differs from the oracle, on purpose: the network is shared, because
# synapse must reach Ollama, and the file /etc/resolv.conf links to is bound
# back read-only when it lives under a hidden folder (systemd-resolved keeps it
# in /run), so a host name in SYNAPSE_OLLAMA still resolves. A model's command
# can therefore still reach the network, as it could before; this sandbox
# protects the operator's files, not the network.
#
# The environment is cleared and rebuilt: HOME is the playground, XDG_* and
# TMPDIR are unset so they default to folders under that HOME and to the
# sandbox's own /tmp, and every SYNAPSE_* variable passes through, since that
# is how the bench configures synapse. synapse's own state (~/.synapse, the
# undo journal) therefore also lands in the playground, which the bench
# reseeds between tasks, instead of in the operator's journal.

BENCH_HOME_IN=/home/user
BENCH_BIN_IN=/run/model-bench/bin
BENCH_HIDDEN_DIRS=(/home /root /tmp /run /var/tmp /mnt /media /srv)

# bench_sandbox_ready fails, naming the install line, when bubblewrap cannot
# start a sandbox here. The bench refuses to run unsandboxed.
bench_sandbox_ready() {
  if ! command -v bwrap >/dev/null 2>&1; then
    echo "model-bench: bubblewrap is required to sandbox model commands; install it with: sudo apt install -y bubblewrap" >&2
    return 1
  fi
  if ! bwrap --ro-bind / / --unshare-all --die-with-parent true 2>/dev/null; then
    echo "model-bench: bubblewrap is installed but cannot start a sandbox (are unprivileged user namespaces disabled?)" >&2
    return 1
  fi
}

# bench_run_task SANDBOX_HOME BIN_DIR MODEL TASK runs BIN_DIR/synapse TASK
# inside the sandbox, with stdin empty and SANDBOX_HOME as its home and working
# directory.
bench_run_task() {
  local sandbox_home="$1" bin_dir="$2" model="$3" task="$4"
  local args=(--ro-bind / / --dev /dev --proc /proc)
  local d h resolv name
  for d in "${BENCH_HIDDEN_DIRS[@]}"; do
    if [ -d "$d" ] && [ ! -L "$d" ]; then args+=(--tmpfs "$d"); fi
  done
  # The operator's home, wherever it is: passwd's entry and $HOME, in case
  # either lies outside the folders above.
  for h in "$(getent passwd "$(id -u)" | cut -d: -f6)" "${HOME:-}"; do
    if [ -n "$h" ] && [ "$h" != / ] && [ -d "$h" ]; then args+=(--tmpfs "$h"); fi
  done
  resolv="$(readlink -f /etc/resolv.conf 2>/dev/null || true)"
  if [ -n "$resolv" ] && [ "$resolv" != /etc/resolv.conf ] && [ -f "$resolv" ]; then
    args+=(--ro-bind "$resolv" "$resolv")
  fi
  args+=(--ro-bind "$bin_dir" "$BENCH_BIN_IN" --bind "$sandbox_home" "$BENCH_HOME_IN"
    --unshare-all --share-net --die-with-parent --clearenv
    --setenv PATH "$BENCH_BIN_IN:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    --setenv HOME "$BENCH_HOME_IN" --setenv PWD "$BENCH_HOME_IN"
    --setenv USER user --setenv LOGNAME user --setenv SHELL /bin/bash
    --setenv LANG C.UTF-8 --setenv TERM dumb)
  while IFS= read -r name; do
    args+=(--setenv "$name" "${!name}")
  done < <(compgen -e | grep '^SYNAPSE_' || true)
  args+=(--setenv SYNAPSE_MODEL "$model" --chdir "$BENCH_HOME_IN")
  bwrap "${args[@]}" -- synapse "$task" </dev/null
}
