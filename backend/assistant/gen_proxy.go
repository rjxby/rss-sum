package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type genProxyProvider struct {
	baseURL *url.URL
	http    *http.Client
	model   string
	apiKey  string
}

func newGenProxyProvider(settings *Settings) *genProxyProvider {
	baseURL, _ := url.Parse(settings.GenProxyBaseURL)
	if baseURL == nil {
		baseURL = &url.URL{}
	}

	return &genProxyProvider{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: time.Duration(settings.RequestTimeoutInSeconds) * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if !sameGenProxyOrigin(baseURL, req.URL) {
					return errors.New("gen-proxy redirect must stay within the configured provider origin")
				}
				return nil
			},
		},
		model:  settings.GenProxyModel,
		apiKey: settings.GenProxyAPIKey,
	}
}

func sameGenProxyOrigin(baseURL, targetURL *url.URL) bool {
	return strings.EqualFold(baseURL.Scheme, targetURL.Scheme) &&
		strings.EqualFold(baseURL.Hostname(), targetURL.Hostname()) &&
		genProxyOriginPort(baseURL) == genProxyOriginPort(targetURL)
}

func genProxyOriginPort(providerURL *url.URL) string {
	if port := providerURL.Port(); port != "" {
		return port
	}
	if strings.EqualFold(providerURL.Scheme, "https") {
		return "443"
	}
	return "80"
}

type genProxyRequest struct {
	Model          string                 `json:"model"`
	Input          []genProxyInputMessage `json:"input"`
	ResponseFormat genProxyResponseFormat `json:"response_format"`
	Metadata       map[string]string      `json:"metadata,omitempty"`
}

type genProxyInputMessage struct {
	Type    string                     `json:"type"`
	Role    string                     `json:"role"`
	Content []genProxyInputContentPart `json:"content"`
}

type genProxyInputContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type genProxyResponseFormat struct {
	Type       string             `json:"type"`
	JSONSchema genProxyJSONSchema `json:"json_schema"`
}

type genProxyJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type genProxyResponse struct {
	OutputText string `json:"output_text"`
}

func (p *genProxyProvider) Generate(ctx context.Context, request generationRequest) (string, error) {
	prompt := request.UserPrompt
	if request.SystemPrompt != "" {
		// gen-proxy accepts one user message and has no instructions field.
		prompt = request.SystemPrompt + "\n\n" + prompt
	}
	reqBody := &genProxyRequest{
		Model: p.model,
		Input: []genProxyInputMessage{
			{
				Type: "message",
				Role: "user",
				Content: []genProxyInputContentPart{
					{
						Type: "input_text",
						Text: prompt,
					},
				},
			},
		},
		ResponseFormat: genProxyResponseFormat{
			Type: "json_schema",
			JSONSchema: genProxyJSONSchema{
				Name:   request.Format.Name,
				Strict: true,
				Schema: request.Format.Schema,
			},
		},
		Metadata: map[string]string{
			"source": "rss-sum",
		},
	}

	requestBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal gen-proxy request data: %v", err)
	}

	requestURL := p.baseURL.JoinPath("/v1/responses")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create gen-proxy request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("X-API-Key", p.apiKey)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to perform gen-proxy request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[WARN] failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("gen-proxy API returned non-2xx status code: %d: %s", resp.StatusCode, readErrorSnippet(resp.Body))
	}

	var parsedResult genProxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsedResult); err != nil {
		return "", fmt.Errorf("failed to unmarshal gen-proxy response: %v", err)
	}

	return parseSummaryOutput("gen-proxy", parsedResult.OutputText)
}

func readErrorSnippet(reader io.Reader) string {
	const maxErrorSnippetBytes = 512

	body, err := io.ReadAll(io.LimitReader(reader, maxErrorSnippetBytes))
	if err != nil {
		return "failed to read response body"
	}

	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		return "empty response body"
	}

	return snippet
}
