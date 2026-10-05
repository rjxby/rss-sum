package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunWorkerCancellationDuringStartupDNS(t *testing.T) {
	t.Setenv("FEEDS", "https://slow-feed.example./feed")
	original := net.DefaultResolver
	started := make(chan struct{}, 1)
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	t.Cleanup(func() { net.DefaultResolver = original })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runWorker(ctx) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("startup DNS resolution did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("startup cancellation error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after startup cancellation")
	}
}

func TestRunWorkerPreservesSettingsErrorAfterCancellation(t *testing.T) {
	t.Setenv("FEEDS", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWorker(ctx); err == nil || !strings.Contains(err.Error(), "FEEDS") {
		t.Fatalf("startup error = %v, want missing FEEDS error", err)
	}
}
