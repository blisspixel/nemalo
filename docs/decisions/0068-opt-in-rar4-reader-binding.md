# 0068: opt-in read-only RAR4 reader binding

Status: accepted, 2026-10-20. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no RAR4 application cleanup permission.

## Boundary

Decision 0067 measures standalone RAR4 framing without reader or write authority.
A requested coverage result additionally needs every declared physical file
record bound to the maintained reader's exact table over the same private bytes.
The framing implementation, including final name-work cancellation, needs its
complete local/native/hosted verification. This binding then needs its own full
gate before any later application permission.

The current cleanup service requests generic coverage and records `rar5-frames/1`
for a RAR root because the adapter admits only a RAR5 signature there. Simply
admitting RAR4 under that generic request would grant it the wrong application
proof. Do not infer write permission from a newly available reader capability.

## Decision

Require an explicit internal `Limits.RAR4Coverage` opt-in alongside `Coverage`
for standalone root RAR4 reader binding. Its default is false, and no application
service sets it in this increment. It is not a CLI, JSON or MCP option. Existing
RAR5 coverage and default RAR4 intake retain their contracts. Child RARs and RAR
sets receive no new coverage authority.

Open the copied private snapshot through the existing checked root/regular-file
mechanism, bind expected source size/SHA-256 during copying, and measure its RAR4
framing through the shared proof reader. Charge the same lifetime 16 MiB and
whole-tree read, process, diagnostic, output, member and deadline budgets,
including failed work. Do not reopen a validated public source path.

Use one UTF-8 technical listing, checked before extraction. Bind the complete
unique raw-name set to known unpacked size, packed size, directory state and
CRC. Zero is an explicit CRC value, not missing evidence. Reject normalized
aliases, missing/duplicate records, links, special files, encryption and split
members. The same checked table drives existing exact-name stdout extraction.
Reader ordering confers no permission. Physical coverage and successful output
size/CRC verification remain separate facts.

Share the RAR5 table matcher through bounded private common file facts and small
format-specific conversions. Keep all existing RAR5 refusal rules and tests;
do not create another extraction or eligibility policy. No new dependency,
executable, decoder, cgo path or network requirement is introduced.

## Verification and later work

Use synthetic Go fixtures and injected readers/scanners for every table mismatch,
explicit zero CRC, failed shared proof work before a subprocess, source mismatch,
child/set refusal, default-intake behavior and changed output. A valid held RAR4
must still receive zero cleanup moves while the explicitly opted-in standalone
adapter can return coverage. Exercise this application guard in offline tests and
native generated-book experiments.

Compare stored books/directories and the independent compressed synthetic pattern
from decision 0067 with the installed maintained reader outside the checkout.
Keep unlisted records and unsupported metadata preserved, and do not infer book
validity or broad RAR conformance from the reference. Require canonical verifier,
native-cgo race, docs checks and actual all-six hosted jobs on the final commit.

A later RAR4 application subset requires its own `rar4-frames/1`, signature-bound
proof selection, exact leaves, reviewed recovery and grouped-purge contracts, and
complete verification. Never broaden `rar5-frames/1` or `archive-tree/1` to RAR4.
Volume and nested layouts remain separately specified work; this binding does not
complete issue 57 or roadmap stage 2.
