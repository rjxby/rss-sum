package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Settings struct {
	OllamaHost              string
	OllamaPort              string
	OllamaScheme            string
	OllamaModel             string
	RequestTimeoutInSeconds int
}

// AssistantProc processes the text
type AssistantProc struct {
	settings Settings
	client   *ollamaClient
}

func ParseSettings() (*Settings, error) {
	settings := Settings{}

	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		return nil, fmt.Errorf("OLLAMA_HOST environment variable is empty")
	}
	settings.OllamaHost = ollamaHost

	ollamaPort := os.Getenv("OLLAMA_PORT")
	if ollamaPort == "" {
		return nil, fmt.Errorf("OLLAMA_PORT environment variable is empty")
	}
	settings.OllamaPort = ollamaPort

	ollamaScheme := os.Getenv("OLLAMA_SCHEME")
	if ollamaScheme == "" {
		return nil, fmt.Errorf("OLLAMA_SCHEME environment variable is empty")
	}
	settings.OllamaScheme = ollamaScheme

	ollamaModel := os.Getenv("OLLAMA_MODEL")
	if ollamaModel == "" {
		return nil, fmt.Errorf("OLLAMA_MODEL environment variable is empty")
	}
	settings.OllamaModel = ollamaModel

	settings.RequestTimeoutInSeconds = 30
	if timeoutStr := os.Getenv("OLLAMA_TIMEOUT_IN_SECONDS"); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			settings.RequestTimeoutInSeconds = timeout
		}
	}

	return &settings, nil
}

func New(settings *Settings) *AssistantProc {
	client := newOlamaClient(settings)

	return &AssistantProc{
		settings: *settings,
		client:   client,
	}
}

type ollamaClient struct {
	baseURL *url.URL
	http    *http.Client
}

func newOlamaClient(settings *Settings) *ollamaClient {
	return &ollamaClient{
		baseURL: &url.URL{
			Scheme: settings.OllamaScheme,
			Host:   net.JoinHostPort(settings.OllamaHost, settings.OllamaPort),
		},
		http: &http.Client{
			Timeout: time.Duration(settings.RequestTimeoutInSeconds) * time.Second,
		},
	}
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

type ollamaResponseFunc func(ollamaResponse) error

func (p AssistantProc) doText(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(p.settings.RequestTimeoutInSeconds)*time.Second)
	defer cancel()

	req := &ollamaRequest{
		Model:  p.settings.OllamaModel,
		System: "Act like assistant that returns only result text. Result text should not contain any text formatting, sections or web links.",
		Prompt: prompt,
	}

	var result string
	respFunc := func(model ollamaResponse) error {
		result += model.Response
		return nil
	}

	if err := p.client.streamData(ctx, http.MethodPost, "/api/generate", req, respFunc); err != nil {
		return "", fmt.Errorf("failed to stream Ollama response: %v", err)
	}

	return result, nil
}

func (c *ollamaClient) streamData(ctx context.Context, method, path string, data *ollamaRequest, fn ollamaResponseFunc) error {
	var requestBody []byte
	if data != nil {
		var err error
		requestBody, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal request data: %v", err)
		}
	}

	requestURL := c.baseURL.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[WARN] failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama API returned non-200 status code: %d", resp.StatusCode)
	}

	s := bufio.NewScanner(resp.Body)
	for s.Scan() {
		var parsedResult ollamaResponse
		if err := json.Unmarshal(s.Bytes(), &parsedResult); err != nil {
			return fmt.Errorf("failed to unmarshal part of response: %v", err)
		}

		if err := fn(parsedResult); err != nil {
			return fmt.Errorf("failed to aggregate result: %v", err)
		}
	}
	if s.Err() != nil {
		return fmt.Errorf("failed to scan response: %v", err)
	}

	return nil
}

func (p AssistantProc) SummarizeText(text string) (string, error) {
	prompt := fmt.Sprintf(`Summarize the following text with the following guidelines:
 - Limit the summary to around 500 characters
 - Capture the core message and most important points
 - Write it as a brief, engaging narrative
 - Preserve the tone of the original
 - Ensure the summary is coherent and self-contained
 - Do not include any explanation, formatting, or introduction—just return the summary text

-------------------------------------------------------------
Example:

Walgreens is collapsing, closing thousands of stores—not due to mismanagement or Amazon—but because of monopoly power from Pharmacy Benefit Managers (PBMs). PBMs (like CVS Caremark, Express Scripts, and OptumRx) control 80 per cent of drug pricing and insurance reimbursements. With unfair pricing, CVS profits while competitors like Walgreens and independents are squeezed out, worsening access and creating pharmacy deserts across the U.S.
--------------------------------------------------------------

The text to summarize is: '%s'`, text)

	result, err := p.doText(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to summarize text: %v", err)
	}
	return result, nil
}
