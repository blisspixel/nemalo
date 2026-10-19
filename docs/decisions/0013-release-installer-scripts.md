# 0013: shell and PowerShell installers for published releases

Status: accepted and implemented, 2026-10-11. Resolves
[issue 7](https://github.com/blisspixel/nemalo/issues/7). A recorded exception to
the Go-only tooling rule in [decision 0001](0001-language-and-stack.md).

## Need

Releases publish checksummed cgo-free archives for Linux, macOS, and Windows,
but the only documented installation was a Go source build. A person without a
Go toolchain cannot run a Go installer, so the installer itself cannot be Go.
This is the concrete unmet requirement that justifies the exception.

## Decision

`scripts/install.sh` (POSIX `sh`, Linux and macOS) and `scripts/install.ps1`
(Windows PowerShell 5.1 and PowerShell 7) install one published release:

- Resolve the highest semantic version among non-draft releases, because
  current releases are prereleases and `/releases/latest` excludes them, or use
  `--version` / `-Version`. The releases list endpoint does not return that
  version first. On 2026-10-13 its first item was `v0.1.0-alpha.9` after
  `v0.1.0-alpha.10` was published, despite the later commit date, later
  `published_at`, and higher release id. The scripts read one page of up to
  100 releases and compare versions themselves. A full page fails closed
  rather than ignoring a later page.
- Download the platform archive and `SHA256SUMS` over HTTPS, require exactly one
  valid digest for the archive, and refuse a mismatch. Extract only the
  executable, run `nemalo version` from the staging directory, then replace the
  installed file atomically within its directory.
- Install to `~/.local/bin` or `%LOCALAPPDATA%\Programs\nemalo`, overridable
  with `--dir`, `-Dir`, or `NEMALO_INSTALL_DIR`. `--uninstall` / `-Uninstall`
  removes only the executable (and on Windows its user PATH entry).
- The shell installer never edits shell profiles; it prints the PATH line to
  add. The PowerShell installer adds the directory to the user PATH unless
  `-NoPath`, editing the registry value directly so unexpanded entries such as
  `%USERPROFILE%\bin` and the value kind are preserved.
- No library, intake folder, configuration, or state is touched. Nothing else is
  downloaded or installed, and there is no Go fallback inside the scripts.
- `SHA256SUMS` is fetched before the archive. Each download starts from an
  empty file and is tried up to three times with a growing pause after a
  dropped connection, a timeout, or a server error; curl also retries its own
  transient HTTP errors. A missing release or archive (HTTP 404) fails at once
  and names the version. A failure to write the file (curl exit 23, wget exit
  3, or a .NET I/O error) fails at once and says the temporary folder may be
  full, since a retry cannot help. An empty response is a failure. A temporary
  folder that cannot be created falls back to `~/.cache` or
  `%LOCALAPPDATA%`. The checksum is verified as before.

## Limits

`SHA256SUMS` detects corruption relative to the release assets; it is not an
independent signature or publisher identity, as [RELEASING](../RELEASING.md)
states. The scripts are first-party tooling with no runtime role. They are
exercised by hosted CI against the newest published release on Linux, macOS, and
Windows, including a pinned version and uninstall; the Go build remains the
source option.
