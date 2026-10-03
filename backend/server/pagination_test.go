package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/require"
)

func TestPostsPaginationHTTPBounds(t *testing.T) {
	database, err := store.NewDatabaseWithPath(filepath.Join(t.TempDir(), "pagination.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.NoError(t, database.Migrate())
	service := blogger.New(database)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	require.NoError(t, service.SavePosts([]blogger.Post{
		{ID: "a", PartitionKey: "feed", Title: "First", Text: "First summary", SourceURL: "https://example.com/a", CreatedAt: now},
		{ID: "b", PartitionKey: "feed", Title: "Second", Text: "Second summary", SourceURL: "https://example.com/b", CreatedAt: now},
		{ID: "c", PartitionKey: "feed", Title: "Third", Text: "Third summary", SourceURL: "https://example.com/c", CreatedAt: now},
	}))
	cache, err := NewTemplateCache()
	require.NoError(t, err)

	cases := []struct {
		name     string
		page     string
		pageSize int
		status   int
		ids      []string
		nextPage int
	}{
		{name: "first page", page: "1", pageSize: 2, status: http.StatusOK, ids: []string{"c", "b"}, nextPage: 2},
		{name: "last ordinary page", page: "2", pageSize: 2, status: http.StatusOK, ids: []string{"a"}},
		{name: "maximum int page", page: strconv.Itoa(math.MaxInt), pageSize: 1, status: http.StatusOK},
		{name: "page exceeds int", page: strconv.FormatUint(uint64(math.MaxInt)+1, 10), pageSize: 1, status: http.StatusBadRequest},
		{name: "last safe offset for size two", page: strconv.Itoa(math.MaxInt/2 + 1), pageSize: 2, status: http.StatusOK},
		{name: "overflow offset for size two", page: strconv.Itoa(math.MaxInt/2 + 2), pageSize: 2, status: http.StatusBadRequest},
		{name: "last safe offset for size one hundred", page: strconv.Itoa(math.MaxInt/100 + 1), pageSize: 100, status: http.StatusOK},
		{name: "overflow offset for size one hundred", page: strconv.Itoa(math.MaxInt/100 + 2), pageSize: 100, status: http.StatusBadRequest},
	}
	for _, htmx := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/htmx=%t", tc.name, htmx), func(t *testing.T) {
				server := Server{Blogger: service, templateCache: cache}
				request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/posts?page=%s&pageSize=%d&partitionKey=feed", tc.page, tc.pageSize), nil)
				if htmx {
					request.Header.Set("HX-Request", "true")
				}
				response := httptest.NewRecorder()
				server.routes().ServeHTTP(response, request)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				if tc.status == http.StatusBadRequest {
					var body map[string]string
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
					require.Equal(t, "invalid posts query", body["message"])
					return
				}
				if htmx {
					require.Equal(t, len(tc.ids), strings.Count(response.Body.String(), "<article"))
					if tc.nextPage == 0 {
						require.NotContains(t, response.Body.String(), "hx-get=")
					} else {
						require.Contains(t, response.Body.String(), fmt.Sprintf(`hx-get="/api/v1/posts?page=%d&amp;pageSize=%d&amp;partitionKey=feed"`, tc.nextPage, tc.pageSize))
					}
					return
				}
				var body PostsResultsJSON
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				page, err := strconv.Atoi(tc.page)
				require.NoError(t, err)
				require.Equal(t, page, body.Page)
				require.Equal(t, tc.pageSize, body.PageSize)
				require.Len(t, body.Posts, len(tc.ids))
				for i, id := range tc.ids {
					require.Equal(t, id, body.Posts[i].ID)
				}
			})
		}
	}
}

func TestPostsPaginationRejectsOverflowBeforeListing(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(fmt.Sprintf("htmx=%t", htmx), func(t *testing.T) {
			server := Server{Blogger: new(MockBlogger)}
			request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/posts?page=%d&pageSize=2", math.MaxInt/2+2), nil)
			if htmx {
				request.Header.Set("HX-Request", "true")
			}
			response := httptest.NewRecorder()
			server.routes().ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

func TestPostsPaginationNextPageBounds(t *testing.T) {
	for _, pageSize := range []int{1, 2, 10, 100} {
		t.Run(strconv.Itoa(pageSize), func(t *testing.T) {
			lastPage := math.MaxInt
			if pageSize > 1 {
				lastPage = math.MaxInt/pageSize + 1
			}
			require.Empty(t, postsNextPageURL(postsQuery{Page: lastPage, PageSize: pageSize}))
			query := postsQuery{Page: lastPage - 1, PageSize: pageSize, PartitionKey: "feed"}
			require.Equal(t, fmt.Sprintf("/api/v1/posts?page=%d&pageSize=%d&partitionKey=feed", lastPage, pageSize), postsNextPageURL(query))
		})
	}
}

func TestParsePostsQueryPaginationDefaultSize(t *testing.T) {
	lastPage := math.MaxInt/defaultPostsPageSize + 1
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/posts?page=%d", lastPage), nil)
	query, err := parsePostsQuery(request)
	require.NoError(t, err)
	require.Equal(t, lastPage, query.Page)
	require.Equal(t, defaultPostsPageSize, query.PageSize)
	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/posts?page=%d", lastPage+1), nil)
	_, err = parsePostsQuery(request)
	require.Error(t, err)
}

func TestPostsPaginationHtmxCountBounds(t *testing.T) {
	cache, err := NewTemplateCache()
	require.NoError(t, err)
	cases := []struct {
		name     string
		page     int
		pageSize int
		size     int64
		hasMore  bool
	}{
		{name: "before final page", page: math.MaxInt / 2, pageSize: 2, size: math.MaxInt64, hasMore: true},
		{name: "final partial page", page: math.MaxInt/2 + 1, pageSize: 2, size: math.MaxInt64},
		{name: "final int page", page: math.MaxInt, pageSize: 1, size: math.MaxInt64},
		{name: "empty collection", page: 1, pageSize: 2, size: 0},
		{name: "exact last page", page: 2, pageSize: 2, size: 4},
		{name: "one more post", page: 2, pageSize: 2, size: 5, hasMore: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockBlogger := new(MockBlogger)
			mockBlogger.On("ListPosts", tc.page, tc.pageSize, "").Return(&blogger.PostsPage{Size: tc.size}, nil)
			server := Server{Blogger: mockBlogger, templateCache: cache}
			request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/posts?page=%d&pageSize=%d", tc.page, tc.pageSize), nil)
			response := httptest.NewRecorder()
			server.getPostsHtmxCtrl(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, tc.hasMore, strings.Contains(response.Body.String(), "hx-get="))
			mockBlogger.AssertExpectations(t)
		})
	}
}
