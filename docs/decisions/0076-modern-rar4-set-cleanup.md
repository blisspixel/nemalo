# 0076: recoverable cleanup of completely covered modern RAR4 sets

Status: accepted, 2026-10-21 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
Remaining RAR volume layouts keep this stage unfinished. No release is included.

## Prerequisite

Decision 0075's read-only modern RAR4 volume framing, isolated/assembled table
binding and shared limits passed local/native verification and all six hosted
jobs at `10466f6587f330298566194c2cf0a4abadacabf0` in
[run 39563293625](https://github.com/blisspixel/nemalo/actions/runs/39563293625).
[Receipt 6310970649](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6310970649)
was posted and read back while issue 57 remained open, before this decision and
application edits. The accepted prerequisite retains every RAR4 volume source.

## Decision

Permit modern root RAR4 sets through the same cleanup service only after checked
private snapshots, complete physical framing, exact isolated and assembled
reader tables, complete extraction, every currently verified held leaf and final
original-source hashes pass. Explicitly request RAR4SetCoverage with Coverage and
RAR4Coverage. Select distinct `rar4-set/1` from measured RARSetVersion=4;
RARSetVersion=5 keeps `rar5-set/1`. Filenames, child versions and standalone facts
cannot select either outer-set authority. Require Whole, Complete and no coverage
gap. No parser subset, decoder, module, executable, public option or network
operation is added.

Reuse the supported modern 2..1000-part shape: exact order from ordinal one,
one portable numeric family and one exact parent, 256 MiB per source, 4 GiB total
snapshots and the shared 64 GiB source-hash budget. Keep known metadata and
reserved-end rules unchanged. Old naming, nested volume sets, volume-wide CRCs,
hidden/service/recovery material and every uncertain or unsupported fact remain
preserved. Completely covered standalone children reuse existing tree checks.

The whole set is one recoverable entry with canonical exact leaf chains, sizes,
hashes and holdings. Existing standalone and tree proof IDs keep their meanings;
neither tree proof accepts an outer RAR set. Persisted shape validation is shared
by the two set IDs and cannot infer physical format without bytes. Bind format
through measured planning, fresh purge and the immutable original proof instead.

Reuse cleanup's verified copy, hard-link publication, removal and recovery
journals, and raw no-replace restore. Fresh purge repeats the complete held-set
and leaf check, including actual format/proof equality. Its schema-2 intent binds
the distinct proof, exact original file indices and canonical leaf digest.
Partial recovery uses only supported immutable original evidence plus fresh
remaining identities and current verified holdings. Never reconstruct missing
parts, adopt replacements, migrate receipts or create a new recovery plan.
Validate every known shape and group binding before independent removal;
unknown authority retains bytes.

An actual accepted prerequisite executable must refuse or retain unfamiliar
authority and preserve remaining bytes. It already understands modern RAR layout
syntax, so retention may complete an unfamiliar pending intent with zero purged
files rather than an error. Verify JSON outcomes and bytes, and test raw restore
separately without a reader. Use separate snapshots for preceding-binary retention
and current partial recovery; do not claim recovery of an already completed
preceding intent. Explicitly distinguish constructed durable snapshots from
observed interruption hooks or power failures.

## Verification required

Use synthetic Go fixtures and injected readers/scanners in offline tests. Retain
every existing RAR5 case and assertion. Reuse lifecycle, per-part cleanup/purge
and restore interruptions, proof/leaf/index bindings, independent groups,
replacements, cancellation, stale sources, hash budgets and current holdings.
Cover 2/3/more parts, direct and nested leaves, known metadata, zero-reserved ends,
empty directories and complete siblings. Exercise cross-version fresh-proof
substitution in both directions, immutable known-proof mismatches and malformed
later groups before any independent removal. Keep missing/unknown evidence,
hidden records, aliases, missing readers/volumes and unheld material preserved.

Separately run generated native stored sets through cleanup, exact raw restore
and purge with installed 7-Zip, plus hidden/unheld refusals and exact compressed
reference output. Unsupported binary reference output establishes no book
eligibility. Exercise the actual preceding executable built from clean accepted
`10466f6`, SHA-256
`1b43dc8c2f69a816f60f160d6e16712ef305f0cd70c26f2dafd7f5470a038c47`.
Keep generated/reference content and reports outside the checkout. Require
canonical verifier, full native-cgo race, Windows cross-vet, documentation checks
and actual all-six hosted CI on the exact final commit. Preserve instrumentation,
cases, assertions, thresholds, timeouts and real durability operations. Post/read
acceptance while issue 57 remains open; keep it open for remaining volume work.

Native Linux validation with installed 7-Zip 26.04 passed seven generated set
lifecycles: 2/3/7 stored parts, standalone RAR4 and RAR5 children, complete
first/last-volume siblings with an empty directory, and zero-reserved footers.
Cleanup, exact raw restore and fresh purge passed. The actual accepted preceding
executable refused new fresh cleanup/purge plans and restored the raw originals
with no reader on PATH. Separate constructed durable snapshots showed its
unknown-proof recovery retained every remaining byte with zero purged and no
error, including one absent part reported honestly on restore. Separate current
snapshots resumed whole and partial purge without reopening the reader or
reconstructing missing parts. These are constructed states, not observed crashes.

Hidden middle-volume service data, unheld notes and unsupported compressed binary
content retained every source through preview/apply. The compressed reference
returned its independently regenerated exact 64-byte pattern, SHA-256
`353d5f7a0789034186922e5834f666f36fa1f98e18f53fb4140b336ba090a923`.
No book eligibility, broad conformance or safety was inferred, and no installed
scanner, network operation or downloaded code was executed. Full acceptance
remains separate and tracked in issue 57.
The existing native RAR5 harness also passed its six lifecycles, preceding-binary
checks, separate unsupported retentions and exact 1,200-byte compressed output
after this extension; its proof meaning remains unchanged.
