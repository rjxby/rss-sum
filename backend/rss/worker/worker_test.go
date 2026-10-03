package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/config"
	"github.com/stretchr/testify/assert"
)

type fetchResult struct {
	feed *Feed
	err  error
}

type fakeFeedFetcher struct {
	results map[string][]fetchResult
	calls   []string
}

func (f *fakeFeedFetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	f.calls = append(f.calls, feedURL)
	results := f.results[feedURL]
	if len(results) == 0 {
		return nil, fmt.Errorf("unexpected fetch for %s", feedURL)
	}
	result := results[0]
	f.results[feedURL] = results[1:]
	return result.feed, result.err
}

type cancelingFeedFetcher struct {
	cancel context.CancelFunc
	calls  int
}

func (f *cancelingFeedFetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	f.calls++
	f.cancel()
	return &Feed{}, nil
}

type findCall struct {
	partitionKey string
	limit        int
}

type fakePostService struct {
	recentIDs map[string][]string
	findErrs  map[string]error
	saveErrs  []error
	findCalls []findCall
	saved     [][]blogger.Post
}

func (f *fakePostService) FindRecentPostIDs(partitionKey string, limit int) ([]string, error) {
	f.findCalls = append(f.findCalls, findCall{partitionKey: partitionKey, limit: limit})
	if err := f.findErrs[partitionKey]; err != nil {
		return nil, err
	}
	return f.recentIDs[partitionKey], nil
}

func (f *fakePostService) SavePosts(posts []blogger.Post) error {
	f.saved = append(f.saved, posts)
	if len(f.saveErrs) == 0 {
		return nil
	}
	err := f.saveErrs[0]
	f.saveErrs = f.saveErrs[1:]
	return err
}

type summaryResult struct {
	text string
	err  error
}

type fakeSummarizer struct {
	results map[string][]summaryResult
	calls   []string
}

func (f *fakeSummarizer) SummarizeText(ctx context.Context, text string) (string, error) {
	f.calls = append(f.calls, text)
	results := f.results[text]
	if len(results) == 0 {
		return "", fmt.Errorf("unexpected summary for %s", text)
	}
	result := results[0]
	f.results[text] = results[1:]
	return result.text, result.err
}

type fakeHasher struct {
	values map[string]string
	calls  []string
}

func (f *fakeHasher) HashString(text string) string {
	f.calls = append(f.calls, text)
	if value, ok := f.values[text]; ok {
		return value
	}
	return "hash:" + text
}

type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time {
	return f.now
}

type fakeSleeper struct {
	durations []time.Duration
}

func (f *fakeSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	f.durations = append(f.durations, duration)
	return nil
}

func TestRunOnceFetchesSummarizesAndSavesNewPosts(t *testing.T) {
	feedURL := "https://example.com/feed"
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	fetcher := &fakeFeedFetcher{
		results: map[string][]fetchResult{
			feedURL: {
				{
					feed: &Feed{
						Items: []FeedItem{
							{ID: "existing", SourceURL: "https://example.com/existing", Title: "Existing", Text: "existing text"},
							{ID: "new-1", SourceURL: "https://example.com/new-1", Title: "New 1", Text: "new text 1"},
							{ID: "new-2", SourceURL: "https://example.com/new-2", Title: "New 2", Text: "new text 2"},
							{ID: "over-limit", SourceURL: "https://example.com/over-limit", Title: "Over", Text: "over limit"},
						},
					},
				},
			},
		},
	}
	postService := &fakePostService{
		recentIDs: map[string][]string{"partition-1": {"hash:partition-1\nexisting"}},
	}
	summarizer := &fakeSummarizer{
		results: map[string][]summaryResult{
			"new text 1": {{text: "summary 1"}},
			"new text 2": {{text: "summary 2"}},
		},
	}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           3,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: fetcher,
		PostService: postService,
		Summarizer:  summarizer,
		Hasher:      &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:       fakeClock{now: now},
		Sleeper:     &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []string{feedURL}, fetcher.calls)
	assert.Equal(t, []findCall{{partitionKey: "partition-1", limit: 0}}, postService.findCalls)
	assert.Equal(t, []string{"new text 1", "new text 2"}, summarizer.calls)
	assert.Len(t, postService.saved, 1)
	assert.Equal(t, []blogger.Post{
		{
			ID:           "hash:partition-1\nnew-1",
			PartitionKey: "partition-1",
			Title:        "New 1",
			Text:         "summary 1",
			SourceURL:    "https://example.com/new-1",
			CreatedAt:    now.UTC(),
		},
		{
			ID:           "hash:partition-1\nnew-2",
			PartitionKey: "partition-1",
			Title:        "New 2",
			Text:         "summary 2",
			SourceURL:    "https://example.com/new-2",
			CreatedAt:    now.UTC(),
		},
	}, postService.saved[0])
}

