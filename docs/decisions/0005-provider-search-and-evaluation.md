# Decision 0005: provider search and source evaluation

Status: accepted and implemented, 2026-10-08.

## Product increment

Discovery and evaluation are core library operations. `search --source archive`
adds Internet Archive alongside the default Open Library search;
`evaluate archive:ITEM` inspects a specific source item before acquisition. CLI, JSON, and
TUI use the provider registry in `internal/app`, adapters in `internal/discovery`,
and shared presentation. There is no new runtime dependency.

This increment performs metadata reads only. Production transfer, resume,
rights-policy selection, checked publication, and source cleanup remain planned.
It neither ports nor wraps the Archive Python client. The existing Go collection
harness remains the transfer building block for later production acquisition.

## External contracts

Reviewed the Archive [advanced search interface](https://archive.org/advancedsearch.php),
[metadata read API](https://archive.org/developers/md-read.html),
[metadata schema](https://archive.org/developers/metadata-schema/index.html), and
[automated-access guidance](https://archive.org/developers/bots.html).
Small live requests verified JSON search parameters and arbitrary `start` offsets.

Archive search uses `advancedsearch.php`, requesting only identifier, title,
creator, language, and media type. It bounds pages to 50, offsets to 10000,
and queries to 1000 bytes. User query syntax is preserved inside a text/audio
media filter. Results are sorted by identifier for predictable pagination; the
remote catalog can still change between requests. A mismatched response offset,
invalid/duplicate ID, unexpected media type, or malformed result fails the request.
Audio media type does not establish that an item is an audiobook.

Evaluation uses `/metadata/ITEM?extended_err=1` and validates the returned item
identifier. Archive IDs are explicitly namespaced and follow its documented ASCII
identifier contract, up to 100 characters. Missing/deleted items, extended errors,
missing file lists, or more than 5000 listed files produce incomplete evaluations.
String/list variations in source metadata are handled without inventing normalized
languages or licensing. Date, publisher, creators, languages, rights statements,
and license URL strings are retained as declarations.

Every offered file retains source format, filename candidate kind, declared size
when valid, validated source MD5/SHA-1 when present, and separate access evidence.
Names are unique relative paths without traversal, backslashes, colons, NUL, or
line breaks. Size and checksums are source claims, not locally measured facts.
The metadata JSON value receives a SHA-256 and observation time; this is not
authentication or a content hash.

## Access evidence

Restriction flags are four-state evidence: `true`, `false`, `not_declared`, or
`unknown`. Missing flags are not proof of public access. Item restriction, dark,
lending, and private-file indicators are checked. Present unrecognized values
remain uncertain. Any declared or uncertain item restriction suppresses all file
URLs; file-level restrictions suppress that file's URL. The initial policy is
deliberately conservative and can hide public ancillary files on restricted items.
ACSM/LCPL delivery documents receive no content download URL.

Other files are `public_file_candidate`, with URLs constructed from the validated
item ID and escaped relative filename on `archive.org`. Source-provided server,
directory, and URL fields never authorize a fetch. Candidate URLs are not probed,
downloaded, or opened. DRM remains `not_assessed`, and rights remain unresolved at
file level. Item declarations cannot license every translation, image, recording,
or file. Format labels and candidate extensions do not validate actual content.

`complete` describes successful bounded metadata interpretation, including a
restricted item. It is not permission, safety, quality, recording completeness,
peer review, or publication eligibility. A future acquisition operation must
resolve the selection afresh under its own scopes, limits, rights, and recovery
policy. A saved evaluation cannot authorize an agent write.

## Shared metadata HTTP boundary

Open Library and Archive share the standard-library metadata client. Each provider
instance paces requests at one per second, with a 30-second deadline including
quota waits, a 2 MiB decompressed JSON limit, and at most two connections per host.
It validates JSON content types and rejects trailing JSON values. HTTP failures
are explicit without automatic retries. Valid `Retry-After` on 429/503 postpones
subsequent requests in that instance, including already waiting callers; cancellation
can end the wait. Separate CLI processes do not coordinate quotas.

Production endpoints and HTTPS hosts are fixed. Redirects must retain that exact
host, HTTPS, no credentials/fragment, and at most three redirects. Direct dialing
resolves the host, rejects nonpublic/reserved or mixed public/private results,
and connects to the validated numeric IP rather than resolving the hostname again.
Normal TLS hostname/certificate validation remains enabled. No arbitrary provider
URLs or endpoint configuration are exposed.

Address exclusions were checked against the IANA
[IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/) and
[IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/) special-purpose
registries. IPv6 is conservatively limited to global-unicast allocations in
`2000::/3`, excluding special ranges. The policy can reject unusual legitimate
networks; it is not a complete network sandbox. Host OS routing and trust still
matter. Revisit exclusions when supported network requirements change.

Environment proxies are disabled for these metadata providers because they can
bypass checked direct destinations. Proxy-only networks need an explicit future
proxy trust policy. This does not change the collection harness's transfer policy.
No metadata links, license URLs, local catalogs, notes, or reading history are
fetched or uploaded. Only explicit search terms or a selected item ID are sent.
The User-Agent identifies Nemalo and its project URL.

## Verification and limits

Offline HTTP/DNS/dial fixtures exercise search paging, field variations, malformed
records, restrictions, unsafe names, body limits, redirects, private/mixed DNS,
numeric-IP dialing, quota waits, cancellation, and CLI/TUI routing/escaping.
Live smoke checks are separate evidence, not part of ordinary CI and not promises
that every source item will remain available.

No unified multi-provider relevance ranking, automatic catalog harvesting, cache,
credential handling, lending client, DRM removal, chapter grouping, or download
manager is introduced. Add those only through the shared core with their own
bounded requirements and deterministic evidence.
