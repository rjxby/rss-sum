# RSS Sum

[![Go Report Card](https://goreportcard.com/badge/github.com/rjxby/rss-sum)](https://goreportcard.com/report/github.com/rjxby/rss-sum)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**RSS Sum** is a modern Go application that aggregates content from RSS feeds, uses AI to generate concise summaries, and presents them through a responsive web interface. It demonstrates a practical application of AI in content curation while showcasing clean Go architecture and modern web development patterns.

## 🚀 Features

- **Automated RSS Feed Processing**: Collects articles from multiple RSS feeds with configurable intervals
- **AI-Powered Summarization**: Uses Ollama and local LLMs to create concise summaries of articles (~500 chars)
- **Responsive Web Interface**: Clean, mobile-friendly UI built with HTML, CSS and HTMX
- **Efficient Data Storage**: SQLite backend with GORM for persistence
- **Modern Web Patterns**: Server-driven UI with progressive enhancement via HTMX
- **Containerized Deployment**: Ready for Docker deployment with multi-stage builds
- **CI/CD Integration**: Built-in versioning system for CI/CD pipelines (Drone compatible)
- **Fault Tolerance**: Automatic retries with backoff for RSS fetching and summarization
- **Incremental Updates**: Only processes new articles to avoid duplicate content
- **Hashed Partitioning**: Efficient content organization using SHA-256 hash partitioning

### Core Components

- **Worker (RSS)**: Periodically fetches content from configured RSS feeds with automatic retry logic and error handling
- **Assistant (Ollama)**: Interfaces with local LLMs via Ollama to generate high-quality, condensed summaries
- **Blogger**: Manages data persistence using GORM with SQLite, providing clean abstractions for data operations
- **Server**: Delivers content via both REST API and HTML endpoints with progressive enhancement
- **Web UI**: Modern, responsive interface with infinite scroll and dynamic content loading

## 📊 Project Structure

```
├── backend/
│   ├── assistant/        # Ollama API integration for AI summarization
│   ├── blogger/          # Database operations and post management
│   ├── hasher/           # SHA-256 hashing utilities
│   ├── rss/              # RSS feed processing
│   │   └── worker/       # Background worker for RSS feeds
│   ├── server/           # HTTP server and API endpoints
│   └── store/            # Database models and operations
├── frontend/
│   └── html/             # HTML templates for web UI
├── main.go               # Application entry point
└── Dockerfile            # Multi-stage Docker build
```

## 🛠️ Technology Stack

- **Backend**: Go 1.24+
- **ORM**: GORM with SQLite
- **Web Framework**: Chi router with middleware
- **Frontend**: HTML/CSS with HTMX for dynamic interactions
- **AI**: Ollama API integration with local LLM models
- **Testing**: Comprehensive test suite with mocks and assertions
- **RSS Processing**: Go Feed Parser (gofeed)
- **Deployment**: Multi-stage Docker builds with CI/CD integration
- **Security**: Rate limiting and request throttling
- **Web UI**: Responsive design with Pico CSS

## 📋 Prerequisites

- Go 1.24+
- Ollama server with LLM model (e.g., llama3:8b)
- Docker (optional, for containerized deployment)
- SQLite (included in Go build with CGO enabled)

## 🏗️ Installation & Setup

### Local Development

1. Clone the repository:
   ```bash
   git clone https://github.com/rjxby/rss-sum.git
   cd rss-sum
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Set up environment variables:
   ```bash
   # Database configuration
   export RUN_MIGRATION=true

   # RSS Worker configuration
   export WORKER_TIMEOUT_IN_SECONDS=1800
   export WORKER_INTERVAL_IN_SECONDS=3600
   export FEEDS=https://example.com/feed1,https://example.com/feed2
   export FEED_ITEMS_LIMIT=3

   # Ollama configuration
   export OLLAMA_HOST=0.0.0.0
   export OLLAMA_PORT=11434
   export OLLAMA_SCHEME=http
   export OLLAMA_MODEL=llama3.2:3b
   ```

4. Run the application:
   ```bash
   go run main.go
   ```

### Docker Deployment

The project includes a multi-stage Dockerfile that optimizes for small image size and clean build process.

1. Build the Docker image:
   ```bash
   docker build -t rss-sum .
   ```

2. Run the container:
   ```bash
   docker run -p 8080:8080 \
     -e RUN_MIGRATION=true \
     -e WORKER_TIMEOUT_IN_SECONDS=1800 \
     -e WORKER_INTERVAL_IN_SECONDS=3600 \
     -e FEEDS=https://example.com/feed1,https://example.com/feed2 \
     -e FEED_ITEMS_LIMIT=3 \
     -e OLLAMA_HOST=host.docker.internal \
     -e OLLAMA_PORT=11434 \
     -e OLLAMA_SCHEME=http \
     -e OLLAMA_MODEL=llama3.2:3b \
     rss-sum
   ```

3. With build arguments (for CI/CD integration):
   ```bash
   docker build -t rss-sum \
     --build-arg DRONE=true \
     --build-arg DRONE_TAG=v1.0.0 \
     --build-arg DRONE_COMMIT=abc1234567890 \
     --build-arg DRONE_BRANCH=main \
     .
   ```

## ⚙️ Configuration

The service is highly configurable through environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `RUN_MIGRATION` | Whether to run database migrations on startup | `false` |
| `WORKER_TIMEOUT_IN_SECONDS` | RSS worker operation timeout | `1800` (30 min) |
| `WORKER_INTERVAL_IN_SECONDS` | RSS feed check interval | `3600` (1 hour) |
| `FEEDS` | Comma-separated list of RSS feed URLs | *Required* |
| `FEED_ITEMS_LIMIT` | Maximum number of items to process per feed | `3` |
| `OLLAMA_HOST` | Ollama API host | *Required* |
| `OLLAMA_PORT` | Ollama API port | *Required* |
| `OLLAMA_SCHEME` | Ollama API protocol (http/https) | *Required* |
| `OLLAMA_MODEL` | LLM model to use | *Required* |
| `OLLAMA_TIMEOUT_IN_SECONDS` | Timeout for Ollama API requests | `30` |

## 🧪 Testing

Run the test suite:

```bash
go test ./...
```

The project includes comprehensive tests with mock implementations for all major components, ensuring reliability and maintainability:

- **Unit Tests**: Focused tests for each component with mocks for dependencies
- **Table-Driven Tests**: Efficient testing of multiple scenarios
- **HTTP Testing**: Mock servers for testing API endpoints
- **Mock Objects**: Using `testify/mock` for dependency isolation
- **Environment Variable Management**: Test-specific environment setup
- **Assertions**: Clear assertions with descriptive messages
- **Edge Cases**: Tests for error conditions and boundary situations

Test coverage focuses on core functionality including:
- RSS worker post processing
- AI summarization workflow
- Database operations
- HTTP API endpoints

## 🔍 API Reference

### REST API

- `GET /api/v1/posts` - Fetch posts with pagination
  - Query Parameters:
    - `page`: Page number (default: 1)
    - `pageSize`: Number of posts per page (default: 10)
    - `partitionKey`: Filter by specific feed (optional)

### HTML Endpoints

- `GET /` - Main web interface
- `GET /api/v1/posts` (with HX-Request header) - HTMX-compatible endpoint for infinite scroll

## 📱 UI Features

- Responsive design that works on mobile and desktop
- Infinite scroll for seamless content browsing (implemented with HTMX)
- Clean, dark-themed interface for comfortable reading
- Card-based layout with hover effects and animations
- PicoCSS for lightweight, semantic styling
- Links to original articles
- Pagination with lazy loading
- Optimized for readability with carefully selected typography
- Server-side rendered templates with embedded assets

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 👨‍💻 Author

Built with ❤️ by rjxby
