package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSettings(t *testing.T) {
	t.Run("ValidSettings", func(t *testing.T) {
		t.Setenv("OLLAMA_HOST", "localhost")
		t.Setenv("OLLAMA_PORT", "11434")
		t.Setenv("OLLAMA_SCHEME", "http")
		t.Setenv("OLLAMA_MODEL", "llama3:8b")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, "localhost", settings.OllamaHost)
		assert.Equal(t, "11434", settings.OllamaPort)
		assert.Equal(t, "http", settings.OllamaScheme)
		assert.Equal(t, "llama3:8b", settings.OllamaModel)
		assert.Equal(t, 30, settings.RequestTimeoutInSeconds) // Default value
	})

	t.Run("CustomTimeout", func(t *testing.T) {
		t.Setenv("OLLAMA_HOST", "localhost")
		t.Setenv("OLLAMA_PORT", "11434")
		t.Setenv("OLLAMA_SCHEME", "http")
		t.Setenv("OLLAMA_MODEL", "llama3:8b")
		t.Setenv("OLLAMA_TIMEOUT_IN_SECONDS", "60")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 60, settings.RequestTimeoutInSeconds)
	})

	tbl := []struct {
		name        string
		envVar      string
		envValue    string
		expectError bool
	}{
		{"MissingHost", "OLLAMA_HOST", "", true},
		{"MissingPort", "OLLAMA_PORT", "", true},
		{"MissingScheme", "OLLAMA_SCHEME", "", true},
		{"MissingModel", "OLLAMA_MODEL", "", true},
	}

	for _, tt := range tbl {
		t.Run(tt.name, func(t *testing.T) {
			// Set all required variables first
			t.Setenv("OLLAMA_HOST", "localhost")
			t.Setenv("OLLAMA_PORT", "11434")
			t.Setenv("OLLAMA_SCHEME", "http")
			t.Setenv("OLLAMA_MODEL", "llama3:8b")

			// Then override with the test case
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

func TestSummarizeText(t *testing.T) {
	// Create a mock server that returns a successful response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		assert.Equal(t, "/api/generate", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		// Return a response
		resp := ollamaResponse{Response: "This is a test summary."}
		respJSON, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(respJSON); err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer ts.Close()

	// Create assistant with test server URL
	settings := &Settings{
		OllamaHost:              "localhost",
		OllamaPort:              "8080",
		OllamaScheme:            "http",
		OllamaModel:             "llama3:8b",
		RequestTimeoutInSeconds: 5,
	}

	// Override client to use test server
	assistant := New(settings)

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse test server URL: %v", err)
	}

	assistant.client.baseURL = serverURL
	assistant.client.http = ts.Client()

	// Test summarization
	summary, err := assistant.SummarizeText("Test input text")

	assert.NoError(t, err)
	assert.Equal(t, "This is a test summary.", summary)
}

func TestSummarizeTextError(t *testing.T) {
	// Create a mock server that returns an error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	// Create assistant with test server URL
	settings := &Settings{
		OllamaHost:              "localhost",
		OllamaPort:              "8080",
		OllamaScheme:            "http",
		OllamaModel:             "llama3:8b",
		RequestTimeoutInSeconds: 5,
	}

	// Override client to use test server
	assistant := New(settings)

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Failed to parse test server URL: %v", err)
	}

	assistant.client.baseURL = serverURL
	assistant.client.http = ts.Client()

	// Test summarization with error
	summary, err := assistant.SummarizeText("Test input text")

	assert.Error(t, err)
	assert.Empty(t, summary)
	assert.Contains(t, err.Error(), "non-200 status code")
}
