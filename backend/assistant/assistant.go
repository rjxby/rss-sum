package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/rjxby/rss-sum/backend/config"
)

const defaultSystemPrompt = "Act like an assistant that returns concise, direct results without text formatting, sections, or web links."

type Settings struct {
	LLMProvider             string
	SystemPrompt            string
	OllamaHost              string
	OllamaPort              string
	OllamaScheme            string
	OllamaModel             string
	GenProxyBaseURL         string
	GenProxyModel           string
	GenProxyAPIKey          string
	RequestTimeoutInSeconds int
}

type AssistantProc struct {
	settings Settings
	provider llmProvider
}

func ParseSettings() (*Settings, error) {
	provider := config.OptionalString(config.EnvLLMProvider, ProviderOllama)
	systemPrompt, err := parseSystemPrompt()
	if err != nil {
		return nil, err
	}

	settings := &Settings{
		LLMProvider:  provider,
		SystemPrompt: systemPrompt,
	}

	switch provider {
	case ProviderOllama:
		ollamaHost, err := config.RequiredString(config.EnvOllamaHost)
		if err != nil {
			return nil, err
		}
		ollamaPort, err := config.RequiredString(config.EnvOllamaPort)
		if err != nil {
			return nil, err
		}
		ollamaScheme, err := config.RequiredString(config.EnvOllamaScheme)
		if err != nil {
			return nil, err
		}
		ollamaModel, err := config.RequiredString(config.EnvOllamaModel)
		if err != nil {
			return nil, err
		}
		timeout, err := config.PositiveInt(config.EnvOllamaTimeoutInSeconds, 30)
		if err != nil {
			return nil, err
		}

		settings.OllamaHost = ollamaHost
		settings.OllamaPort = ollamaPort
		settings.OllamaScheme = ollamaScheme
		settings.OllamaModel = ollamaModel
		settings.RequestTimeoutInSeconds = timeout
	case ProviderGenProxy:
		baseURL, err := config.RequiredString(config.EnvGenProxyBaseURL)
		if err != nil {
			return nil, err
		}
		parsedBaseURL, err := url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s environment variable: %v", config.EnvGenProxyBaseURL, err)
		}
		if parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
			return nil, fmt.Errorf("%s environment variable must be an absolute URL", config.EnvGenProxyBaseURL)
		}
		model, err := config.RequiredString(config.EnvGenProxyModel)
		if err != nil {
			return nil, err
		}
		timeout, err := config.PositiveInt(config.EnvGenProxyTimeoutInSeconds, 30)
		if err != nil {
			return nil, err
		}

		settings.GenProxyBaseURL = baseURL
		settings.GenProxyModel = model
		settings.GenProxyAPIKey = config.OptionalString(config.EnvGenProxyAPIKey, "")
		settings.RequestTimeoutInSeconds = timeout
	default:
		return nil, fmt.Errorf("%s must be one of %q or %q", config.EnvLLMProvider, ProviderOllama, ProviderGenProxy)
	}

	return settings, nil
}

func parseSystemPrompt() (string, error) {
	promptFile := config.OptionalString(config.EnvLLMSystemPromptFile, "")
	if promptFile == "" {
		return defaultSystemPrompt, nil
	}

	content, err := os.ReadFile(promptFile)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %v", config.EnvLLMSystemPromptFile, err)
	}

	prompt := strings.TrimSpace(string(content))
	if prompt == "" {
		return "", fmt.Errorf("%s must not be empty", config.EnvLLMSystemPromptFile)
	}

	return prompt, nil
}

func New(settings *Settings) *AssistantProc {
	normalizedSettings := *settings
	if normalizedSettings.SystemPrompt == "" {
		normalizedSettings.SystemPrompt = defaultSystemPrompt
	}

	return &AssistantProc{
		settings: normalizedSettings,
		provider: newLLMProvider(&normalizedSettings),
	}
}

func (p AssistantProc) doText(ctx context.Context, request generationRequest) (string, error) {
	return p.provider.Generate(ctx, request)
}

func (p AssistantProc) SummarizeText(ctx context.Context, text string) (string, error) {
	systemPrompt := p.settings.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	prompt := fmt.Sprintf(`Summarize the following text with the following guidelines:
 - Limit the summary to around 500 characters
 - Capture the core message and most important points
 - Write it as a brief, engaging narrative
 - Preserve the tone of the original
 - Ensure the summary is coherent and self-contained
 - Do not include explanation or introduction in the summary

-------------------------------------------------------------
Example:

Walgreens is collapsing, closing thousands of stores—not due to mismanagement or Amazon—but because of monopoly power from Pharmacy Benefit Managers (PBMs). PBMs (like CVS Caremark, Express Scripts, and OptumRx) control 80 per cent of drug pricing and insurance reimbursements. With unfair pricing, CVS profits while competitors like Walgreens and independents are squeezed out, worsening access and creating pharmacy deserts across the U.S.
--------------------------------------------------------------

The text to summarize is: '%s'`, text)

	result, err := p.doText(ctx, generationRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   prompt,
		Format:       summaryOutputFormat(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to summarize text: %v", err)
	}
	return result, nil
}

func summaryOutputFormat() structuredOutputFormat {
	return structuredOutputFormat{
		Name: "rss_summary",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"summary": map[string]any{
					"type":        "string",
					"description": "A concise summary of the source text.",
				},
			},
			"required": []string{"summary"},
		},
	}
}

func parseSummaryOutput(providerName, output string) (string, error) {
	var parsed struct {
		Summary *string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s structured summary: %v", providerName, err)
	}
	if parsed.Summary == nil {
		return "", fmt.Errorf("%s structured summary response missing summary field", providerName)
	}
	if strings.TrimSpace(*parsed.Summary) == "" {
		return "", fmt.Errorf("%s structured summary response has blank summary field", providerName)
	}

	return *parsed.Summary, nil
}
