package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/render"
	"github.com/rjxby/rss-sum/backend/store"
)

type PostsResultsJSON struct {
	Page         int        `json:"page"`
	PageSize     int        `json:"pageSize"`
	PartitionKey string     `json:"partitionKey,omitempty"`
	Posts        []PostJSON `json:"posts"`
}

type PostJSON struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Text      string `json:"text,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

// GET /v1/posts
func (s Server) getPostsCtrl(w http.ResponseWriter, r *http.Request) {

	// Parse the page pageSize, and partitionKey from the query parameters
	page, err := parseQueryParam(r.URL.Query().Get("page"))
	if err != nil {
		renderBadRequest(w, r, "invalid page parameter", err)
		return
	}

	pageSize, err := parseQueryParam(r.URL.Query().Get("pageSize"))
	if err != nil {
		renderBadRequest(w, r, "invalid pageSize parameter", err)
		return
	}

	partitionKey := strings.TrimSpace(r.URL.Query().Get("partitionKey"))

	posts, err := s.Blogger.GetPosts(page, pageSize, partitionKey)
	if err != nil {
		renderInternalServerError(w, r, "failed to load posts", err)
		return
	}

	postsResults := mapToJSON(posts)

	render.Status(r, http.StatusOK)
	render.JSON(w, r, postsResults)
}

func mapToJSON(posts *store.PaginationPostsResult) *PostsResultsJSON {
	var mappedPosts []PostJSON
	for _, post := range posts.Posts {
		mappedPosts = append(mappedPosts, PostJSON{
			ID:        post.ID,
			Title:     post.Title,
			Text:      post.Text,
			SourceURL: post.SourceURL,
		})
	}

	return &PostsResultsJSON{
		Page:         posts.Page,
		PageSize:     posts.PageSize,
		PartitionKey: posts.PartitionKey,
		Posts:        mappedPosts,
	}
}
