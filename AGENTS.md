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
- `backend/assistant/` contains summary prompts in `assistant.go`, provider selection in `provider_factory.go`, and request/response handling in `ollama.go` and `gen_proxy.go`.
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
- `LLM_PROVIDER`
- `LLM_SYSTEM_PROMPT_FILE`

Provider-specific settings:

- Ollama: `OLLAMA_HOST`, `OLLAMA_PORT`, `OLLAMA_SCHEME`, `OLLAMA_MODEL`, `OLLAMA_TIMEOUT_IN_SECONDS`
- gen-proxy: `GEN_PROXY_BASE_URL`, `GEN_PROXY_MODEL`, `GEN_PROXY_API_KEY`, `GEN_PROXY_TIMEOUT_IN_SECONDS`

## Constraints

- Keep documentation direct and factual.
- Keep comments for non-obvious constraints or decisions; avoid narrating code and assertions.
- Document each contract once and link to it from other docs.
- Update README configuration guidance when adding or changing environment variables.
- Prefer direct Go commands for local work.
- Frontend files are embedded with `frontend/embed.go`; update embed patterns if new asset paths are added.
- Persistence is SQLite through GORM; keep store changes behind `backend/store` and application mapping behind `backend/blogger`.
- Feed URL validation blocks local/private destinations. Preserve that boundary when changing RSS fetch behavior.
- LLM providers must return structured JSON with a `summary` field.
