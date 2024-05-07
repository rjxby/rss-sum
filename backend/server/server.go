package server

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/rjxby/rss-sum/backend/store"
)

type Server struct {
	Blogger Blogger
	Version string
}

type Blogger interface {
	GetPosts(page int, pageSize int, partitionKey string) (result *store.PaginationPostsResult, err error)
}

// Run the lisener and request's router, activate rest server
func (s Server) Run(ctx context.Context) error {
	log.Printf("[INFO] activate rest server")

	httpServer := &http.Server{
		Addr:              ":8080",
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil {
			log.Printf("[WARN] http server terminated, %s", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpServer.Shutdown(shutdownCtx)

	return nil
}

func (s Server) routes() chi.Router {
	router := chi.NewRouter()

	router.Use(middleware.Throttle(1000), middleware.Timeout(60*time.Second))
	router.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(10, nil)))

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

func parseQueryParam(param string) (int, error) {
	value, err := strconv.Atoi(param)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func renderBadRequest(w http.ResponseWriter, r *http.Request, message string, err error) {
	log.Printf("[ERROR] %s: %v", message, err)
	render.Status(r, http.StatusBadRequest)
	render.JSON(w, r, JSON{"error": err.Error(), "message": message})
}

func renderInternalServerError(w http.ResponseWriter, r *http.Request, message string, err error) {
	log.Printf("[ERROR] %s: %v", message, err)
	render.Status(r, http.StatusInternalServerError)
	render.JSON(w, r, JSON{"error": err.Error(), "message": message})
}
