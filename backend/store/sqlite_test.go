package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/config"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestNewDatabaseUsesConfiguredPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configured.sqlite")
	t.Setenv(config.EnvDatabasePath, path)

	database, err := NewDatabase()
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	assert.NoError(t, database.Migrate())
}

func TestNewDatabaseCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "created.sqlite")

	database, err := NewDatabaseWithPath(path)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	assert.DirExists(t, filepath.Dir(path))
	assert.NoError(t, database.Migrate())
}

func TestNewDatabaseConfiguresSQLiteForConcurrentAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "configured.sqlite")

	database, err := NewDatabaseWithPath(path)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	sqlDB, err := database.db.DB()
	assert.NoError(t, err)
	assert.Equal(t, 1, sqlDB.Stats().MaxOpenConnections)

	var busyTimeout int
	assert.NoError(t, database.db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error)
	assert.Equal(t, 5000, busyTimeout)

	var journalMode string
	assert.NoError(t, database.db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error)
	assert.Equal(t, "wal", strings.ToLower(journalMode))
}

func TestSQLiteDSNLeavesMemoryDatabaseUnchanged(t *testing.T) {
	assert.Equal(t, ":memory:", sqliteDSN(":memory:"))
	assert.Equal(t, "file::memory:?cache=shared", sqliteDSN("file::memory:?cache=shared"))
}

func TestGetPostsSuccess(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	_, err := database.SavePostsBulk([]*PostV1{
		{
			ID:           "1",
			PartitionKey: "feed-a",
			Title:        "Post 1",
			Text:         "Summary 1",
			SourceURL:    "https://example.com/1",
		},
		{
			ID:           "2",
			PartitionKey: "feed-b",
			Title:        "Post 2",
			Text:         "Summary 2",
			SourceURL:    "https://example.com/2",
		},
	})
	assert.NoError(t, err)

	allPosts, err := database.GetPostsContext(context.Background(), 1, 10, "")
	assert.NoError(t, err)
	assert.Equal(t, int64(2), allPosts.Size)
	assert.Len(t, allPosts.Posts, 2)

	filteredPosts, err := database.GetPostsContext(context.Background(), 1, 10, "feed-a")
	assert.NoError(t, err)
	assert.Equal(t, int64(1), filteredPosts.Size)
	assert.Len(t, filteredPosts.Posts, 1)
	assert.Equal(t, "1", filteredPosts.Posts[0].ID)
}

func TestGetPostsOrdersByNewestThenID(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	_, err := database.SavePostsBulk([]*PostV1{
		{
			ID:           "b",
			PartitionKey: "feed-a",
			Title:        "Post B",
			Text:         "Summary B",
			SourceURL:    "https://example.com/b",
			CreatedAt:    now,
		},
		{
			ID:           "a",
			PartitionKey: "feed-a",
			Title:        "Post A",
			Text:         "Summary A",
			SourceURL:    "https://example.com/a",
			CreatedAt:    now,
		},
		{
			ID:           "old",
			PartitionKey: "feed-a",
			Title:        "Old",
			Text:         "Old summary",
			SourceURL:    "https://example.com/old",
			CreatedAt:    now.Add(-time.Hour),
		},
	})
	assert.NoError(t, err)

	result, err := database.GetPostsContext(context.Background(), 1, 10, "feed-a")

	assert.NoError(t, err)
	if assert.Len(t, result.Posts, 3) {
		assert.Equal(t, []string{"b", "a", "old"}, []string{
			result.Posts[0].ID,
			result.Posts[1].ID,
			result.Posts[2].ID,
		})
	}
}

func TestGetPostsReturnsEmptySliceWhenNoRows(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	result, err := database.GetPostsContext(context.Background(), 1, 10, "")

	assert.NoError(t, err)
	assert.Equal(t, int64(0), result.Size)
	assert.Empty(t, result.Posts)
	assert.NotNil(t, result.Posts)
}

