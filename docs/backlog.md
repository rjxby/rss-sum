# Project backlog

Proposed work for RSS Sum. All items are open. Priorities suggest an order for picking work; they do not commit to release dates.

P1 covers reliability and summary quality. P2 covers operating costs, deployment support, and reader improvements. Mark an item complete when its acceptance criteria pass, and link the implementing pull request here.

Follow the [package boundaries](architecture.md#boundaries) and [verification requirements](../AGENTS.md#quality-gates) when implementing these items. Configuration and API changes must update the relevant README contracts.

## P1

### BL-001: Cancel worker database operations

The worker passes cancellation to feed fetching and summarization, but its post lookup and save methods do not accept a context. See [worker.go](../backend/rss/worker/worker.go).

- [ ] Pass the worker pass context through blogger to store for duplicate lookup and bulk saves.
- [ ] Stop database work when the pass times out or the process shuts down.
- [ ] Add regression tests for canceled lookups and saves, including transaction rollback without partial writes.

### BL-002: Bound feed and provider input sizes

The feed fetcher parses response bodies without an application size limit, and the assistant builds a prompt from the supplied item text. See [worker.go](../backend/rss/worker/worker.go) and [assistant.go](../backend/assistant/assistant.go).

- [ ] Define and document limits for fetched feed bodies and article text sent to providers.
- [ ] Report oversized feeds and items clearly, with a documented reject or truncate policy.
- [ ] Test oversized and chunked responses, multibyte text, and cancellation.
- [ ] Keep the existing destination validation active for fetches and redirects.

### BL-003: Add reproducible live summary evaluations

The README describes how to record model comparisons, but the repository has no dedicated evaluation runner or dataset. See [agent verification](../README.md#agent-verification).

- [ ] Add a versioned dataset with source text and expected facts, including short, long, and HTML-heavy feed items.
- [ ] Record provider, model hash, prompt, generation settings, and dataset revision with each run.
- [ ] Report output validity, factual retention, unsupported claims, and summary length.
- [ ] Keep live provider calls outside ordinary tests and CI verification. Document an explicit command for running evaluations.

## P2

### BL-004: Look up duplicate IDs in bounded batches

Each feed pass currently loads every stored ID for that feed, even though it considers only a limited number of items. See [worker.go](../backend/rss/worker/worker.go) and [sqlite.go](../backend/store/sqlite.go).

- [ ] Query stored IDs for the current candidate items instead of loading the feed's full history.
- [ ] Bound query sizes and preserve GUID, URL, fallback identity, and within-feed duplicate behavior.
- [ ] Add regression coverage for duplicates older than the latest page and benchmark against a large feed history.

### BL-005: Support conditional feed requests

The fetcher creates a new parser and fetches each feed on every pass. It does not retain HTTP validators. See [worker.go](../backend/rss/worker/worker.go).

- [ ] Retain successful ETag and Last-Modified values and send conditional requests on later passes.
- [ ] Treat HTTP 304 as an unchanged feed without parsing or summarizing articles.
- [ ] Define whether validators survive restarts, and document that behavior.
- [ ] Test changed feeds, missing validators, redirects, and transient failures.

### BL-006: Expose application and worker health

Operators currently rely on logs to see worker outcomes. The documented HTTP routes serve the reader and posts. See the [API](../README.md#api).

- [ ] Add documented liveness and readiness responses with clear behavior when the worker is disabled or the database is unavailable.
- [ ] Report the last worker pass time, outcome, and counts of fetched, skipped, summarized, failed, and saved items.
- [ ] Exclude API keys and article bodies from health responses and logs.
- [ ] Test startup, successful and failed passes, and shutdown behavior.

### BL-007: Add a source filter to the reader

The posts API accepts a partition filter, but the reader has no documented source selector. See the [posts query contract](../README.md#api).

- [ ] Let readers select a feed or return to all feeds using readable source names.
- [ ] Preserve the selected source during pagination and reset the list when the selection changes.
- [ ] Keep JSON and HTMX filtering consistent.
- [ ] Add browser coverage for switching sources, empty results, pagination, and keyboard operation.

### BL-008: Document and verify SQLite backup and restore

RSS Sum stores posts in a local SQLite file and enables WAL for file databases. See [sqlite.go](../backend/store/sqlite.go).

- [ ] Document a backup procedure that handles WAL and states whether the app must stop.
- [ ] Document restoring to a separate database path and checking stored posts before replacing a live database.
- [ ] Verify the procedure with a temporary database containing multiple feeds and summaries.
- [ ] Link the procedure from the README configuration guidance.
