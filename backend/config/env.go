package config

const (
	EnvRunMigration             = "RUN_MIGRATION"
	EnvHTTPServerEnabled        = "HTTP_SERVER_ENABLED"
	EnvRSSWorkerEnabled         = "RSS_WORKER_ENABLED"
	EnvHTTPAddr                 = "HTTP_ADDR"
	EnvDatabasePath             = "DATABASE_PATH"
	EnvFeeds                    = "FEEDS"
	EnvFeedItemsLimit           = "FEED_ITEMS_LIMIT"
	EnvWorkerTimeoutInSeconds   = "WORKER_TIMEOUT_IN_SECONDS"
	EnvWorkerIntervalInSeconds  = "WORKER_INTERVAL_IN_SECONDS"
	EnvLLMProvider              = "LLM_PROVIDER"
	EnvLLMSystemPromptFile      = "LLM_SYSTEM_PROMPT_FILE"
	EnvOllamaHost               = "OLLAMA_HOST"
	EnvOllamaPort               = "OLLAMA_PORT"
	EnvOllamaScheme             = "OLLAMA_SCHEME"
	EnvOllamaModel              = "OLLAMA_MODEL"
	EnvOllamaTimeoutInSeconds   = "OLLAMA_TIMEOUT_IN_SECONDS"
	EnvGenProxyBaseURL          = "GEN_PROXY_BASE_URL"
	EnvGenProxyModel            = "GEN_PROXY_MODEL"
	EnvGenProxyAPIKey           = "GEN_PROXY_API_KEY"
	EnvGenProxyTimeoutInSeconds = "GEN_PROXY_TIMEOUT_IN_SECONDS"
)
