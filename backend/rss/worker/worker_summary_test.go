package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/assistant"
	"github.com/stretchr/testify/require"
)

func TestRunOnceBlankProviderSummaryRemainsEligible(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		summary := "\u00a0\u2003\u3000"
		if calls.Add(1) > 4 {
			summary = "A valid summary on the next pass."
		}
		output, err := json.Marshal(map[string]string{"summary": summary})
		if err != nil {
			t.Errorf("marshal structured summary: %v", err)
			http.Error(w, "marshal failure", http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"output_text": string(output)}); err != nil {
			t.Errorf("write provider response: %v", err)
		}
	}))
	defer server.Close()
	proc := assistant.New(&assistant.Settings{
		GenProxyBaseURL:         server.URL,
		GenProxyModel:           "test-model",
		RequestTimeoutInSeconds: 5,
	})
	feedURL := "https://example.com/feed"
	feed := &Feed{Items: []FeedItem{{
		ID:        "article-1",
		SourceURL: "https://example.com/article-1",
		Title:     "Article",
		Text:      "Article text.",
	}}}
	posts := &fakePostService{}
	sleeper := &fakeSleeper{}
	w := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 30,
		},
		Summarizer: proc,
		FeedFetcher: &fakeFeedFetcher{results: map[string][]fetchResult{
			feedURL: {{feed: feed}, {feed: feed}},
		}},
		PostService: posts,
		Sleeper:     sleeper,
	}

	err := w.RunOnce(context.Background())

	require.ErrorContains(t, err, "blank summary field")
	require.Equal(t, int32(3), calls.Load())
	require.Empty(t, posts.saved)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, sleeper.durations)

	err = w.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, int32(5), calls.Load())
	require.Len(t, posts.saved, 1)
	require.Len(t, posts.saved[0], 1)
	require.Equal(t, "A valid summary on the next pass.", posts.saved[0][0].Text)
	require.Equal(t, "https://example.com/article-1", posts.saved[0][0].SourceURL)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, time.Second}, sleeper.durations)
}
