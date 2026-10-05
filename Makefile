.PHONY: test run-tests run run-gen-proxy run-worker-only-gen-proxy

test:
	go test ./...

run-tests: test

run:
	go run main.go

run-gen-proxy: run

run-worker-only-gen-proxy:
	HTTP_SERVER_ENABLED=false go run main.go

.PHONY: verify-fast verify verify-e2e fuzz-summary
GO_PACKAGES ?= ./...
GO_TEST_FILTER ?= .
verify-fast:
	python3 -m unittest discover -s scripts/tests -v
	@unformatted="$$(git ls-files --cached --others --exclude-standard '*.go' | while IFS= read -r file; do test ! -f "$$file" || gofmt -l "$$file"; done)"; \
	 test -z "$$unformatted" || { echo 'Go formatting differs; run gofmt on the listed files'; echo "$$unformatted"; exit 1; }
	go vet ./...
	python3 scripts/test-go.py --packages "$(GO_PACKAGES)" --filter "$(GO_TEST_FILTER)"
	go test ./quality -count=1
	node --test frontend/tests/*.test.cjs
	git diff --check

verify-e2e:
	node scripts/verify-e2e.cjs $(E2E_ARGS)

verify: verify-fast verify-e2e
	go test -race -timeout=100s -covermode=atomic -coverprofile=profile.cov_tmp ./...
	govulncheck ./...
	golangci-lint run

fuzz-summary:
	go test ./backend/assistant -run '^$$' -fuzz '^FuzzParseSummaryOutput$$' -fuzztime=10s
