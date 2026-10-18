## Overview

These are the facts about the Claude Code CLI that this driver depends on. Each was checked on 2026-10-18 against the installed CLI (2.1.302, `claude --help`), the official docs at code.claude.com, or one real capture, and each line names its source. The facts that matter most: a long-lived token from `claude setup-token` is supplied as `CLAUDE_CODE_OAUTH_TOKEN`, and `--bare` must not be used, because bare mode never reads that variable. The stream carries no exit-code field for Bash, so the exit status is inferred from what the CLI was seen to emit. The CLI gives task-level effects only, so the oracle's before/after verdict goes on the last record of each task. Re-check this list when the CLI version in the records changes.

## Table of Contents

- [Overview](#overview)
- [Invocation](#invocation)
- [Authentication](#authentication)
- [Environment variables set in the sandbox](#environment-variables-set-in-the-sandbox)
- [Stream schema](#stream-schema)
- [Installation on this machine](#installation-on-this-machine)
- [Effect capture](#effect-capture)

## Invocation

- Headless mode is `-p`; `--output-format stream-json` with `--verbose` gives newline-delimited JSON events, the last being a `result` message. Source: https://code.claude.com/docs/en/headless ("Stream responses").
- `--max-turns N` exists (print mode only) and exits with an error when reached; no limit by default. It is not listed by `claude --help`, and the reference says `--help` "does not list every flag". Source: https://code.claude.com/docs/en/cli-reference.
- `--dangerously-skip-permissions` is "Equivalent to `--permission-mode bypassPermissions`". Without a mode, a `-p` run on this machine started in `auto` (init event of the capture). Source: https://code.claude.com/docs/en/cli-reference; `testdata/ls_two_commands.jsonl`.
- `--model` takes an alias (`sonnet`, `opus`, `haiku`, `fable`) or a full model name. Source: cli-reference.
- `--session-id <uuid>` fixes a session's id; `--resume <id>` continues it, and finds it in any project on the machine. `--no-session-persistence` would make resuming impossible, so the runner does not pass it. Source: cli-reference; headless ("Continue conversations").
- `--allowedTools` uses permission-rule syntax: `Bash(ls *)`, with the space before `*` (the older `Bash(ls:*)` form is not what the docs show). Source: headless ("Create a commit").
- `-p` skips the workspace-trust dialog. Without `--bare` it still loads hooks, MCP servers, skills, plugins and CLAUDE.md from the working directory and `~/.claude`. Inside the sandbox `~` is a fresh empty directory, so none of the operator's configuration loads. Source: headless ("Start faster with bare mode"); `claude --help`.

## Authentication

- `claude setup-token` prints a one-year OAuth token and saves it nowhere; set it as `CLAUDE_CODE_OAUTH_TOKEN`. It requires a Pro, Max, Team or Enterprise plan and can only make model requests. Source: https://code.claude.com/docs/en/authentication ("Generate a long-lived token").
- Precedence: cloud-provider variables, then `ANTHROPIC_AUTH_TOKEN`, then `ANTHROPIC_API_KEY`, then `apiKeyHelper`, then `CLAUDE_CODE_OAUTH_TOKEN`, then `/login` credentials. The sandbox passes only `CLAUDE_CODE_OAUTH_TOKEN`, so nothing outranks it. Source: authentication ("Authentication precedence").
- `--bare` (and `CLAUDE_CODE_SIMPLE=1`) never reads `CLAUDE_CODE_OAUTH_TOKEN`. Source: authentication; https://code.claude.com/docs/en/env-vars.
- On Linux, `/login` credentials live in `~/.claude/.credentials.json`, or under `CLAUDE_CONFIG_DIR` when that is set. The sandbox's `~` is empty, so there are none to find. Source: authentication ("Credential management").

## Environment variables set in the sandbox

- `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` strips credentials from the environment of the Bash commands, hooks and MCP servers the CLI starts. It matches by name or value shape; the docs' table does not list `CLAUDE_CODE_OAUTH_TOKEN` by name, so it is assumed, not shown, to be removed. Source: env-vars ("What the subprocess environment scrub removes"). **Not used by the driver:** in the first live runs (2026-10-18) setting it forced permission mode `default` ("allowed_non_write_users hardening") and turned on the CLI's own Bash sandbox, which failed inside bubblewrap because `socat` is missing, so no command ran.
- `DISABLE_AUTOUPDATER=1` disables background updates (the install directory is read-only anyway). Source: env-vars.
- Not set: `DISABLE_TELEMETRY` and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, because both also turn off feature-flag fetching, which can change behaviour, and the study measures the CLI as people run it. Source: env-vars.

## Stream schema

- `system`/`init` carries `session_id`, `model` (exact id, for example `claude-opus-5-5`), `claude_code_version`, `cwd`, `tools` and `permissionMode`. Source: https://code.claude.com/docs/en/agent-sdk/typescript (`SDKSystemMessage`); capture.
- `assistant` events carry `message.content` blocks; a tool call is `{"type":"tool_use","id","name","input"}`. A message with several tool calls arrives as several `assistant` events with the same message id, one block each. Source: `SDKAssistantMessage`; capture.
- `user` events carry `{"type":"tool_result","tool_use_id","content","is_error"}` plus a top-level `tool_use_result`, the tool's structured output. Source: `SDKUserMessage` (`tool_use_result?: unknown`); capture.
- Tool inputs: Bash `{command, timeout?, description?, run_in_background?}`; Write `{file_path, content}`; Edit `{file_path, old_string, new_string, replace_all?}`; Read `{file_path, offset?, limit?}`; Glob `{pattern, path?}`; Grep `{pattern, path?, ...}`; NotebookEdit `{notebook_path, new_source, ...}`. Source: agent-sdk/typescript ("Tool input types").
- `BashOutput` has `stdout`, `stderr`, `interrupted`, `backgroundTaskId?` and no exit-code field. Source: agent-sdk/typescript ("Tool output types").
- Observed, not documented: a non-zero exit comes back as `is_error: true` with content `Exit code N\n...`, and `tool_use_result` is then the string `Error: Exit code N...`. A zero exit comes back as `is_error: false` with a `BashOutput` object. `stream.go` infers the exit status from exactly these two forms and leaves it nil otherwise. Source: capture.
- `result` carries `subtype` (`success`, `error_max_turns`, `error_during_execution`, ...), `is_error`, `num_turns`, `session_id`, `total_cost_usd` and `usage`. Source: `SDKResultMessage`; capture.
- Subagent traffic appears in the same stream with `parent_tool_use_id` set; its tool calls are recorded like the main loop's. Source: headless ("Follow subagent messages").

## Installation on this machine

- `which claude` resolves through a symlink to `lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe` under the nvm prefix, and that file is a native x86-64 ELF, so the CLI needs no `node`. The runner finds the package directory from the symlink and mounts it read-only at `/run/agentprobe/claude-code`. If a future install is a script, it also mounts the `node` prefix. Source: `readlink -f`, `file`.

## Effect capture

The CLI runs a whole turn on its own, so the oracle can only snapshot the task folder before the first turn and after the last. The verdict (`Lost`) is a task-level effect and is set on the task's last record. It is not a per-step effect, unlike the API driver's.
