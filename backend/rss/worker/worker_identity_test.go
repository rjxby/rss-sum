package worker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/hasher"
	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type repeatingFeedFetcher map[string]*Feed

func (f repeatingFeedFetcher) Fetch(_ context.Context, feedURL string) (*Feed, error) {
	return f[feedURL], nil
}

func testPostService(t *testing.T) *blogger.BloggerProc {
	t.Helper()
	database, err := store.NewDatabaseWithPath(filepath.Join(t.TempDir(), "posts.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	require.NoError(t, database.Migrate())
	return blogger.New(database)
}

func TestRunOnceDeduplicatesScopedIDsAcrossFeeds(t *testing.T) {
	feedURL := "https://example.com/feed"
	otherFeedURL := "https://other.example.com/feed"
	postService := testPostService(t)
	hash := hasher.New()
	partition := hash.HashString(feedURL)
	otherPartition := hash.HashString(otherFeedURL)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seed := []blogger.Post{
		{ID: hash.HashString(partition + "\nknown-guid"), PartitionKey: partition, Title: "Scoped", Text: "Scoped summary", CreatedAt: now},
	}
	require.NoError(t, postService.SavePosts(seed))
	summarizer := &fakeSummarizer{results: map[string][]summaryResult{
		"shared text": {{text: "shared summary"}},
		"new text":    {{text: "new summary"}},
		"other text":  {{text: "other summary"}},
	}}
	worker := Worker{
		PostService: postService,
		Hasher:      hash,
		Summarizer:  summarizer,
		Clock:       fakeClock{now: now},
		FeedFetcher: repeatingFeedFetcher{
			feedURL: {Items: []FeedItem{
				{ID: "shared-guid", Text: "shared text"},
				{ID: "known-guid", Text: "known content"},
				{ID: "new-guid", Text: "new text"},
				{ID: "new-guid", Text: "new text"},
			}},
			otherFeedURL: {Items: []FeedItem{{ID: "shared-guid", Text: "other text"}}},
		},
		Settings: Settings{RSSFeedsURLs: []string{feedURL, otherFeedURL}, RSSFeedLimit: 10, WorkerTimeoutInSeconds: 10},
	}

	for range 3 {
		require.NoError(t, worker.RunOnce(context.Background()))
	}

	assert.Equal(t, []string{"shared text", "new text", "other text"}, summarizer.calls)
	page, err := postService.ListPosts(1, 10, "")
	require.NoError(t, err)
	assert.Equal(t, int64(4), page.Size)
	assert.ElementsMatch(t, append(seed,
		blogger.Post{ID: hash.HashString(partition + "\nshared-guid"), PartitionKey: partition, Text: "shared summary", CreatedAt: now},
		blogger.Post{ID: hash.HashString(partition + "\nnew-guid"), PartitionKey: partition, Text: "new summary", CreatedAt: now},
		blogger.Post{ID: hash.HashString(otherPartition + "\nshared-guid"), PartitionKey: otherPartition, Text: "other summary", CreatedAt: now},
	), page.Posts)
}

func TestRunOnceDeduplicatesItemsWithoutGUIDs(t *testing.T) {
	feedURL := "https://example.com/feed"
	sourceURL := "https://example.com/fallback"
	postService := testPostService(t)
	hash := hasher.New()
	partition := hash.HashString(feedURL)
	summarizer := &fakeSummarizer{results: map[string][]summaryResult{
		"URL fallback content":   {{text: "URL fallback summary"}},
		"title fallback content": {{text: "title fallback summary"}},
	}}
	worker := Worker{
		PostService: postService,
		Hasher:      hash,
		Summarizer:  summarizer,
		FeedFetcher: repeatingFeedFetcher{
			feedURL: {Items: []FeedItem{
				{SourceURL: sourceURL, Text: "URL fallback content"},
				{Title: "Fallback title", Text: "title fallback content"},
			}},
		},
		Settings: Settings{RSSFeedsURLs: []string{feedURL}, RSSFeedLimit: 3, WorkerTimeoutInSeconds: 10},
	}

	for range 3 {
		require.NoError(t, worker.RunOnce(context.Background()))
	}

	assert.Equal(t, []string{"URL fallback content", "title fallback content"}, summarizer.calls)
	ids, err := postService.FindRecentPostIDs(partition, 0)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		hash.HashString(partition + "\n" + sourceURL),
		hash.HashString(partition + "\nFallback title\ntitle fallback content"),
	}, ids)
}
