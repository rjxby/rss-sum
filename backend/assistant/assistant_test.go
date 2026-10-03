package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/rjxby/rss-sum/backend/config"
	"github.com/stretchr/testify/assert"
)

func TestParseSettings(t *testing.T) {
	t.Run("DefaultProviderIsOllama", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, ProviderOllama, settings.LLMProvider)
		assert.Equal(t, "localhost", settings.OllamaHost)
		assert.Equal(t, "11434", settings.OllamaPort)
		assert.Equal(t, "http", settings.OllamaScheme)
		assert.Equal(t, "llama3:8b", settings.OllamaModel)
		assert.Equal(t, 30, settings.RequestTimeoutInSeconds)
		assert.Equal(t, defaultSystemPrompt, settings.SystemPrompt)
	})

	t.Run("ExplicitOllamaProvider", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderOllama)
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, ProviderOllama, settings.LLMProvider)
		assert.Equal(t, "llama3:8b", settings.OllamaModel)
	})

	t.Run("CustomTimeout", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")
		t.Setenv(config.EnvOllamaTimeoutInSeconds, "60")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 60, settings.RequestTimeoutInSeconds)
	})

	t.Run("InvalidTimeout", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")
		t.Setenv(config.EnvOllamaTimeoutInSeconds, "0")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("PromptFile", func(t *testing.T) {
		promptFile := filepath.Join(t.TempDir(), "system-prompt.txt")
		err := os.WriteFile(promptFile, []byte("  Custom system prompt.  \n"), 0o600)
		assert.NoError(t, err)

		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")
		t.Setenv(config.EnvLLMSystemPromptFile, promptFile)

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, "Custom system prompt.", settings.SystemPrompt)
	})

	t.Run("MissingPromptFile", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")
		t.Setenv(config.EnvLLMSystemPromptFile, filepath.Join(t.TempDir(), "missing.txt"))

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), config.EnvLLMSystemPromptFile)
	})

	t.Run("EmptyPromptFile", func(t *testing.T) {
		promptFile := filepath.Join(t.TempDir(), "system-prompt.txt")
		err := os.WriteFile(promptFile, []byte(" \n\t"), 0o600)
		assert.NoError(t, err)

		t.Setenv(config.EnvLLMProvider, "")
		t.Setenv(config.EnvOllamaHost, "localhost")
		t.Setenv(config.EnvOllamaPort, "11434")
		t.Setenv(config.EnvOllamaScheme, "http")
		t.Setenv(config.EnvOllamaModel, "llama3:8b")
		t.Setenv(config.EnvLLMSystemPromptFile, promptFile)

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("GenProxyProvider", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderGenProxy)
		t.Setenv(config.EnvGenProxyBaseURL, "https://localhost:7001")
		t.Setenv(config.EnvGenProxyModel, "gpt-5.1")
		t.Setenv(config.EnvGenProxyAPIKey, "public-api-key")
		t.Setenv(config.EnvGenProxyTimeoutInSeconds, "45")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, ProviderGenProxy, settings.LLMProvider)
		assert.Equal(t, "https://localhost:7001", settings.GenProxyBaseURL)
		assert.Equal(t, "gpt-5.1", settings.GenProxyModel)
		assert.Equal(t, "public-api-key", settings.GenProxyAPIKey)
		assert.Equal(t, 45, settings.RequestTimeoutInSeconds)
	})

	t.Run("GenProxyDoesNotRequireAPIKey", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderGenProxy)
		t.Setenv(config.EnvGenProxyBaseURL, "https://localhost:7001")
		t.Setenv(config.EnvGenProxyModel, "gpt-5.1")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, "", settings.GenProxyAPIKey)
		assert.Equal(t, 30, settings.RequestTimeoutInSeconds)
	})

	t.Run("InvalidProvider", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, "openai")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), config.EnvLLMProvider)
	})

	t.Run("MissingGenProxyBaseURL", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderGenProxy)
		t.Setenv(config.EnvGenProxyModel, "gpt-5.1")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), config.EnvGenProxyBaseURL)
	})

	t.Run("MissingGenProxyModel", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderGenProxy)
		t.Setenv(config.EnvGenProxyBaseURL, "https://localhost:7001")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), config.EnvGenProxyModel)
	})

	t.Run("InvalidGenProxyBaseURL", func(t *testing.T) {
		t.Setenv(config.EnvLLMProvider, ProviderGenProxy)
		t.Setenv(config.EnvGenProxyBaseURL, "localhost:7001")
		t.Setenv(config.EnvGenProxyModel, "gpt-5.1")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
		assert.Contains(t, err.Error(), "absolute URL")
	})

	tbl := []struct {
		name        string
		envVar      string
		envValue    string
		expectError bool
	}{
		{"MissingHost", config.EnvOllamaHost, "", true},
		{"MissingPort", config.EnvOllamaPort, "", true},
		{"MissingScheme", config.EnvOllamaScheme, "", true},
		{"MissingModel", config.EnvOllamaModel, "", true},
	}

	for _, tt := range tbl {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.EnvLLMProvider, "")
			t.Setenv(config.EnvOllamaHost, "localhost")
			t.Setenv(config.EnvOllamaPort, "11434")
			t.Setenv(config.EnvOllamaScheme, "http")
			t.Setenv(config.EnvOllamaModel, "llama3:8b")

			t.Setenv(tt.envVar, tt.envValue)

			settings, err := ParseSettings()

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, settings)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, settings)
			}
		})
	}
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

