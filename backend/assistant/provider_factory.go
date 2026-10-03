package assistant

import "context"

const (
	ProviderOllama   = "ollama"
	ProviderGenProxy = "gen-proxy"
)

type llmProvider interface {
	Generate(ctx context.Context, request generationRequest) (string, error)
}

type generationRequest struct {
	SystemPrompt string
	UserPrompt   string
	Format       structuredOutputFormat
}

type structuredOutputFormat struct {
	Name   string
	Schema map[string]any
}

func newLLMProvider(settings *Settings) llmProvider {
	switch settings.LLMProvider {
	case ProviderGenProxy:
		return newGenProxyProvider(settings)
	default:
		return newOllamaProvider(settings)
	}
}
