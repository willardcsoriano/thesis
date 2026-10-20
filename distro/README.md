## Overview

This directory holds everything needed to assemble the SynapseOS image without the network, plus the GUI-mode session that makes a provisioned machine actually run SynapseOS at login. `manifest.tsv` names every dependency (Debian packages, the Go toolchain and modules, the PDF build's pip packages, the language models, the model runtime, and the agent CLIs under study), each with its role: `runtime` rows are what the image itself carries, `study` rows what the agent command and user studies add, `build` rows what building, testing and the paper need. A Go test (`prototype/internal/depcheck`, part of `make -C prototype ci`) fails when the code starts a program no row lists. `check.sh` tells you which are missing on a machine and prints the single install line that fixes them. `hoard.sh` downloads them into `hoard/`, which is git-ignored because it is large, and writes checksums. `install-session.sh` installs `synapseos.desktop` and `synapseos-session` — the login-screen session entry that starts an ordinary XFCE session and launches the conversational TUI fullscreen on top of it (`docs/interface-modes.md` §5). The image build will read from `hoard/`, so nothing is fetched at build time.

## Table of Contents

- [Overview](#overview)
- [Usage](#usage)
- [GUI-mode session](#gui-mode-session)
- [Roles and the image](#roles-and-the-image)
- [Layout of the hoard](#layout-of-the-hoard)
- [Adding a dependency](#adding-a-dependency)

## Usage

```
bash distro/check.sh                 # what is missing, and the one install line
bash distro/hoard.sh --all           # debs, Go modules, toolchain, models  (no elevated privilege needed)
bash distro/hoard.sh --debs          # only the packages
bash distro/hoard.sh --ollama        # also fetch the model runtime archive
bash distro/hoard.sh --pip           # the PDF build's wheels, from tools/requirements.txt (needs tools/venv)
```

`apt-get download` and `go mod download` write only to the current directory. Package downloads use the local package index, so refresh it first with `sudo apt update`, or some packages return 404 as their versions move on.

## GUI-mode session

```
sudo bash distro/install-session.sh   # after: make -C prototype build
```

Installs the built `synapse` binary to `/usr/bin/synapse`, `synapseos-session` to `/usr/bin/`, and `synapseos.desktop` to `/usr/share/xsessions/`. After that, log out — "SynapseOS" appears as a session choice at the greeter, alongside "Xfce Session". Picking it starts an ordinary XFCE session and launches the TUI fullscreen on top of it via `xfce4-terminal`, already part of the desktop this session starts. Not tried through a real login/logout cycle yet.

## Roles and the image

The columns are `kind name probe required purpose`, tab-separated. `probe` is the command whose presence proves the row is installed, so a package the code runs several programs from has one row per program (`coreutils` for `rm`, `true` and `sleep`; `util-linux` for `flock` and `prlimit`). `check.sh` finds a probe on `PATH` or in `/usr/sbin` and `/sbin`. A package with no program of its own has a file it installs as its probe, as an absolute path (`libc6-dev`: `/usr/include/stdlib.h`). Every `purpose` starts with the role:

| Role | Who needs it | In the image |
|---|---|---|
| `runtime:` | `synapse` itself: the analysis and its resolvers, undo, the package and service models, the GUI session (XFCE, lightdm, X), the model and Ollama | yes |
| `study:` | the agent command study (`cmd/agentprobe`: passt, iproute2, util-linux, Node, Claude Code, Gemini CLI) and the corpus oracle's commands | on study machines only |
| `build:` | compiling and testing (Go, its modules, make, gcc), hoarding (curl, jq), and the paper (Chromium, poppler-utils, the `tools/venv` pip packages) | no |

`required` is `yes` only for runtime essentials; `check.sh` exits 1 when one of those is missing. The image's Debian package set is the runtime apt rows:

```
awk -F'\t' '$1=="apt" && $5 ~ /^runtime:/ {print $2}' distro/manifest.tsv | sort -u
```

`hoard.sh --debs` still collects every apt row, so one hoard serves the image, a study machine and an offline build. The kinds are `apt` (a Debian package), `toolchain` (the Go tarball), `gomod` (a module `go.mod` requires directly; the indirect ones come with `go mod download all`), `pip` (a top-level name in `tools/requirements.txt`, installed into `tools/venv`, because Playwright has no Debian package and the venv does not see Debian's `python3-pypdf` or `python3-reportlab`), `model` (an Ollama model) and `binary` (installed by its own installer, named in its purpose: Ollama, and Claude Code from `https://claude.ai/install.sh`, a native binary that needs no Node; Gemini CLI comes from `npm install -g @google/gemini-cli` and runs on Debian's `nodejs`).

## Layout of the hoard

| Path | Contents |
|---|---|
| `hoard/debs/` | every apt-kind package in the manifest plus its dependency closure, minus what a Debian base already carries (Essential, or priority required or important) |
| `hoard/gomod/` | the Go module cache for `prototype/` (mvdan.cc/sh v3.13.1 and the rest) |
| `hoard/toolchain/` | the Go tarball matching `prototype/go.mod`, verified against the published checksum |
| `hoard/pip/` | wheels for `tools/requirements.txt` (only with `--pip`) |
| `hoard/models/` | Ollama manifests and blobs for the models in the manifest, copied from `~/.ollama` |
| `hoard/SHA256SUMS` | checksums of everything above |

## Adding a dependency

Add one line to `manifest.tsv` (kind, name, the command that proves it is present, required or not, purpose starting with its role) and re-run `hoard.sh`. Check the package name on Debian first (`dpkg -S "$(command -v PROGRAM)"` or `apt-cache show PACKAGE`) rather than guessing it. Prefer an existing tool over new code, including inside dependencies. A package the analysis or the corpus oracle shells out to must be listed, because a missing binary makes resolution fail closed rather than silently.

`prototype/internal/depcheck` enforces this for the Go code and `synapseos-session`: it collects the programs named in `exec.Command`, `exec.CommandContext` and `exec.LookPath`, in argv slices (a `[]string` literal passed to a call, or an inner slice of a `[][]string`), in absolute paths under `/bin`, `/usr/bin`, `/sbin` and `/usr/sbin`, and in command lines handed to `executor.Run`, and fails when one is no row's probe. A program the scan cannot see (reached through a table, or run by a Go module such as the clipboard library) goes in the test's `indirect` list, pinned to the file that makes it a dependency; a name it sees that is not a dependency goes in `notDependencies` with the reason. The same test checks the five-column schema, the role prefix, that `required yes` rows are runtime rows, and that every module `go.mod` requires directly has a `gomod` row. Commands under analysis inside corpus and test strings are not dependencies of the code; the ones the corpus oracle needs are listed by hand as `study:` rows. The build scripts (`check.sh`, `hoard.sh`, `tools/`, `pilot/`) are not scanned, so a new program there is added by hand.
