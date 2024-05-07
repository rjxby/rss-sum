package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/rjxby/rss-sum/backend/assistant"
	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/hasher"
	"github.com/rjxby/rss-sum/backend/rss/worker"
	"github.com/rjxby/rss-sum/backend/server"
	"github.com/rjxby/rss-sum/backend/store"
)

var revision = "latest"

type settings struct {
	RunMigration bool
}

func main() {
	log.Printf("rss-sum %s\n", revision)

	settings, err := parseSettings()
	if err != nil {
		log.Fatalf("[ERROR] failed to parse settings: %v", err)
	}

	if settings.RunMigration {
		if err := runDatabaseMigration(); err != nil {
			log.Fatalf("[ERROR] failed to run database migration: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := sync.WaitGroup{}

	wg.Add(1)
	go runServer(ctx, &wg)

	wg.Add(1)
	go runWorker(ctx, &wg)

	// listen for C-c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// tell the goroutines to stop
	cancel()

	// and wait for them both to reply back
	wg.Wait()
}

func parseSettings() (*settings, error) {
	settings := settings{}

	runMigrationStr := os.Getenv("RUN_MIGRATION")
	if runMigrationStr == "" {
		runMigrationStr = "false"
	}

	runMigration, err := strconv.ParseBool(runMigrationStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RUN_MIGRATION environment variable: %v", err)
	}
	settings.RunMigration = runMigration

	return &settings, nil
}

func runDatabaseMigration() error {
	database, err := store.NewDatabase()
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}

	if err := database.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %v", err)
	}

	return nil
}

func runServer(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	dataStore, err := store.NewDatabase()
	if err != nil {
		log.Fatalf("[ERROR] failed to create data store: %v", err)
	}

	srv := server.Server{
		Blogger: blogger.New(dataStore),
		Version: revision,
	}

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[ERROR] failed to run server: %v", err)
	}
}

func runWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	workerSettings, err := worker.ParseSettings()
	if err != nil {
		log.Fatalf("[ERROR] failed to parse worker settings: %v", err)
	}

	assistantSettings, err := assistant.ParseSettings()
	if err != nil {
		log.Fatalf("[ERROR] failed to parse assistant settings: %v", err)
	}

	dataStore, err := store.NewDatabase()
	if err != nil {
		log.Fatalf("[ERROR] failed to create data store: %v", err)
	}

	worker := worker.Worker{
		Settings:  *workerSettings,
		Assistent: assistant.New(assistantSettings),
		Blogger:   blogger.New(dataStore),
		Hasher:    hasher.New(),
		Version:   revision,
	}

	if err := worker.Run(ctx); err != nil {
		log.Fatalf("[ERROR] failed to run RSS worker: %v", err)
	}
}
