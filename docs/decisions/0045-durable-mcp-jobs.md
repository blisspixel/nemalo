# 0045: durable jobs for long MCP operations

Status: accepted and implemented, 2026-10-19. Adds the durable jobs that
[decision 0014](0014-read-only-mcp-server.md) and the agent integration design
required before longer MCP write tools, beside the import tools of
[decision 0034](0034-mcp-import-tools.md).

## Context

An acquisition or a rescan can take minutes to hours. An MCP tool call has a
ten-minute deadline, hosts drop connections, and over stdio the server
process lives only as long as its connection. The design requires jobs with
explicit IDs, journaled state, progress, cancellation, and restart
reconciliation, independent of the connection, without a queue service or a
daemon.

## Decision

`internal/jobs` keeps one append-only journal per job,
`<state>/jobs/<HEX>.jsonl`, with `queued`, `running`, `progress`,
`cancel_requested`, and `finished` lines; the finished line carries the
status (`succeeded`, `failed`, `cancelled`, or `interrupted`), the operation's
own result (at most 1 MiB), and its error. Job IDs are `job:` and 128 random
bits.

- **Ownership.** A job runs in the process that started it, which holds an
  exclusive operating system lock on `<HEX>.lock` from creation to the end of
  its run; the system releases it however the process ends. A reader that
  finds a job unfinished takes the lock: if it can, the owner is gone, and it
  records `interrupted`, under the lock so it is recorded once. If it cannot,
  the job is reported as running in another process, which alone can cancel
  it.
- **Limits.** One server runs at most two jobs and queues six more; a start
  beyond that is refused. Progress is journaled at most once a second and a
  thousand times; a journal grows to at most 4 MiB, and a torn last line, as
  a crash leaves, is dropped by the next write. Finished jobs older than 30
  days are removed when a server opens the store. A panic in a job's work is
  a failed job, not a dead server.
- **Lifetime.** `nemalo mcp serve --allow-write` opens the store in the state
  folder. When the connection closes, the server starts no new jobs, says on
  standard error how many are running, and exits once they finish, so a
  later connection reads their results. Interrupting the process ends them;
  they are then interrupted.
- **Tools.** With `--allow-write`: `library_rescan_start` (an applied rescan of
  a scoped library), `job_get`, `job_list`, and `job_cancel`; with
  `--allow-network` too, `acquisition_start`, for one selected Archive file or
  every track of a declared recording, into a new intake folder whose parent
  is in scope, or with `resume` into the existing one. Each start validates
  its request as the CLI does, returns the queued job at once, and runs the
  same application service. Acquisition reports bytes received and the scan
  phase; a recording reports tracks done. Annotations mark the reads
  read-only and the starts and cancel not destructive.

## Consequences

Agents can start long operations, return later, and read what happened, and a
killed server leaves no job looking alive. An interrupted acquisition keeps
its packet and resumes through the existing validator-bound resume
([decision 0023](0023-validator-bound-download-resume.md)). Jobs are per
user, not per library, and are not visible in the CLI or TUI yet. Ingest,
cleanup, and purge are not job tools: cleanup and purge stay out of MCP.

## Validation

Unit tests cover a result, a failure with a partial result, a panic,
progress, a queue of one with a refused third start, cancelling a queued and
a running job, refusing a start while closing, a job whose lock another
handle holds reported as running elsewhere and not cancellable, the same job
recorded once as interrupted when that lock is released, a torn last line,
and pruning. An MCP test starts an acquisition through a stub Archive
provider, outside-scope, ambiguous, and unnamed outputs refused, reads the
succeeded job and its packet, refuses cancelling it, and runs a rescan job.

Live, through `nemalo mcp serve --allow-write --allow-network` on stdio: an
acquisition of a 4,055,167-byte LibriVox MP3 returned a queued job at once;
the client then closed its input, the server reported it was finishing one
job and exited when the download was complete, and a new connection read the
job as succeeded with its receipt result. A 55,296,087-byte M4B acquisition
whose server was killed with `kill -9` after six seconds was read by the next
connection as interrupted, its last progress 2,531,328 bytes, and
`acquisition_start` with `resume` then completed it.
