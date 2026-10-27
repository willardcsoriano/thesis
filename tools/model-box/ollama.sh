# shellcheck shell=bash
# Sourced by tools/model-box/run.sh and tools/layer-box/run.sh: use an Ollama
# that answers at $url, or fetch the version distro/ollama-pin names
# (distro/hoard.sh --ollama, checksum-verified), unpack it under ~/.local/ollama and serve it for the run. The
# caller sets url, out and repo; started_pid is the server this started, which
# stop_ollama ends by that PID when the caller exits.
: "${url:?the caller sets url}" "${out:?the caller sets out}" "${repo:?the caller sets repo}"
started_pid=""
stop_ollama() { if [ -n "$started_pid" ]; then kill "$started_pid" 2>/dev/null; wait "$started_pid" 2>/dev/null; fi; }
trap stop_ollama EXIT
if ! curl -sf "$url/api/version" >/dev/null; then
  if ! command -v ollama >/dev/null; then
    read -r ollama_version _ < "$repo/distro/ollama-pin"
    archive=$repo/distro/hoard/toolchain/ollama-linux-amd64-v$ollama_version.tar.zst
    [ -f "$archive" ] || bash "$repo/distro/hoard.sh" --ollama
    [ -f "$archive" ] || { echo "no Ollama archive at $archive" >&2; exit 1; }
    mkdir -p ~/.local/ollama
    tar --zstd -xf "$archive" -C ~/.local/ollama || { echo "could not unpack $archive (is zstd installed?)" >&2; exit 1; }
    bin=$(find ~/.local/ollama -type f -name ollama -perm -u+x | head -1)
    [ -n "$bin" ] || { echo "the archive holds no ollama binary" >&2; exit 1; }
    # Recent archives already put the binary at bin/ollama.
    mkdir -p ~/.local/ollama/bin
    [ "$bin" -ef ~/.local/ollama/bin/ollama ] || ln -sf "$bin" ~/.local/ollama/bin/ollama
  fi
  host=${url#http://}
  OLLAMA_HOST=$host ollama serve > "$out/ollama-serve.log" 2>&1 &
  started_pid=$!
  for _ in $(seq 60); do curl -sf "$url/api/version" >/dev/null && break; sleep 1; done
  curl -sf "$url/api/version" >/dev/null || { echo "ollama serve did not answer at $url; see $out/ollama-serve.log" >&2; exit 1; }
  # A server started here must be the pinned one (distro/ollama-pin): an
  # ollama already on PATH may be any version.
  read -r pinned _ < "$repo/distro/ollama-pin"
  serving=$(curl -sf "$url/api/version" | jq -r .version)
  [ "$serving" = "$pinned" ] || { echo "ollama $serving is serving, but distro/ollama-pin wants $pinned" >&2; exit 1; }
fi