func TestRunOnceRetriesFeedFetch(t *testing.T) {
	feedURL := "https://example.com/feed"
	sleeper := &fakeSleeper{}
	fetcher := &fakeFeedFetcher{
		results: map[string][]fetchResult{
			feedURL: {
				{err: errors.New("first failure")},
				{err: errors.New("second failure")},
				{feed: &Feed{}},
			},
		},
	}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           3,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: fetcher,
		PostService: &fakePostService{},
		Summarizer:  &fakeSummarizer{},
		Hasher:      &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:       fakeClock{now: time.Now()},
		Sleeper:     sleeper,
	}.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []string{feedURL, feedURL, feedURL}, fetcher.calls)
	assert.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second}, sleeper.durations)
}

func TestRunExecutesImmediatelyBeforeFirstTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fetcher := &cancelingFeedFetcher{cancel: cancel}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:            []string{"https://example.com/feed"},
			RSSFeedLimit:            1,
			WorkerIntervalInSeconds: 3600,
			WorkerTimeoutInSeconds:  30,
		},
		FeedFetcher: fetcher,
		PostService: &fakePostService{},
		Summarizer:  &fakeSummarizer{},
		Hasher:      &fakeHasher{values: map[string]string{"https://example.com/feed": "partition-1"}},
		Clock:       fakeClock{now: time.Now()},
		Sleeper:     &fakeSleeper{},
	}.Run(ctx)

	assert.NoError(t, err)
	assert.Equal(t, 1, fetcher.calls)
}

func TestRunOnceRetriesSummarization(t *testing.T) {
	feedURL := "https://example.com/feed"
	sleeper := &fakeSleeper{}
	postService := &fakePostService{recentIDs: map[string][]string{"partition-1": {}}}
	summarizer := &fakeSummarizer{
		results: map[string][]summaryResult{
			"post text": {
				{err: errors.New("first failure")},
				{err: errors.New("second failure")},
				{text: "summary"},
			},
		},
	}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{
				feedURL: {{feed: &Feed{Items: []FeedItem{{ID: "post-1", SourceURL: "https://example.com/post-1", Text: "post text"}}}}},
			},
		},
		PostService: postService,
		Summarizer:  summarizer,
		Hasher:      &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:       fakeClock{now: time.Now()},
		Sleeper:     sleeper,
	}.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, []string{"post text", "post text", "post text"}, summarizer.calls)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, sleeper.durations)
	assert.Len(t, postService.saved, 1)
	assert.Equal(t, "summary", postService.saved[0][0].Text)
}

func TestRunOnceUsesStableFallbackIDWhenFeedItemIDMissing(t *testing.T) {
	feedURL := "https://example.com/feed"
	postService := &fakePostService{recentIDs: map[string][]string{"partition-1": {}}}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{
				feedURL: {{feed: &Feed{Items: []FeedItem{{SourceURL: "https://example.com/post-1", Text: "post text"}}}}},
			},
		},
		PostService: postService,
		Summarizer: &fakeSummarizer{
			results: map[string][]summaryResult{"post text": {{text: "summary"}}},
		},
		Hasher:  &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:   fakeClock{now: time.Now()},
		Sleeper: &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Len(t, postService.saved, 1)
	assert.Equal(t, "hash:partition-1\nhttps://example.com/post-1", postService.saved[0][0].ID)
}

func TestMapFeedItemUsesDescriptionWhenContentIsEmpty(t *testing.T) {
	item := mapFeedItem(&gofeed.Item{
		GUID:        " post-id ",
		Link:        " https://example.com/post ",
		Title:       " Post title ",
		Content:     " \n",
		Description: " Description text ",
	})

	assert.Equal(t, FeedItem{
		ID:        "post-id",
		SourceURL: "https://example.com/post",
		Title:     "Post title",
		Text:      "Description text",
	}, item)
}

func TestSafeDialContextRejectsBlockedDirectIP(t *testing.T) {
	conn, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:80")

	assert.Nil(t, conn)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blocked IP")
}

