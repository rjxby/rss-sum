package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

type ollamaProvider struct {
	baseURL *url.URL
	http    *http.Client
	model   string
}

func newOllamaProvider(settings *Settings) *ollamaProvider {
	return &ollamaProvider{
		baseURL: &url.URL{
			Scheme: settings.OllamaScheme,
			Host:   net.JoinHostPort(settings.OllamaHost, settings.OllamaPort),
		},
		http: &http.Client{
			Timeout: time.Duration(settings.RequestTimeoutInSeconds) * time.Second,
		},
		model: settings.OllamaModel,
	}
}

type ollamaRequest struct {
	Model  string         `json:"model"`
	Prompt string         `json:"prompt"`
	System string         `json:"system"`
	Stream bool           `json:"stream"`
	Format map[string]any `json:"format"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

func (p *ollamaProvider) Generate(ctx context.Context, request generationRequest) (string, error) {
	req := &ollamaRequest{
		Model:  p.model,
		System: request.SystemPrompt,
		Prompt: request.UserPrompt,
		Stream: false,
		Format: request.Format.Schema,
	}

	resp, err := p.do(ctx, http.MethodPost, "/api/generate", req)
	if err != nil {
		return "", err
	}

	return parseSummaryOutput("ollama", resp.Response)
}

func (p *ollamaProvider) do(ctx context.Context, method, path string, data *ollamaRequest) (*ollamaResponse, error) {
	var requestBody []byte
	if data != nil {
		var err error
		requestBody, err = json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request data: %v", err)
		}
	}

	requestURL := p.baseURL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[WARN] failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API returned non-200 status code: %d", resp.StatusCode)
	}

	var parsedResult ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsedResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Ollama response: %v", err)
	}
	return &parsedResult, nil
}
