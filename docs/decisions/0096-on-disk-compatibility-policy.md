# 0096: on-disk compatibility policy toward 1.0

Status: accepted, 2026-10-24. First stage 6 increment. It states the policy that
existing behavior already follows, closes the reads that did not, and adds a
regression fixture. It freezes nothing by itself; the 1.0 freeze applies this
policy to [FORMATS.md](../FORMATS.md).

## Evidence

An inventory of every persisted and externally read format found that each
Nemalo-written record carries `schema_version` 1 and that library journals are
read strictly: unknown fields, non-canonical lines and higher versions stop the
library for review. There is no migration code and no alpha-library detection.

On 2026-10-24 the published `0.1.0-alpha.2` executable created a library,
imported an unscanned EPUB and wrote a catalog snapshot. A clean `782dadc` build
then ran status, list, whole-library check, content read, citation, catalog list
and catalog audit with unchanged bytes, imported another book and enabled and
saved a bookmark; the check reported both holdings verified. The library also
opened after being copied to another folder. Every format had changed only by
addition, so alpha libraries need no upgrade step. Alpha.2 then refused the
library because of the new `bookmarks.jsonl`, safely but with a generic
"control state needs review" message.

Four reads were looser than the policy: intake `receipt.json` and
`recording.json` versions were not checked, and the cleanup `manifest.json`
version was protected only by its journal hash.

## Policy

1. Every Nemalo-written persisted record or file carries a version. Readers
   accept only versions they know and refuse others without changing bytes.
2. Formats change by addition until a deliberate version change. A new control
   file or field is new behavior; older executables refuse it, never guess.
3. A newer executable opens every older supported library in place. A real
   format change needs its own decision, a new version and either in-place
   reading of the old version or a refusal that names the upgrade path.
4. Representation, extractor, proof and plan identifiers never change meaning.
   Changed rules get a new identifier; earlier ones stay selectable and their
   continuations valid (decision 0011).
5. Refusals caused by an unknown control file or a higher record version say
   that a newer Nemalo may have written it. Other malformed state keeps the
   plain review refusal. Nothing that was refused becomes accepted.

## Decision

Check `schema_version` on intake receipts (refusing above 1 with a newer-version
message, and a missing version as not Nemalo intake), on recording receipts and
on cleanup manifests. Add the newer-version explanation to library journal and
control-file refusals. Commit `internal/library/testdata/alpha2-library`, made
by the alpha.2 release under neutral paths, and require every build to open it
in place, verify its holding, import into it and enable bookmarks. Synthetic
test receipts now carry `schema_version` 1, as every real receipt has since
the first acquisition commit.

The remaining stage 6 work is to complete and freeze FORMATS.md, define the
stability of CLI and MCP result fields, and pass the release gates.
