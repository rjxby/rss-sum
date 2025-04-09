package worker

import (
	"strconv"
	"testing"

	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock implementations
type MockBlogger struct {
	mock.Mock
}

func (m *MockBlogger) GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error) {
	args := m.Called(page, pageSize, partitionKey)
	return args.Get(0).(*store.PaginationPostsResult), args.Error(1)
}

func (m *MockBlogger) SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error) {
	args := m.Called(postsToSave)
	return args.Get(0).([]*store.PostV1), args.Error(1)
}

type MockAssistant struct {
	mock.Mock
}

func (m *MockAssistant) SummarizeText(text string) (string, error) {
	args := m.Called(text)
	return args.String(0), args.Error(1)
}

type MockHasher struct {
	mock.Mock
}

func (m *MockHasher) HashString(text string) string {
	args := m.Called(text)
	return args.String(0)
}

func TestDistinctNewPosts(t *testing.T) {
	tbl := []struct {
		fresh    []*store.PostV1
		stored   []*store.PostV1
		expected int
	}{
		{
			// Case 0: No duplicates
			fresh: []*store.PostV1{
				{ID: "1"},
				{ID: "2"},
			},
			stored: []*store.PostV1{
				{ID: "3"},
				{ID: "4"},
			},
			expected: 2,
		},
		{
			// Case 1: All duplicates
			fresh: []*store.PostV1{
				{ID: "1"},
				{ID: "2"},
			},
			stored: []*store.PostV1{
				{ID: "1"},
				{ID: "2"},
			},
			expected: 0,
		},
		{
			// Case 2: Mixed duplicates
			fresh: []*store.PostV1{
				{ID: "1"},
				{ID: "2"},
				{ID: "3"},
			},
			stored: []*store.PostV1{
				{ID: "1"},
				{ID: "3"},
			},
			expected: 1,
		},
		{
			// Case 3: Empty fresh posts
			fresh:    []*store.PostV1{},
			stored:   []*store.PostV1{{ID: "1"}},
			expected: 0,
		},
		{
			// Case 4: Empty stored posts
			fresh:    []*store.PostV1{{ID: "1"}},
			stored:   []*store.PostV1{},
			expected: 1,
		},
	}

	for i, tt := range tbl {
		i := i
		tt := tt
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			w := Worker{}
			result := w.distinctNewPosts(tt.fresh, tt.stored)
			assert.Equal(t, tt.expected, len(result))
		})
	}
}

func TestParseSettings(t *testing.T) {
	// Test valid settings
	t.Run("ValidSettings", func(t *testing.T) {
		t.Setenv("FEEDS", "http://example.com/feed1,http://example.com/feed2")
		t.Setenv("WORKER_TIMEOUT_IN_SECONDS", "120")
		t.Setenv("WORKER_INTERVAL_IN_SECONDS", "60")
		t.Setenv("FEED_ITEMS_LIMIT", "5")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 2, len(settings.RSSFeedsURLs))
		assert.Equal(t, 120, settings.WorkerTimeoutInSeconds)
		assert.Equal(t, 60, settings.WorkerIntervalInSeconds)
		assert.Equal(t, 5, settings.RSSFeedLimit)
	})

	// Test default values
	t.Run("DefaultValues", func(t *testing.T) {
		t.Setenv("FEEDS", "http://example.com/feed")
		t.Setenv("WORKER_TIMEOUT_IN_SECONDS", "")
		t.Setenv("WORKER_INTERVAL_IN_SECONDS", "")
		t.Setenv("FEED_ITEMS_LIMIT", "")

		settings, err := ParseSettings()

		assert.NoError(t, err)
		assert.Equal(t, 1800, settings.WorkerTimeoutInSeconds)
		assert.Equal(t, 3600, settings.WorkerIntervalInSeconds)
		assert.Equal(t, 3, settings.RSSFeedLimit)
	})

	// Test missing required settings
	t.Run("MissingFeeds", func(t *testing.T) {
		t.Setenv("FEEDS", "")

		settings, err := ParseSettings()

		assert.Error(t, err)
		assert.Nil(t, settings)
	})
}
