# RSS Sum

[![Go Report Card](https://goreportcard.com/badge/github.com/rjxby/rss-sum)](https://goreportcard.com/report/github.com/rjxby/rss-sum)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

RSS Sum is an AI-assisted feed reader built in Go. It watches RSS feeds, summarizes new articles with an LLM, stores the results in SQLite, and serves a fast HTML interface plus a JSON API.

This project is designed to show practical backend engineering: safe external fetching, provider-based AI integration, SQLite persistence, server-rendered UI, and a small production-style CI setup.

## How it works

```mermaid
flowchart TB
    subgraph sources["Outside the app process"]
        direction LR
        Feeds["External source<br/>Public RSS feeds"]
        Client["External client<br/>Browser or API client"]
    end

    subgraph app["RSS Sum · one Go process"]
        direction TB
        Main["main.go<br/>Startup + shutdown"]
        Config["backend/config<br/>.env + environment settings"]
        Worker["backend/rss/worker<br/>Fetch + filter + retry"]
        Hasher["backend/hasher<br/>SHA-256 identifiers"]
        Assistant["backend/assistant<br/>Prompt + structured summary"]
        Server["backend/server<br/>Routes, middleware, HTML + JSON"]
        Frontend["frontend<br/>Embedded HTML, CSS, JS + htmx"]
        Blogger["backend/blogger<br/>Map + list + save posts"]
        Store["backend/store<br/>GORM + SQLite access"]

        Config -.-> Main
        Main -. "start when enabled" .-> Worker
        Main -. "start when enabled" .-> Server
        Main -. "optional migration" .-> Store
        Worker --> Hasher
        Worker -- "summarize new items" --> Assistant
        Worker -- "check IDs + save summaries" --> Blogger
        Server -- "read posts" --> Blogger
        Server -- "render + serve assets" --> Frontend
        Blogger --> Store
    end

    subgraph providers["External LLM services · choose one"]
        direction LR
        Ollama["Ollama<br/>POST /api/generate"]
        Proxy["gen-proxy<br/>POST /v1/responses"]
    end

    Database[("Local file outside the binary<br/>SQLite database")]
    Feeds -- "feed items over HTTP / HTTPS" --> Worker
    Client -- "GET / or /api/v1/posts" --> Server
    Assistant -- "HTTP + structured JSON" --> Ollama
    Assistant -- "HTTP + structured JSON" --> Proxy
    Store -- "read / write" --> Database

    classDef owned fill:#eef2ff,stroke:#6366a0,color:#1e2448,stroke-width:1.5px
    classDef external fill:#ecfdf5,stroke:#16836b,color:#134e42,stroke-width:1.5px,stroke-dasharray:6 4
    class Main,Config,Worker,Hasher,Assistant,Server,Frontend,Blogger,Store owned
    class Feeds,Client,Ollama,Proxy,Database external
    style app fill:#f8faff,stroke:#9ba8cf,stroke-width:2px
    style sources fill:#f5fcf9,stroke:#90bbae,stroke-dasharray:6 4
    style providers fill:#f5fcf9,stroke:#90bbae,stroke-dasharray:6 4
    linkStyle default stroke:#64748b,stroke-width:1.5px
```

Solid purple boxes show app code. Dashed green boxes show components outside the app process. Arrow labels describe requests, data, or package access; dotted arrows show startup wiring. Ollama or gen-proxy runs as a separate service, even when hosted on the same machine. The app selects one provider.

See the [architecture and dependency inventory](docs/architecture.md) for package responsibilities and library usage.

## Highlights

- **AI summaries**: Generates concise summaries through Ollama or a gen-proxy Responses API endpoint.
- **RSS worker**: Polls configured feeds, retries transient failures, and only stores new posts.
- **Safe feed fetching**: Rejects localhost, private, link-local, multicast, unspecified, and `.local` feed targets.
- **SQLite persistence**: Uses GORM with a simple post model and migration path.
- **Web and API surface**: Serves both a responsive HTMX UI and `GET /api/v1/posts` JSON.
- **Embedded frontend**: Ships templates and static assets inside the Go binary.

## What It Demonstrates

| Area | Implementation |
| --- | --- |
| Go backend design | Package boundaries for server, worker, assistant, service, and store layers. |
| AI integration | Provider abstraction with structured JSON output parsing. |
| Reliability | Worker timeouts, retries, duplicate filtering, and graceful shutdown. |
| Security basics | Feed URL validation, rate limiting, throttling, and browser security headers. |
| Maintainability | Focused tests, GitHub Actions checks, and agent-friendly architecture docs. |

## Requirements

- Go 1.27.1 or newer. CI pins Go 1.27.1.
- CGO-capable local toolchain for SQLite.
- One LLM provider:
  - Ollama, using `/api/generate`
  - gen-proxy, using Responses API-compatible `/v1/responses`

## Run locally

Copy the example configuration once, then edit `.env` for your feeds and provider:

```bash
cp .env.example .env
go mod download
```

For the local [gen-proxy](https://github.com/rjxby/gen-proxy) stack, set `GEN_PROXY_API_KEY` to its `PUBLIC_API_KEY`, or `ApiKeys__Keys__0` from its `.env`. The stack defaults to `dev-local-key`. Set `GEN_PROXY_MODEL` to its `MAIN_MODEL_ID`. The proxy currently uses this name for response metadata and logs; the stack's `MAIN_MODEL_PATH` selects the actual model.

Start the stack in a separate terminal from the gen-proxy repository:

```bash
make stack-run
```

The stack serves `https://localhost:7001`. Trust its development certificate before using HTTPS clients:

```bash
dotnet dev-certs https --trust
```

Then, from the RSS Sum repository:

```bash
go run main.go
```

Open `http://localhost:8080`. The example configuration enables migrations and reads the Go blog feed. Edit `FEEDS` to choose other public feeds.

For Ollama, set `LLM_PROVIDER=ollama` and edit the `OLLAMA_*` values in `.env`.

Both providers are expected to return structured JSON in this shape:

```json
{"summary":"..."}
```

## Releases

[GitHub Releases](https://github.com/rjxby/rss-sum/releases) provide archives for Linux AMD64 and macOS ARM64. Each archive contains the `rss-sum` executable, `README.md`, `LICENSE`, and `.env.example`. Templates and static assets are embedded in the executable.

Extract the archive for your platform, copy `.env.example` to `.env`, configure your feeds and LLM provider as described in [Run locally](#run-locally), then run `./rss-sum`. Release binaries do not require Go or a C compiler. Linux binaries require glibc 2.35 or newer; macOS binaries require macOS 14 or newer.

To verify a downloaded archive, download `SHA256SUMS` beside it and run `sha256sum --ignore-missing -c SHA256SUMS` on Linux, or `shasum -a 256 rss-sum_<tag>_darwin_arm64.tar.gz` on macOS and compare the result with `SHA256SUMS`.

## Configuration

At startup the app loads `.env` from the current working directory before parsing settings. A missing file is allowed. Existing environment variables take precedence, including explicitly empty values. Invalid or unreadable files stop startup. `.env` is ignored by Git; `.env.example` contains local setup values.

The table lists defaults used when a setting is absent. `.env.example` enables migrations, selects gen-proxy, and sets its timeout to 300 seconds for local generation.

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `RUN_MIGRATION` | no | `false` | Runs SQLite migrations at startup. |
| `HTTP_SERVER_ENABLED` | no | `true` | Starts the HTTP server when enabled. |
| `RSS_WORKER_ENABLED` | no | `true` | Starts the RSS worker when enabled. |
| `HTTP_ADDR` | no | `:8080` | Address used by the HTTP server. |
| `DATABASE_PATH` | no | `data/rss-sum.sqlite` | SQLite database path. |
| `FEEDS` | when the worker is enabled | none | Comma-separated public `http` or `https` feed URLs. Local, private, link-local, multicast, unspecified, and `.local` hosts are rejected. |
| `FEED_ITEMS_LIMIT` | no | `3` | Max items processed per feed run. |
| `WORKER_TIMEOUT_IN_SECONDS` | no | `1800` | Timeout for one worker pass. |
| `WORKER_INTERVAL_IN_SECONDS` | no | `3600` | Delay between worker passes. |
| `LLM_PROVIDER` | no | `ollama` | `ollama` or `gen-proxy`. |
| `LLM_SYSTEM_PROMPT_FILE` | no | empty | Optional plain text system prompt file. |
| `OLLAMA_HOST` | for Ollama | none | Ollama host. |
| `OLLAMA_PORT` | for Ollama | none | Ollama port. |
| `OLLAMA_SCHEME` | for Ollama | none | `http` or `https`. |
| `OLLAMA_MODEL` | for Ollama | none | Ollama model name. |
| `OLLAMA_TIMEOUT_IN_SECONDS` | no | `30` | Ollama request timeout. |
| `GEN_PROXY_BASE_URL` | for gen-proxy | none | Absolute gen-proxy base URL. The local stack uses `https://localhost:7001`. |
| `GEN_PROXY_MODEL` | for gen-proxy | none | Model name sent to gen-proxy. Use the stack's `MAIN_MODEL_ID`. |
| `GEN_PROXY_API_KEY` | no | empty | Sent as `X-API-Key` when set. |
| `GEN_PROXY_TIMEOUT_IN_SECONDS` | no | `30` | gen-proxy request timeout. |

## API

- `GET /` serves the HTML interface.
- `GET /api/v1/posts` returns posts as JSON.
- `GET /api/v1/posts` with `HX-Request: true` returns the HTML fragment used by infinite scroll.

Query parameters for `/api/v1/posts`:

| Name | Default | Notes |
| --- | --- | --- |
| `page` | `1` | Must be at least `1`; `(page - 1) * pageSize` must fit in a Go `int`. Invalid values return `400`. |
| `pageSize` | `10` | Must be between `1` and `100`. |
| `partitionKey` | empty | Optional feed partition filter. |

## Development

Useful local checks:

```bash
make test
make run-tests
go test ./...
go test -race -v -timeout=100s -covermode=atomic -coverprofile=profile.cov_tmp ./...
govulncheck ./...
golangci-lint run
```

Local run targets:

```bash
make run
make run-ollama
make run-gen-proxy
make run-worker-only-ollama
make run-worker-only-gen-proxy
```

All run targets load `.env`. Provider targets override `LLM_PROVIDER`; worker-only targets also override `HTTP_SERVER_ENABLED`. Other values come from `.env` or exported variables.

Worker-only targets run the same application process with the HTTP server disabled, so they can also be used for manual provider verification with a public feed and reachable LLM provider.

GitHub Actions runs tests with race detection and coverage, then runs `govulncheck`, `golangci-lint`, and coverage submission.

## UI libraries

The frontend vendors [htmx 4.0.0](https://github.com/bigskysoftware/htmx/releases/tag/v4.0.0) in `frontend/static/htmx.min.js`. The asset is embedded in the Go binary and served locally. No CDN or frontend build step is required. Styling and request status handling use the project's `app.css` and `app.js`.

The `htmx-config` meta tag disables swaps for HTTP errors because the posts API returns JSON errors, preserving the current article list when a request fails.

## Project Docs

- [Architecture](docs/architecture.md)
- [Release process](docs/releasing.md)
- [Agent instructions](AGENTS.md)

## License

MIT. See [LICENSE](LICENSE).
