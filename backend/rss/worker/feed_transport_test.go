package worker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSafeFeedHTTPClientIgnoresEnvironmentProxies(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "http://8.8.8.8:3128")
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("REQUEST_METHOD", "")

	for _, feedURL := range []string{"http://feed.example/feed", "https://feed.example/feed"} {
		t.Run(feedURL, func(t *testing.T) {
			client := safeFeedHTTPClient()
			transport := client.Transport.(*http.Transport)
			t.Cleanup(transport.CloseIdleConnections)
			if transport.Proxy != nil {
				t.Fatal("feed transport retained a proxy function")
			}

			dialErr := errors.New("stop before connecting")
			var dialAddress string
			transport.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
				dialAddress = address
				return nil, dialErr
			}
			_, err := client.Get(feedURL)
			if !errors.Is(err, dialErr) {
				t.Fatalf("request error = %v, want dial error", err)
			}
			wantAddress := "feed.example:80"
			if strings.HasPrefix(feedURL, "https:") {
				wantAddress = "feed.example:443"
			}
			if dialAddress != wantAddress {
				t.Fatalf("dial address = %q, want %q", dialAddress, wantAddress)
			}
		})
	}
}

func TestSafeFeedHTTPClientDoesNotInheritDefaultProxy(t *testing.T) {
	originalTransport := http.DefaultTransport
	defaultTransport := originalTransport.(*http.Transport).Clone()
	proxyCalled := false
	defaultTransport.Proxy = func(_ *http.Request) (*url.URL, error) {
		proxyCalled = true
		return url.Parse("http://8.8.8.8:3128")
	}
	http.DefaultTransport = defaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
		defaultTransport.CloseIdleConnections()
	})

	client := safeFeedHTTPClient()
	transport := client.Transport.(*http.Transport)
	t.Cleanup(transport.CloseIdleConnections)
	dialErr := errors.New("stop before connecting")
	var dialAddress string
	transport.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
		dialAddress = address
		return nil, dialErr
	}
	_, err := client.Get("http://feed.example/feed")
	if !errors.Is(err, dialErr) {
		t.Fatalf("request error = %v, want dial error", err)
	}
	if proxyCalled {
		t.Error("feed request called the inherited proxy function")
	}
	if dialAddress != "feed.example:80" {
		t.Fatalf("dial address = %q, want feed.example:80", dialAddress)
	}
}

func TestSafeFeedHTTPClientRejectsBlockedDestinations(t *testing.T) {
	for _, feedURL := range []string{
		"http://127.0.0.1/feed",
		"https://10.0.0.1/feed",
		"http://[::1]/feed",
		"https://localhost/feed",
		"http://feed.local/feed",
	} {
		t.Run(feedURL, func(t *testing.T) {
			client := safeFeedHTTPClient()
			t.Cleanup(client.CloseIdleConnections)
			_, err := client.Get(feedURL)
			if err == nil {
				t.Fatal("request to a blocked destination succeeded")
			}
			if !strings.Contains(err.Error(), "blocked IP") && !strings.Contains(err.Error(), "host is not allowed") {
				t.Fatalf("request error = %v, want blocked destination error", err)
			}
		})
	}
}

func TestSafeFeedHTTPClientValidatesRedirectDestinations(t *testing.T) {
	client := safeFeedHTTPClient()
	t.Cleanup(client.CloseIdleConnections)
	for _, tt := range []struct {
		feedURL string
		wantErr bool
	}{
		{feedURL: "https://8.8.8.8/feed"},
		{feedURL: "http://127.0.0.1/feed", wantErr: true},
		{feedURL: "https://localhost/feed", wantErr: true},
		{feedURL: "file:///tmp/feed", wantErr: true},
	} {
		t.Run(tt.feedURL, func(t *testing.T) {
			redirectURL, err := url.Parse(tt.feedURL)
			if err != nil {
				t.Fatal(err)
			}
			err = client.CheckRedirect(&http.Request{URL: redirectURL}, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("redirect error = %v, want error = %v", err, tt.wantErr)
			}
		})
	}
}