func TestRunOnceSkipsEmptyFeeds(t *testing.T) {
	feedURL := "https://example.com/feed"
	postService := &fakePostService{}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           3,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{feedURL: {{feed: &Feed{}}}},
		},
		PostService: postService,
		Summarizer:  &fakeSummarizer{},
		Hasher:      &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:       fakeClock{now: time.Now()},
		Sleeper:     &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.NoError(t, err)
	assert.Empty(t, postService.findCalls)
	assert.Empty(t, postService.saved)
}

func TestRunOnceContinuesAfterFetchLoadAndSaveErrors(t *testing.T) {
	feedWithFetchError := "https://example.com/fetch-error"
	feedWithLoadError := "https://example.com/load-error"
	feedWithSaveError := "https://example.com/save-error"
	feedSuccess := "https://example.com/success"
	postService := &fakePostService{
		recentIDs: map[string][]string{
			"save-partition":    {},
			"success-partition": {},
		},
		findErrs: map[string]error{
			"load-partition": errors.New("load failed"),
		},
		saveErrs: []error{errors.New("save failed"), nil},
	}
	summarizer := &fakeSummarizer{
		results: map[string][]summaryResult{
			"save text":    {{text: "save summary"}},
			"success text": {{text: "success summary"}},
		},
	}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs: []string{
				feedWithFetchError,
				feedWithLoadError,
				feedWithSaveError,
				feedSuccess,
			},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{
				feedWithFetchError: {
					{err: errors.New("fetch failed 1")},
					{err: errors.New("fetch failed 2")},
					{err: errors.New("fetch failed 3")},
				},
				feedWithLoadError: {{feed: &Feed{Items: []FeedItem{{ID: "load-post", Text: "load text"}}}}},
				feedWithSaveError: {{feed: &Feed{Items: []FeedItem{{ID: "save-post", Text: "save text"}}}}},
				feedSuccess:       {{feed: &Feed{Items: []FeedItem{{ID: "success-post", Text: "success text"}}}}},
			},
		},
		PostService: postService,
		Summarizer:  summarizer,
		Hasher: &fakeHasher{values: map[string]string{
			feedWithFetchError: "fetch-partition",
			feedWithLoadError:  "load-partition",
			feedWithSaveError:  "save-partition",
			feedSuccess:        "success-partition",
		}},
		Clock:   fakeClock{now: time.Now()},
		Sleeper: &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save posts")
	assert.Len(t, postService.saved, 2)
	assert.Equal(t, "hash:save-partition\nsave-post", postService.saved[0][0].ID)
	assert.Equal(t, "hash:success-partition\nsuccess-post", postService.saved[1][0].ID)
}

func TestRunOnceSkipsPostsWhenSummarizationFails(t *testing.T) {
	feedURL := "https://example.com/feed"
	postService := &fakePostService{recentIDs: map[string][]string{"partition-1": {}}}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           1,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{
				feedURL: {{feed: &Feed{Items: []FeedItem{{ID: "post-1", SourceURL: "https://example.com/post-1", Text: "post text"}}}}},
			},
		},
		PostService: postService,
		Summarizer: &fakeSummarizer{
			results: map[string][]summaryResult{
				"post text": {
					{err: errors.New("first failure")},
					{err: errors.New("second failure")},
					{err: errors.New("third failure")},
				},
			},
		},
		Hasher:  &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:   fakeClock{now: time.Now()},
		Sleeper: &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to summarize post")
	assert.Empty(t, postService.saved)
}

func TestRunOnceSavesSuccessfulPostsWhenAnotherSummaryFails(t *testing.T) {
	feedURL := "https://example.com/feed"
	postService := &fakePostService{recentIDs: map[string][]string{"partition-1": {}}}

	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:           []string{feedURL},
			RSSFeedLimit:           2,
			WorkerTimeoutInSeconds: 30,
		},
		FeedFetcher: &fakeFeedFetcher{
			results: map[string][]fetchResult{
				feedURL: {{feed: &Feed{Items: []FeedItem{
					{ID: "post-1", SourceURL: "https://example.com/post-1", Text: "failing text"},
					{ID: "post-2", SourceURL: "https://example.com/post-2", Text: "successful text"},
				}}}},
			},
		},
		PostService: postService,
		Summarizer: &fakeSummarizer{
			results: map[string][]summaryResult{
				"failing text": {
					{err: errors.New("first failure")},
					{err: errors.New("second failure")},
					{err: errors.New("third failure")},
				},
				"successful text": {{text: "successful summary"}},
			},
		},
		Hasher:  &fakeHasher{values: map[string]string{feedURL: "partition-1"}},
		Clock:   fakeClock{now: time.Now()},
		Sleeper: &fakeSleeper{},
	}.RunOnce(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to summarize post")
	assert.Len(t, postService.saved, 1)
	assert.Equal(t, "hash:partition-1\npost-2", postService.saved[0][0].ID)
	assert.Equal(t, "successful summary", postService.saved[0][0].Text)
}