func TestGetPostsReturnsCountError(t *testing.T) {
	database := newTestDatabase(t)
	assert.NoError(t, database.Close())

	result, err := database.GetPostsContext(context.Background(), 1, 10, "")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to count posts")
}

func TestGetPostsReturnsFindError(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	forcedErr := errors.New("forced find error")
	queryCount := 0
	err := database.db.Callback().Query().Before("gorm:query").Register("rss_sum_force_second_query_error", func(db *gorm.DB) {
		queryCount++
		if queryCount == 2 {
			assert.ErrorIs(t, db.AddError(forcedErr), forcedErr)
		}
	})
	assert.NoError(t, err)

	result, err := database.GetPostsContext(context.Background(), 1, 10, "")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to find posts")
}

func TestFindRecentPostIDs(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	_, err := database.SavePostsBulk([]*PostV1{
		{
			ID:           "old-feed-a",
			PartitionKey: "feed-a",
			Title:        "Old",
			Text:         "Old summary",
			SourceURL:    "https://example.com/old",
			CreatedAt:    now.Add(-2 * time.Hour),
		},
		{
			ID:           "new-feed-b",
			PartitionKey: "feed-b",
			Title:        "Other",
			Text:         "Other summary",
			SourceURL:    "https://example.com/other",
			CreatedAt:    now,
		},
		{
			ID:           "new-feed-a",
			PartitionKey: "feed-a",
			Title:        "New",
			Text:         "New summary",
			SourceURL:    "https://example.com/new",
			CreatedAt:    now.Add(-1 * time.Hour),
		},
	})
	assert.NoError(t, err)

	filtered, err := database.FindRecentPostIDs("feed-a", 10)
	assert.NoError(t, err)
	assert.Equal(t, []string{"new-feed-a", "old-feed-a"}, filtered)

	limited, err := database.FindRecentPostIDs("", 2)
	assert.NoError(t, err)
	assert.Equal(t, []string{"new-feed-b", "new-feed-a"}, limited)

	emptyPartition, err := database.FindRecentPostIDs("missing", 10)
	assert.NoError(t, err)
	assert.Empty(t, emptyPartition)
	assert.NotNil(t, emptyPartition)

	zeroLimit, err := database.FindRecentPostIDs("feed-a", 0)
	assert.NoError(t, err)
	assert.Equal(t, []string{"new-feed-a", "old-feed-a"}, zeroLimit)
}

func TestSavePostsBulkReturnsBeginError(t *testing.T) {
	database := newTestDatabase(t)
	assert.NoError(t, database.Close())

	posts, err := database.SavePostsBulk([]*PostV1{{
		ID:           "1",
		PartitionKey: "feed-a",
		Title:        "Post",
		Text:         "Summary",
		SourceURL:    "https://example.com/post",
	}})

	assert.Error(t, err)
	assert.Nil(t, posts)
	assert.Contains(t, err.Error(), "failed to begin posts creation transaction")
}

func TestSavePostsBulkReturnsPanicError(t *testing.T) {
	database := newTestDatabase(t)
	defer func() {
		assert.NoError(t, database.Close())
	}()

	err := database.db.Callback().Create().Before("gorm:create").Register("rss_sum_force_create_panic", func(db *gorm.DB) {
		panic("forced create panic")
	})
	assert.NoError(t, err)

	posts, err := database.SavePostsBulk([]*PostV1{{
		ID:           "1",
		PartitionKey: "feed-a",
		Title:        "Post",
		Text:         "Summary",
		SourceURL:    "https://example.com/post",
	}})

	assert.Error(t, err)
	assert.Nil(t, posts)
	assert.Contains(t, err.Error(), "forced create panic")
}

func newTestDatabase(t *testing.T) *Database {
	t.Helper()

	database, err := NewDatabaseWithPath(filepath.Join(t.TempDir(), "test.sqlite"))
	assert.NoError(t, err)
	assert.NoError(t, database.Migrate())
	return database
}
