# 0077: bounded old-name RAR identity-probe diagnostics

Status: accepted, 2026-10-22. Intake only, tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
Old-name and nested-volume cleanup remain unsupported.

## Evidence

The modern root RAR4 writer passed all six hosted checks at
`b13eb80637d249a92082677d657eff67ca192226` in
[run 39842481833](https://github.com/blisspixel/nemalo/actions/runs/39842481833).
Its [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6350139725)
was posted and read back while issue 57 remained open, before this decision and
application edits. Its source-retention boundary remains unchanged here.

Separate [old-name research](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6346987389)
used generated RAR4/RAR5 sets of 2, 3, 7, 103 and 901 parts and installed 7-Zip
26.04. Five of ten Nemalo intake cases completed; five refused isolated probes.
All ten source sets remained unchanged and every cleanup preview moved zero.
Direct full-set listing and extraction of both 901-part layouts succeeded with
independently expected PDF bytes. This does not establish Nemalo intake success,
framing completeness, checked publication or cleanup authority for those layouts.

The pinned reader's
[volume-name implementation](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarVol.h)
recognizes only a few old-name starting extensions and otherwise generates a
fallback. Its numeric naming path increments a trailing number without old-style
letter rollover. Isolated RAR4 probes from zero-based index 3 report the current
private basename plus `.r00`; RAR5 probes at indices 100, 200, through 800 report
the current letter followed by `100`. Full assembly from the first volume uses
the actual closed set. No upstream implementation is incorporated into Nemalo.

## Decision

Extend only decision 0056's isolated missing-next identity evidence. In addition
to its exact canonical next name, accept one exact reader fallback only when:

- The layout is old naming, the current zero-based index is within 1..899,
  and the caller's next name equals the canonical next private old-name volume.
  The terminal `.z99` volume has no fallback exception.
- RAR4's index is at least 3 and the missing name is exactly the current generated
  private basename followed by `.r00`.
- RAR5's index is a positive multiple of 100 and the missing name is exactly its
  current generated letter followed by `100`.

Keep exact handler, physical size, one visible volume, actual ordinal, first
role and RAR4 naming flags. Allow only the existing exit-code/diagnostic contract.
Reject arbitrary prefixes, duplicate/extra errors, warnings and contradictory
evidence. Preserve private isolated snapshots, per-boundary split continuity and
the later error-free assembled table before extraction. Shared read, byte,
member, output, process, cancellation and deadline limits remain unchanged.

This changes neither framing nor writer authority. Old-name intake keeps
`Whole=false` and no RAR set proof version. Explicit old-name set coverage stays
refused, and existing standalone, tree, modern-set and recovery proof meanings
do not expand. Sources remain intact. Do not change the decoder, reader version,
dependencies, public API, network behavior, schemas or persisted evidence.

## Acceptance

Require synthetic header cases for every letter rollover and terminal boundary,
the exact permitted fallbacks, malformed/error/role/ordinal/type/size mutations,
and modern/early/non-rollover refusals. Injected-reader integration must retain
continuity, clean-assembly, budgets, cancellation and source-preservation checks.
Separately repeat the generated native 2/3/7/103/901-part RAR4/RAR5 cases, expecting
exact PDF bytes, unchanged sources and application preview state, and cleanup
Move=0. Unit tests invoke no installed reader, scanner or public network.

Canonical verification, native-cgo race, documentation review and actual hosted
Linux/macOS/Windows success must bind the final commit. Post and read back its
acceptance while issue 57 is open before a later framing or writer increment.
This bounded intake correction does not close issue 57 or release Nemalo.
