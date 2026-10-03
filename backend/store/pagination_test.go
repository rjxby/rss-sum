package store

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetPostsRejectsUnsafePagination(t *testing.T) {
	cases := []struct {
		page     int
		pageSize int
	}{
		{page: 0, pageSize: 1},
		{page: -1, pageSize: 1},
		{page: math.MinInt, pageSize: 1},
		{page: 1, pageSize: 0},
		{page: 1, pageSize: -1},
		{page: 1, pageSize: math.MinInt},
		{page: math.MaxInt/2 + 2, pageSize: 2},
		{page: math.MaxInt/100 + 2, pageSize: 100},
		{page: math.MaxInt, pageSize: math.MaxInt},
		{page: 3, pageSize: math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("page=%d/size=%d", tc.page, tc.pageSize), func(t *testing.T) {
			database := &Database{}
			result, err := database.GetPosts(tc.page, tc.pageSize, "")
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestGetPostsPaginationBounds(t *testing.T) {
	database := newTestDatabase(t)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	_, err := database.SavePostsBulk([]*PostV1{
		{ID: "a", PartitionKey: "feed", Title: "A", Text: "A", SourceURL: "https://example.com/a", CreatedAt: now},
		{ID: "b", PartitionKey: "feed", Title: "B", Text: "B", SourceURL: "https://example.com/b", CreatedAt: now},
		{ID: "other", PartitionKey: "other", Title: "Other", Text: "Other", SourceURL: "https://example.com/other", CreatedAt: now},
	})
	require.NoError(t, err)
	for _, pageSize := range []int{1, 2, 10, 100, math.MaxInt} {
		for _, partitionKey := range []string{"", "feed"} {
			t.Run(strconv.Itoa(pageSize)+"/"+partitionKey, func(t *testing.T) {
				page := math.MaxInt
				if pageSize > 1 {
					page = math.MaxInt/pageSize + 1
				}
				result, err := database.GetPosts(page, pageSize, partitionKey)
				require.NoError(t, err)
				require.Equal(t, page, result.Page)
				require.Equal(t, pageSize, result.PageSize)
				require.Empty(t, result.Posts)
				require.NotNil(t, result.Posts)
				count := int64(3)
				if partitionKey == "feed" {
					count = 2
				}
				require.Equal(t, count, result.Size)
			})
		}
	}
	result, err := database.GetPosts(2, 1, "feed")
	require.NoError(t, err)
	require.Len(t, result.Posts, 1)
	require.Equal(t, "a", result.Posts[0].ID)
	result, err = database.GetPosts(1, math.MaxInt, "feed")
	require.NoError(t, err)
	require.Len(t, result.Posts, 2)
}
