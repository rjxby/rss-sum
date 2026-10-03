package worker

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/rjxby/rss-sum/backend/blogger"
	"github.com/rjxby/rss-sum/backend/config"
)

const maxRSSFeeds = 100

var lookupIP = net.LookupIP

type Settings struct {
	RSSFeedsURLs            []string
	RSSFeedLimit            int
	WorkerIntervalInSeconds int
	WorkerTimeoutInSeconds  int
}

type Feed struct {
	Items []FeedItem
}

type FeedItem struct {
	ID        string
	SourceURL string
	Title     string
	Text      string
}

type FeedFetcher interface {
	Fetch(ctx context.Context, feedURL string) (*Feed, error)
}

type Summarizer interface {
	SummarizeText(ctx context.Context, text string) (string, error)
}

type PostService interface {
	FindRecentPostIDs(partitionKey string, limit int) ([]string, error)
	SavePosts(posts []blogger.Post) error
}

type Hasher interface {
	HashString(text string) string
}

type Clock interface {
	Now() time.Time
}

type Sleeper interface {
	Sleep(ctx context.Context, duration time.Duration) error
}

type Worker struct {
	Summarizer  Summarizer
	PostService PostService
	FeedFetcher FeedFetcher
	Hasher      Hasher
	Clock       Clock
	Sleeper     Sleeper
	Settings    Settings
	Version     string
}

func (w Worker) withDefaults() Worker {
	if w.FeedFetcher == nil {
		w.FeedFetcher = gofeedFetcher{}
	}
	if w.Clock == nil {
		w.Clock = realClock{}
	}
	if w.Sleeper == nil {
		w.Sleeper = realSleeper{}
	}
	return w
}

type gofeedFetcher struct{}

func (gofeedFetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	parser := gofeed.NewParser()
	parser.Client = safeFeedHTTPClient()

	parsedFeed, err := parser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}

	feed := &Feed{
		Items: make([]FeedItem, 0, len(parsedFeed.Items)),
	}
	for _, item := range parsedFeed.Items {
		feed.Items = append(feed.Items, mapFeedItem(item))
	}

	return feed, nil
}

func mapFeedItem(item *gofeed.Item) FeedItem {
	sourceURL := strings.TrimSpace(item.Link)
	text := strings.TrimSpace(item.Content)
	if text == "" {
		text = strings.TrimSpace(item.Description)
	}

	return FeedItem{
		ID:        strings.TrimSpace(item.GUID),
		SourceURL: sourceURL,
		Title:     strings.TrimSpace(item.Title),
		Text:      text,
	}
}

func safeFeedHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Dial directly so safeDialContext validates the feed destination.
	transport.Proxy = nil
	transport.DialContext = safeDialContext

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if err := validateFeedURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if err := validateFeedHost(host); err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("feed host %q is a blocked IP", host)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("failed to resolve feed host %q: no IPs returned", host)
	}

	var lastErr error
	for _, address := range addresses {
		if isBlockedIP(address.IP) {
			return nil, fmt.Errorf("feed host %q resolved to blocked IP %s", host, address.IP)
		}

		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ParseSettings() (*Settings, error) {
	feeds, err := config.StringList(config.EnvFeeds, maxRSSFeeds)
	if err != nil {
		return nil, err
	}
	if err := validateFeedURLs(feeds); err != nil {
		return nil, err
	}

	workerTimeoutInSeconds, err := config.PositiveInt(config.EnvWorkerTimeoutInSeconds, 1800)
	if err != nil {
		return nil, err
	}

	workerIntervalInSeconds, err := config.PositiveInt(config.EnvWorkerIntervalInSeconds, 3600)
	if err != nil {
		return nil, err
	}

	rssFeedLimit, err := config.PositiveInt(config.EnvFeedItemsLimit, 3)
	if err != nil {
		return nil, err
	}

	return &Settings{
		RSSFeedsURLs:            feeds,
		RSSFeedLimit:            rssFeedLimit,
		WorkerIntervalInSeconds: workerIntervalInSeconds,
		WorkerTimeoutInSeconds:  workerTimeoutInSeconds,
	}, nil
}

func validateFeedURLs(feedURLs []string) error {
	for _, feedURL := range feedURLs {
		if err := validateFeedURL(feedURL); err != nil {
			return err
		}
	}
	return nil
}

func validateFeedURL(feedURL string) error {
	parsed, err := url.Parse(feedURL)
	if err != nil {
		return fmt.Errorf("invalid %s URL %q: %v", config.EnvFeeds, feedURL, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid %s URL %q: scheme must be http or https", config.EnvFeeds, feedURL)
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("invalid %s URL %q: host is empty", config.EnvFeeds, feedURL)
	}
	if err := validateFeedHost(host); err != nil {
		return fmt.Errorf("invalid %s URL %q: %v", config.EnvFeeds, feedURL, err)
	}

	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("invalid %s URL %q: host IP is not allowed", config.EnvFeeds, feedURL)
		}
		return nil
	}

	ips, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve %s URL %q: %v", config.EnvFeeds, feedURL, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("failed to resolve %s URL %q: no IPs returned", config.EnvFeeds, feedURL)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("invalid %s URL %q: resolved IP is not allowed", config.EnvFeeds, feedURL)
		}
	}

	return nil
}

func validateFeedHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host is empty")
	}
	if isBlockedHostname(host) {
		return fmt.Errorf("host is not allowed")
	}
	return nil
}

