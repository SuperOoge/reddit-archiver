// Package db owns the SQLite connection and schema migration.
package db

import (
	"fmt"

	"github.com/SuperOoge/reddit-archiver/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects to the SQLite database at path, enabling WAL mode and
// foreign keys, and migrates the schema to the current model set.
func Open(path string) (*gorm.DB, error) {
	dsn := path + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", path, err)
	}

	if err := db.AutoMigrate(&models.Post{}, &models.ScrapeRun{}); err != nil {
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	return db, nil
}
