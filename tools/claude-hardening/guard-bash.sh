#!/usr/bin/env bash
# PreToolUse hook for the Bash tool. Refuses commands that would hang with no
# terminal, and says what to do instead, so a session gets an answer in
# milliseconds rather than a stalled turn. Exit 2 blocks the call and returns
# stderr to the model. Anything not matched here passes through unchanged.
#
# It inspects only real command positions: quoted strings and heredoc bodies are
# removed first, so a command that merely mentions a word (in a commit message, a
# document being written, a snippet) is never blocked for it.
set -u
input=$(cat)
raw=$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)
bg=$(printf '%s' "$input" | jq -r '.tool_input.run_in_background // false' 2>/dev/null)
[ -z "$raw" ] && exit 0

# Remove heredoc bodies, then single- and double-quoted strings.
cmd=$(printf '%s' "$raw" | perl -0pe 's/<<-?\s*(["\x27]?)(\w+)\1[^\n]*\n.*?\n\s*\2(?=\n|\z)/\n/sg; s/\x27[^\x27]*\x27//g; s/"(?:[^"\\]|\\.)*"//g' 2>/dev/null || printf '%s' "$raw")

block() { printf 'BLOCKED by guard-bash: %s\n' "$1" >&2; exit 2; }
has()   { printf '%s\n' "$cmd" | grep -Eq -- "$1"; }
# A command word: start of the line, or right after a separator, ( , $( , or a backtick.
CW='(^|[;&|(`]|\$\()[[:space:]]*'

# 1. sudo asks for a password on a terminal that does not exist.
if has "${CW}sudo[[:space:]]" && ! has "${CW}sudo[[:space:]]+(-n|--non-interactive)([[:space:]]|\$)"; then
  sudo -n true 2>/dev/null || block "sudo needs a password and there is no terminal. Do not retry variants. Give the user the exact command to run themselves (typed after '!'), and carry on with other work."
fi

# 2. Long-running or never-returning commands must not run in the foreground.
if [ "$bg" != "true" ] && has "${CW}(ollama[[:space:]]+(serve|pull|run)|docker[[:space:]]+(pull|compose[[:space:]]+up)|go[[:space:]]+run[[:space:]]+\./cmd/synapse[[:space:]]+(repl|tui))([[:space:]]|\$)"; then
  block "this is long-running or interactive. Re-run with run_in_background: true and read its log, or ask the user to run it."
fi

# 3. Bare editors, pagers, and REPLs wait for input forever.
if has "^[[:space:]]*(vim?|nano|emacs|less|more|top|htop|python3?|node|irb|psql|mysql)([[:space:]]+-[A-Za-z-]+)*[[:space:]]*\$"; then
  block "this opens an interactive program that waits for input. Use a non-interactive form."
fi

# 4. Interactive git: commit without a message opens an editor; -i and -p prompt.
if has "${CW}git[[:space:]]+commit" && ! has 'git[[:space:]]+commit.*([[:space:]]-[a-zA-Z]*m|--message|[[:space:]]-F|--file|--no-edit|[[:space:]]-C[[:space:]])' && ! printf '%s' "$raw" | grep -Eq 'git[[:space:]]+commit.*(-m|--message|-F|--file|--no-edit)'; then
  block "git commit without -m opens an editor. Pass the message with -m (heredoc for multi-line)."
fi
if has "${CW}git[[:space:]]+(rebase|add|checkout|clean|stash)[^;&|]*([[:space:]]-[a-zA-Z]*[ip]([[:space:]]|\$)|--interactive|--patch)"; then
  block "interactive git prompt. Use the non-interactive form."
fi

exit 0
