package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSummaryOutputRejectsBlankSummary(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary string
	}{
		{name: "empty"},
		{name: "spaces", summary: "   "},
		{name: "line breaks and tabs", summary: "\r\n\t "},
		{name: "Unicode whitespace", summary: "\u0085\u00a0\u1680\u2000\u2003\u2028\u2029\u202f\u205f\u3000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := json.Marshal(map[string]string{"summary": test.summary})
			require.NoError(t, err)

			summary, err := parseSummaryOutput("test-provider", string(output))

			require.ErrorContains(t, err, "test-provider structured summary response has blank summary field")
			require.Empty(t, summary)
		})
	}
}

func TestParseSummaryOutputPreservesValidSummary(t *testing.T) {
	for _, summary := range []string{
		"A useful summary.",
		" \tA useful summary.\n ",
		"\u3000記事の要約。\u00a0",
		strings.Repeat("A longer valid summary. ", 30),
	} {
		output, err := json.Marshal(map[string]string{"summary": summary})
		require.NoError(t, err)

		got, err := parseSummaryOutput("test-provider", string(output))

		require.NoError(t, err)
		require.Equal(t, summary, got)
	}
}

func TestSummarizeTextValidatesProviderSummary(t *testing.T) {
	for _, provider := range []string{ProviderOllama, ProviderGenProxy} {
		t.Run(provider, func(t *testing.T) {
			for _, test := range []struct {
				name    string
				summary string
				blank   bool
			}{
				{name: "empty", blank: true},
				{name: "ASCII whitespace", summary: " \r\n\t", blank: true},
				{name: "Unicode whitespace", summary: "\u00a0\u2003\u2028\u3000", blank: true},
				{name: "valid", summary: " \tAn informative summary.\n"},
			} {
				t.Run(test.name, func(t *testing.T) {
					output, err := json.Marshal(map[string]string{"summary": test.summary})
					require.NoError(t, err)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						field := "response"
						path := "/api/generate"
						if provider == ProviderGenProxy {
							field = "output_text"
							path = "/v1/responses"
						}
						if r.Method != http.MethodPost || r.URL.Path != path {
							t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
							http.Error(w, "unexpected request", http.StatusBadRequest)
							return
						}
						if err := json.NewEncoder(w).Encode(map[string]string{field: string(output)}); err != nil {
							t.Errorf("write provider response: %v", err)
						}
					}))
					defer server.Close()
					serverURL, err := url.Parse(server.URL)
					require.NoError(t, err)
					proc := New(&Settings{
						LLMProvider:             provider,
						OllamaHost:              serverURL.Hostname(),
						OllamaPort:              serverURL.Port(),
						OllamaScheme:            serverURL.Scheme,
						OllamaModel:             "test-model",
						GenProxyBaseURL:         server.URL,
						GenProxyModel:           "test-model",
						RequestTimeoutInSeconds: 5,
					})

					summary, err := proc.SummarizeText(context.Background(), "Article text.")

					if test.blank {
						require.ErrorContains(t, err, provider+" structured summary response has blank summary field")
						require.Empty(t, summary)
					} else {
						require.NoError(t, err)
						require.Equal(t, test.summary, summary)
					}
				})
			}
		})
	}
}
