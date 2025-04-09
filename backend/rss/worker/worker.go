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
	SummarizeText(text string) (string, error)
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

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(w.Settings.WorkerTimeoutInSeconds)*time.Second)
	defer cancel()

	var finalErr error

	// Fetch fresh posts from all feeds
	for _, feedURL := range w.Settings.RSSFeedsURLs {
		var feed *gofeed.Feed
		var err error

		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				log.Printf("[INFO] retry %d fetching feed %s", attempt, feedURL)
				time.Sleep(time.Duration(attempt*2) * time.Second)
			}

			feed, err = fp.ParseURLWithContext(feedURL, ctx)
			if err == nil {
				break
			}
			log.Printf("[WARN] attempt %d to fetch feed %s failed: %v", attempt+1, feedURL, err)
		}

		if err != nil {
			log.Printf("[ERROR] failed to parse feed (%v) after retries: %v", feedURL, err)
			finalErr = fmt.Errorf("failed to parse feed: %v", err)
			continue
		}

		if len(feed.Items) == 0 {
			// feed is empty
			continue
		}

		partitionKey := w.Hasher.HashString(feedURL)
		freshPosts := []*store.PostV1{}
		for _, item := range feed.Items[:min(len(feed.Items), w.Settings.RSSFeedLimit)] {
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
			log.Printf("[ERROR] failed to load existing posts for feed %s: %v", feedURL, err)
			finalErr = fmt.Errorf("failed to load existing posts: %v", err)
			continue
		}
		storedPosts := storedPostsResult.Posts

		postsToCreate := w.distinctNewPosts(freshPosts, storedPosts)

		var successfulPosts []*store.PostV1

		for _, postToCreate := range postsToCreate {
			var summirizedText string
			var summarizeErr error

			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					log.Printf("[INFO] retry %d summarizing post %s", attempt, postToCreate.SourceURL)
					time.Sleep(time.Duration(attempt) * time.Second)
				}

				summirizedText, summarizeErr = w.Assistent.SummarizeText(postToCreate.Text)
				if summarizeErr == nil {
					break
				}
				log.Printf("[WARN] attempt %d to summarize post %s failed: %v",
					attempt+1, postToCreate.SourceURL, err)
			}

			if summarizeErr != nil {
				log.Printf("[ERROR] failed to summarize post (%v) after retries: %v",
					postToCreate.SourceURL, err)
				continue
			}

			log.Printf("[INFO] runFetchPosts processed post: {%s}", postToCreate.SourceURL)
			postToCreate.Text = summirizedText
			successfulPosts = append(successfulPosts, postToCreate)
		}

		if len(successfulPosts) > 0 {
			_, err := w.Blogger.SavePostsBulk(successfulPosts)
			if err != nil {
				log.Printf("[ERROR] failed to save posts for feed %s: %v", feedURL, err)
				finalErr = fmt.Errorf("failed to save posts: %v", err)
				continue
			}

			log.Printf("[INFO] posts were updated for feed %s", feedURL)
		}
	}

	log.Printf("[INFO] runFetchPosts finished at {%v}", time.Now())
	return finalErr
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
