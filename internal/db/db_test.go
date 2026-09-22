package db

import (
	"path/filepath"
	"testing"

	"github.com/SuperOoge/reddit-archiver/internal/models"
)

func TestOpenMigratesSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	gormDB, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if !gormDB.Migrator().HasTable(&models.Post{}) {
		t.Error("posts table not created")
	}
	if !gormDB.Migrator().HasTable(&models.ScrapeRun{}) {
		t.Error("scrape_runs table not created")
	}

	post := models.Post{Source: models.SourceReddit, ExternalID: "abc123", URL: "https://example.com/x.png"}
	if err := gormDB.Create(&post).Error; err != nil {
		t.Fatalf("create post: %v", err)
	}
	if post.ID == 0 {
		t.Error("post ID not assigned after create")
	}
}

func TestOpenInvalidPath(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "no-such-dir", "test.db")); err == nil {
		t.Fatal("Open: expected error for path in nonexistent directory, got nil")
	}
}
