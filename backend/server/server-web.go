package server

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"

	"github.com/rjxby/rss-sum/backend/store"
	"github.com/rjxby/rss-sum/frontend"
)

const (
	baseTmpl       = "base"
	clientTmplName = "index.tmpl.html"
	postsTmplName  = "posts.tmpl.html"
)

type postsView struct {
	Posts    []*store.PostV1
	HasMore  bool
	NextPage int
	PageSize int
}

type templateData struct {
	Version string
	View    any
}

// NewTemplateCache initializes and returns a map of parsed templates
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

// render renders a template
func (s *Server) render(w http.ResponseWriter, status int, page, tmplName string, data templateData) {
	ts, ok := s.templateCache[page]
	if !ok {
		err := fmt.Errorf("the template %s does not exist", page)
		log.Printf("[ERROR] %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	buf := new(bytes.Buffer)

	if tmplName == "" {
		tmplName = baseTmpl
	}

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

// clientCtrl serves the main HTML page
func (s *Server) indexCtrl(w http.ResponseWriter, r *http.Request) {
	data := templateData{
		Version: s.Version,
	}

	s.render(w, http.StatusOK, clientTmplName, clientTmplName, data)
}

// getPostsHtmxCtrl handles HTMX requests for posts with pagination
func (s *Server) getPostsHtmxCtrl(w http.ResponseWriter, r *http.Request) {
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

	partitionKey := r.URL.Query().Get("partitionKey")

	// Reuse the same logic from getPostsCtrl to fetch posts
	posts, err := s.Blogger.GetPosts(page, pageSize, partitionKey)
	if err != nil {
		renderInternalServerError(w, r, "failed to load posts", err)
		return
	}

	// Check if there are more posts for pagination
	hasMore := int64((page)*pageSize) < posts.Size

	// Prepare data for the template
	data := templateData{
		Version: s.Version,
		View: postsView{
			Posts:    posts.Posts,
			HasMore:  hasMore,
			NextPage: page + 1,
			PageSize: pageSize,
		},
	}

	// Render the template
	s.render(w, http.StatusOK, postsTmplName, postsTmplName, data)
}
