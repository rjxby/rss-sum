package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/store"
	"github.com/rjxby/rss-sum/frontend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockBlogger struct {
	mock.Mock
}

func (m *MockBlogger) ListPosts(page int, pageSize int, partitionKey string) (*blogger.PostsPage, error) {
	args := m.Called(page, pageSize, partitionKey)
	return args.Get(0).(*blogger.PostsPage), args.Error(1)
}

func TestParsePostsQuery(t *testing.T) {
	tbl := []struct {
		name        string
		target      string
		expected    postsQuery
		expectError bool
	}{
		{
			name:   "Valid",
			target: "/api/v1/posts?page=2&pageSize=20&partitionKey=%20feed-key%20",
			expected: postsQuery{
				Page:         2,
				PageSize:     20,
				PartitionKey: "feed-key",
			},
		},
		{
			name:   "Defaults",
			target: "/api/v1/posts",
			expected: postsQuery{
				Page:     1,
				PageSize: 10,
			},
		},
		{name: "InvalidPage", target: "/api/v1/posts?page=invalid&pageSize=10", expectError: true},
		{name: "NegativePage", target: "/api/v1/posts?page=-1&pageSize=10", expectError: true},
		{name: "ZeroPage", target: "/api/v1/posts?page=0&pageSize=10", expectError: true},
		{name: "InvalidPageSize", target: "/api/v1/posts?page=1&pageSize=invalid", expectError: true},
		{name: "NegativePageSize", target: "/api/v1/posts?page=1&pageSize=-1", expectError: true},
		{name: "ZeroPageSize", target: "/api/v1/posts?page=1&pageSize=0", expectError: true},
		{name: "OversizedPageSize", target: "/api/v1/posts?page=1&pageSize=101", expectError: true},
	}

	for i, tt := range tbl {
		i := i
		tt := tt
		t.Run(tt.name+"_"+strconv.Itoa(i), func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.target, nil)
			result, err := parsePostsQuery(req)

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
		mockBlogger := new(MockBlogger)
		expectedResult := &blogger.PostsPage{
			Posts: []blogger.Post{
				{ID: "1", Text: "Content 1", SourceURL: "http://example.com/1"},
				{ID: "2", Text: "Content 2", SourceURL: "http://example.com/2"},
			},
			Page:         1,
			PageSize:     10,
			PartitionKey: "test-key",
			Size:         2,
		}
		mockBlogger.On("ListPosts", 1, 10, "test-key").Return(expectedResult, nil)

		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10&partitionKey=test-key", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

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
		mockBlogger := new(MockBlogger)
		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=invalid&pageSize=10", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "invalid posts query", response["message"])
	})

	t.Run("InvalidPageSize", func(t *testing.T) {
		mockBlogger := new(MockBlogger)
		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=invalid", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "invalid posts query", response["message"])
	})

	t.Run("DatabaseError", func(t *testing.T) {
		mockBlogger := new(MockBlogger)
		expectedError := errors.New("database error")
		mockBlogger.On("ListPosts", 1, 10, "").Return((*blogger.PostsPage)(nil), expectedError)

		server := Server{
			Blogger: mockBlogger,
			Version: "test",
		}

		r := chi.NewRouter()
		r.Get("/api/v1/posts", server.getPostsCtrl)
		req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10", nil)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)

		var response map[string]string
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Contains(t, response, "message")
		assert.Equal(t, "failed to load posts", response["message"])
		assert.Equal(t, "internal server error", response["error"])
		assert.NotContains(t, rec.Body.String(), "database error")

		mockBlogger.AssertExpectations(t)
	})
}

func TestGetPostsHtmxCtrlPreservesPartitionKeyInNextPageURL(t *testing.T) {
	mockBlogger := new(MockBlogger)
	expectedResult := &blogger.PostsPage{
		Posts: []blogger.Post{
			{ID: "1", Title: "Post 1", Text: "Content 1", SourceURL: "https://example.com/1"},
		},
		Page:         1,
		PageSize:     10,
		PartitionKey: "feed-key",
		Size:         11,
	}
	mockBlogger.On("ListPosts", 1, 10, "feed-key").Return(expectedResult, nil)

	templateCache, err := NewTemplateCache()
	assert.NoError(t, err)

	server := Server{
		Blogger:       mockBlogger,
		Version:       "test",
		templateCache: templateCache,
	}
	req := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10&partitionKey=feed-key", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	server.getPostsHtmxCtrl(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-get="/api/v1/posts?page=2&amp;pageSize=10&amp;partitionKey=feed-key"`)
	mockBlogger.AssertExpectations(t)
}

func TestRoutesSecurityHeaders(t *testing.T) {
	templateCache, err := NewTemplateCache()
	assert.NoError(t, err)

	server := Server{
		Blogger:       new(MockBlogger),
		Version:       "test",
		templateCache: templateCache,
	}

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'none'", rec.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
}

