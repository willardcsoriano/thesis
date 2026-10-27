#!/usr/bin/env bash
# Prepares a box set up by tools/verify-box/setup.sh for Part 2 of
# docs/cli-breaking.md: Ollama serving the runtime model on loopback, the
# synapse binary built from the copied tree, and attacker users atk1..atkN
# with no sudo and their own homes, so a destructive model command cannot
# reach the system or another attacker's undo journal.
# Run as root:  bash live-setup.sh [attackers, default 4]
set -euo pipefail
n=${1:-4}
model=$(awk -F'\t' '$1=="model" && $5 ~ /^runtime:/ {print $2; exit}' /home/verify/thesis/distro/manifest.tsv)
[ -n "$model" ] || { echo "no runtime model row in the manifest" >&2; exit 1; }

export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq zstd curl jq >/dev/null

sudo -u verify -H bash -c '
  set -euo pipefail
  cd ~/thesis
  export PATH="$HOME/.local/go/bin:$PATH" GOTOOLCHAIN=local
  make -s -C prototype build
  bash distro/hoard.sh --ollama
'
install -m 0755 /home/verify/thesis/prototype/bin/synapse /usr/local/bin/synapse

rm -rf /opt/ollama && mkdir -p /opt/ollama /opt/ollama-models
read -r ollama_version _ < /home/verify/thesis/distro/ollama-pin
tar --zstd -xf "/home/verify/thesis/distro/hoard/toolchain/ollama-linux-amd64-v$ollama_version.tar.zst" -C /opt/ollama
bin=$(find /opt/ollama -type f -name ollama -perm -u+x | head -1)
ln -sf "$bin" /usr/local/bin/ollama
chown -R verify:verify /opt/ollama-models

# Served by verify on loopback only; every local user reaches it there.
# A rerun stops the server this script started before (by its recorded PID).
pidfile=/home/verify/ollama.pid
if [ -f "$pidfile" ]; then kill "$(cat "$pidfile")" 2>/dev/null || true; sleep 1; fi
# The log is opened by this script, which runs as root, and handed to the
# server it starts as verify: intended, so root owns the log.
# shellcheck disable=SC2024
sudo -u verify -H env OLLAMA_HOST=127.0.0.1:11434 OLLAMA_MODELS=/opt/ollama-models \
  OLLAMA_NUM_PARALLEL="$n" OLLAMA_KEEP_ALIVE=-1 \
  setsid nohup ollama serve > /home/verify/ollama-serve.log 2>&1 < /dev/null &
echo $! > "$pidfile"
for _ in $(seq 30); do curl -sf http://127.0.0.1:11434/api/version >/dev/null && break; sleep 1; done
curl -sf http://127.0.0.1:11434/api/version
sudo -u verify -H env OLLAMA_HOST=127.0.0.1:11434 ollama pull "$model" >/dev/null
echo "model $model ready"

for i in $(seq "$n"); do
  u=atk$i
  id "$u" >/dev/null 2>&1 || useradd -m -s /bin/bash "$u"
  install -d -m 700 -o "$u" -g "$u" "/home/$u/.ssh"
  install -m 600 -o "$u" -g "$u" /root/.ssh/authorized_keys "/home/$u/.ssh/"
done
echo "attackers: $(seq -f 'atk%g' -s ' ' "$n")"
synapse version
