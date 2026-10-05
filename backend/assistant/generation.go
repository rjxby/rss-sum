package assistant

import "context"

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
