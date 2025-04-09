# Backend build stage
FROM golang:1.24.2-alpine AS build-backend

WORKDIR /backend

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN apk add --no-cache gcc musl-dev && go env -w CGO_ENABLED=1

ARG DRONE=false
ARG DRONE_TAG=""
ARG DRONE_COMMIT=""
ARG DRONE_BRANCH=""

# Compute version info
RUN \
    if [ "$DRONE" = "true" ]; then \
        DRONE_COMMIT_SHORT=$(echo $DRONE_COMMIT | cut -c 1-7) ; \
        version="${DRONE_TAG}${DRONE_BRANCH}-${DRONE_COMMIT_SHORT}-$(date +%Y%m%d-%H%M%S)" ; \
    else \
        version="dev-$(date +%Y%m%d-%H%M%S)" ; \
    fi && \
    echo "Building version: $version" && \
    go build --tags "embed" -o service -ldflags "-X main.revision=${version} -s -w"

# Final stage
FROM alpine:3.21.3

WORKDIR /srv

COPY --from=build-backend /backend/service .

# Runtime envs
ARG RUN_MIGRATION=false
ENV RUN_MIGRATION=$RUN_MIGRATION

ARG FEEDS=""
ARG OLLAMA_HOST="0.0.0.0"
ARG OLLAMA_PORT="11434"
ARG OLLAMA_SCHEME="http"
ARG OLLAMA_MODEL="llama3:8b"

# Optional: expose envs at runtime for your app to consume
ENV FEEDS=$FEEDS \
    OLLAMA_HOST=$OLLAMA_HOST \
    OLLAMA_PORT=$OLLAMA_PORT \
    OLLAMA_SCHEME=$OLLAMA_SCHEME \
    OLLAMA_MODEL=$OLLAMA_MODEL

EXPOSE 8080

CMD ["/srv/service"]
