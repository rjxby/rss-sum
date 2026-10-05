package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetPostsContextCancelsWhileWaitingForConnection(t *testing.T) {
	for _, partition := range []string{"", "feed"} {
		t.Run("partition="+partition, func(t *testing.T) {
			database := newTestDatabase(t)
			t.Cleanup(func() { assert.NoError(t, database.Close()) })
			sqlDB, err := database.db.DB()
			require.NoError(t, err)
			connection, err := sqlDB.Conn(context.Background())
			require.NoError(t, err)
			defer func() { assert.NoError(t, connection.Close()) }()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := database.GetPostsContext(ctx, 1, 10, partition)
				done <- err
			}()
			select {
			case err = <-done:
			case <-time.After(time.Second):
				t.Fatal("listing did not cancel while waiting for a database connection")
			}
			assert.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Contains(t, err.Error(), "failed to count posts")
		})
	}
}

func TestGetPostsContextCancelsBetweenCountAndListing(t *testing.T) {
	for _, partition := range []string{"", "feed"} {
		t.Run("partition="+partition, func(t *testing.T) {
			database := newTestDatabase(t)
			t.Cleanup(func() { assert.NoError(t, database.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queries := 0
			err := database.db.Callback().Query().Before("gorm:query").Register("cancel_listing", func(_ *gorm.DB) {
				queries++
				if queries == 2 {
					cancel()
				}
			})
			require.NoError(t, err)
			result, err := database.GetPostsContext(ctx, 1, 10, partition)
			assert.Nil(t, result)
			assert.ErrorIs(t, err, context.Canceled)
			assert.Contains(t, err.Error(), "failed to find posts")
		})
	}
}
