# Backend build stage
FROM golang:1.22.2-alpine AS build-backend

WORKDIR /backend

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN apk add --no-cache gcc musl-dev && go env -w CGO_ENABLED=1

ARG DRONE
ARG DRONE_TAG
ARG DRONE_COMMIT
ARG DRONE_BRANCH

# if DRONE presented use DRONE_* git env to make version
RUN \
    if [ "$DRONE" = "true" ]; then \
    DRONE_COMMIT_SHORT=$(echo $DRONE_COMMIT | cut -c 1-7) ; \
    version=${DRONE_TAG}${DRONE_BRANCH}-${DRONE_COMMIT_SHORT}-$(date +%Y%m%d-%H:%M:%S) ; \
    else \
    echo "runs outside of drone" && version="unknown" ; \
    fi && \
    echo "version=$version" && \
    go build --tags "embed" -o service -ldflags "-X main.revision=${version} -s -w"

# Final stage
FROM alpine:3.19.1

WORKDIR /srv

COPY --from=build-backend /backend/service .

ARG RUN_MIGRATION
ENV RUN_MIGRATION=$RUN_MIGRATION

# Example: `FEEDS=https://www.somefeed.com/feed`
ARG FEEDS
# Example: `OLLAMA_HOST=0.0.0.0`
ARG OLLAMA_HOST
# Example: `OLLAMA_PORT=11434`
ARG OLLAMA_PORT
# Example: `OLLAMA_SCHEME=http`
ARG OLLAMA_SCHEME
# Example: `OLLAMA_MODEL=llama3:8b`
ARG OLLAMA_MODEL

EXPOSE 8080

CMD ["/srv/service"]
