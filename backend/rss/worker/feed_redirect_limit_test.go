package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/require"
)

type redirectTestTransport func(*http.Request) (*http.Response, error)

func (f redirectTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func redirectTestResponse(req *http.Request, location string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{location}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
}

func TestSafeFeedHTTPClientStopsRedirectLoops(t *testing.T) {
	for _, loop := range []string{"self", "mutual"} {
		t.Run(loop, func(t *testing.T) {
			client := safeFeedHTTPClient()
			requests := 0
			client.Transport = redirectTestTransport(func(req *http.Request) (*http.Response, error) {
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				requests++
				location := req.URL.String()
				if loop == "mutual" {
					location = "https://8.8.8.8/second"
					if req.URL.Path == "/second" {
						location = "https://1.1.1.1/first"
					}
				}
				return redirectTestResponse(req, location), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://1.1.1.1/first", nil)
			require.NoError(t, err)

			response, err := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
			}

			require.ErrorContains(t, err, "stopped after 10 redirects")
			require.Equal(t, 10, requests)
			require.NoError(t, ctx.Err())
		})
	}
}

func TestSafeFeedHTTPClientFollowsRedirectsBelowLimit(t *testing.T) {
	client := safeFeedHTTPClient()
	requests := 0
	client.Transport = redirectTestTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		step, err := strconv.Atoi(strings.TrimPrefix(req.URL.Path, "/"))
		if err != nil {
			return nil, err
		}
		if step < 9 {
			return redirectTestResponse(req, fmt.Sprintf("/%d", step+1)), nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("feed content")),
			Request:    req,
		}, nil
	})

	response, err := client.Get("https://1.1.1.1/0")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "feed content", string(body))
	require.Equal(t, "/9", response.Request.URL.Path)
	require.Equal(t, 10, requests)
}

func TestSafeFeedHTTPClientRejectsBlockedRedirectBeforeRequest(t *testing.T) {
	client := safeFeedHTTPClient()
	requests := 0
	client.Transport = redirectTestTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		return redirectTestResponse(req, "https://127.0.0.1/feed"), nil
	})

	response, err := client.Get("https://1.1.1.1/feed")
	if response != nil {
		_ = response.Body.Close()
	}

	require.ErrorContains(t, err, "host IP is not allowed")
	require.Equal(t, 1, requests)
}

func TestSafeFeedHTTPClientPreservesRedirectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := safeFeedHTTPClient()
	requests := 0
	client.Transport = redirectTestTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		requests++
		cancel()
		return redirectTestResponse(req, "/next"), nil
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://1.1.1.1/feed", nil)
	require.NoError(t, err)

	response, err := client.Do(req)
	if response != nil {
		_ = response.Body.Close()
	}

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, requests)
}

type redirectTestFeedFetcher struct {
	client *http.Client
}

func (f redirectTestFeedFetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	parser := gofeed.NewParser()
	parser.Client = f.client
	parsed, err := parser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}
	feed := &Feed{}
	for _, item := range parsed.Items {
		feed.Items = append(feed.Items, mapFeedItem(item))
	}
	return feed, nil
}

func TestRunOnceSavesFollowingFeedAfterRedirectLoop(t *testing.T) {
	const loopURL = "https://1.1.1.1/loop"
	const feedURL = "https://8.8.8.8/feed"
	client := safeFeedHTTPClient()
	loopRequests := 0
	feedRequests := 0
	client.Transport = redirectTestTransport(func(req *http.Request) (*http.Response, error) {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		if req.URL.String() == loopURL {
			loopRequests++
			return redirectTestResponse(req, loopURL), nil
		}
		if req.URL.String() != feedURL {
			return nil, errors.New("unexpected feed request")
		}
		feedRequests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
			Body: io.NopCloser(strings.NewReader(`<rss version="2.0"><channel>
<title>Following feed</title><link>https://8.8.8.8/</link><description>Feed</description>
<item><guid>article-1</guid><title>Article</title><link>https://8.8.8.8/article-1</link>
<description>Article text</description></item></channel></rss>`)),
			Request: req,
		}, nil
	})
	posts := &fakePostService{}
	w := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{loopURL, feedURL},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 1,
		},
		FeedFetcher: redirectTestFeedFetcher{client: client},
		PostService: posts,
		Summarizer: &fakeSummarizer{results: map[string][]summaryResult{
			"Article text": {{text: "Article summary"}},
		}},
		Sleeper: &fakeSleeper{},
	}

	err := w.RunOnce(context.Background())

	require.ErrorContains(t, err, "stopped after 10 redirects")
	require.Equal(t, 30, loopRequests)
	require.Equal(t, 1, feedRequests)
	require.Len(t, posts.saved, 1)
	require.Len(t, posts.saved[0], 1)
	require.Equal(t, "Article summary", posts.saved[0][0].Text)
	require.Equal(t, "https://8.8.8.8/article-1", posts.saved[0][0].SourceURL)
}
