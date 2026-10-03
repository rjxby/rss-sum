.PHONY: test run-tests run run-ollama run-gen-proxy run-worker-only-ollama run-worker-only-gen-proxy

test:
	go test ./...

run-tests: test

run:
	go run main.go

run-ollama:
	LLM_PROVIDER=ollama go run main.go

run-gen-proxy:
	LLM_PROVIDER=gen-proxy go run main.go

run-worker-only-ollama:
	HTTP_SERVER_ENABLED=false LLM_PROVIDER=ollama go run main.go

run-worker-only-gen-proxy:
	HTTP_SERVER_ENABLED=false LLM_PROVIDER=gen-proxy go run main.go
