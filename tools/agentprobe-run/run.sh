#!/usr/bin/env bash
# Runs agentprobe with the Claude token from the desktop keyring, so whoever
# starts a batch (including a Claude Code session, which is denied
# `secret-tool lookup` directly) never sees the token. The token goes from
# the keyring into agentprobe's environment and nowhere else; agentprobe
# redacts it in everything it writes.
#
#   run.sh check            report whether a usable token is stored (length
#                           and shape only, never the value)
#   run.sh <agentprobe args> build agentprobe from this checkout and run it,
#                           e.g. run.sh claude -tasks FILE -run-id ID -out runs
#
# The token is stored once by the owner:
#   claude setup-token && secret-tool store --label='agentprobe Claude token' service agentprobe account claude
set -euo pipefail
set +x # never trace: a trace would print the token

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
proto=$(cd "$here/../../prototype" && pwd)

token=$(secret-tool lookup service agentprobe account claude 2>/dev/null || true)
case "$token" in
sk-ant-oat*) ok=1 ;;
*) ok=0 ;;
esac

if [ "${1:-}" = check ]; then
	echo "stored: ${#token} characters, token shape: $([ "$ok" = 1 ] && echo yes || echo no)"
	[ "$ok" = 1 ]
	exit
fi
if [ "$ok" != 1 ]; then
	echo "run.sh: no Claude token in the keyring (service agentprobe, account claude); see the header of this script" >&2
	exit 1
fi

bin=$(mktemp -d)/agentprobe
trap 'rm -rf "$(dirname "$bin")"' EXIT
(cd "$proto" && PATH="$HOME/.local/go/bin:$PATH" GOTOOLCHAIN=local go build -o "$bin" ./cmd/agentprobe)

cd "$proto"
CLAUDE_CODE_OAUTH_TOKEN=$token "$bin" "$@"
