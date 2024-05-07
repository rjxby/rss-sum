package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/rjxby/rss-sum/backend/store"
)

type Settings struct {
	RSSFeedsURLs            []string
	RSSFeedLimit            int
	WorkerIntervalInSeconds int
	WorkerTimeoutInSeconds  int
}

// Blogger defines an interface to save and load data
type Blogger interface {
	GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error)
	SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error)
}

// Assistent defines an interface to work with text
type Assistent interface {
	SummirizeText(text string) (string, error)
}

// Hasher defines an interface to hash data
type Hasher interface {
	HashString(text string) string
}

type Worker struct {
	Assistent Assistent
	Blogger   Blogger
	Hasher    Hasher
	Settings  Settings
	Version   string
}

func ParseSettings() (*Settings, error) {
	settings := Settings{}

	workerFeedsStr := os.Getenv("FEEDS")
	if workerFeedsStr == "" {
		return nil, fmt.Errorf("FEEDS environment variable is empty; nothing to subscribe to")
	}
	settings.RSSFeedsURLs = strings.Split(workerFeedsStr, ",")

	if len(settings.RSSFeedsURLs) == 0 {
		return nil, fmt.Errorf("failed to parse FEEDS environment variable")
	}

	workerTimeoutInSecondsStr := os.Getenv("WORKER_TIMEOUT_IN_SECONDS")
	if workerTimeoutInSecondsStr == "" {
		workerTimeoutInSecondsStr = "1800" // 1800s = 30 min
	}
	workerTimeoutInSeconds, err := strconv.Atoi(workerTimeoutInSecondsStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WORKER_TIMEOUT_IN_SECONDS environment variable: %v", err)
	}
	settings.WorkerTimeoutInSeconds = workerTimeoutInSeconds

	workerIntervalInSecondsStr := os.Getenv("WORKER_INTERVAL_IN_SECONDS")
	if workerIntervalInSecondsStr == "" {
		workerIntervalInSecondsStr = "3600" // 3600s = 1 hour
	}
	workerIntervalInSeconds, err := strconv.Atoi(workerIntervalInSecondsStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WORKER_INTERVAL_IN_SECONDS environment variable: %v", err)
	}
	settings.WorkerIntervalInSeconds = workerIntervalInSeconds

	rssFeedLimitStr := os.Getenv("FEED_ITEMS_LIMIT")
	if rssFeedLimitStr == "" {
		rssFeedLimitStr = "3"
	}
	rssFeedLimit, err := strconv.Atoi(rssFeedLimitStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse FEED_ITEMS_LIMIT environment variable: %v", err)
	}
	settings.RSSFeedLimit = rssFeedLimit

	return &settings, nil
}

// Run the RSS worker
func (w Worker) Run(ctx context.Context) error {
	log.Printf("[INFO] activate RSS worker")

	ticker := time.NewTicker(time.Duration(w.Settings.WorkerIntervalInSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.runFetchPosts(); err != nil {
				log.Printf("[ERROR] failed to fetch posts: %v", err)
			}
		}
	}
}

func (w Worker) runFetchPosts() error {
	log.Printf("[INFO] runFetchPosts triggered at {%v}", time.Now())

	fp := gofeed.NewParser()

	// Fetch fresh posts from all feeds
	freshPosts := []*store.PostV1{}
	for _, feedURL := range w.Settings.RSSFeedsURLs {
		feed, err := fp.ParseURL(feedURL)
		if err != nil {
			return fmt.Errorf("failed to parse feed (%v): %v", feedURL, err)
		}

		if len(feed.Items) == 0 {
			// feed is empty
			continue
		}

		partitionKey := w.Hasher.HashString(feedURL)
		for _, item := range feed.Items[:w.Settings.RSSFeedLimit] {
			freshPosts = append(freshPosts, &store.PostV1{
				ID:           item.GUID,
				PartitionKey: partitionKey,
				SourceURL:    item.Link,
				Title:        item.Title,
				Text:         item.Content,
				CreatedAt:    time.Now().UTC(),
			})
		}

		// Load stored posts from the database
		storedPostsResult, err := w.Blogger.GetPosts(1, w.Settings.RSSFeedLimit, partitionKey)
		if err != nil {
			return fmt.Errorf("failed to load existing posts: %v", err)
		}
		storedPosts := storedPostsResult.Posts

		postsToCreate := w.distinctNewPosts(freshPosts, storedPosts)
		for _, postToCreate := range postsToCreate {
			summirizedText, err := w.Assistent.SummirizeText(postToCreate.Text)
			if err != nil {
				return fmt.Errorf("failed to sumirize post (%v): %v", postToCreate.SourceURL, err)
			}

			log.Printf("[INFO] runFetchPosts processed post: {%s}", postToCreate.SourceURL)

			postToCreate.Text = summirizedText
		}

		if len(postsToCreate) > 0 {
			_, err := w.Blogger.SavePostsBulk(postsToCreate)
			if err != nil {
				return fmt.Errorf("failed to save posts %v", err)
			}

			log.Printf("[INFO] posts were updated")
		}
	}

	log.Printf("[INFO] runFetchPosts finished at {%v}", time.Now())

	return nil
}

func (w Worker) distinctNewPosts(freshPosts []*store.PostV1, storedPosts []*store.PostV1) []*store.PostV1 {
	storedMap := make(map[string]bool)
	for _, post := range storedPosts {
		storedMap[post.ID] = true
	}

	var postsToCreate []*store.PostV1
	for _, freshPost := range freshPosts {
		if _, exists := storedMap[freshPost.ID]; !exists {
			postsToCreate = append(postsToCreate, freshPost)
		}
	}

	return postsToCreate
}
