package server

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/frontend"
)

const (
	defaultPostsPage     = 1
	defaultPostsPageSize = 10
	maxPostsPageSize     = 100
	defaultHTTPAddr      = ":8080"
)

type Server struct {
	Blogger       Blogger
	Version       string
	Addr          string
	templateCache map[string]*template.Template
}

type Blogger interface {
	ListPosts(page int, pageSize int, partitionKey string) (result *blogger.PostsPage, err error)
}

func (s Server) Run(ctx context.Context) error {
	log.Printf("[INFO] activate rest server")

	templateCache, err := NewTemplateCache()
	if err != nil {
		return fmt.Errorf("[ERROR] failed to load templates: %v", err)
	}
	s.templateCache = templateCache

	httpServer := &http.Server{
		Addr:              s.addr(),
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http server terminated: %v", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("[WARN] server shutdown error: %v", err)
	}

	return nil
}

func (s Server) addr() string {
	if strings.TrimSpace(s.Addr) == "" {
		return defaultHTTPAddr
	}
	return s.Addr
}

func (s Server) routes() chi.Router {
	router := chi.NewRouter()

	router.Use(SecurityHeaders)
	router.Use(middleware.Throttle(1000), middleware.Timeout(60*time.Second))
	router.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(10, nil)))

	staticFS, err := fs.Sub(frontend.Templates, "static")
	if err != nil {
		log.Printf("[ERROR] failed to load static assets: %v", err)
	} else {
		router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	router.Get("/", s.indexCtrl)

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(Logger(log.Default()))
		r.Get("/posts", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("HX-Request") == "true" {
				s.getPostsHtmxCtrl(w, r)
			} else {
				s.getPostsCtrl(w, r)
			}
		})
	})

	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, JSON{"error": "not found"})
	})

	return router
}

type postsQuery struct {
	Page         int
	PageSize     int
	PartitionKey string
}

func parsePostsQuery(r *http.Request) (postsQuery, error) {
	query := r.URL.Query()

	page, err := parseBoundedQueryInt(query.Get("page"), defaultPostsPage, 1, 0, "page")
	if err != nil {
		return postsQuery{}, err
	}

	pageSize, err := parseBoundedQueryInt(query.Get("pageSize"), defaultPostsPageSize, 1, maxPostsPageSize, "pageSize")
	if err != nil {
		return postsQuery{}, err
	}
	if page-1 > math.MaxInt/pageSize {
		return postsQuery{}, fmt.Errorf("page parameter is too large for pageSize")
	}

	return postsQuery{
		Page:         page,
		PageSize:     pageSize,
		PartitionKey: strings.TrimSpace(query.Get("partitionKey")),
	}, nil
}

func parseBoundedQueryInt(raw string, defaultValue, minValue, maxValue int, name string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter", name)
	}
	if parsed < minValue {
		return 0, fmt.Errorf("%s parameter must be at least %d", name, minValue)
	}
	if maxValue > 0 && parsed > maxValue {
		return 0, fmt.Errorf("%s parameter must be at most %d", name, maxValue)
	}
	return parsed, nil
}

func renderBadRequest(w http.ResponseWriter, r *http.Request, message string, err error) {
	log.Printf("[ERROR] %s: %v", message, err)
	render.Status(r, http.StatusBadRequest)
	render.JSON(w, r, JSON{"error": err.Error(), "message": message})
}

func renderInternalServerError(w http.ResponseWriter, r *http.Request, message string, err error) {
	log.Printf("[ERROR] %s: %v", message, err)
	render.Status(r, http.StatusInternalServerError)
	render.JSON(w, r, JSON{"error": "internal server error", "message": message})
}
