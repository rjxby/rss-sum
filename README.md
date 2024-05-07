# Project: RSS Sum

The RSS Sum Service is a GoLang application designed to collect text from various RSS feeds, process the text using a local language model via the Ollama API, and provide summarized content. The service is containerized using Docker for easy deployment and management.

## Features

- **RSS Feed Collection**
- **Ollama API Integration**
- **Database Interaction**
- **HTTP Server**

## Architecture

The project follows a modular architecture, with different components responsible for specific tasks:

- **Worker (RSS)**: Collects text from RSS feeds and processes them for further summarization.
- **Assistant (Ollama)**: Interacts with the Ollama API for language model processing.
- **Blogger**: Handles interactions with the database for storing and retrieving data.
- **Server**: Provides an HTTP server for external communication and data access.

## Environment Variables

The service can be configured using the following environment variables:

- **RUN_MIGRATION**:
  - Description: Controls whether database migration should be performed on startup.
  - Example: `RUN_MIGRATION=true`

- **WORKER_TIMEOUT_IN_SECONDS**:
  - Description: Specifies the timeout duration (in seconds) for the worker process.
  - Example: `WORKER_TIMEOUT_IN_SECONDS=4000`

- **WORKER_INTERVAL_IN_SECONDS**:
  - Description: Specifies the interval (in seconds) at which the worker collects text from RSS feeds.
  - Example: `WORKER_INTERVAL_IN_SECONDS=10`

- **FEEDS**:
  - Description: Specifies the URL(s) of the RSS feed(s) from which text will be collected.
  - Example: `FEEDS=https://www.somefeed.com/feed`

- **FEED_ITEMS_LIMIT**:
  - Description: Specifies the maximum number of items to collect from each RSS feed.
  - Example: `FEED_ITEMS_LIMIT=3`

- **OLLAMA_HOST**:
  - Description: Specifies the hostname/IP address of the Ollama API server.
  - Example: `OLLAMA_HOST=0.0.0.0`

- **OLLAMA_PORT**:
  - Description: Specifies the port number of the Ollama API server.
  - Example: `OLLAMA_PORT=11434`

- **OLLAMA_SCHEME**:
  - Description: Specifies the protocol scheme (http/https) for communicating with the Ollama API server.
  - Example: `OLLAMA_SCHEME=http`

- **OLLAMA_MODEL**:
  - Description: Specifies the Ollama model to use for language model processing.
  - Example: `OLLAMA_MODEL=llama3:8b`

Feel free to explore and enhance the project as needed. If you have any questions or suggestions, please let me know!
