package store

import (
	"fmt"
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var databaseName = "data/rss-sum.sqlite"

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

// NewDatabase makes persistent sqlite based store
func NewDatabase() (*Database, error) {
	log.Printf("[INFO] sqlite (persistent) store")
	result := Database{}

	db, err := gorm.Open(sqlite.Open(databaseName), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("[ERROR] failed to open database: %v", err)
	}

	result.db = db

	return &result, nil
}

func (s *Database) Migrate() error {
	log.Printf("[INFO] migrating database")

	if err := s.db.AutoMigrate(&PostV1{}); err != nil {
		return fmt.Errorf("[ERROR] failed to migrate database: %v", err)
	}

	log.Printf("[INFO] database migrated")
	return nil
}

func (s *Database) GetPosts(page int, pageSize int, partitionKey string) (result *PaginationPostsResult, err error) {
	var posts []*PostV1
	var size int64
	offset := (page - 1) * pageSize

	if partitionKey != "" {
		s.db.Model(&PostV1{}).Where("partition_key = ?", partitionKey).Count(&size)
		s.db.Where("partition_key = ?", partitionKey).Offset(offset).Limit(pageSize).Find(&posts)
	} else {
		s.db.Model(&PostV1{}).Count(&size)
		s.db.Offset(offset).Limit(pageSize).Find(&posts)
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

func (s *Database) SavePostsBulk(postsToSave []*PostV1) ([]*PostV1, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
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
