package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rjxby/rss-sum/backend/config"
)

func TestRunApplicationWorkerStartupError(t *testing.T) {
	t.Setenv("FEEDS", "")
	previous := runtime.GOMAXPROCS(2)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	settings := &config.RuntimeSettings{RSSWorkerEnabled: true}
	// Repeat with two processors to exercise error reporting racing with completion.
	for i := 0; i < 1000; i++ {
		err := runApplication(settings)
		if err == nil || !strings.Contains(err.Error(), "worker failed: failed to parse worker settings") || !strings.Contains(err.Error(), "FEEDS") {
			t.Fatalf("attempt %d: expected missing FEEDS startup error, got %v", i, err)
		}
	}
}

func TestRunApplicationServerStartupError(t *testing.T) {
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "rss-sum.sqlite"))
	previous := runtime.GOMAXPROCS(2)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	settings := &config.RuntimeSettings{
		HTTPServerEnabled: true,
		HTTPAddr:          "invalid-address",
	}
	for i := 0; i < 100; i++ {
		err := runApplication(settings)
		if err == nil || !strings.Contains(err.Error(), "server failed: failed to run server") || !strings.Contains(err.Error(), "missing port in address") {
			t.Fatalf("attempt %d: expected invalid address startup error, got %v", i, err)
		}
	}
}

func TestRunApplicationNoServices(t *testing.T) {
	if err := runApplication(&config.RuntimeSettings{}); err != nil {
		t.Fatalf("expected successful completion without services, got %v", err)
	}
}
