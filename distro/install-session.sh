#!/usr/bin/env bash
# Installs GUI mode as a login-screen session choice: synapseos-session to
# /usr/bin, synapseos.desktop to /usr/share/xsessions, and the built synapse
# binary to /usr/bin/synapse (docs/interface-modes.md §5). Run as root, after
# `make -C prototype build`:
#
#   sudo bash distro/install-session.sh
#
# To try it: log out, and "SynapseOS" appears as a session choice at the
# greeter alongside "Xfce Session". Picking it starts an ordinary XFCE
# session with the conversational TUI fullscreen on top. This is not
# something to run inside an already-open desktop session — it replaces
# what happens at the *next* login, so it needs a real logout/login cycle
# to see, which is yours to do, not something to script.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/.." && pwd)
bin="$repo/prototype/bin/synapse"

[ "$(id -u)" -eq 0 ] || { echo "run this with sudo: sudo bash $0" >&2; exit 1; }
[ -x "$bin" ] || { echo "no built binary at $bin (run: make -C prototype build)" >&2; exit 1; }
for dep in xfce4-session xfce4-terminal; do
  command -v "$dep" >/dev/null || { echo "missing dependency: $dep (sudo apt install -y $dep)" >&2; exit 1; }
done

install -m 0755 "$bin" /usr/bin/synapse
install -m 0755 "$here/synapseos-session" /usr/bin/synapseos-session
install -m 0644 "$here/synapseos.desktop" /usr/share/xsessions/synapseos.desktop

echo "installed:"
echo "  /usr/bin/synapse             (from $bin)"
echo "  /usr/bin/synapseos-session"
echo "  /usr/share/xsessions/synapseos.desktop"
echo
echo "log out and pick \"SynapseOS\" at the login screen's session menu to try it."
