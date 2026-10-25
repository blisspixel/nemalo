# 0098: settle the remaining format items before the 1.0 freeze

Status: accepted, 2026-10-25. Third stage 6 increment, closing the items
[FORMATS.md](../FORMATS.md) listed as not yet settled under decision 0096.

## Decision

- `config.json` gains an optional `schema_version`. Absent means 1, which keeps
  every existing user file valid; a higher or negative value is refused with a
  message that it may come from a newer Nemalo. Unknown fields stay refused.
- The job writer reads its own journal strictly, refusing unknown fields and
  trailing data, as the passive snapshot already did. A torn final line,
  without its newline after a crash mid-write, is still tolerated. The job
  record has only ever gained fields, so no journal an earlier release wrote
  becomes unreadable.
- The cleanup plan digest stays without a version tag, and its construction is
  frozen. Cleanup manifests record that digest and resume compares it, so
  adding a tag would make resuming a cleanup started by an earlier release
  fail. Any future change needs a new manifest version instead.

The field contract recorded the two additions (`config.Config` and its copy in
doctor output). Tests cover each configuration version and journal lines with
unknown fields, trailing data and torn tails.
