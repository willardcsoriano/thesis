#!/usr/bin/env bash
# Copies this checkout to a box as /root/thesis, uncommitted and untracked work
# included and git-ignored files left out (the hoard, venvs, GPL data), so a
# rented machine tests exactly what is on the development machine without a
# push (docs/cli-breaking.md).
#   bash tools/break-box/push.sh <server-ip>          # to root:/root/thesis, for setup.sh --copied
#   bash tools/break-box/push.sh <server-ip> verify   # straight into ~verify/thesis, to resync after setup
# BOX_SSH replaces the ssh command rsync uses (tools/rent/rent.sh passes its own
# known-hosts file and options).
set -euo pipefail
ip=${1:?usage: push.sh <server-ip> [user]}
user=${2:-root}
dest="root@$ip:/root/thesis/"
[ "$user" = root ] || dest="$user@$ip:thesis/"
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo"
{ git ls-files -z -co --exclude-standard; find .git -type f -print0; } |
  rsync -a --from0 --files-from=- -e "${BOX_SSH:-ssh -o StrictHostKeyChecking=accept-new}" ./ "$dest"
echo "copied $(git rev-parse --short HEAD) plus working-tree changes to $dest"
