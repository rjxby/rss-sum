# Browser verification

`make verify-e2e` builds RSS Sum and tests it in Chromium against temporary SQLite databases. It verifies the stored-post-to-reader path. It does not fetch feeds, call an LLM, or evaluate summary quality.

## Setup and commands

Use the Go version in `go.mod`, a CGO-capable toolchain, and Node.js 24 or later. Install the pinned browser test dependency and Chromium once:

```bash
npm ci
npx playwright install chromium
make verify-e2e
```

On Linux, install Chromium's system dependencies with `npx playwright install --with-deps chromium`. CI uses this command.

To select tests or watch the browser:

```bash
make verify-e2e E2E_ARGS='--grep "pagination"'
npm run test:e2e -- --headed
```

`make verify` includes this suite alongside the [other quality checks](../README.md#agent-verification). `make verify-fast` keeps using the Go tests and Node frontend tests without starting the app or a browser. Browser installation is separate from verification; a missing dependency fails the command.

## Isolation and coverage

The runner builds the actual application binary, creates one database with 25 fixture posts and one empty database, and starts two servers on dynamically selected loopback ports. Fixtures use blogger and store for migration and persistence. Article dates and content are fixed, including an oversized summary to exercise digest fitting.

The servers run outside the checkout to avoid loading its `.env` file. The runner overrides runtime settings with explicit database paths, enables the HTTP server, and disables migration and the RSS worker. It stops its child processes and removes the temporary binary and databases after success, failure, or SIGINT/SIGTERM. It never reuses a developer's running server. Browser requests to external origins fail the tests.

The suite checks:

- HTMX initial loading and scrolling pagination, including article order and duplicates.
- Agreement between JSON, HTML fragments, and visible posts.
- Empty reading and Wall digest views using the empty database.
- Initial request failure and retry, and pagination failure without discarding loaded articles.
- Dark Wall digest on a fresh visit, saved reading preferences, and E-ink settings across reloads.
- Complete summaries, visible controls, and footer separation at 390px and 1280px widths.
- Digest rotation, refresh failure preservation, and automatic recovery using the browser clock.

Failure scenarios intercept only the affected browser requests. Successful requests use the real HTTP server and database. Assertions wait for requests or visible state; elapsed browser time advances through Playwright's clock instead of real sleeps. These tests complement the backend and frontend unit tests.

## Failure evidence

Each run prints its unique directory under `artifacts/e2e/run-*`. It retains server logs, fixture logs, an HTML report, and browser diagnostics. Failed tests also retain screenshots and Playwright traces from the failing attempt. Reports stay available after temporary databases are removed and are ignored by Git.

Open a report or trace with the path printed by the runner:

```bash
npx playwright show-report artifacts/e2e/run-EXAMPLE/report
npx playwright show-trace artifacts/e2e/run-EXAMPLE/test-results/TEST/trace.zip
```

CI uploads the run directories even when verification fails. An empty test selection fails. Keep failures reproducible; do not add retries or weaken assertions to make a run pass.
