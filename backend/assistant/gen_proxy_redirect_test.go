package assistant

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type genProxyRedirectTransportFunc func(*http.Request) (*http.Response, error)

func (f genProxyRedirectTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func genProxyRedirectRequest() generationRequest {
	return generationRequest{
		SystemPrompt: "private system instructions",
		UserPrompt:   "private article content",
		Format: structuredOutputFormat{
			Name:   "summary",
			Schema: map[string]any{"type": "object"},
		},
	}
}

func genProxyRedirectResponse(req *http.Request, status int, location, body string) *http.Response {
	header := make(http.Header)
	if location != "" {
		header.Set("Location", location)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestGenProxyProviderRejectsCrossOriginRedirect(t *testing.T) {
	targets := []struct {
		name string
		url  string
	}{
		{name: "other host", url: "https://other.example/v1/responses"},
		{name: "subdomain", url: "https://child.provider.example/v1/responses"},
		{name: "other port", url: "https://provider.example:8443/v1/responses"},
		{name: "HTTPS downgrade", url: "http://provider.example/v1/responses"},
	}
	for _, target := range targets {
		for _, status := range []int{
			http.StatusMovedPermanently,
			http.StatusFound,
			http.StatusSeeOther,
			http.StatusTemporaryRedirect,
			http.StatusPermanentRedirect,
		} {
			t.Run(target.name+"/"+http.StatusText(status), func(t *testing.T) {
				provider := newGenProxyProvider(&Settings{
					GenProxyBaseURL: "https://provider.example",
					GenProxyModel:   "model",
					GenProxyAPIKey:  "private-api-key",
				})
				requests := 0
				provider.http.Transport = genProxyRedirectTransportFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					if requests > 1 {
						t.Fatalf("redirect reached %s with API key %q", req.URL, req.Header.Get("X-API-Key"))
					}
					if req.URL.String() != "https://provider.example/v1/responses" {
						t.Fatalf("unexpected initial URL: %s", req.URL)
					}
					assertGenProxyRedirectPayload(t, req)
					return genProxyRedirectResponse(req, status, target.url, ""), nil
				})

				_, err := provider.Generate(context.Background(), genProxyRedirectRequest())
				if err == nil || !strings.Contains(err.Error(), "configured provider origin") {
					t.Fatalf("expected origin rejection, got %v", err)
				}
				if requests != 1 {
					t.Fatalf("expected only the configured provider request, got %d", requests)
				}
			})
		}
	}
}

func TestGenProxyProviderFollowsSameOriginRedirect(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		location string
	}{
		{name: "relative path", baseURL: "https://provider.example", location: "/redirected"},
		{name: "same explicit port", baseURL: "https://provider.example:8443", location: "https://provider.example:8443/redirected"},
		{name: "default HTTPS port", baseURL: "https://provider.example", location: "https://provider.example:443/redirected"},
		{name: "default HTTP port", baseURL: "http://provider.example:80", location: "http://provider.example/redirected"},
		{name: "hostname case", baseURL: "https://provider.example", location: "https://PROVIDER.EXAMPLE/redirected"},
	}
	for _, test := range tests {
		for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			t.Run(test.name+"/"+http.StatusText(status), func(t *testing.T) {
				provider := newGenProxyProvider(&Settings{
					GenProxyBaseURL: test.baseURL,
					GenProxyModel:   "model",
					GenProxyAPIKey:  "private-api-key",
				})
				requests := 0
				provider.http.Transport = genProxyRedirectTransportFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					assertGenProxyRedirectPayload(t, req)
					if requests == 1 {
						return genProxyRedirectResponse(req, status, test.location, ""), nil
					}
					if req.URL.Path != "/redirected" {
						t.Fatalf("unexpected redirected path: %s", req.URL.Path)
					}
					return genProxyRedirectResponse(req, http.StatusOK, "", `{"output_text":"{\"summary\":\"redirected summary\"}"}`), nil
				})

				summary, err := provider.Generate(context.Background(), genProxyRedirectRequest())
				if err != nil {
					t.Fatalf("Generate failed: %v", err)
				}
				if summary != "redirected summary" || requests != 2 {
					t.Fatalf("unexpected result: summary=%q requests=%d", summary, requests)
				}
			})
		}
	}
}

func TestGenProxyProviderBoundsRedirectLoop(t *testing.T) {
	provider := newGenProxyProvider(&Settings{
		GenProxyBaseURL: "https://provider.example",
		GenProxyModel:   "model",
		GenProxyAPIKey:  "private-api-key",
	})
	requests := 0
	provider.http.Transport = genProxyRedirectTransportFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		assertGenProxyRedirectPayload(t, req)
		return genProxyRedirectResponse(req, http.StatusTemporaryRedirect, "/v1/responses", ""), nil
	})

	_, err := provider.Generate(context.Background(), genProxyRedirectRequest())
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
	if requests != 10 {
		t.Fatalf("expected redirect loop to stop after 10 requests, got %d", requests)
	}
}

func assertGenProxyRedirectPayload(t *testing.T, req *http.Request) {
	t.Helper()
	if req.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", req.Method)
	}
	if req.Header.Get("X-API-Key") != "private-api-key" {
		t.Fatal("request lost API key")
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	for _, text := range []string{"private system instructions", "private article content"} {
		if !strings.Contains(string(body), text) {
			t.Fatalf("request lost prompt %q", text)
		}
	}
}