func TestDistinctNewPosts(t *testing.T) {
	tbl := []struct {
		fresh    []blogger.Post
		stored   []string
		expected []string
	}{
		{fresh: []blogger.Post{{ID: "1"}, {ID: "2"}}, stored: []string{"3", "4"}, expected: []string{"1", "2"}},
		{fresh: []blogger.Post{{ID: "1"}, {ID: "2"}}, stored: []string{"1", "2"}, expected: []string{}},
		{fresh: []blogger.Post{{ID: "1"}, {ID: "2"}, {ID: "3"}}, stored: []string{"1", "3"}, expected: []string{"2"}},
		{fresh: []blogger.Post{}, stored: []string{"1"}, expected: []string{}},
		{fresh: []blogger.Post{{ID: "1"}}, stored: []string{}, expected: []string{"1"}},
		{fresh: []blogger.Post{{ID: "1"}, {ID: "1"}, {ID: "2"}}, stored: []string{}, expected: []string{"1", "2"}},
	}

	for i, tt := range tbl {
		i := i
		tt := tt
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			result := distinctNewPosts(tt.fresh, tt.stored)
			actual := make([]string, 0, len(result))
			for _, post := range result {
				actual = append(actual, post.ID)
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestParseSettings(t *testing.T) {
	withLookupIP(t, func(host string) ([]net.IP, error) {
		if host == "private.example.com" {
			return []net.IP{net.ParseIP("10.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})

	t.Run("ValidSettings", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, " http://example.com/feed1, https://example.org/feed2 ")
		t.Setenv(config.EnvWorkerTimeoutInSeconds, "120")
		t.Setenv(config.EnvWorkerIntervalInSeconds, "60")
		t.Setenv(config.EnvFeedItemsLimit, "5")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 2, len(settings.RSSFeedsURLs))
		assert.Equal(t, "http://example.com/feed1", settings.RSSFeedsURLs[0])
		assert.Equal(t, "https://example.org/feed2", settings.RSSFeedsURLs[1])
		assert.Equal(t, 120, settings.WorkerTimeoutInSeconds)
		assert.Equal(t, 60, settings.WorkerIntervalInSeconds)
		assert.Equal(t, 5, settings.RSSFeedLimit)
	})

	t.Run("DefaultValues", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://example.com/feed")
		t.Setenv(config.EnvWorkerTimeoutInSeconds, "")
		t.Setenv(config.EnvWorkerIntervalInSeconds, "")
		t.Setenv(config.EnvFeedItemsLimit, "")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 1800, settings.WorkerTimeoutInSeconds)
		assert.Equal(t, 3600, settings.WorkerIntervalInSeconds)
		assert.Equal(t, 3, settings.RSSFeedLimit)
	})

	t.Run("MissingFeeds", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("EmptyFeedEntries", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, " , , ")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("InvalidScheme", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "file:///tmp/feed.xml")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("LocalhostFeed", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://localhost/feed")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("PrivateIPFeed", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://192.168.1.10/feed")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("PrivateDNSResolution", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "https://private.example.com/feed")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("TooManyFeeds", func(t *testing.T) {
		feeds := make([]string, 101)
		for i := range feeds {
			feeds[i] = fmt.Sprintf("https://example.com/feed-%d", i)
		}
		t.Setenv(config.EnvFeeds, strings.Join(feeds, ","))

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("InvalidTimeout", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://example.com/feed")
		t.Setenv(config.EnvWorkerTimeoutInSeconds, "0")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("InvalidInterval", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://example.com/feed")
		t.Setenv(config.EnvWorkerIntervalInSeconds, "-1")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})

	t.Run("InvalidFeedLimit", func(t *testing.T) {
		t.Setenv(config.EnvFeeds, "http://example.com/feed")
		t.Setenv(config.EnvFeedItemsLimit, "0")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})
}

func withLookupIP(t *testing.T, fn func(string) ([]net.IP, error)) {
	t.Helper()

	original := lookupIP
	lookupIP = fn
	t.Cleanup(func() {
		lookupIP = original
	})
}
