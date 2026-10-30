# 0108: Acquisition output lock files

Status: accepted and implemented, 2026-10-30
([issue 75](https://github.com/blisspixel/nemalo/issues/75)). Test housekeeping;
no production, format or receipt change.

## Context

`lockOutput` in `internal/acquisition` takes an operating system lock on
`nemalo-acquire-<hash>.lock` in `os.TempDir()`, named by the cleaned absolute
output folder, and never removes the file. Acquisition and recording
acquisition both hold it for the whole write, so two acquisitions into one
output folder, from jobs or terminals, never write one packet at once. The
system releases the lock however the process ends.

Removing the file after unlock is unsafe without more protocol: a second
process may have opened the old file and wait on its lock while a third
creates a new file at the same name, so two writers would hold locks on
different files for one folder. Windows also refuses to remove a file another
process holds open unless every opener allowed it.

In ordinary use this leaves one empty file per distinct output folder. The test
suites create a new output folder per case: on 2026-10-28 a development
temporary folder held 19,087 of these files and `/tmp` 5,253. Measured per run
on 2026-10-30: acquisition 98, app 12, cli 9, tui 9, mcpserver 2, and none
elsewhere.

## Options considered

- Keep the production files and give each acquiring test binary its own
  temporary folder, removed when it ends. No production change.
- Move the lock to a `locks/` folder in Nemalo's state folder. Files still
  accumulate, one per output folder, now in a folder no system cleaner visits.
- Remove the file after unlock, with a recheck after locking that the path
  still names the opened file and a retry otherwise. This needs a remove and
  reopen race protocol and adversarial tests on three operating systems for a
  few empty files.

## Decision

- Production keeps the lock file in the system temporary folder and never
  removes it. The exclusion argument is unchanged: every acquisition into one
  output folder opens the same name, never deleted by Nemalo, so all of them
  lock the same file.
- `internal/tempdirtest.Run` points `TMPDIR`, `TMP` and `TEMP` at a new folder
  for one test binary and removes it when the run ends; a folder that cannot be
  removed fails the run. Every test package that runs an acquisition calls it
  from `TestMain`: acquisition, app, cli, mcpserver and tui. A new package that
  acquires adds the same `TestMain`.
- Lock files in ordinary use stay where system cleaners already remove old
  temporary files (at reboot for a memory-backed `/tmp`, by age elsewhere).

## Consequences

A full `go test ./...` leaves no `nemalo-acquire-*.lock` in the temporary
folder it was given. Users still see one empty file per distinct output folder
until their system clears its temporary folder.

Two limitations predate this decision and are unchanged. Exclusion holds only
between processes that resolve the same temporary folder; processes run with
different `TMPDIR` values are not excluded from each other. An age-based
temporary cleaner that removes a lock file while an acquisition holds it would
let a later acquisition lock a new file; acquisitions are bounded by their
transfer deadline, far shorter than the usual cleaner age.

## Verification

`internal/tempdirtest` tests that the run uses a new folder in the original
temporary folder, sets all three variables and removes the folder with a file
left in it. Each of the five packages was run with an empty temporary folder
and left nothing; a full `go test ./...` run with an empty temporary folder
left no lock file.
