#!/usr/bin/env bash
# Collects everything the manifest names into distro/hoard/, so the bootable image can
# be assembled without the network. Needs no elevated privilege: apt-get download
# and go mod download write only to the current directory.
#   bash distro/hoard.sh --debs --gomod --toolchain --models     (or --all)
# Layout: hoard/debs/*.deb  hoard/gomod/  hoard/toolchain/  hoard/models/  hoard/SHA256SUMS
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/.." && pwd)
out="$here/hoard"; mkdir -p "$out"
want_debs=0; want_go=0; want_tc=0; want_models=0; want_ollama=0
for a in "$@"; do case "$a" in
  --debs) want_debs=1;; --gomod) want_go=1;; --toolchain) want_tc=1;; --models) want_models=1;; --ollama) want_ollama=1;;
  --all) want_debs=1; want_go=1; want_tc=1; want_models=1;;
  *) echo "unknown option $a" >&2; exit 2;; esac; done
[ $((want_debs+want_go+want_tc+want_models+want_ollama)) -gt 0 ] || { echo "nothing selected; pass --all" >&2; exit 2; }

manifest() { awk -F'\t' -v k="$1" 'NR>1 && $1==k {print $2}' "$here/manifest.tsv"; }

if [ $want_debs -eq 1 ]; then
  mkdir -p "$out/debs"; cd "$out/debs"
  # The closure of every apt entry, minus what any Debian base already carries
  # (Essential or Priority required/important), which the image starts from.
  pkgs=$(manifest apt | xargs apt-cache depends --recurse --no-recommends --no-suggests --no-conflicts \
           --no-breaks --no-replaces --no-enhances 2>/dev/null | grep -E '^[a-z0-9]' | sort -u)
  todo=()
  for p in $pkgs; do
    prio=$(apt-cache show "$p" 2>/dev/null | awk -F': ' '/^Priority:/ {print $2; exit}')
    ess=$(apt-cache show "$p" 2>/dev/null | awk -F': ' '/^Essential:/ {print $2; exit}')
    case "$prio:$ess" in required:*|important:*|*:yes) continue;; esac
    todo+=("$p")
  done
  echo "downloading ${#todo[@]} packages"
  apt-get download "${todo[@]}" 2>&1 | tail -3 || echo "some packages could not be downloaded (virtual or unavailable); see above"
  cd "$here"
fi

if [ $want_go -eq 1 ]; then
  export GOMODCACHE="$out/gomod" GOFLAGS=-modcacherw GOTOOLCHAIN=local
  export PATH="$HOME/.local/go/bin:$PATH"
  (cd "$repo/prototype" && go mod download all)
  echo "go modules hoarded in $out/gomod"
fi

if [ $want_tc -eq 1 ]; then
  mkdir -p "$out/toolchain"
  ver=$(awk '/^go /{print $2}' "$repo/prototype/go.mod")
  file="go${ver}.linux-amd64.tar.gz"
  if [ ! -f "$out/toolchain/$file" ]; then
    curl -fsSL "https://go.dev/dl/$file" -o "$out/toolchain/$file"
  fi
  want=$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' | jq -r --arg f "$file" '.[].files[] | select(.filename==$f) | .sha256' | head -1)
  got=$(sha256sum "$out/toolchain/$file" | awk '{print $1}')
  [ -n "$want" ] && [ "$want" = "$got" ] || { echo "toolchain checksum mismatch or unknown: want=$want got=$got" >&2; rm -f "$out/toolchain/$file"; exit 1; }
  echo "toolchain $file verified"
fi

if [ $want_models -eq 1 ]; then
  src="${OLLAMA_MODELS:-$HOME/.ollama/models}"
  mkdir -p "$out/models"
  for m in $(manifest model); do
    name=${m%%:*}; tag=${m##*:}
    mf="$src/manifests/registry.ollama.ai/library/$name/$tag"
    [ -f "$mf" ] || { echo "model $m is not pulled here (ollama pull $m); skipped" >&2; continue; }
    mkdir -p "$out/models/manifests/registry.ollama.ai/library/$name" "$out/models/blobs"
    cp -f "$mf" "$out/models/manifests/registry.ollama.ai/library/$name/$tag"
    for d in $(jq -r '.config.digest, .layers[].digest' "$mf"); do
      cp -n "$src/blobs/${d/:/-}" "$out/models/blobs/" 2>/dev/null || true
    done
    echo "model $m hoarded"
  done
fi

if [ $want_ollama -eq 1 ]; then
  mkdir -p "$out/toolchain"
  curl -fsSL https://ollama.com/download/ollama-linux-amd64.tar.zst -o "$out/toolchain/ollama-linux-amd64.tar.zst" \
    || echo "ollama archive not fetched; the installer script at https://ollama.com/install.sh is the fallback" >&2
fi

(cd "$out" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
echo "wrote $out/SHA256SUMS"
