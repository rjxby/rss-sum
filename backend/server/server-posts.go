package server

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/rjxby/rss-sum/backend/blogger"
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

func (s Server) getPostsCtrl(w http.ResponseWriter, r *http.Request) {
	query, err := parsePostsQuery(r)
	if err != nil {
		renderBadRequest(w, r, "invalid posts query", err)
		return
	}

	posts, err := s.Blogger.ListPostsContext(r.Context(), query.Page, query.PageSize, query.PartitionKey)
	if err != nil {
		renderInternalServerError(w, r, "failed to load posts", err)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		s.renderPostsHTML(w, query, posts)
		return
	}

	postsResults := mapToJSON(posts)

	render.Status(r, http.StatusOK)
	render.JSON(w, r, postsResults)
}

func mapToJSON(posts *blogger.PostsPage) *PostsResultsJSON {
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
