package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rjxby/rss-sum/backend/config"
	"github.com/stretchr/testify/assert"
)

func TestParseSettings(t *testing.T) {
	t.Setenv(config.EnvGenProxyBaseURL, "http://127.0.0.1:7001")
	t.Setenv(config.EnvGenProxyModel, "local-model")
	t.Setenv(config.EnvGenProxyAPIKey, "")
	t.Setenv(config.EnvGenProxyTimeoutInSeconds, "")
	t.Setenv(config.EnvLLMSystemPromptFile, "")

	t.Run("GenProxyDefaults", func(t *testing.T) {
		settings, err := ParseSettings()
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, "http://127.0.0.1:7001", settings.GenProxyBaseURL)
		assert.Equal(t, "local-model", settings.GenProxyModel)
		assert.Equal(t, "", settings.GenProxyAPIKey)
		assert.Equal(t, 30, settings.RequestTimeoutInSeconds)
		assert.Equal(t, defaultSystemPrompt, settings.SystemPrompt)
	})

	t.Run("CustomTimeoutAndAPIKey", func(t *testing.T) {
		t.Setenv(config.EnvGenProxyTimeoutInSeconds, "60")
		t.Setenv(config.EnvGenProxyAPIKey, "public-key")
		settings, err := ParseSettings()
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, 60, settings.RequestTimeoutInSeconds)
		assert.Equal(t, "public-key", settings.GenProxyAPIKey)
	})

	for _, test := range []struct {
		name      string
		variable  string
		value     string
		errorText string
	}{
		{"MissingBaseURL", config.EnvGenProxyBaseURL, "", config.EnvGenProxyBaseURL},
		{"MissingModel", config.EnvGenProxyModel, "", config.EnvGenProxyModel},
		{"InvalidBaseURL", config.EnvGenProxyBaseURL, "localhost:7001", "absolute URL"},
		{"MalformedBaseURL", config.EnvGenProxyBaseURL, "http://[invalid", config.EnvGenProxyBaseURL},
		{"ZeroTimeout", config.EnvGenProxyTimeoutInSeconds, "0", config.EnvGenProxyTimeoutInSeconds},
		{"NegativeTimeout", config.EnvGenProxyTimeoutInSeconds, "-1", config.EnvGenProxyTimeoutInSeconds},
		{"NonNumericTimeout", config.EnvGenProxyTimeoutInSeconds, "invalid", config.EnvGenProxyTimeoutInSeconds},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.variable, test.value)
			settings, err := ParseSettings()
			assert.ErrorContains(t, err, test.errorText)
			assert.Nil(t, settings)
		})
	}

	t.Run("PromptFile", func(t *testing.T) {
		promptFile := filepath.Join(t.TempDir(), "system-prompt.txt")
		assert.NoError(t, os.WriteFile(promptFile, []byte("  Custom system prompt.  \n"), 0o600))
		t.Setenv(config.EnvLLMSystemPromptFile, promptFile)
		settings, err := ParseSettings()
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, "Custom system prompt.", settings.SystemPrompt)
	})

	t.Run("MissingPromptFile", func(t *testing.T) {
		t.Setenv(config.EnvLLMSystemPromptFile, filepath.Join(t.TempDir(), "missing.txt"))
		settings, err := ParseSettings()
		assert.ErrorContains(t, err, config.EnvLLMSystemPromptFile)
		assert.Nil(t, settings)
	})

	t.Run("EmptyPromptFile", func(t *testing.T) {
		promptFile := filepath.Join(t.TempDir(), "system-prompt.txt")
		assert.NoError(t, os.WriteFile(promptFile, []byte(" \n\t"), 0o600))
		t.Setenv(config.EnvLLMSystemPromptFile, promptFile)
		settings, err := ParseSettings()
		assert.ErrorContains(t, err, "must not be empty")
		assert.Nil(t, settings)
	})
}

type fakeLLMProvider struct {
	result  string
	err     error
	ctxErr  error
	request generationRequest
}

func (p *fakeLLMProvider) Generate(ctx context.Context, request generationRequest) (string, error) {
	p.request = request
	p.ctxErr = ctx.Err()
	if p.ctxErr != nil {
		return "", p.ctxErr
	}
	return p.result, p.err
}

func TestSummarizeTextUsesProvider(t *testing.T) {
	provider := &fakeLLMProvider{result: "This is a test summary."}
	assistant := &AssistantProc{
		settings: Settings{RequestTimeoutInSeconds: 5},
		provider: provider,
	}

	summary, err := assistant.SummarizeText(context.Background(), "Test input text")

	assert.NoError(t, err)
	assert.Equal(t, "This is a test summary.", summary)
	assert.Equal(t, defaultSystemPrompt, provider.request.SystemPrompt)
	assert.Contains(t, provider.request.UserPrompt, "Test input text")
	assert.Equal(t, "rss_summary", provider.request.Format.Name)
}

