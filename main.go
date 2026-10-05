package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/rjxby/rss-sum/backend/assistant"
	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/config"
	"github.com/rjxby/rss-sum/backend/rss/worker"
	"github.com/rjxby/rss-sum/backend/server"
	"github.com/rjxby/rss-sum/backend/store"
)

var revision = "latest"

func main() {
	log.Printf("rss-sum %s\n", revision)

	if err := config.LoadEnvFile(".env"); err != nil {
		log.Fatalf("[ERROR] failed to load configuration: %v", err)
	}

	settings, err := config.ParseRuntimeSettings()
	if err != nil {
		log.Fatalf("[ERROR] failed to parse settings: %v", err)
	}

	if settings.RunMigration {
		if err := runDatabaseMigration(); err != nil {
			log.Fatalf("[ERROR] failed to run database migration: %v", err)
		}
	}

	if err := runApplication(settings); err != nil {
		log.Fatalf("[ERROR] application failed: %v", err)
	}
}

func runApplication(settings *config.RuntimeSettings) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	if settings.HTTPServerEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runServer(ctx, settings.HTTPAddr); err != nil {
				errCh <- fmt.Errorf("server failed: %w", err)
			}
		}()
	}

	if settings.RSSWorkerEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runWorker(ctx); err != nil {
				errCh <- fmt.Errorf("worker failed: %w", err)
			}
		}()
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	interruptCh := make(chan os.Signal, 1)
	signal.Notify(interruptCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interruptCh)

	var err error
	select {
	case <-interruptCh:
	case err = <-errCh:
	case <-doneCh:
	}
	cancel()
	<-doneCh

	close(errCh)
	for serviceErr := range errCh {
		err = errors.Join(err, serviceErr)
	}
	return err
}

func runDatabaseMigration() error {
	database, err := store.NewDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("[WARN] failed to close database: %v", err)
		}
	}()

	if err := database.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %v", err)
	}

	return nil
}

func runServer(ctx context.Context, httpAddr string) error {
	dataStore, err := store.NewDatabase()
	if err != nil {
		return fmt.Errorf("failed to create data store: %v", err)
	}
	defer func() {
		if err := dataStore.Close(); err != nil {
			log.Printf("[WARN] failed to close database: %v", err)
		}
	}()

	srv := &server.Server{
		Blogger: blogger.New(dataStore),
		Version: revision,
		Addr:    httpAddr,
	}

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("failed to run server: %w", err)
	}
	return nil
}

func runWorker(ctx context.Context) error {
	workerSettings, err := worker.ParseSettingsContext(ctx)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return fmt.Errorf("failed to parse worker settings: %w", err)
	}

	assistantSettings, err := assistant.ParseSettings()
	if err != nil {
		return fmt.Errorf("failed to parse assistant settings: %v", err)
	}

	dataStore, err := store.NewDatabase()
	if err != nil {
		return fmt.Errorf("failed to create data store: %v", err)
	}
	defer func() {
		if err := dataStore.Close(); err != nil {
			log.Printf("[WARN] failed to close database: %v", err)
		}
	}()

	worker := worker.Worker{
		Settings:    *workerSettings,
		Summarizer:  assistant.New(assistantSettings),
		PostService: blogger.New(dataStore),
	}

	if err := worker.Run(ctx); err != nil {
		return fmt.Errorf("failed to run RSS worker: %v", err)
	}
	return nil
}
