package blogger

import (
	"fmt"

	"github.com/rjxby/rss-sum/backend/store"
)

// BloggerProc creates and save blogs
type BloggerProc struct {
	engine Engine
}

// New makes BloggerProc
func New(engine Engine) *BloggerProc {
	return &BloggerProc{
		engine: engine,
	}
}

// Engine defines interface to save and load data
type Engine interface {
	GetPosts(page int, pageSize int, partitionKey string) (*store.PaginationPostsResult, error)
	SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error)
}

func (p BloggerProc) GetPosts(page int, pageSize int, searchTerm string) (*store.PaginationPostsResult, error) {
	results, err := p.engine.GetPosts(page, pageSize, searchTerm)
	if err != nil {
		return nil, fmt.Errorf("failed to get posts: %v", err)
	}

	return results, nil
}

func (p BloggerProc) SavePostsBulk(postsToSave []*store.PostV1) ([]*store.PostV1, error) {
	results, err := p.engine.SavePostsBulk(postsToSave)
	if err != nil {
		return nil, fmt.Errorf("failed to save posts bulk: %v", err)
	}

	return results, nil
}
