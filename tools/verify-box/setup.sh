#!/usr/bin/env bash
# Prepares a fresh Debian 13 server as a headless verification box: an
# ordinary user (the sandboxes and the identity tests must not run as root),
# every apt dependency in distro/manifest.tsv except the desktop and agent-CLI
# ones, the Go toolchain go.mod requires, and the sandbox check CI runs.
# Run as root, from the development machine:
#   scp tools/verify-box/setup.sh root@<ip>: && ssh root@<ip> bash setup.sh <branch>
set -euo pipefail
branch=${1:?usage: setup.sh <branch>}
user=verify
repo=https://github.com/willardcsoriano/thesis.git

# Not needed headless: the desktop session and the agent CLIs, which only the
# live study runs, on the researcher's machine (docs/testing-environment.md).
skip=" xfce4 xfce4-session xfce4-terminal lightdm lightdm-gtk-greeter xserver-xorg chromium xclip nodejs npm "

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq git sudo >/dev/null

id "$user" >/dev/null 2>&1 || useradd -m -s /bin/bash "$user"
echo "$user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/"$user"
chmod 440 /etc/sudoers.d/"$user"
install -d -m 700 -o "$user" -g "$user" /home/"$user"/.ssh
install -m 600 -o "$user" -g "$user" /root/.ssh/authorized_keys /home/"$user"/.ssh/

home=/home/$user
[ -d "$home/thesis" ] || sudo -u "$user" git clone -q "$repo" "$home/thesis"
sudo -u "$user" git -C "$home/thesis" fetch -q origin
sudo -u "$user" git -C "$home/thesis" checkout -q "$branch"
sudo -u "$user" git -C "$home/thesis" pull -q --ff-only

# The package list comes from the manifest, so a dependency added there
# reaches the box without editing this script.
pkgs=()
while IFS=$'\t' read -r kind name _; do
  [ "$kind" = apt ] || continue
  case "$skip" in *" $name "*) continue ;; esac
  pkgs+=("$name")
done < "$home/thesis/distro/manifest.tsv"
apt-get install -y -qq "${pkgs[@]}" >/dev/null

# Not sudo -i: it joins a multi-line command into one line.
sudo -u "$user" -H bash -c '
  set -euo pipefail
  cd ~/thesis
  bash distro/hoard.sh --toolchain
  mkdir -p ~/.local && tar -C ~/.local -xzf distro/hoard/toolchain/go*.linux-amd64.tar.gz
  ~/.local/go/bin/go version
  bwrap --ro-bind / / --unshare-all --die-with-parent true
  timeout 30 pasta --config-net --quiet -- true
  echo "sandbox ok"
  git rev-parse HEAD
'
