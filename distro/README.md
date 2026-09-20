## Overview

This directory holds everything needed to assemble the SynapseOS image without the network. `manifest.tsv` names every dependency (Debian packages, the Go toolchain and modules, the language models, and the model runtime) with what it is for and whether it is required. `check.sh` tells you which are missing on a machine and prints the single install line that fixes them. `hoard.sh` downloads them into `hoard/`, which is git-ignored because it is large, and writes checksums. The image build will read from `hoard/`, so nothing is fetched at build time.

## Table of Contents

- [Overview](#overview)
- [Usage](#usage)
- [Layout of the hoard](#layout-of-the-hoard)
- [Adding a dependency](#adding-a-dependency)

## Usage

```
bash distro/check.sh                 # what is missing, and the one install line
bash distro/hoard.sh --all           # debs, Go modules, toolchain, models  (no elevated privilege needed)
bash distro/hoard.sh --debs          # only the packages
bash distro/hoard.sh --ollama        # also fetch the model runtime archive
```

`apt-get download` and `go mod download` write only to the current directory. Package downloads use the local package index, so refresh it first with `sudo apt update`, or some packages return 404 as their versions move on.

## Layout of the hoard

| Path | Contents |
|---|---|
| `hoard/debs/` | every apt-kind package in the manifest plus its dependency closure, minus what a Debian base already carries (Essential, or priority required or important) |
| `hoard/gomod/` | the Go module cache for `prototype/` (mvdan.cc/sh v3.13.1 and the rest) |
| `hoard/toolchain/` | the Go tarball matching `prototype/go.mod`, verified against the published checksum |
| `hoard/models/` | Ollama manifests and blobs for the models in the manifest, copied from `~/.ollama` |
| `hoard/SHA256SUMS` | checksums of everything above |

## Adding a dependency

Add one line to `manifest.tsv` (kind, name, the command that proves it is present, required or not, purpose) and re-run `hoard.sh`. Prefer an existing tool over new code, including inside dependencies. A package the analysis or the corpus oracle shells out to must be listed, because a missing binary makes resolution fail closed rather than silently.
