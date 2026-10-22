# Installation

Nemalo is an early Go CLI/TUI application for Linux, macOS and Windows.
Published prereleases may lag the current source. Review the
[implemented capabilities and limits](DEVELOPMENT.md) before handling a library.

## Native releases

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1 | iex
```

The installers fetch the newest published release archive, verify it against the
release `SHA256SUMS`, and install one executable to `~/.local/bin` or
`%LOCALAPPDATA%\Programs\nemalo`. Use `--version` on Linux/macOS or `-Version`
on Windows to pin a release, and `--uninstall` or `-Uninstall` to remove it.
Checksums establish consistency with the published checksum file. You can inspect
the scripts before executing them or download the archive and checksum file from
[GitHub Releases](https://github.com/blisspixel/nemalo/releases).
See [the installer decision](decisions/0013-release-installer-scripts.md).

## Build from source

With Go 1.27.3 or a compatible supported toolchain, from the repository root:

```sh
go build -trimpath -o bin/ ./cmd/nemalo
```

The executable is `bin/nemalo` (`bin/nemalo.exe` on Windows). No Python or Node
runtime is needed. Run `nemalo help` for the command overview and `nemalo doctor`
for scanner setup and optional tool availability. Neither installs tools.

## Agent hosts

With `nemalo` on PATH, install the read-only tools in Claude Code:

```text
/plugin marketplace add blisspixel/nemalo
/plugin install nemalo@nemalo
```

See [agent integration](AGENT-INTEGRATION.md#plugin-packages) for MCP configuration,
plugin packages, explicit filesystem scopes and write/network permissions.
