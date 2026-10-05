package server

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostsDigestResponseModes(t *testing.T) {
	post := blogger.Post{
		ID:        "post-1",
		Title:     `<script>alert("title")</script>`,
		Text:      strings.Repeat("A résumé of the article. ", 20) + "\n\n" + `<script>alert("summary")</script>`,
		SourceURL: "https://www.example.com/article?one=1&two=2",
	}
	cache, err := NewTemplateCache()
	require.NoError(t, err)
	for _, htmx := range []bool{false, true} {
		t.Run(strconv.FormatBool(htmx), func(t *testing.T) {
			service := new(MockBlogger)
			service.On("ListPostsContext", 1, 10, "").Return(&blogger.PostsPage{
				Posts: []blogger.Post{post}, Page: 1, PageSize: 10, Size: 1,
			}, nil)
			srv := Server{Blogger: service, templateCache: cache}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
			if htmx {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			if htmx {
				assert.Equal(t, "text/html; charset=utf-8", rec.Result().Header.Get("Content-Type"))
				body := rec.Body.String()
				assert.Contains(t, body, `<p class="story-source">example.com</p>`)
				assert.Contains(t, body, `<p class="story-text">`+html.EscapeString(post.Text)+`</p>`)
				assert.NotContains(t, body, "<details")
				assert.Contains(t, body, "&lt;script&gt;")
				assert.NotContains(t, body, "<script>")
				assert.NotContains(t, body, "pagination-sentinel")
			} else {
				var response PostsResultsJSON
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.Len(t, response.Posts, 1)
				assert.Equal(t, post.Text, response.Posts[0].Text)
				assert.Equal(t, post.SourceURL, response.Posts[0].SourceURL)
			}
			service.AssertExpectations(t)
		})
	}
}

func TestEmptyDigestPages(t *testing.T) {
	cache, err := NewTemplateCache()
	require.NoError(t, err)
	for _, page := range []int{1, 2} {
		t.Run(strconv.Itoa(page), func(t *testing.T) {
			service := new(MockBlogger)
			service.On("ListPostsContext", page, 10, "").Return(&blogger.PostsPage{
				Page: page, PageSize: 10,
			}, nil)
			srv := Server{Blogger: service, templateCache: cache}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?page="+strconv.Itoa(page), nil)
			req.Header.Set("HX-Request", "true")
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			if page == 1 {
				assert.Contains(t, rec.Body.String(), "No articles yet")
			} else {
				assert.NotContains(t, rec.Body.String(), "No articles yet")
			}
			assert.NotContains(t, rec.Body.String(), "pagination-sentinel")
			service.AssertExpectations(t)
		})
	}
}

func TestPostsHTMLBlocksUnsafeSourceURLs(t *testing.T) {
	cache, err := NewTemplateCache()
	require.NoError(t, err)
	for _, sourceURL := range []string{"javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "file:///etc/passwd"} {
		t.Run(sourceURL, func(t *testing.T) {
			service := new(MockBlogger)
			service.On("ListPostsContext", 1, 10, "").Return(&blogger.PostsPage{
				Posts: []blogger.Post{{Title: "Article", Text: "Summary", SourceURL: sourceURL}},
				Page:  1, PageSize: 10, Size: 1,
			}, nil).Once()
			srv := Server{Blogger: service, templateCache: cache}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
			req.Header.Set("HX-Request", "true")
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), `href="#ZgotmplZ"`)
			assert.NotContains(t, rec.Body.String(), sourceURL)
			service.AssertExpectations(t)
		})
	}
}

func TestPostsErrorsInBothResponseModes(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			target  string
			status  int
			message string
			errText string
		}{
			{name: "invalid query", target: "/api/v1/posts?page=invalid", status: http.StatusBadRequest, message: "invalid posts query", errText: "invalid page parameter"},
			{name: "load failure", target: "/api/v1/posts", status: http.StatusInternalServerError, message: "failed to load posts", errText: "internal server error"},
		} {
			t.Run(tc.name+"/htmx="+strconv.FormatBool(htmx), func(t *testing.T) {
				service := new(MockBlogger)
				if tc.status == http.StatusInternalServerError {
					service.On("ListPostsContext", 1, 10, "").Return((*blogger.PostsPage)(nil), errors.New("private database failure")).Once()
				}
				srv := Server{Blogger: service}
				req := httptest.NewRequest(http.MethodGet, tc.target, nil)
				if htmx {
					req.Header.Set("HX-Request", "true")
				}
				rec := httptest.NewRecorder()
				srv.routes().ServeHTTP(rec, req)
				require.Equal(t, tc.status, rec.Code)
				var body map[string]string
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tc.message, body["message"])
				assert.Equal(t, tc.errText, body["error"])
				assert.NotContains(t, rec.Body.String(), "private database failure")
				service.AssertExpectations(t)
			})
		}
	}
}
