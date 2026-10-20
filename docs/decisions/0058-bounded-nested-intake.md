# 0058: nested intake with one archive-tree budget

Status: accepted and implemented, 2026-10-20. Stage 2 of the
[ordered roadmap](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 50](https://github.com/blisspixel/nemalo/issues/50). Extends decisions
[0017](0017-folder-and-zip-ingest.md), [0043](0043-rar-and-7z-through-7-zip.md),
and [0057](0057-split-zip-intake.md).

## Decision

Ingest traverses supported standalone ZIP, RAR, and 7z children through an
explicit staging tree operation. Existing single-container extraction stays the
cleanup path and continues to preserve nested members. No new decoder, module,
executable, network operation, repair, or source-removal permission is added.

- Read children only from size/CRC-checked private staging files. Use generated
  node directories and flat indexed files, never member paths. Reuse fixed
  handlers, regular-file opens, source identity checks, exact-name stdout
  extraction, ambiguity checks, and the existing per-member Import service.
  Tree selection preserves every occurrence of a duplicate name, rather than
  citing an ambiguous first occurrence. Historical holding labels stay intact.
  One outer tree is staged, imported, and removed before the next begins.
- Allow four archive levels including the outer container. All descendants
  share 10,000 listed members, a 1 GiB output allowance, 8 GiB of Go ZIP reader
  work, 16 MiB of reader diagnostics, and 1,000 subprocess invocations including
  probes and failed runs. Charge each attempted member's declared size plus
  its overrun-detection byte before copying; failed copies do not refund it.
  Listing stdout and all subprocess stderr share the diagnostic allowance;
  extracted stdout uses the reserved member allowance. Retain the 256 MiB
  per-member and 100:1 declared expansion limits. Each standard ZIP directory
  still has its existing bounded parser and ZIP64 preallocation checks.
- Start one deadline from the outer source size using the existing archive
  deadline policy, capped at an hour. Descendants and probes inherit it; shorter
  operation deadlines still apply. Extraction is sequential. Aggregate staged
  member bytes cannot exceed the output allowance. A temporary reader snapshot
  remains at most 4 GiB for the outer source/set, or 256 MiB for a child; only
  one such snapshot is active at a time. Thus the existing outside-checkout
  staging can need up to 5 GiB including a root snapshot, with space checked
  before each copy. A subprocess is not an OS sandbox or a claim of bounded
  decoder-internal memory.
- Nested volume sets remain unsupported in this increment. Preserve recognized
  continuation names and their possible first/terminal siblings before copying
  them as children. Do not assemble renamed private members or infer set identity
  from names. Supported outer volume sets retain their existing checked grouping
  and probes, charged to the same tree diagnostic and subprocess allowances.
  Even a lone nested RAR must explicitly declare a single non-volume container
  through the maintained reader before member extraction; a filename or absence
  of visible continuation names cannot establish that fact.
  These declarations were checked against the tagged
  [RAR4](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHandler.cpp)
  and [RAR5](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/Rar5Handler.cpp)
  reader properties. An empty optional member volume index is unknown, as a
  standalone reader prints it; ZIP set evidence must still state its index.
- A nested ZIP also needs installed-reader proof that it is one ordinary
  container, using the strict extent check already used for joined ZIP chunks.
  Go's public ZIP reader does not establish this: the native experiment in
  decision 0057 reproduced extraction of a contained member from a standalone
  terminal split segment. Probe a fixed private snapshot with `-tzip`, then read
  that same snapshot with the Go extractor. Missing tools preserve the child;
  multivolume, stub, tail, comment, or diagnostic ambiguity refuses it.
  Ordinary outer ZIPs keep their existing Go-only path. The maintained reader's
  [ZIP properties](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Zip/ZipHandler.cpp)
  and the pinned standard reader were checked; no reference code is incorporated.
- Every container and listed member gets a disposition. Unsupported children
  remain preserved with a reason. Depth/work exhaustion, corrupt bytes, or a
  failed child extraction makes the tree incomplete; independently verified
  leaves may still import, as in the existing per-member policy. Missing tools
  remain an explicit unsupported capability, never a claim of checked content.
  Antivirus, format assessment, and semantic completeness remain separate.

## Provenance and recovery

Direct members keep `OUTER!/MEMBER`, including literal `!/` inside a valid
member name. Nested leaves use `OUTER!//["inner.zip","book.epub"]`: the suffix
after the first `!/` is a slash followed by the canonical, parse-equivalent JSON
array of exact member names. A previously eligible direct member cannot begin
with slash, so these labels cannot collide with historical direct-member labels.
Use `textsafe` serialization; never interpret the chain as a filesystem path.
The existing 4,096-byte source-record limit still applies before publication.

Holdings and import intents keep their existing schemas and `archive_member`
source kind. The full source label binds the exact member chain and original
outer path. On recovery, freshly extract the same tree, select the exact label,
and resume it before any other import. Import still verifies its byte identity
and full proposal. Preserve legacy terminal-part labels and stop before new
imports if a pending leaf is unavailable. Repeated ingest deduplicates by the
same source label and bytes. Older executables can read completed source records
but cannot resume a pending nested import; retain it for this version or newer.

## Validation

Use synthetic Go fixtures, injected readers/scanners, and adversarial budget and
recovery tests. Cover mixed nesting, literal provenance separators, corrupt and
unsupported children, ambiguous and incomplete volumes, missing tools, shared
limits including failed attempts, cancellation, cleanup preservation, and lost
staging after an import intent. Tests also cover standalone-container proof,
active-reader cancellation, unavailable or changed pending leaves, provenance
record limits, repeat ingest after failure, and accurate early-return outcomes.

Native validation on Linux amd64 with installed 7-Zip 26.04 used freshly
generated synthetic data outside the checkout. ZIP, stored RAR4, and 7z outer
archives traversed mixed RAR/ZIP children and returned two PDFs with exact
expected SHA-256 bytes. Preview, apply, repeat ingest, source preservation and
cleanup retention passed for each outer format. A lone RAR volume with a fully
contained member was refused before child extraction. A nested ZIP member CRC
failure produced an incomplete ingest, one failed member, and one independent
import, retaining the damaged source unchanged. This proves those reader paths,
not every archive variant or installed reader on every platform. Automated tests
run injected Go readers and scanners, never installed executables or a network.
Canonical local and actual hosted verification are recorded in issue 50's
acceptance receipt before closure.
