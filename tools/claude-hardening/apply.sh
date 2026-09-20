#!/usr/bin/env bash
# Applies the dead-end hardening to your Claude Code config. Run it yourself:
#   ! bash tools/claude-hardening/apply.sh --yes    (from the thesis repo; no terminal is needed)
#   ! bash tools/claude-hardening/apply.sh --target /tmp/copy.json --yes   (test on a copy)
# It never runs on its own and Claude is not allowed to edit these files.
#
# What it does, in order, with a backup first:
#   1. Merges settings.patch.json into ~/.claude/settings.json (the file behind the
#      symlink into your claude-config repo). Arrays are unioned in their existing
#      order, env keys are added, the PreToolUse guard is added once.
#   2. Copies guard-bash.sh to ~/projects/claude-config/claude/hooks/ and makes it executable.
#   3. Appends claude-md-snippet.txt and claude-md-philosophy.txt and claude-md-blockers.txt to your global CLAUDE.md (once each).
# Nothing is committed; the claude-config repo is left for you to review and commit.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
target="$(readlink -f "$HOME/.claude/settings.json")"; yes=0; test_only=0
while [ $# -gt 0 ]; do case "$1" in
  --target) target="$2"; test_only=1; shift 2;; --yes) yes=1; shift;; *) echo "unknown arg $1" >&2; exit 2;; esac; done
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
[ -f "$target" ] || { echo "no settings file at $target" >&2; exit 1; }

bak_dir="$HOME/.claude/backups"; mkdir -p "$bak_dir"
bak="$bak_dir/settings.json.$(date +%Y%m%d-%H%M%S).bak"; cp -- "$target" "$bak"; echo "backup: $bak"

tmp=$(mktemp); trap 'rm -f "$tmp"' EXIT
jq -s '
  def union($a; $b): ($a // []) + (($b // []) - ($a // []));
  .[0] as $cur | .[1] as $add
  | $cur
  | .permissions.allow = union($cur.permissions.allow; $add.permissions.allow)
  | .permissions.ask   = union($cur.permissions.ask;   $add.permissions.ask)
  | .env = (($cur.env // {}) + ($add.env // {}))
  | .autoMode.environment = union($cur.autoMode.environment; $add.autoMode.environment)
  | .hooks.PreToolUse = (
      ($cur.hooks.PreToolUse // []) as $have
      | if ($have | tostring | contains("guard-bash.sh")) then $have
        else $have + $add.hooks.PreToolUse end)
' "$target" "$here/settings.patch.json" > "$tmp"
jq -e . "$tmp" >/dev/null || { echo "merge produced invalid JSON; nothing changed" >&2; exit 1; }

echo "--- changes to $target ---"; diff -u "$target" "$tmp" || true
# Under `!` there is no terminal to answer a prompt, so a failed read must mean "no",
# not an abort with nothing said. Pass --yes to apply without asking.
ask() { local a=""; if [ "$yes" -eq 1 ]; then return 0; fi; if [ -t 0 ]; then read -r -p "$1 [y/N] " a || true; fi; [ "$a" = "y" ]; }
if ! ask "Write these changes?"; then echo "not applied. Re-run with --yes to apply without a prompt: bash tools/claude-hardening/apply.sh --yes"; exit 0; fi
cat "$tmp" > "$target"; echo "updated $target"

if [ "$test_only" -eq 0 ]; then
  hooks="$HOME/projects/claude-config/claude/hooks"; mkdir -p "$hooks"
  install -m 0755 "$here/guard-bash.sh" "$hooks/guard-bash.sh"; echo "installed $hooks/guard-bash.sh"
  md="$(readlink -f "$HOME/.claude/CLAUDE.md")"
  for pair in "Dead ends: never stall|claude-md-snippet.txt" "Working philosophy:|claude-md-philosophy.txt" "Blockers:|claude-md-blockers.txt"; do
    marker="${pair%%|*}"; file="${pair##*|}"
    if grep -q "$marker" "$md"; then echo "CLAUDE.md already has: $marker"
    elif ask "Append '$marker' paragraph to $md?"; then cat "$here/$file" >> "$md"; echo "appended: $marker"
    else echo "skipped: $marker (re-run with --yes to append)"; fi
  done
  echo "Done. Start a new session, then run /hooks and /permissions to confirm. Review and commit the claude-config repo yourself."
fi
