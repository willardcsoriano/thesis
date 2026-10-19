#!/usr/bin/env bash
# Reports which manifest dependencies are missing on this machine and prints the one
# install line that fixes them. Exit 1 if a required one is missing.
#   bash distro/check.sh            # apt, binary and pip entries and the toolchain
#   bash distro/check.sh --quiet    # only the install line, if any
set -u
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/.." && pwd)
quiet=0; [ "${1:-}" = "--quiet" ] && quiet=1
missing_req=(); missing_opt=()

# A probe is present when it is on PATH or in a system binary directory: a user's
# PATH may leave out /usr/sbin, where lightdm and its greeter live.
present() {
  command -v "$1" >/dev/null 2>&1 || [ -x "/usr/sbin/$1" ] || [ -x "/sbin/$1" ]
}
# add LIST NAME appends NAME unless it is there already (one package can have
# several probes, one per program the code runs).
add() {
  local -n list=$1
  local p
  for p in "${list[@]}"; do [ "$p" = "$2" ] && return; done
  list+=("$2")
}

while IFS=$'\t' read -r kind name probe required purpose; do
  [ "$kind" = "kind" ] && continue
  case "$kind" in
    apt)
      if ! present "$probe"; then
        if [ "$required" = "yes" ]; then add missing_req "$name"; else add missing_opt "$name"; fi
      fi ;;
    binary)
      present "$probe" || { [ $quiet -eq 0 ] && echo "missing binary: $name (required: $required) — ${purpose}"; } ;;
    pip)
      if ! "$repo/tools/venv/bin/python" -c "import $name" >/dev/null 2>&1; then
        [ $quiet -eq 0 ] && echo "missing pip: $name in tools/venv (python3 -m venv tools/venv && tools/venv/bin/pip install -r tools/requirements.txt)"
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