func isBlockedHostname(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(host), ".")
	return normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") || strings.HasSuffix(normalized, ".local")
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func (w Worker) Run(ctx context.Context) error {
	log.Printf("[INFO] activate RSS worker")
	w = w.withDefaults()

	if err := w.RunOnce(ctx); err != nil {
		log.Printf("[ERROR] failed to fetch posts: %v", err)
	}

	ticker := time.NewTicker(time.Duration(w.Settings.WorkerIntervalInSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				log.Printf("[ERROR] failed to fetch posts: %v", err)
			}
		}
	}
}

func (w Worker) RunOnce(ctx context.Context) error {
	w = w.withDefaults()

	log.Printf("[INFO] runFetchPosts triggered at {%v}", w.Clock.Now())

	runCtx, cancel := context.WithTimeout(ctx,
		time.Duration(w.Settings.WorkerTimeoutInSeconds)*time.Second)
	defer cancel()

	var finalErr error

	for _, feedURL := range w.Settings.RSSFeedsURLs {
		feed, err := w.fetchWithRetry(runCtx, feedURL)
		if err != nil {
			log.Printf("[ERROR] failed to parse feed (%v) after retries: %v", feedURL, err)
			finalErr = fmt.Errorf("failed to parse feed: %v", err)
			continue
		}

		if len(feed.Items) == 0 {
			continue
		}

		partitionKey := w.Hasher.HashString(feedURL)
		storedPostIDs, err := w.PostService.FindRecentPostIDs(partitionKey, 0)
		if err != nil {
			log.Printf("[ERROR] failed to load existing posts for feed %s: %v", feedURL, err)
			finalErr = fmt.Errorf("failed to load existing posts: %v", err)
			continue
		}

		freshPosts := make([]blogger.Post, 0, min(len(feed.Items), w.Settings.RSSFeedLimit))
		for _, item := range feed.Items[:min(len(feed.Items), w.Settings.RSSFeedLimit)] {
			postID := strings.TrimSpace(item.ID)
			if postID == "" {
				postID = item.SourceURL
			}
			if postID == "" {
				postID = item.Title + "\n" + item.Text
			}
			postID = w.Hasher.HashString(partitionKey + "\n" + postID)

			freshPosts = append(freshPosts, blogger.Post{
				ID:           postID,
				PartitionKey: partitionKey,
				SourceURL:    item.SourceURL,
				Title:        item.Title,
				Text:         item.Text,
				CreatedAt:    w.Clock.Now().UTC(),
			})
		}

		postsToCreate := distinctNewPosts(freshPosts, storedPostIDs)

		successfulPosts := make([]blogger.Post, 0, len(postsToCreate))

		for _, postToCreate := range postsToCreate {
			summarizedText, summarizeErr := w.summarizeWithRetry(runCtx, postToCreate)
			if summarizeErr != nil {
				log.Printf("[ERROR] failed to summarize post (%v) after retries: %v",
					postToCreate.SourceURL, summarizeErr)
				finalErr = fmt.Errorf("failed to summarize post: %v", summarizeErr)
				continue
			}

			log.Printf("[INFO] runFetchPosts processed post: {%s}", postToCreate.SourceURL)
			postToCreate.Text = summarizedText
			successfulPosts = append(successfulPosts, postToCreate)
		}

		if len(successfulPosts) > 0 {
			if err := w.PostService.SavePosts(successfulPosts); err != nil {
				log.Printf("[ERROR] failed to save posts for feed %s: %v", feedURL, err)
				finalErr = fmt.Errorf("failed to save posts: %v", err)
				continue
			}

			log.Printf("[INFO] posts were updated for feed %s", feedURL)
		}
	}

	log.Printf("[INFO] runFetchPosts finished at {%v}", w.Clock.Now())
	return finalErr
}

func (w Worker) fetchWithRetry(ctx context.Context, feedURL string) (*Feed, error) {
	var feed *Feed
	var err error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			log.Printf("[INFO] retry %d fetching feed %s", attempt, feedURL)
			if sleepErr := w.Sleeper.Sleep(ctx, time.Duration(attempt*2)*time.Second); sleepErr != nil {
				return nil, sleepErr
			}
		}

		feed, err = w.FeedFetcher.Fetch(ctx, feedURL)
		if err == nil {
			return feed, nil
		}
		log.Printf("[WARN] attempt %d to fetch feed %s failed: %v", attempt+1, feedURL, err)
	}

	return nil, err
}

func (w Worker) summarizeWithRetry(ctx context.Context, post blogger.Post) (string, error) {
	var summarizedText string
	var err error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			log.Printf("[INFO] retry %d summarizing post %s", attempt, post.SourceURL)
			if sleepErr := w.Sleeper.Sleep(ctx, time.Duration(attempt)*time.Second); sleepErr != nil {
				return "", sleepErr
			}
		}

		summarizedText, err = w.Summarizer.SummarizeText(ctx, post.Text)
		if err == nil {
			return summarizedText, nil
		}
		log.Printf("[WARN] attempt %d to summarize post %s failed: %v",
			attempt+1, post.SourceURL, err)
	}

	return "", err
}

func distinctNewPosts(freshPosts []blogger.Post, storedPostIDs []string) []blogger.Post {
	storedMap := make(map[string]bool)
	for _, postID := range storedPostIDs {
		storedMap[postID] = true
	}

	var postsToCreate []blogger.Post
	for _, freshPost := range freshPosts {
		if _, exists := storedMap[freshPost.ID]; !exists {
			postsToCreate = append(postsToCreate, freshPost)
			storedMap[freshPost.ID] = true
		}
	}

	return postsToCreate
}
