package store

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/rjxby/rss-sum/backend/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const defaultDatabasePath = "data/rss-sum.sqlite"

type Database struct {
	db *gorm.DB
}

type PaginationPostsResult struct {
	Posts        []*PostV1
	PartitionKey string
	Page         int
	PageSize     int
	Size         int64
}

func NewDatabase() (*Database, error) {
	return NewDatabaseWithPath(config.OptionalString(config.EnvDatabasePath, defaultDatabasePath))
}

func NewDatabaseWithPath(path string) (*Database, error) {
	log.Printf("[INFO] sqlite (persistent) store: %s", path)
	result := Database{}

	if err := ensureDatabaseDir(path); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(sqliteDSN(path)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("[ERROR] failed to open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("[ERROR] failed to access underlying database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	result.db = db

	return &result, nil
}

func sqliteDSN(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return path
	}

	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_busy_timeout=5000&_journal_mode=WAL"
}

func ensureDatabaseDir(path string) error {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}

	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create database directory %q: %v", dir, err)
	}

	return nil
}

func (s *Database) Close() error {
	db, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("failed to access underlying database: %v", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %v", err)
	}
	return nil
}

func (s *Database) Migrate() error {
	log.Printf("[INFO] migrating database")

	if err := s.db.AutoMigrate(&PostV1{}); err != nil {
		return fmt.Errorf("[ERROR] failed to migrate database: %v", err)
	}

	log.Printf("[INFO] database migrated")
	return nil
}

func (s *Database) GetPostsContext(ctx context.Context, page int, pageSize int, partitionKey string) (result *PaginationPostsResult, err error) {
	if page < 1 || pageSize < 1 {
		return nil, fmt.Errorf("page and pageSize must be positive")
	}
	if page-1 > math.MaxInt/pageSize {
		return nil, fmt.Errorf("pagination offset exceeds the supported integer range")
	}
	var posts []*PostV1
	var size int64
	offset := (page - 1) * pageSize
	db := s.db.WithContext(ctx).Model(&PostV1{})

	if partitionKey != "" {
		db = db.Where("partition_key = ?", partitionKey)
	}
	db = db.Session(&gorm.Session{})
	if err := db.Count(&size).Error; err != nil {
		return nil, fmt.Errorf("failed to count posts: %w", err)
	}
	if err := db.Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&posts).Error; err != nil {
		return nil, fmt.Errorf("failed to find posts: %w", err)
	}

	if posts == nil {
		posts = make([]*PostV1, 0)
	}

	return &PaginationPostsResult{
		Posts:        posts,
		PartitionKey: partitionKey,
		Page:         page,
		PageSize:     pageSize,
		Size:         size}, nil
}

func (s *Database) SavePostsBulk(postsToSave []*PostV1) (saved []*PostV1, err error) {
	tx := s.db.Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to begin posts creation transaction: %v", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			saved = nil
			err = fmt.Errorf("failed to create posts: panic: %v", r)
		}
	}()

	for _, postToSave := range postsToSave {
		if err := tx.Create(postToSave).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create posts: %v", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit posts creation transaction: %v", err)
	}

	return postsToSave, nil
}

func (s *Database) FindRecentPostIDs(partitionKey string, limit int) ([]string, error) {
	query := s.db.Model(&PostV1{}).Select("id").Order("created_at DESC, id DESC")
	if partitionKey != "" {
		query = query.Where("partition_key = ?", partitionKey)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	var postIDs []string
	if err := query.Find(&postIDs).Error; err != nil {
		return nil, fmt.Errorf("failed to find recent post ids: %v", err)
	}
	if postIDs == nil {
		postIDs = []string{}
	}

	return postIDs, nil
}
