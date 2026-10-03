package blogger

import (
	"errors"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
)

type fakeRepository struct {
	getPostsResult      *store.PaginationPostsResult
	getPostsErr         error
	savePostsErr        error
	findRecentPostIDs   []string
	findRecentPostIDErr error
	savedPosts          []*store.PostV1
}

func (f *fakeRepository) GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error) {
	return f.getPostsResult, f.getPostsErr
}

func (f *fakeRepository) SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error) {
	f.savedPosts = postsToSave
	return postsToSave, f.savePostsErr
}

func (f *fakeRepository) FindRecentPostIDs(partitionKey string, limit int) ([]string, error) {
	return f.findRecentPostIDs, f.findRecentPostIDErr
}

func TestListPostsMapsStorePostsToDomainPosts(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		getPostsResult: &store.PaginationPostsResult{
			Posts: []*store.PostV1{
				{
					ID:           "post-1",
					PartitionKey: "feed-1",
					Title:        "Post 1",
					Text:         "Summary 1",
					SourceURL:    "https://example.com/1",
					CreatedAt:    createdAt,
				},
			},
			PartitionKey: "feed-1",
			Page:         2,
			PageSize:     10,
			Size:         21,
		},
	}

	result, err := New(repository).ListPosts(2, 10, "feed-1")

	assert.NoError(t, err)
	assert.Equal(t, &PostsPage{
		Posts: []Post{
			{
				ID:           "post-1",
				PartitionKey: "feed-1",
				Title:        "Post 1",
				Text:         "Summary 1",
				SourceURL:    "https://example.com/1",
				CreatedAt:    createdAt,
			},
		},
		PartitionKey: "feed-1",
		Page:         2,
		PageSize:     10,
		Size:         21,
	}, result)
}

func TestListPostsWrapsRepositoryError(t *testing.T) {
	result, err := New(&fakeRepository{getPostsErr: errors.New("database error")}).
		ListPosts(1, 10, "")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to list posts")
}

func TestSavePostsMapsDomainPostsToStorePosts(t *testing.T) {
	createdAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{}

	err := New(repository).SavePosts([]Post{
		{
			ID:           "post-1",
			PartitionKey: "feed-1",
			Title:        "Post 1",
			Text:         "Summary 1",
			SourceURL:    "https://example.com/1",
			CreatedAt:    createdAt,
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, []*store.PostV1{
		{
			ID:           "post-1",
			PartitionKey: "feed-1",
			Title:        "Post 1",
			Text:         "Summary 1",
			SourceURL:    "https://example.com/1",
			CreatedAt:    createdAt,
		},
	}, repository.savedPosts)
}

func TestSavePostsWrapsRepositoryError(t *testing.T) {
	err := New(&fakeRepository{savePostsErr: errors.New("database error")}).
		SavePosts([]Post{{ID: "post-1"}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save posts")
}

func TestFindRecentPostIDsWrapsRepository(t *testing.T) {
	result, err := New(&fakeRepository{findRecentPostIDs: []string{"post-2", "post-1"}}).
		FindRecentPostIDs("feed-1", 2)

	assert.NoError(t, err)
	assert.Equal(t, []string{"post-2", "post-1"}, result)
}

func TestFindRecentPostIDsWrapsRepositoryError(t *testing.T) {
	result, err := New(&fakeRepository{findRecentPostIDErr: errors.New("database error")}).
		FindRecentPostIDs("feed-1", 2)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to find recent post ids")
}
