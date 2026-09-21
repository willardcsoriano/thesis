#!/usr/bin/env bash
# Copies the hoarded Ollama models into the system service's model directory and restarts it.
# Run as root:  sudo bash distro/install-models.sh
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
src="$here/hoard/models"
dst=/usr/share/ollama/.ollama/models

[ "$(id -u)" -eq 0 ] || { echo "run this with sudo: sudo bash $0" >&2; exit 1; }
[ -d "$src/manifests" ] && [ -d "$src/blobs" ] || { echo "no hoarded models at $src (run distro/hoard.sh --models first)" >&2; exit 1; }

mkdir -p "$dst"
cp -rn "$src"/. "$dst"/
chown -R ollama:ollama /usr/share/ollama/.ollama
systemctl restart ollama
sleep 3
echo "models now served on the default port:"
curl -s -m 5 http://127.0.0.1:11434/api/tags | grep -o '"name":"[^"]*"' || echo "  none yet; check: systemctl status ollama"
