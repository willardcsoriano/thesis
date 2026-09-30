#!/usr/bin/env bash
# Tests guard-bash.sh with hook-shaped input. Run: bash tools/claude-hardening/test-guard.sh
# Each case: expected exit code, background flag, command text.
here=$(cd "$(dirname "$0")" && pwd)
fail=0
t() {
  local want=$1 bg=$2 cmd=$3
  printf '{"tool_input":{"command":%s,"run_in_background":%s}}' "$(printf '%s' "$cmd" | jq -Rs .)" "$bg" | "$here/guard-bash.sh" >/dev/null 2>&1
  local got=$?
  if [ "$got" -eq "$want" ]; then echo "ok   ($got) $cmd" | head -1 | cut -c1-90
  else echo "FAIL want=$want got=$got :: $cmd" | head -1; fail=1; fi
}
# must pass
t 0 false 'make test'
t 0 false 'go build ./...'
t 0 false "git commit -m 'x'"
t 0 false 'git status'
t 0 true  'ollama serve'
t 0 false 'python3 script.py'
t 0 false 'ls | xargs echo'
t 0 false 'echo "run it with sudo apt update"'
t 0 false "cat >> notes.txt <<'EOF'
give the exact install command, with sudo, in one line
EOF"
t 0 false 'grep -c "git rebase -i" docs/notes.md'
# must block
t 2 false 'git commit'
t 2 false 'git rebase -i HEAD~3'
t 2 false 'ollama serve'
t 2 false 'vim'
t 2 false 'python3'
t 2 false 'go run ./cmd/synapse tui'
if sudo -n true 2>/dev/null; then echo "skip: passwordless sudo works here, so the sudo block is not exercised"
else
  t 2 false 'sudo apt update'
  t 2 false 'ls && sudo rm x'
fi
exit $fail