func TestNewLLMProvider(t *testing.T) {
	t.Run("Ollama", func(t *testing.T) {
		settings := &Settings{
			LLMProvider:             ProviderOllama,
			OllamaHost:              "localhost",
			OllamaPort:              "11434",
			OllamaScheme:            "http",
			OllamaModel:             "llama3:8b",
			RequestTimeoutInSeconds: 5,
		}

		provider := newLLMProvider(settings)

		assert.IsType(t, &ollamaProvider{}, provider)
	})

	t.Run("GenProxy", func(t *testing.T) {
		settings := &Settings{
			LLMProvider:             ProviderGenProxy,
			GenProxyBaseURL:         "https://localhost:7001",
			GenProxyModel:           "gpt-5.1",
			RequestTimeoutInSeconds: 5,
		}

		provider := newLLMProvider(settings)

		assert.IsType(t, &genProxyProvider{}, provider)
	})

	t.Run("UnknownProviderFallsBackToOllama", func(t *testing.T) {
		settings := &Settings{
			LLMProvider:             "unknown",
			OllamaHost:              "localhost",
			OllamaPort:              "11434",
			OllamaScheme:            "http",
			OllamaModel:             "llama3:8b",
			RequestTimeoutInSeconds: 5,
		}

		provider := newLLMProvider(settings)

		assert.IsType(t, &ollamaProvider{}, provider)
	})
}

func testGenerationRequest(prompt string) generationRequest {
	return generationRequest{
		SystemPrompt: "test system prompt",
		UserPrompt:   prompt,
		Format:       summaryOutputFormat(),
	}
}

func TestOllamaProviderGenerate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/generate", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req ollamaRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "llama3:8b", req.Model)
		assert.Equal(t, "test system prompt", req.System)
		assert.Equal(t, "Test input text", req.Prompt)
		assert.False(t, req.Stream)
		assert.Equal(t, "object", req.Format["type"])
		assert.Contains(t, req.Format, "properties")

		resp := ollamaResponse{Response: `{"summary":"This is a test summary."}`}
		respJSON, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respJSON); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse test server URL: %v", err)
	}

	settings := &Settings{
		LLMProvider:             ProviderOllama,
		OllamaHost:              serverURL.Hostname(),
		OllamaPort:              serverURL.Port(),
		OllamaScheme:            serverURL.Scheme,
		OllamaModel:             "llama3:8b",
		RequestTimeoutInSeconds: 5,
	}

	provider := newOllamaProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.NoError(t, err)
	assert.Equal(t, "This is a test summary.", summary)
}

func TestOllamaProviderGenerateInvalidStructuredResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := ollamaResponse{Response: "not json"}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse test server URL: %v", err)
	}

	settings := &Settings{
		LLMProvider:             ProviderOllama,
		OllamaHost:              serverURL.Hostname(),
		OllamaPort:              serverURL.Port(),
		OllamaScheme:            serverURL.Scheme,
		OllamaModel:             "llama3:8b",
		RequestTimeoutInSeconds: 5,
	}

	provider := newOllamaProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.Contains(t, err.Error(), "structured summary")
}

func TestOllamaProviderGenerateError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse test server URL: %v", err)
	}

	settings := &Settings{
		LLMProvider:             ProviderOllama,
		OllamaHost:              serverURL.Hostname(),
		OllamaPort:              serverURL.Port(),
		OllamaScheme:            serverURL.Scheme,
		OllamaModel:             "llama3:8b",
		RequestTimeoutInSeconds: 5,
	}

	provider := newOllamaProvider(settings)
	provider.http = ts.Client()

	summary, err := provider.Generate(context.Background(), testGenerationRequest("Test input text"))

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.Contains(t, err.Error(), "non-200 status code")
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
		LLMProvider:             ProviderGenProxy,
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
		LLMProvider:             ProviderGenProxy,
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
		LLMProvider:             ProviderGenProxy,
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
