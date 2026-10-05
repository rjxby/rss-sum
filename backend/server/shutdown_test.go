package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cancelingBlogger struct {
	started chan struct{}
}

func (b cancelingBlogger) ListPostsContext(ctx context.Context, _, _ int, _ string) (*blogger.PostsPage, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPostsRouteCancelsListingInBothResponseModes(t *testing.T) {
	for _, htmx := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "HTMX"}[htmx], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			srv := Server{Blogger: cancelingBlogger{started: started}}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil).WithContext(ctx)
			if htmx {
				req.Header.Set("HX-Request", "true")
			}
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				srv.routes().ServeHTTP(rec, req)
				close(done)
			}()
			awaitSignal(t, started)
			cancel()
			awaitSignal(t, done)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)
			assert.NotContains(t, rec.Body.String(), context.Canceled.Error())
		})
	}
}

func TestHTTPServerShutdownCancelsAndWaitsForActiveHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		w.WriteHeader(http.StatusNoContent)
		close(finished)
	})
	addr, done := startHTTPServer(t, ctx, handler, time.Second)
	response := make(chan error, 1)
	go func() {
		resp, err := http.Get(addr)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			err = resp.Body.Close()
		}
		response <- err
	}()
	awaitSignal(t, started)
	cancel()
	require.NoError(t, awaitError(t, done))
	awaitSignal(t, finished)
	require.NoError(t, awaitError(t, response))
}

func TestHTTPServerShutdownDeadlineClosesConnectionAndWaitsForHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	requestCanceled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	finished := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(requestCanceled)
		<-release
		close(finished)
	})
	addr, done := startHTTPServer(t, ctx, handler, 25*time.Millisecond)
	response := make(chan error, 1)
	go func() {
		resp, err := http.Get(addr)
		if err == nil {
			_ = resp.Body.Close()
		}
		response <- err
	}()
	awaitSignal(t, started)
	cancel()
	awaitSignal(t, requestCanceled)
	require.Error(t, awaitError(t, response))
	select {
	case err := <-done:
		t.Fatalf("server returned while its handler was active: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	release <- struct{}{}
	require.ErrorIs(t, awaitError(t, done), context.DeadlineExceeded)
	awaitSignal(t, finished)
}

func TestHTTPServerCleanShutdownWithoutRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, done := startHTTPServer(t, ctx, http.NotFoundHandler(), time.Second)
	cancel()
	require.NoError(t, awaitError(t, done))
}

func TestHTTPServerReturnsServeError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	err = runHTTPServer(context.Background(), &http.Server{Handler: http.NotFoundHandler()}, listener, time.Second)
	require.ErrorIs(t, err, net.ErrClosed)
	assert.Contains(t, err.Error(), "http server terminated")
}

func TestCanceledPostsRequestWithTemporaryDatabase(t *testing.T) {
	database, err := store.NewDatabaseWithPath(t.TempDir() + "/cancellation.sqlite")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	require.NoError(t, database.Migrate())
	for _, htmx := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil).WithContext(ctx)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		srv := Server{Blogger: blogger.New(database)}
		srv.getPostsCtrl(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	}
}

func startHTTPServer(t *testing.T, ctx context.Context, handler http.Handler, timeout time.Duration) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- runHTTPServer(ctx, &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}, listener, timeout)
	}()
	return "http://" + listener.Addr().String(), done
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler")
	}
}

func awaitError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server")
		return nil
	}
}
