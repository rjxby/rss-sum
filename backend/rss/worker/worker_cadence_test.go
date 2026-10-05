package worker

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rjxby/rss-sum/backend/hasher"
	"github.com/stretchr/testify/require"
)

type cadenceFeedFetcher func(context.Context, string) (*Feed, error)

func (f cadenceFeedFetcher) Fetch(ctx context.Context, feedURL string) (*Feed, error) {
	return f(ctx, feedURL)
}

type cadenceSleeper func(context.Context, time.Duration) error

func (s cadenceSleeper) Sleep(ctx context.Context, duration time.Duration) error {
	return s(ctx, duration)
}

func TestRunWaitsFullIntervalAfterEachPass(t *testing.T) {
	for _, tc := range []struct {
		name         string
		passDuration time.Duration
		passErr      error
	}{
		{name: "fast pass", passDuration: time.Second},
		{name: "slow pass", passDuration: 25 * time.Second},
		{name: "failed slow pass", passDuration: 25 * time.Second, passErr: errors.New("store unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const interval = 10 * time.Second
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				started := time.Now()
				var passStarts []time.Duration
				var waits []time.Duration
				feedURL := "https://example.com/feed"
				w := Worker{
					Settings: Settings{
						RSSFeedsURLs:            []string{feedURL},
						RSSFeedLimit:            1,
						WorkerIntervalInSeconds: 10,
						WorkerTimeoutInSeconds:  60,
					},
					FeedFetcher: cadenceFeedFetcher(func(context.Context, string) (*Feed, error) {
						passStarts = append(passStarts, time.Since(started))
						if len(passStarts) == 3 {
							cancel()
							return &Feed{}, nil
						}
						time.Sleep(tc.passDuration)
						if tc.passErr != nil {
							return &Feed{Items: []FeedItem{{ID: "post"}}}, nil
						}
						return &Feed{}, nil
					}),
					PostService: &fakePostService{findErrs: map[string]error{hasher.HashString(feedURL): tc.passErr}},
					Sleeper: cadenceSleeper(func(ctx context.Context, duration time.Duration) error {
						waits = append(waits, duration)
						return (realSleeper{}).Sleep(ctx, duration)
					}),
				}

				require.NoError(t, w.Run(ctx))
				require.Equal(t, []time.Duration{0, tc.passDuration + interval, 2 * (tc.passDuration + interval)}, passStarts)
				require.Equal(t, []time.Duration{interval, interval}, waits)
			})
		})
	}
}

func TestRunCancellationDuringIntervalStopsPromptly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		passes := 0
		w := Worker{
			Settings: Settings{
				RSSFeedsURLs:            []string{"https://example.com/feed"},
				WorkerIntervalInSeconds: 3600,
				WorkerTimeoutInSeconds:  60,
			},
			FeedFetcher: cadenceFeedFetcher(func(context.Context, string) (*Feed, error) {
				passes++
				return &Feed{}, nil
			}),
		}
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()
		synctest.Wait()
		require.Equal(t, 1, passes)
		started := time.Now()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			require.NoError(t, err)
		default:
			t.Fatal("worker is still waiting after cancellation")
		}
		require.Zero(t, time.Since(started))
		require.Equal(t, 1, passes)
	})
}

func TestRunSkipsPassWhenAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := &cancelingFeedFetcher{cancel: cancel}
	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:            []string{"https://example.com/feed"},
			WorkerIntervalInSeconds: 10,
			WorkerTimeoutInSeconds:  60,
		},
		FeedFetcher: fetcher,
	}.Run(ctx)
	require.NoError(t, err)
	require.Zero(t, fetcher.calls)
}

func TestRunSkipsPassWhenCancellationCoincidesWithWaitCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	passes := 0
	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:            []string{"https://example.com/feed"},
			WorkerIntervalInSeconds: 10,
			WorkerTimeoutInSeconds:  60,
		},
		FeedFetcher: cadenceFeedFetcher(func(context.Context, string) (*Feed, error) {
			passes++
			return &Feed{}, nil
		}),
		Sleeper: cadenceSleeper(func(context.Context, time.Duration) error {
			cancel()
			return nil
		}),
	}.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, passes)
}

func TestRunReturnsUnexpectedWaitFailure(t *testing.T) {
	waitErr := errors.New("wait failed")
	passes := 0
	err := Worker{
		Settings: Settings{
			RSSFeedsURLs:            []string{"https://example.com/feed"},
			WorkerIntervalInSeconds: 10,
			WorkerTimeoutInSeconds:  60,
		},
		FeedFetcher: cadenceFeedFetcher(func(context.Context, string) (*Feed, error) {
			passes++
			return &Feed{}, nil
		}),
		Sleeper: cadenceSleeper(func(context.Context, time.Duration) error {
			return waitErr
		}),
	}.Run(context.Background())
	require.ErrorIs(t, err, waitErr)
	require.Equal(t, 1, passes)
}
