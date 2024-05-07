package store

import (
	"time"
)

type PostV1 struct {
	ID           string `gorm:"primaryKey"`
	PartitionKey string `gorm:"not null"`

	Title     string `gorm:"type:varchar(500);not null"`
	Text      string `gorm:"type:varchar(4000);not null"`
	SourceURL string `gorm:"not null"`

	CreatedAt time.Time
}
