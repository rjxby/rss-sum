# Agent notes

Use this file as the first stop for coding agents working in this repository.

Read [docs/architecture.md](docs/architecture.md) before changing package boundaries or runtime flow.

## Shared instructions

Keep project guidance in `AGENTS.md`.
Claude Code supports it directly starting with v2.1.277; use v2.1.281 or later for Bedrock or telemetry-disabled sessions.
With the default setting, a project or ancestor `CLAUDE.md`, `.claude/CLAUDE.md`, or `CLAUDE.local.md` prevents AGENTS.md loading.
If needed, select `claude-md-and-agents-md` under `/config` > Project instructions.
See [Claude Code's AGENTS.md documentation](https://code.claude.com/docs/en/memory#agentsmd).

## Repo map

- `main.go` starts migration, HTTP serving, and the RSS worker.
- `backend/config/` parses environment settings and loads the optional `.env` file.
- `backend/assistant/` contains summary prompts and settings in `assistant.go`, generation contracts in `generation.go`, and gen-proxy request/response handling in `gen_proxy.go`.
- `backend/rss/worker/` fetches feeds, filters duplicate items, summarizes new items, and saves them.
- `backend/blogger/` maps application posts to persistence operations.
- `backend/store/` owns SQLite/GORM models and queries.
- `backend/server/` serves JSON, HTML, static assets, middleware, and request validation.
- `frontend/` embeds HTML templates and static assets.
- `docs/architecture.md` describes runtime flow and package boundaries.

Tests live beside the implementation in `*_test.go` files.

## Commands

Use the Go version in `go.mod` and a CGO-capable toolchain for SQLite.

```bash
go test ./...
go test -race -v -timeout=100s -covermode=atomic -coverprofile=profile.cov_tmp ./...
govulncheck ./...
golangci-lint run
go run main.go
```

The CI workflow is in `.github/workflows/ci.yaml`.

## Verification

- During development, run focused tests with `go test ./backend/<package> -run '<TestName>'`.
- Before finishing code changes, format touched Go files with `gofmt` and run the CI checks listed above.
  Repeat checks only after further changes, failures, or unresolved concerns.
- For documentation-only changes, check links, commands, and `git diff --check`.
- Use temporary SQLite databases and a separate `HTTP_ADDR` for manual testing.
  Set `RSS_WORKER_ENABLED=false` for UI-only checks to avoid feed fetching and LLM requests.
- Verify server or template changes in both JSON and HTMX response modes when they affect the shared posts route.
- Report which checks ran and any failures or checks that could not run.

## Runtime configuration

The app is configured through environment variables. The most important ones are:

- `RUN_MIGRATION`
- `HTTP_SERVER_ENABLED`
- `RSS_WORKER_ENABLED`
- `HTTP_ADDR`
- `DATABASE_PATH`
- `FEEDS`
- `FEED_ITEMS_LIMIT`
- `WORKER_TIMEOUT_IN_SECONDS`
- `WORKER_INTERVAL_IN_SECONDS`
- `LLM_SYSTEM_PROMPT_FILE`

Gen-proxy settings:

- gen-proxy: `GEN_PROXY_BASE_URL`, `GEN_PROXY_MODEL`, `GEN_PROXY_API_KEY`, `GEN_PROXY_TIMEOUT_IN_SECONDS`

## Constraints

- Before implementing, briefly outline the simplest approach that solves the task with the least code and the fewest changes. Look for existing code to reuse or simplify.
- Prefer less code and smaller changes while preserving correctness, readability, and required verification. Avoid unrelated edits and abstractions the task does not need.
- Keep documentation direct and factual.
- Do not add comments that restate code, describe obvious steps, narrate assertions, or duplicate documentation. Add comments only for non-obvious constraints or decisions, and preserve required directives and public API contracts.
- Document each contract once and link to it from other docs.
- Update README configuration guidance when adding or changing environment variables.
- Prefer direct Go commands for local work.
- Frontend files are embedded with `frontend/embed.go`; update embed patterns if new asset paths are added.
- Persistence is SQLite through GORM; keep store changes behind `backend/store` and application mapping behind `backend/blogger`.
- Feed URL validation blocks local/private destinations. Preserve that boundary when changing RSS fetch behavior.
- LLM providers must return structured JSON with a `summary` field.

## Quality gates

- Use `make verify-fast` for formatting, vet, Go tests, architecture rules, and frontend tests. Narrow the Go tests with `GO_PACKAGES='./backend/assistant' GO_TEST_FILTER='TestName'`. Empty or all-skipped selections fail the command.
- Use `make verify` for final verification, including race detection, vulnerability checks, and lint. It requires installed govulncheck and golangci-lint tools.
- For server, template, CSS, or browser JavaScript changes, run `make verify-e2e` during development. It also runs in `make verify`. See [browser verification](docs/e2e.md) for setup, coverage, and failure artifacts.
- Add a browser regression test when a bug depends on browser behavior. Report the scenarios tested and link failure artifacts. If E2E cannot run, state the blocker and what remains unverified.
- Architecture rules live in quality/architecture_test.go and protect the package ownership described in docs/architecture.md.
- `make fuzz-summary` explores provider-output parsing for ten seconds. The seed corpus runs during ordinary Go tests without making provider calls.
- For bug fixes, demonstrate a failing regression test before the fix where possible. Explain fixture changes, skipped tests, analyzer suppressions, and weakened assertions.
- Keep live summary quality evals separate from unit tests. Compare factual retention, unsupported claims, and output validity using pinned model hashes, prompts, and provider settings. Generated wording alone is not a stable fixture.
- Use synchronization signals for concurrency tests. Timeouts bound hangs; sleeps do not establish ordering.
