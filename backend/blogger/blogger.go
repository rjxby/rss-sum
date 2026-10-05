package blogger

import (
	"context"
	"fmt"
	"time"

	"github.com/rjxby/rss-sum/backend/store"
)

type BloggerProc struct {
	repository Repository
}

func New(repository Repository) *BloggerProc {
	return &BloggerProc{
		repository: repository,
	}
}

type Post struct {
	ID           string
	PartitionKey string
	Title        string
	Text         string
	SourceURL    string
	CreatedAt    time.Time
}

type PostsPage struct {
	Posts        []Post
	PartitionKey string
	Page         int
	PageSize     int
	Size         int64
}

type Repository interface {
	GetPostsContext(ctx context.Context, page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error)
	SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error)
	FindRecentPostIDs(partitionKey string, limit int) ([]string, error)
}

func (p BloggerProc) ListPostsContext(ctx context.Context, page int, pageSize int, partitionKey string) (*PostsPage, error) {
	results, err := p.repository.GetPostsContext(ctx, page, pageSize, partitionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to list posts: %w", err)
	}

	return mapPostsPage(results), nil
}

func (p BloggerProc) SavePosts(postsToSave []Post) error {
	if len(postsToSave) == 0 {
		return nil
	}

	if _, err := p.repository.SavePostsBulk(mapStorePosts(postsToSave)); err != nil {
		return fmt.Errorf("failed to save posts: %v", err)
	}

	return nil
}

func (p BloggerProc) FindRecentPostIDs(partitionKey string, limit int) ([]string, error) {
	postIDs, err := p.repository.FindRecentPostIDs(partitionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find recent post ids: %v", err)
	}

	return postIDs, nil
}

func mapPostsPage(result *store.PaginationPostsResult) *PostsPage {
	if result == nil {
		return nil
	}

	posts := make([]Post, 0, len(result.Posts))
	for _, post := range result.Posts {
		posts = append(posts, mapPost(post))
	}

	return &PostsPage{
		Posts:        posts,
		PartitionKey: result.PartitionKey,
		Page:         result.Page,
		PageSize:     result.PageSize,
		Size:         result.Size,
	}
}

func mapPost(post *store.PostV1) Post {
	if post == nil {
		return Post{}
	}

	return Post{
		ID:           post.ID,
		PartitionKey: post.PartitionKey,
		Title:        post.Title,
		Text:         post.Text,
		SourceURL:    post.SourceURL,
		CreatedAt:    post.CreatedAt,
	}
}

func mapStorePosts(posts []Post) []*store.PostV1 {
	mapped := make([]*store.PostV1, 0, len(posts))
	for _, post := range posts {
		mapped = append(mapped, &store.PostV1{
			ID:           post.ID,
			PartitionKey: post.PartitionKey,
			Title:        post.Title,
			Text:         post.Text,
			SourceURL:    post.SourceURL,
			CreatedAt:    post.CreatedAt,
		})
	}
	return mapped
}
