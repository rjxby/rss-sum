package server

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/frontend"
)

const (
	clientTmplName = "index.tmpl.html"
	postsTmplName  = "posts.tmpl.html"
)

type postsView struct {
	Posts       []postView
	FirstPage   bool
	HasMore     bool
	NextPage    int
	PageSize    int
	NextPageURL string
}

type postView struct {
	blogger.Post
	SourceHost string
}

func mapPostViews(posts []blogger.Post) []postView {
	views := make([]postView, 0, len(posts))
	for _, post := range posts {
		view := postView{Post: post}
		if source, err := url.Parse(post.SourceURL); err == nil {
			view.SourceHost = strings.TrimPrefix(source.Hostname(), "www.")
		}
		views = append(views, view)
	}
	return views
}

type templateData struct {
	Version string
	View    any
}

func NewTemplateCache() (map[string]*template.Template, error) {
	cache := map[string]*template.Template{}

	pages, err := frontend.Templates.ReadDir("html")
	if err != nil {
		return nil, err
	}

	for _, page := range pages {
		if page.IsDir() {
			continue
		}

		name := page.Name()
		if filepath.Ext(name) != ".html" {
			continue
		}

		path := filepath.Join("html", name)
		ts, err := template.ParseFS(frontend.Templates, path)
		if err != nil {
			return nil, err
		}

		cache[name] = ts
	}

	return cache, nil
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data templateData) {
	ts, ok := s.templateCache[page]
	if !ok {
		err := fmt.Errorf("the template %s does not exist", page)
		log.Printf("[ERROR] %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	buf := new(bytes.Buffer)

	err := ts.Execute(buf, data)
	if err != nil {
		log.Printf("[ERROR] %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	if err != nil {
		log.Printf("[ERROR] %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (s *Server) indexCtrl(w http.ResponseWriter, r *http.Request) {
	data := templateData{
		Version: s.Version,
	}

	s.render(w, http.StatusOK, clientTmplName, data)
}

func (s *Server) getPostsHtmxCtrl(w http.ResponseWriter, r *http.Request) {
	postsQuery, err := parsePostsQuery(r)
	if err != nil {
		renderBadRequest(w, r, "invalid posts query", err)
		return
	}

	posts, err := s.Blogger.ListPosts(postsQuery.Page, postsQuery.PageSize, postsQuery.PartitionKey)
	if err != nil {
		renderInternalServerError(w, r, "failed to load posts", err)
		return
	}

	nextPage, canAdvance := postsQuery.nextPage()
	hasMore := canAdvance && posts.Size > 0 && int64(postsQuery.Page) <= (posts.Size-1)/int64(postsQuery.PageSize)

	data := templateData{
		Version: s.Version,
		View: postsView{
			Posts:       mapPostViews(posts.Posts),
			FirstPage:   postsQuery.Page == 1,
			HasMore:     hasMore,
			NextPage:    nextPage,
			PageSize:    postsQuery.PageSize,
			NextPageURL: postsNextPageURL(postsQuery),
		},
	}

	s.render(w, http.StatusOK, postsTmplName, data)
}

func postsNextPageURL(query postsQuery) string {
	nextPage, ok := query.nextPage()
	if !ok {
		return ""
	}
	values := url.Values{}
	values.Set("page", strconv.Itoa(nextPage))
	values.Set("pageSize", strconv.Itoa(query.PageSize))
	if query.PartitionKey != "" {
		values.Set("partitionKey", query.PartitionKey)
	}
	return "/api/v1/posts?" + values.Encode()
}

func (query postsQuery) nextPage() (int, bool) {
	if query.Page < 1 || query.PageSize < 1 || query.Page == math.MaxInt || query.Page > math.MaxInt/query.PageSize {
		return 0, false
	}
	return query.Page + 1, true
}
