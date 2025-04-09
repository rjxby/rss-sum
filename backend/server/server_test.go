package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock blogger for testing
type MockBlogger struct {
	mock.Mock
}

func (m *MockBlogger) GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error) {
	args := m.Called(page, pageSize, partitionKey)
	return args.Get(0).(*store.PaginationPostsResult), args.Error(1)
}

func TestParseQueryParam(t *testing.T) {
	tbl := []struct {
		input       string
		expected    int
		expectError bool
	}{
		{"10", 10, false},
		{"0", 0, false},
		{"-5", -5, false},
		{"", 0, true},
		{"abc", 0, true},
		{"10.5", 0, true},
	}

	for i, tt := range tbl {
		i := i
		tt := tt
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			result, err := parseQueryParam(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGetPostsCtrl(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Setup
		mockBlogger := new(MockBlogger)
		expectedResult := &store.PaginationPostsResult{
			Posts: []*store.PostV1{
				{ID: "1", Text: "Content 1", SourceURL: "http://example.com/1"},
				{ID: "2", Text: "Content 2", SourceURL: "http://example.com/2"},
			},
			Page:         1,
			PageSize:     10,
			PartitionKey: "test-key",
			Size:         2,
		}
		mockBlogger.On("GetPosts", 1, 10, "test-key").Return(expectedResult, nil)

		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		// Create request
		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10&partitionKey=test-key", nil)
		rec := httptest.NewRecorder()

		// Execute
		r.ServeHTTP(rec, req)

		// Verify
		assert.Equal(t, http.StatusOK, rec.Code)

		var response PostsResultsJSON
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, 1, response.Page)
		assert.Equal(t, 10, response.PageSize)
		assert.Equal(t, "test-key", response.PartitionKey)
		assert.Equal(t, 2, len(response.Posts))
		assert.Equal(t, "Content 1", response.Posts[0].Text)

		mockBlogger.AssertExpectations(t)
	})

	t.Run("InvalidPage", func(t *testing.T) {
		// Setup
		mockBlogger := new(MockBlogger)
		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		// Create request
		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=invalid&pageSize=10", nil)
		rec := httptest.NewRecorder()

		// Execute
		r.ServeHTTP(rec, req)

		// Verify
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "invalid page parameter", response["message"])
	})

	t.Run("InvalidPageSize", func(t *testing.T) {
		// Setup
		mockBlogger := new(MockBlogger)
		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		// Create request
		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=invalid", nil)
		rec := httptest.NewRecorder()

		// Execute
		r.ServeHTTP(rec, req)

		// Verify
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "invalid pageSize parameter", response["message"])
	})

	t.Run("DatabaseError", func(t *testing.T) {
		// Setup
		mockBlogger := new(MockBlogger)
		expectedError := errors.New("database error")
		mockBlogger.On("GetPosts", 1, 10, "").Return((*store.PaginationPostsResult)(nil), expectedError)

		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		// Create request
		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10", nil)
		rec := httptest.NewRecorder()

		// Execute
		r.ServeHTTP(rec, req)

		// Verify
		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "failed to load posts", response["message"])

		mockBlogger.AssertExpectations(t)
	})
}

func TestMapToJSON(t *testing.T) {
	// Setup
	input := &store.PaginationPostsResult{
		Posts: []*store.PostV1{
			{ID: "1", Text: "Content 1", SourceURL: "http://example.com/1"},
			{ID: "2", Text: "Content 2", SourceURL: "http://example.com/2"},
		},
		Page:         1,
		PageSize:     10,
		PartitionKey: "test-key",
		Size:         2,
	}

	// Execute
	result := mapToJSON(input)

	// Verify
	assert.Equal(t, 1, result.Page)
	assert.Equal(t, 10, result.PageSize)
	assert.Equal(t, "test-key", result.PartitionKey)
	assert.Equal(t, 2, len(result.Posts))
	assert.Equal(t, "1", result.Posts[0].ID)
	assert.Equal(t, "Content 1", result.Posts[0].Text)
	assert.Equal(t, "http://example.com/1", result.Posts[0].SourceURL)
}

func TestNotFound(t *testing.T) {
	// Setup
	server := Server{
		Version: "test",
	}

	// Create request
	r := server.routes()
	req := httptest.NewRequest("GET", "/non-existent", nil)
	rec := httptest.NewRecorder()

	// Execute
	r.ServeHTTP(rec, req)

	// Verify
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var response map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Contains(t, response, "error")
	assert.Equal(t, "not found", response["error"])
}
