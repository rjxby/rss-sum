package server

import (
	"encoding/json"
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
			service.On("ListPosts", 1, 10, "").Return(&blogger.PostsPage{
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
			service.On("ListPosts", page, 10, "").Return(&blogger.PostsPage{
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
