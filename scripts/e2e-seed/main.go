package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/store"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: e2e-seed populated.sqlite empty.sqlite")
	}
	for i, path := range os.Args[1:] {
		if err := seed(path, i == 0); err != nil {
			log.Fatal(err)
		}
	}
}

func seed(path string, populated bool) (err error) {
	db, err := store.NewDatabaseWithPath(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	if err := db.Migrate(); err != nil {
		return err
	}
	if !populated {
		return nil
	}

	posts := make([]blogger.Post, 25)
	for i := range posts {
		number := i + 1
		text := fmt.Sprintf("Fixture summary %02d. The council approved a public library renovation on 15 January 2025. Work starts in June and the library stays open during construction.", number)
		if number == 1 {
			text = strings.Repeat(text+" ", 12)
		}
		posts[i] = blogger.Post{
			ID:           fmt.Sprintf("e2e-%02d", number),
			PartitionKey: "e2e-fixtures",
			Title:        fmt.Sprintf("Fixture article %02d", number),
			Text:         text,
			SourceURL:    fmt.Sprintf("https://example.com/articles/%02d", number),
			CreatedAt:    time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute),
		}
	}
	return blogger.New(db).SavePosts(posts)
}
