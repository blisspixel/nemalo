# Release process

Nemalo currently uses prereleases. A tag or archive does not establish 1.0 support
or complete the roadmap. Release notes must distinguish implemented behavior,
native validation, cross-built artifacts, and remaining limits.

## Verification and packaging

1. Update the changelog, capability docs, roadmap, and `internal/cli.Version`.
2. Run `go run ./cmd/verify`, native `go test -race ./...`, relevant live smokes,
   documentation checks, and adversarial review for consequential changes.
3. Commit the reviewed source and require the actual main CI run to pass on Linux,
   macOS, and Windows. Do not publish while a relevant job remains queued or failed.
4. From the clean committed checkout, run:

   ```sh
   go run ./cmd/package --output dist/0.1.0-alpha.1
   ```

5. Verify `BUILD.txt` against the intended full commit. Verify every `SHA256SUMS`
   entry, extract and smoke the native archive, and check embedded Go build/VCS
   metadata. Create the matching version tag and upload the six archives,
   `SHA256SUMS`, and `BUILD.txt` with explicit release notes.
6. Confirm the published tag targets the tested commit and published assets match
   local checksums. Review tag-triggered CI as well.

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

Native CI runs only its runner architectures. Cross-building another architecture
does not prove execution there. Checksum files detect accidental changes relative
to the release assets; they are not an independent signature or publisher identity.
Downloaded content, validation libraries, personal metadata, and scanner logs must
never enter release assets or Git.
