package assistant

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
)

type Settings struct {
	OllamaHost   string
	OllamaPort   string
	OllamaScheme string
	OllamaModel  string
}

// AssistantProc precess the text
type AssistantProc struct {
	settings Settings

	client *ollamaClient
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
		http: http.DefaultClient,
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
	req := &ollamaRequest{
		Model:  p.settings.OllamaModel,
		System: "Act like assistant that return only result text. Result text should not contain any text formatting, sections or web links.",
		Prompt: prompt,
	}

	var result string
	respFunc := func(model ollamaResponse) error {
		result += model.Response
		return nil
	}

	if err := p.client.streamData(http.MethodPost, "/api/generate", req, respFunc); err != nil {
		return "", fmt.Errorf("failed to stream Ollama response: %v", err)
	}

	return result, nil
}

func (c *ollamaClient) streamData(method, path string, data *ollamaRequest, fn ollamaResponseFunc) error {
	var requestBody []byte
	if data != nil {
		var err error
		requestBody, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal request data: %v", err)
		}
	}

	requestURL := c.baseURL.JoinPath(path)
	req, err := http.NewRequest(method, requestURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to perform request: %v", err)
	}
	defer resp.Body.Close()

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

func (p AssistantProc) SummirizeText(text string) (string, error) {
	prompt := fmt.Sprintf("Summirize text. Result text length should be around 500 symbols and it should look like short story. The target text is '%s'.", text)
	result, err := p.doText(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to summirize text: %v", err)
	}
	return result, nil
}