func TestSummarizeTextProviderError(t *testing.T) {
	provider := &fakeLLMProvider{err: errors.New("provider failed")}
	assistant := &AssistantProc{
		settings: Settings{RequestTimeoutInSeconds: 5},
		provider: provider,
	}

	summary, err := assistant.SummarizeText(context.Background(), "Test input text")

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.Contains(t, err.Error(), "failed to summarize text")
	assert.Contains(t, err.Error(), "provider failed")
}

func TestSummarizeTextPropagatesContextCancellation(t *testing.T) {
	provider := &fakeLLMProvider{result: "unused"}
	assistant := &AssistantProc{
		settings: Settings{RequestTimeoutInSeconds: 5},
		provider: provider,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	summary, err := assistant.SummarizeText(ctx, "Test input text")

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.ErrorIs(t, provider.ctxErr, context.Canceled)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestNewUsesGenProxy(t *testing.T) {
	proc := New(&Settings{
		GenProxyBaseURL:         "http://127.0.0.1:7001",
		GenProxyModel:           "local-model",
		RequestTimeoutInSeconds: 5,
	})
	assert.IsType(t, &genProxyProvider{}, proc.provider)
}

func testGenerationRequest(prompt string) generationRequest {
	return generationRequest{
		SystemPrompt: "test system prompt",
		UserPrompt:   prompt,
		Format:       summaryOutputFormat(),
	}
}

func TestGenProxyProviderGenerate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/responses", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "public-api-key", r.Header.Get("X-API-Key"))

		var req struct {
			Model          string                 `json:"model"`
			Input          []genProxyInputMessage `json:"input"`
			Metadata       map[string]string      `json:"metadata"`
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Name   string         `json:"name"`
					Strict bool           `json:"strict"`
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "gpt-5.1", req.Model)
		assert.Equal(t, map[string]string{"source": "rss-sum"}, req.Metadata)
		assert.Equal(t, "json_schema", req.ResponseFormat.Type)
		assert.Equal(t, "rss_summary", req.ResponseFormat.JSONSchema.Name)
		assert.True(t, req.ResponseFormat.JSONSchema.Strict)
		assert.Equal(t, "object", req.ResponseFormat.JSONSchema.Schema["type"])
		assert.Equal(t, false, req.ResponseFormat.JSONSchema.Schema["additionalProperties"])
		assert.Equal(t, []any{"summary"}, req.ResponseFormat.JSONSchema.Schema["required"])
		if assert.Len(t, req.Input, 1) {
			assert.Equal(t, "message", req.Input[0].Type)
			assert.Equal(t, "user", req.Input[0].Role)
			if assert.Len(t, req.Input[0].Content, 1) {
				assert.Equal(t, "input_text", req.Input[0].Content[0].Type)
				assert.Equal(t, "test system prompt\n\nTest input text", req.Input[0].Content[0].Text)
			}
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"output_text":"{\"summary\":\"This is a gen-proxy summary.\"}"}`)); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	settings := &Settings{
		GenProxyBaseURL:         ts.URL,
		GenProxyModel:           "gpt-5.1",
		GenProxyAPIKey:          "public-api-key",
		RequestTimeoutInSeconds: 5,
	}

	provider := newGenProxyProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.NoError(t, err)
	assert.Equal(t, "This is a gen-proxy summary.", summary)
}

func TestGenProxyProviderGenerateWithoutAPIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("X-API-Key"))

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"output_text":"{\"summary\":\"No key summary.\"}"}`)); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	settings := &Settings{
		GenProxyBaseURL:         ts.URL,
		GenProxyModel:           "gpt-5.1",
		RequestTimeoutInSeconds: 5,
	}

	provider := newGenProxyProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.NoError(t, err)
	assert.Equal(t, "No key summary.", summary)
}

func TestGenProxyProviderGenerateError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		if _, err := w.Write([]byte(`{"title":"Prompt exceeds token budget."}`)); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	settings := &Settings{
		GenProxyBaseURL:         ts.URL,
		GenProxyModel:           "gpt-5.1",
		RequestTimeoutInSeconds: 5,
	}

	provider := newGenProxyProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.Contains(t, err.Error(), "non-2xx status code: 422")
	assert.Contains(t, err.Error(), "Prompt exceeds token budget")
}
