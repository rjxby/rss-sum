package server

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	ListPostsContext(ctx context.Context, page int, pageSize int, partitionKey string) (result *blogger.PostsPage, err error)
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

	listener, err := net.Listen("tcp", s.addr())
	if err != nil {
		return fmt.Errorf("http server terminated: %w", err)
	}
	return runHTTPServer(ctx, httpServer, listener, 5*time.Second)
}

func runHTTPServer(ctx context.Context, httpServer *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	httpServer.BaseContext = func(net.Listener) context.Context { return requestCtx }

	var handlers sync.WaitGroup
	var admission sync.Mutex
	accepting := true
	handler := httpServer.Handler
	httpServer.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admission.Lock()
		if !accepting {
			admission.Unlock()
			http.Error(w, "server shutting down", http.StatusServiceUnavailable)
			return
		}
		handlers.Add(1)
		admission.Unlock()
		defer handlers.Done()
		handler.ServeHTTP(w, r)
	})
	serverErr := make(chan error, 1)
	go func() { serverErr <- httpServer.Serve(listener) }()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErr:
	}
	cancelRequests()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	var closeErr error
	if shutdownErr != nil {
		shutdownErr = fmt.Errorf("http server shutdown failed: %w", shutdownErr)
		if err := httpServer.Close(); err != nil {
			closeErr = fmt.Errorf("failed to close http server: %w", err)
		}
	}
	if serveErr == nil {
		serveErr = <-serverErr
	}
	// Close admission before waiting: a connection accepted before shutdown may
	// still dispatch a request after Close returns.
	admission.Lock()
	accepting = false
	admission.Unlock()
	handlers.Wait()
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	} else if serveErr != nil {
		serveErr = fmt.Errorf("http server terminated: %w", serveErr)
	}
	return errors.Join(serveErr, shutdownErr, closeErr)
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
		r.Get("/posts", s.getPostsCtrl)
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
