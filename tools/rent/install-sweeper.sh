#!/usr/bin/env bash
# Installs a systemd user timer that runs sweep.sh every 30 minutes, so a test
# server whose rent.sh never came back to delete it is deleted anyway. User
# level: no root, nothing outside ~/.config/systemd/user. Run again to update;
# --remove takes it out.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
units=~/.config/systemd/user
if [ "${1:-}" = --remove ]; then
  systemctl --user disable --now synapse-rent-sweep.timer 2>/dev/null || true
  rm -f "$units/synapse-rent-sweep.service" "$units/synapse-rent-sweep.timer"
  systemctl --user daemon-reload
  echo "sweeper removed"
  exit 0
fi
hcloud_bin=$(command -v hcloud) || { echo "hcloud is not on PATH" >&2; exit 1; }
mkdir -p "$units"
cat > "$units/synapse-rent-sweep.service" <<EOF
[Unit]
Description=Delete rented SynapseOS test servers past their lifetime

[Service]
Type=oneshot
Environment=PATH=$(dirname "$hcloud_bin"):/usr/local/bin:/usr/bin:/bin
ExecStart=/usr/bin/env bash $here/sweep.sh
EOF
cat > "$units/synapse-rent-sweep.timer" <<EOF
[Unit]
Description=Sweep rented SynapseOS test servers every 30 minutes

[Timer]
OnBootSec=5min
OnUnitActiveSec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now synapse-rent-sweep.timer
systemctl --user list-timers synapse-rent-sweep.timer --no-pager
