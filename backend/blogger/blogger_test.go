package blogger

import (
	"errors"
	"testing"

	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock engine for testing
type MockEngine struct {
	mock.Mock
}

func (m *MockEngine) GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error) {
	args := m.Called(page, pageSize, partitionKey)
	return args.Get(0).(*store.PaginationPostsResult), args.Error(1)
}

func (m *MockEngine) SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error) {
	args := m.Called(postsToSave)
	return args.Get(0).([]*store.PostV1), args.Error(1)
}

func TestGetPosts(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Setup
		mockEngine := new(MockEngine)
		expectedResult := &store.PaginationPostsResult{
			Posts: []*store.PostV1{
				{ID: "1", Title: "Post 1"},
				{ID: "2", Title: "Post 2"},
			},
			Page:     1,
			PageSize: 10,
		}
		mockEngine.On("GetPosts", 1, 10, "test-key").Return(expectedResult, nil)
		blogger := New(mockEngine)

		// Execute
		result, err := blogger.GetPosts(1, 10, "test-key")

		// Verify
		assert.NoError(t, err)
		assert.Equal(t, expectedResult, result)
		mockEngine.AssertExpectations(t)
	})

	t.Run("Error", func(t *testing.T) {
		// Setup
		mockEngine := new(MockEngine)
		expectedError := errors.New("database error")
		mockEngine.On("GetPosts", 1, 10, "test-key").Return((*store.PaginationPostsResult)(nil), expectedError)
		blogger := New(mockEngine)

		// Execute
		result, err := blogger.GetPosts(1, 10, "test-key")

		// Verify
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to get posts")
		mockEngine.AssertExpectations(t)
	})
}

func TestSavePostsBulk(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Setup
		mockEngine := new(MockEngine)
		posts := []*store.PostV1{
			{ID: "1", Title: "Post 1"},
			{ID: "2", Title: "Post 2"},
		}
		mockEngine.On("SavePostsBulk", posts).Return(posts, nil)
		blogger := New(mockEngine)

		// Execute
		result, err := blogger.SavePostsBulk(posts)

		// Verify
		assert.NoError(t, err)
		assert.Equal(t, posts, result)
		mockEngine.AssertExpectations(t)
	})

	t.Run("Error", func(t *testing.T) {
		// Setup
		mockEngine := new(MockEngine)
		posts := []*store.PostV1{
			{ID: "1", Title: "Post 1"},
			{ID: "2", Title: "Post 2"},
		}
		expectedError := errors.New("database error")
		mockEngine.On("SavePostsBulk", posts).Return([]*store.PostV1(nil), expectedError)
		blogger := New(mockEngine)

		// Execute
		result, err := blogger.SavePostsBulk(posts)

		// Verify
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to save posts bulk")
		mockEngine.AssertExpectations(t)
	})
}
