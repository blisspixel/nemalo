# 0093: native installed-tool acceptance

Status: recorded before implementation, 2026-10-24, and accepted on all three
hosted OSes at `392f21f`; see [validation](../VALIDATION.md#installed-tool-acceptance).
Stage 5 acceptance tooling, separate from a release, scanner quality, codec
conformance and real host calls.
Decisions [0091](0091-native-executable-lifecycle-acceptance.md) and
[0092](0092-native-terminal-acceptance.md) passed their exact native gates.

## Requirement

Installed ClamAV, 7-Zip and FFmpeg evidence is Linux-only and came from local
review scripts that are not in the repository. macOS and Windows support for
scanning, archive intake and decoding therefore rests on unit tests with
injected adapters. Add a repeatable, opt-in mode to the existing Go acceptance
command instead of more one-off scripts.

## Explicit tools

`nativecheck --installed-tools` runs after the unchanged 39 cases and any
terminal sessions. Every tool is named by an absolute path: `--clamscan` on
Linux and macOS, `--defender` (the system `MpCmdRun.exe`) on Windows, and
`--sevenzip`, `--ffmpeg` and `--ffprobe` on every OS. Each path must be an
existing executable regular file outside the workspace and checkout. Nothing is
discovered from the caller's PATH, downloaded or installed by the tool. Every
selection is validated before any fixture work.

The installed cases use a separate child environment. It keeps the owned
configuration, state, cache and temporary folders, and its PATH contains only the
named tools' directories, in a fixed order and recorded in the report. Those
directories may hold other programs; that is reported rather than hidden. On
Windows, only the one `ProgramData` or `ProgramFiles` root from which Nemalo's
scanner resolution finds the selected Defender is restored. The 39 cases keep
their empty tool environment, so their missing-tool refusals are unchanged.

Before any run, the harness confirms that Nemalo's lookup order (`7zz`, `7z`,
`7za` for 7-Zip) over that PATH yields exactly each selected executable. Scan and
decode results must then name the selected scanner and FFmpeg. A mismatch fails
acceptance; it is never repaired by searching elsewhere. For each tool, the
report records the path, size, SHA-256 within a bounded read allowance, and a
bounded first version line from an argument-array invocation. Where the
selected path is a wrapper script or package shim, those are its own bytes.

## Cases

Use owned synthetic fixtures only, with these exact 13 case identities in order:

1. `installed-doctor`: doctor reports the selected scanner, 7-Zip, FFmpeg and
   FFprobe, and on Linux and macOS ClamAV signatures that are present.
2. `installed-scan-clean`: `check --scan` of a synthetic EPUB reports
   `no_detections_reported` from the selected scanner. This is coverage
   evidence, not a safety claim.
3. `installed-scan-detection`: the standard harmless 68-byte EICAR test file,
   assembled at run time from reversed text and checked against its published
   SHA-256, returns scanner findings and an unsuccessful result. Its exact bytes
   are unchanged; no remediation is requested. A scanner that removes it fails
   acceptance.
4. `installed-library-init`: a separate library for these cases.
5. `missing-archive-reader`: the selected 7-Zip creates a 7z archive of the
   EPUB. `library ingest --apply --scan` under the empty tool environment lists
   it as not imported because 7-Zip is unavailable and imports nothing.
6. `installed-archive-ingest`: the same ingest with the selected tools imports
   its member as checked. Both leave the archive bytes unchanged.
7. `missing-decoder`: the selected FFmpeg encodes a two-second synthetic AAC
   tone as M4A. `check --decode` under the empty tool environment reports the
   decoder unavailable and fails.
8. `installed-decode-check`: the same check reports a passed full decode by the
   selected FFmpeg.
9. `installed-pcm-import-preview` and 10. `installed-pcm-import-apply`: an
   unscanned import that stays in review.
11. `installed-pcm-units`, 12. `installed-pcm-read` and
    13. `installed-pcm-continuation`: explicit `mp4-pcm/1` units, a 500-sample
    read and its continuation. The harness separately runs the selected FFmpeg
    with the same fixed output form (mono s16le at the reported rate) as a
    reference. The total sample count and full PCM hash must equal the
    reference, and both returned ranges and their hashes must equal the
    matching reference samples exactly.

The reference uses the same installed decoder. It checks Nemalo's capture,
selection, WAV packaging, hashes and continuation against that tool, not the
decoder's correctness. Fixture generation and the reference are recorded as tool
runs, separately from Nemalo command cases.

Library, source and archive snapshots must be unchanged across read-only and
refused operations, as in the existing cases. Default tests remain offline and
never run an installed tool; injected runners cover argument validation,
environment construction, mismatched executables, case order and refusals.

## Bounds

Installed commands get a three-minute per-command deadline instead of 30
seconds, because each ClamAV run loads its full database. The installed mode adds
ten minutes to the overall deadline. Tool runs share the existing output,
diagnostic and fixture read allowances; reference PCM is limited to the 1 MiB
fixture read allowance and executable hashing to 1 GiB of reads across all tools.

## Hosted runs

A separate manually dispatched workflow, `installed-tools.yml`, installs the
tools with each runner's package manager (apt, Homebrew, Chocolatey; Windows
images include 7-Zip and Defender), updates ClamAV signatures with freshclam, and
runs the mode against a clean cgo-free executable at the exact commit. It does
not run on every push, so mirror or signature outages cannot make ordinary CI
red. GitHub dispatches only workflows present on the default branch, so the
opt-in mode lands on `main` after ordinary CI and is then dispatched there. Tool
versions float with the runner images and are recorded in each report rather
than pinned. Acceptance requires a dispatched run at the exact head on all
three OSes.

## Doctor and 7-Zip naming

Homebrew's current `sevenzip` formula installs only `7zz`. Doctor looked up an
executable literally named `7z`, so it reported 7-Zip unavailable on such a Mac
while archive intake found and used `7zz`. Doctor now reports the same
`staging.SevenZip` discovery that intake uses. A regression test failed before
the change. This corrects reporting only; intake behavior is unchanged.

This establishes selected installed-tool behavior on hosted runners. It is not
detection coverage, malware resistance, every archive layout, codec or player,
graphical reader, power-loss, or release acceptance.