func TestRoutesStaticAssets(t *testing.T) {
	server := Server{Version: "test"}

	req := httptest.NewRequest("GET", "/static/app.css", nil)
	rec := httptest.NewRecorder()

	server.routes().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/css")
}

func TestLoggerSanitizesRequestURIAndBody(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := log.New(&logBuffer, "", 0)
	handler := Logger(logger, LogBody)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/posts?q=one%0Atwo", strings.NewReader("first\nsecond\tthird"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	output := logBuffer.String()
	assert.Contains(t, output, "/api/v1/posts?q=one%0Atwo")
	assert.Contains(t, output, "first second third")
	assert.NotContains(t, output, "one\ntwo")
	assert.NotContains(t, output, "first\nsecond")
	assert.Equal(t, 1, strings.Count(output, "\n"))
}

func TestSanitizeLogValueRemovesControlCharacters(t *testing.T) {
	assert.Equal(t, "first second third", sanitizeLogValue("first\nsecond\tthird"))
}

func TestRoutesSmokeWithTemporaryDatabase(t *testing.T) {
	database, err := store.NewDatabaseWithPath(t.TempDir() + "/smoke.sqlite")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, database.Close())
	}()
	assert.NoError(t, database.Migrate())

	templateCache, err := NewTemplateCache()
	assert.NoError(t, err)

	server := Server{
		Blogger:       blogger.New(database),
		Version:       "test",
		templateCache: templateCache,
	}
	routes := server.routes()

	indexReq := httptest.NewRequest("GET", "/", nil)
	indexRec := httptest.NewRecorder()
	routes.ServeHTTP(indexRec, indexReq)
	assert.Equal(t, http.StatusOK, indexRec.Code)

	apiReq := httptest.NewRequest("GET", "/api/v1/posts?page=1&pageSize=10", nil)
	apiRec := httptest.NewRecorder()
	routes.ServeHTTP(apiRec, apiReq)
	assert.Equal(t, http.StatusOK, apiRec.Code)

	var response PostsResultsJSON
	assert.NoError(t, json.Unmarshal(apiRec.Body.Bytes(), &response))
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 10, response.PageSize)
	assert.Empty(t, response.Posts)
}

func TestRunReturnsServerStartupError(t *testing.T) {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		t.Skipf("port 8080 is not available for startup error test: %v", err)
	}
	defer func() {
		assert.NoError(t, listener.Close())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = Server{Blogger: new(MockBlogger), Version: "test"}.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "http server terminated")
}

func TestServerAddr(t *testing.T) {
	assert.Equal(t, ":8080", Server{}.addr())
	assert.Equal(t, ":8081", Server{Addr: ":8081"}.addr())
}

func TestRunUsesConfiguredAddr(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, listener.Close())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = Server{
		Blogger: new(MockBlogger),
		Version: "test",
		Addr:    listener.Addr().String(),
	}.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "http server terminated")
}

func TestTemplatesDoNotReferenceExternalCDNs(t *testing.T) {
	content, err := frontend.Templates.ReadFile("html/index.tmpl.html")
	assert.NoError(t, err)

	assert.NotContains(t, string(content), "https://cdn.jsdelivr.net")
	assert.NotContains(t, string(content), "https://unpkg.com")
	assert.NotContains(t, string(content), "pico.min.css")
	assert.Contains(t, string(content), "/static/app.css")
	assert.Contains(t, string(content), "/static/app.js")
	assert.Contains(t, string(content), "/static/htmx.min.js")

	posts, err := frontend.Templates.ReadFile("html/posts.tmpl.html")
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(posts), `rel="noopener noreferrer"`))
	assert.NotContains(t, string(posts), `id="pagination-sentinel"`)
	assert.Contains(t, string(posts), `hx-swap="outerHTML"`)
	assert.Contains(t, string(posts), `hx-target="this"`)
}

func TestMapToJSON(t *testing.T) {
	input := &blogger.PostsPage{
		Posts: []blogger.Post{
			{ID: "1", Text: "Content 1", SourceURL: "http://example.com/1"},
			{ID: "2", Text: "Content 2", SourceURL: "http://example.com/2"},
		},
		Page:         1,
		PageSize:     10,
		PartitionKey: "test-key",
		Size:         2,
	}

	result := mapToJSON(input)

	assert.Equal(t, 1, result.Page)
	assert.Equal(t, 10, result.PageSize)
	assert.Equal(t, "test-key", result.PartitionKey)
	assert.Equal(t, 2, len(result.Posts))
	assert.Equal(t, "1", result.Posts[0].ID)
	assert.Equal(t, "Content 1", result.Posts[0].Text)
	assert.Equal(t, "http://example.com/1", result.Posts[0].SourceURL)
}

func TestNotFound(t *testing.T) {
	server := Server{
		Version: "test",
	}

	r := server.routes()
	req := httptest.NewRequest("GET", "/non-existent", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var response map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Contains(t, response, "error")
	assert.Equal(t, "not found", response["error"])
}
