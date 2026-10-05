package worker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/config"
)

func TestParseSettingsContextInterruptsDNS(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(dnsCancellationName(deadline), func(t *testing.T) {
			started := withBlockingFeedResolver(t)
			t.Setenv(config.EnvFeeds, "https://slow-feed.example./feed")
			ctx, cancel := dnsCancellationContext(deadline)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				settings, err := ParseSettingsContext(ctx)
				if settings != nil {
					err = errors.New("settings returned during blocked DNS resolution")
				}
				result <- err
			}()

			awaitFeedDNS(t, started)
			if !deadline {
				cancel()
			}
			assertFeedDNSInterrupted(t, ctx, result)
		})
	}
}

func TestFeedRedirectInterruptsDNS(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(dnsCancellationName(deadline), func(t *testing.T) {
			started := withBlockingFeedResolver(t)
			client := safeFeedHTTPClient()
			client.CloseIdleConnections()
			requests := 0
			client.Transport = feedPolicyRoundTripper(func(request *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{"https://slow-feed.example./feed"}},
					Body:       io.NopCloser(strings.NewReader("redirect")),
					Request:    request,
				}, nil
			})
			ctx, cancel := dnsCancellationContext(deadline)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://8.8.8.8/feed", nil)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				response, err := client.Do(request)
				if response != nil {
					_ = response.Body.Close()
				}
				result <- err
			}()

			awaitFeedDNS(t, started)
			if !deadline {
				cancel()
			}
			assertFeedDNSInterrupted(t, ctx, result)
			if requests != 1 {
				t.Fatalf("made %d HTTP requests, want only the initial request", requests)
			}
		})
	}
}

func withBlockingFeedResolver(t *testing.T) <-chan struct{} {
	t.Helper()
	started := make(chan struct{}, 1)
	original := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	t.Cleanup(func() { net.DefaultResolver = original })
	return started
}

func dnsCancellationName(deadline bool) string {
	if deadline {
		return "deadline"
	}
	return "cancel"
}

func dnsCancellationContext(deadline bool) (context.Context, context.CancelFunc) {
	if deadline {
		return context.WithTimeout(context.Background(), 200*time.Millisecond)
	}
	return context.WithCancel(context.Background())
}

func awaitFeedDNS(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("DNS resolution did not start")
	}
}

func assertFeedDNSInterrupted(t *testing.T, ctx context.Context, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
			t.Fatalf("error = %v, want context error %v", err, ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DNS resolution did not stop after cancellation")
	}
}
