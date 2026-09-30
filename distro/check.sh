#!/usr/bin/env bash
# Reports which manifest dependencies are missing on this machine and prints the one
# install line that fixes them. Exit 1 if a required one is missing.
#   bash distro/check.sh            # apt-kind entries and the toolchain
#   bash distro/check.sh --quiet    # only the install line, if any
set -u
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
quiet=0; [ "${1:-}" = "--quiet" ] && quiet=1
missing_req=(); missing_opt=()
while IFS=$'\t' read -r kind name probe required purpose; do
  [ "$kind" = "kind" ] && continue
  case "$kind" in
    apt)
      if ! command -v "$probe" >/dev/null 2>&1; then
        if [ "$required" = "yes" ]; then missing_req+=("$name"); else missing_opt+=("$name"); fi
      fi ;;
    toolchain)
      if ! command -v go >/dev/null 2>&1 && [ ! -x "$HOME/.local/go/bin/go" ]; then
        [ $quiet -eq 0 ] && echo "missing toolchain: go (bash distro/hoard.sh --toolchain, then unpack under ~/.local)"
      fi ;;
  esac
done < "$here/manifest.tsv"

all=("${missing_req[@]}" "${missing_opt[@]}")
if [ ${#all[@]} -eq 0 ]; then [ $quiet -eq 0 ] && echo "all apt dependencies present"; exit 0; fi
[ $quiet -eq 0 ] && { echo "missing required: ${missing_req[*]:-none}"; echo "missing optional: ${missing_opt[*]:-none}"; }
echo "sudo apt install -y ${all[*]}"
[ ${#missing_req[@]} -eq 0 ]
