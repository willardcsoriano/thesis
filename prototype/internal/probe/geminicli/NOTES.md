## Overview

These are the facts about the Gemini CLI that this driver depends on. Each was checked on 2026-10-19 against the installed CLI (0.49.0, `gemini --help`), the official docs at the `v0.49.0` tag, the upstream source at that tag, or the installed bundle, and each line names its source. The facts that matter most: headless mode uses the personal subscription when it finds a cached "Sign in with Google" credential, so the driver copies one operator-chosen `oauth_creds.json` into each task's throwaway home. That file holds a refresh token with `cloud-platform` scope, so the agent can read it. The stream carries no exit-code field, so a shell command's exit status is read only from the two texts the shell tool writes, and is nil otherwise. A failed login exits 41 with nothing on stdout, and the runner stops the run on that. No authenticated CLI was available when this was built, so the test stream is hand-made from the source; re-check everything here after the first live run and whenever the CLI version in the records changes.

Doc links below are pinned to the installed version: `https://github.com/google-gemini/gemini-cli/blob/v0.49.0/` + path (shortened to `docs/...` and `packages/...`). `main` is already at 0.66, so do not read the unpinned docs for this install.

## Table of Contents

- [Overview](#overview)
- [Invocation](#invocation)
- [Authentication](#authentication)
  - [What the driver does](#what-the-driver-does)
  - [Risk of the copied credential](#risk-of-the-copied-credential)
- [Environment variables set in the sandbox](#environment-variables-set-in-the-sandbox)
- [Stream schema](#stream-schema)
- [Provenance of the test stream](#provenance-of-the-test-stream)
- [Installation on this machine](#installation-on-this-machine)
- [Effect capture](#effect-capture)

## Invocation

- Headless mode is `-p/--prompt`. It is also triggered by a non-TTY. `-o/--output-format stream-json` gives newline-delimited JSON events. Source: `gemini --help`; `docs/cli/headless.md`.
- `--approval-mode yolo` (same as `-y`) auto-approves all tools. The other modes are `default`, `auto_edit` and `plan`. Source: `gemini --help`.
- `-m/--model` picks the model. `GEMINI_MODEL` sets a default. Source: `gemini --help`; `docs/reference/configuration.md` ("Environment variables").
- `--session-id <uuid>` starts a new session with that id. `--resume <uuid>` continues it ("Resume by ID"). Sessions are stored per project (the working directory) under `~/.gemini`, so all turns of a task share the one throwaway home and cwd. Source: `gemini --help`; `docs/cli/session-management.md`.
- Headless mode refuses an untrusted folder (`FatalUntrustedWorkspaceError`, exit 55). `--skip-trust` or `GEMINI_CLI_TRUST_WORKSPACE=true` trusts it for the session. The driver passes `--skip-trust`. Source: `docs/cli/trusted-folders.md` ("Headless and automated environments").
- Turn limit: no CLI flag. The setting `model.maxSessionTurns` (default -1, unlimited) ends the run with `FatalTurnLimitedError`, exit 53. The counter is an in-memory field of the client, so it counts per invocation, not across resumes. Source: `docs/reference/configuration.md`; `docs/cli/headless.md` ("Exit codes"); `packages/core/src/core/client.ts` (`sessionTurnCount`).
- `-s/--sandbox` (the CLI's own sandbox) is off by default and not passed. Bubblewrap is the isolation. Source: `gemini --help`.
- `-d/--debug` changes what a shell result carries (see "Stream schema"). In the sampled source its other effects are extra logging on stderr and a `debug_enabled` flag in the CLI's usage telemetry. It is off by default (`agentprobe gemini -debug` turns it on), because the study measures the CLI as people run it, and debug stderr was not checked for credential material. Source: installed bundle (`getDebugMode()` call sites).

## Authentication

- Methods: "Sign in with Google" (OAuth, for individual accounts, including Google AI Pro and Ultra subscriptions), a Gemini API key from AI Studio (`GEMINI_API_KEY`), and Vertex AI (ADC, a service-account key via `GOOGLE_APPLICATION_CREDENTIALS`, or `GOOGLE_API_KEY`, each with `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION`). Source: `docs/get-started/authentication.mdx`.
- **Only Sign in with Google uses the personal subscription.** "If you are a Google AI Pro or Google AI Ultra subscriber, use the Google account associated with your subscription." An AI Studio key is billed (or free tier) against the AI Studio project, not the subscription. Source: `docs/get-started/authentication.mdx` ("Sign in with Google", "Use Gemini API key").
- Headless: "Headless mode will use your existing authentication method, if an existing authentication credential is cached." Otherwise it needs the API-key or Vertex environment variables. Source: `docs/get-started/authentication.mdx` ("Running in headless mode").
- The auth type in headless mode is `security.auth.selectedType` from settings (documented setting), or else is derived from the environment: `GOOGLE_GENAI_USE_GCA=true` gives `oauth-personal`, `GOOGLE_GENAI_USE_VERTEXAI=true` gives `vertex-ai`, and `GEMINI_API_KEY` gives `gemini-api-key`. With none of these it exits 41: "Please set an Auth method in your .../settings.json or specify one of the following environment variables...". Source: `docs/reference/configuration.md` (`security.auth.selectedType`); `packages/cli/src/validateNonInterActiveAuth.ts`; `packages/core/src/core/contentGenerator.ts` (`getAuthTypeFromEnv`); observed in the sandbox (`TestRealCLIStartsWithoutHostCredentials`).
- The cached OAuth credential is the file `~/.gemini/oauth_creds.json` (`<GEMINI_CLI_HOME or home>/.gemini/`). It is read and rewritten on refresh by the CLI. A keychain-backed store is used only when `GEMINI_FORCE_ENCRYPTED_FILE_STORAGE=true`. Source: `packages/core/src/config/storage.ts` (`getOAuthCredsPath`); `packages/core/src/code_assist/oauth2.ts` (`fetchCachedCredentials`, `cacheCredentials`); `packages/core/src/utils/paths.ts` (`GEMINI_CLI_HOME`); `docs/reference/configuration.md` (`GEMINI_CLI_HOME`).
- The credential is an OAuth2 user token for the CLI's own client with scopes `cloud-platform`, `userinfo.email` and `userinfo.profile`, and it includes a refresh token. Source: `packages/core/src/code_assist/oauth2.ts` (`OAUTH_SCOPE`).
- No usable cached credential, in a non-interactive session with browser launch suppressed (no `DISPLAY`, or `NO_BROWSER=true`), gives `FatalAuthenticationError`: "Manual authorization is required but the current session is non-interactive...". That is exit 41 with nothing on stdout. Without the suppression, the host CLI instead printed "Opening authentication page in your browser. Do you want to continue? [Y/n]:" on stdout and waited (observed on the host with stdin at /dev/null, killed after 180 s). The sandbox sets `NO_BROWSER=true`. Source: `packages/core/src/code_assist/oauth2.ts`; observed (`TestRealCLIOAuthWithoutLoginFailsFast`, and one host run in an empty temp folder).
- Not documented, found in source: `GOOGLE_GENAI_USE_GCA=true` together with `GOOGLE_CLOUD_ACCESS_TOKEN=<token>` makes the OAuth path use that access token with no file. It would keep the refresh token out of the sandbox, but the token lasts about an hour and must be minted for the CLI's OAuth client, which no documented command does. **Not used**, recorded as the safer option to revisit. Source: `packages/core/src/code_assist/oauth2.ts` (`initOauthClient`).
- `.env` files are loaded from the first `.gemini/.env` found walking up from the cwd, then `~/.gemini/.env`. The throwaway home has none. Source: `docs/get-started/authentication.mdx` ("Persisting environment variables").

### What the driver does

- **Subscription (`-oauth-creds PATH`).** The operator names the file. The driver reads it once, checks only that it is a JSON object with a `refresh_token` (and never prints it), then for each task writes a copy to `<workdir>/home/.gemini/oauth_creds.json` (mode 0600, workdir 0700). It also writes `settings.json` with `selectedType: "oauth-personal"`, and deletes both with the workdir. The real `~/.gemini` is never mounted: `/home` is a tmpfs.
- **API key.** `GEMINI_API_KEY` from the operator's environment goes into the child's environment only, with `selectedType: "gemini-api-key"`.
- Giving both is refused, and so is giving neither. `GOOGLE_CLOUD_PROJECT` is passed through when set (needed for Workspace and Code Assist licences only).

### Risk of the copied credential

The agent runs shell commands as the same uid inside the sandbox, so it can read `~/.gemini/oauth_creds.json`, just as the Claude driver's agent can read its token from `/proc`. This one is broader. It is a refresh token with `cloud-platform` scope over the operator's Google account, valid until revoked, and it can call any Google Cloud API that account can reach, not only model requests. The sandbox's network reaches only the internet by default (`-net isolated`), but that is where the credential works. To reduce the risk:

- Sign in with a Google account used only for the study, and do it under a separate config root so the everyday login stays out of it: `GEMINI_CLI_HOME=$HOME/.agentprobe-gemini gemini`. The file is then `$HOME/.agentprobe-gemini/.gemini/oauth_creds.json`.
- Revoke the CLI's access in the Google account's security settings after the study.
- The raw streams are model output. The credential is never in argv, the environment, the strace file or the run folder (`TestRunTaskInRealSandbox` checks the run folder). A crash that skips cleanup can leave the copy in `$TMPDIR/agentprobe-*/home/.gemini/`, which is mode 0700.

## Environment variables set in the sandbox

- `PATH` (the node binary first), `HOME=/home/agent` (the fixture), `GEMINI_CLI_HOME` (the state folder that holds `.gemini`, outside the fixture), `USER`/`LOGNAME=agent`, `SHELL=/bin/bash`, `TERM=dumb`, `LANG=C.UTF-8`, `NO_BROWSER=true`, plus `GEMINI_API_KEY` or `GOOGLE_CLOUD_PROJECT` when given. Nothing else from the operator's shell is passed.
- Settings written: `general.enableAutoUpdate: false` and `general.enableAutoUpdateNotification: false` (the install is read-only anyway). `model.maxSessionTurns` is set from `-max-turns`. Source: `docs/reference/configuration.md`.
- Not changed: `privacy.usageStatisticsEnabled` (default true) and telemetry. As with the Claude driver, the CLI is measured as people run it. The effect of turning statistics off on behaviour was not checked.

## Stream schema

- Event types: `init`, `message`, `tool_use`, `tool_result`, `error` (non-fatal warnings), and `result`. Source: `docs/cli/headless.md`; `packages/core/src/output/types.ts`.
- `init`: `session_id`, `model`. The model is the configured one and can be a routing alias. No CLI version is carried, so the driver takes it from the mounted package's `package.json`. Source: `packages/cli/src/nonInteractiveCli.ts`.
- `tool_use`: `tool_name`, `tool_id`, `parameters`. `tool_result`: `tool_id`, `status` (`success`/`error`), `output` (the tool's display text), and `error {type, message}`. Source: `packages/cli/src/nonInteractiveCli.ts`.
- Tool names and parameters: `run_shell_command {command, description, dir_path, is_background}`, `read_file`, `write_file`, `replace`, `list_directory`, `glob`, `grep_search`, `read_many_files`, `web_fetch`, `google_web_search`, `write_todos`, `ask_user`, and others. The shell tool is Kind `bash`. Every other tool is recorded under its own name with its parameters as JSON. Source: `docs/reference/tools.md`.
- `result`: `status`, `error {type, message}` on failure (type is the error's class name: `FatalAuthenticationError`, `FatalTurnLimitedError`, `TerminalQuotaError`, ...), and `stats {total_tokens, ..., tool_calls, models: {<model id>: {...}}}`. When exactly one model served the turn and `init` named an `auto...` alias, that model id is recorded as `model_version`. Source: `packages/cli/src/nonInteractiveCli.ts`; `packages/core/src/output/stream-json-formatter.ts`.
- Exit codes: 0 success, 1 general/API error, 41 authentication, 42 input, 44 the CLI's sandbox, 52 configuration, 53 turn limit, 54 tool execution, 55 untrusted workspace, and 130 cancelled. The docs list only 0, 1, 42 and 53. Source: `docs/cli/headless.md`; `packages/core/src/utils/errors.ts`; `packages/core/src/utils/exitCodes.ts`. The runner stops the whole run on 41, 44, 52 or 55, and on result types `FatalAuthenticationError`, `AccountSuspendedError`, `TerminalQuotaError`, `FatalConfigError`, `FatalUntrustedWorkspaceError` and `ProjectIdRequiredError`.
- **No exit-code field.** A command that exits non-zero still has `status: "success"`. `error` is set only when the shell itself failed to run it. In normal mode, `output` is the command's own output, and only a command that printed nothing and failed is described, as exactly `Command exited with code: N`. In debug mode, `output` is the text the model sees: `Output: ...`, then optional `Error:`, `Exit Code: N` (non-zero only), `Signal:`, `Background PIDs:`, and `Process Group PGID: N`. `stream.go` reads N from those two forms. In debug mode it reads 0 from a successful result whose trailer has the PGID line with no exit code and no signal. Everything else is nil. Source: `packages/core/src/tools/shell.ts`. A command whose own last output line is `Exit Code: N` would be misread in debug mode. This was accepted as implausible.
- Policy refusals come back as `tool_result` with `error.type: "policy_violation"` and are counted as denials. Source: `packages/core/src/tools/tool-error.ts`.

## Provenance of the test stream

The host CLI was not signed in on 2026-10-19 (it asked to open a browser), and signing in was out of scope. So `testdata/ls_two_commands.jsonl` and the inline streams in the tests are hand-made from the emitter code cited above, not captured. After the first live run, replace the fixture with a scrubbed capture (paths under /home, emails, session ids) and re-check the exit-code texts.

## Installation on this machine

- `which gemini` resolves through a symlink to `lib/node_modules/@google/gemini-cli/bundle/gemini.js` under the nvm prefix, a `#!/usr/bin/env node` script. The package (with its own `node_modules`, including `node-pty`) is mounted read-only at `/run/agentprobe/gemini-cli`. The `node` binary alone (an ELF that links only system libraries) is mounted at `/run/agentprobe/node/bin/node`. The CLI runs as `node .../bundle/gemini.js`. Source: `readlink -f`, `file`, `ldd`, `package.json` (`bin`, `engines: node >=20`).

## Effect capture

As with the Claude driver, the CLI runs a whole turn on its own. The oracle snapshots the task folder before the first turn and after the last, and the verdict (`Lost`) goes on the task's last record.
