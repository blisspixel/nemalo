# Release process

Nemalo currently uses prereleases. A tag or archive does not establish 1.0 support
or complete the roadmap. Release notes must distinguish implemented behavior,
native validation, cross-built artifacts, and remaining limits.

## Verification and packaging

1. Update the changelog, capability docs, roadmap, `internal/cli.Version`, and
   the `version` in both plugin manifests under `plugins/nemalo/` (a test
   requires them to match).
2. Run `go run ./cmd/verify`, native `go test -race ./...`, relevant live smokes,
   documentation checks, and adversarial review for consequential changes.
3. Commit the reviewed source and require the actual main CI run to pass on Linux,
   macOS, and Windows. Do not publish while a relevant job remains queued or failed.
   At that commit, dispatch `installed-tools.yml` and `fuzz.yml` (every parser
   fuzz target, two minutes each by default) and require both to pass; read
   their logs rather than relying on the status alone.
4. From the clean committed checkout, run:

   ```sh
   go run ./cmd/package --output dist/0.1.0-alpha.18
   ```

5. Verify `BUILD.txt` against the intended full commit. Verify every `SHA256SUMS`
   entry, extract and smoke the native archive, and check embedded Go build/VCS
   metadata. Create the matching version tag and upload the six archives,
   `SHA256SUMS`, and `BUILD.txt` with explicit release notes.
6. Confirm the published tag targets the tested commit and published assets match
   local checksums. Review tag-triggered CI as well.
7. Run `scripts/install.sh` and `scripts/install.ps1` against the new release on
   each native platform, without a pin and with `--version`/`-Version`, then
   `--uninstall`/`-Uninstall`. The installers select the highest semantic
   version among non-draft releases. The GitHub list order is not that version
   order. They reach a prerelease only after it is published.

The Go packaging command refuses a dirty checkout and existing output, validates
each executable's embedded target and clean VCS revision, and rechecks the source
checkout before finalizing manifests. Choose an ignored or external output. It builds
cgo-free amd64/arm64 executables for all three OSes using the pinned toolchain.
Archives contain only the executable, Apache license, upstream third-party license
notices, and short usage guide. Notices are selected from the executable's embedded
module list and pinned module cache, with missing licenses and replacements rejected.
Packaging uses fixed timestamps and is reproducible for identical binary inputs;
this is not a claim of independently reproduced compiler output. Build failures
leave output for diagnosis and never publish automatically.

Native CI runs every shipped architecture: Linux, macOS and Windows on amd64
and arm64 ([decision 0099](decisions/0099-native-ci-for-every-shipped-architecture.md));
Windows arm64 has no race detector. Cross-building alone does not prove
execution on a target. Checksum files detect accidental changes relative
to the release assets; they are not an independent signature or publisher identity.
Downloaded content, validation libraries, personal metadata, and scanner logs must
never enter release assets or Git.

After its existing executable smoke, each native CI job also runs
`go run ./cmd/nativecheck --terminal` against that exact clean cgo-free
executable and commit. The harness exercises 39 CLI lifecycle cases in a new
outside-checkout workspace: previews and changed-plan refusals, import/source
preservation, content and evidence, explicit bookmarks, export, ZIP ingest,
integrity, cleanup, conflicting raw restore and fresh purge. It then runs 55
current-screen observations in real PTY or ConPTY sessions
([decision 0092](decisions/0092-native-terminal-acceptance.md),
[0094](decisions/0094-terminal-keyboard-and-cursor-access.md)). These cases
disable external tool discovery and keep unscanned holdings in review.
Installed ClamAV or Defender, 7-Zip and FFmpeg are exercised only by the
dispatched `installed-tools.yml` workflow
([decision 0093](decisions/0093-native-installed-tool-acceptance.md)); dispatch
it at the release commit. No report is a real host/plugin test or a release.
Existing recovery, race, coverage, native Windows boundaries and live-release
installer checks stay.
