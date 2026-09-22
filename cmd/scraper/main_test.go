package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SuperOoge/reddit-archiver/internal/db"
	"github.com/SuperOoge/reddit-archiver/internal/models"
	"github.com/SuperOoge/reddit-archiver/internal/reddit"
)

func TestScraperRunEndToEnd(t *testing.T) {
	const mediaBody = "pretend this is an image"

	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mediaBody))
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{"data": {"id": "abc123", "subreddit": "golang", "author": "gopher", "title": "a wallpaper", "url": "` + mediaSrv.URL + `/pic.png", "permalink": "/r/golang/comments/abc123/x/", "created_utc": 1700000000}},
				{"data": {"id": "def456", "subreddit": "golang", "author": "gopher2", "title": "a self post", "url": "https://www.reddit.com/r/golang/comments/def456/x/", "permalink": "/r/golang/comments/def456/x/", "created_utc": 1700000100}}
			]
		}
	}`
	redditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listing))
	}))
	defer redditSrv.Close()

	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	redditClient := reddit.NewClient("test-agent/1.0")
	redditClient.BaseURL = redditSrv.URL

	s := scraper{
		db:              gormDB,
		reddit:          redditClient,
		downloadClient:  mediaSrv.Client(),
		downloadRootDir: filepath.Join(dir, "downloads"),
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	var posts []models.Post
	if err := gormDB.Order("external_id").Find(&posts).Error; err != nil {
		t.Fatalf("query posts: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}

	mediaPost, selfPost := posts[0], posts[1]
	if mediaPost.ExternalID != "abc123" {
		t.Fatalf("posts[0].ExternalID = %q, want abc123", mediaPost.ExternalID)
	}

	if mediaPost.LocalPath == "" {
		t.Error("media post: LocalPath is empty, want a downloaded file path")
	}
	if mediaPost.SHA256 == "" {
		t.Error("media post: SHA256 is empty, want a content hash")
	}
	if mediaPost.DownloadedAt == nil {
		t.Error("media post: DownloadedAt is nil, want a timestamp")
	}
	data, err := os.ReadFile(mediaPost.LocalPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", mediaPost.LocalPath, err)
	}
	if string(data) != mediaBody {
		t.Errorf("downloaded content = %q, want %q", data, mediaBody)
	}

	if selfPost.ExternalID != "def456" {
		t.Fatalf("posts[1].ExternalID = %q, want def456", selfPost.ExternalID)
	}
	if selfPost.LocalPath != "" {
		t.Errorf("self post: LocalPath = %q, want empty (not a direct media link)", selfPost.LocalPath)
	}

	var scrapeRun models.ScrapeRun
	if err := gormDB.First(&scrapeRun).Error; err != nil {
		t.Fatalf("query scrape run: %v", err)
	}
	if scrapeRun.Status != models.ScrapeStatusCompleted {
		t.Errorf("ScrapeRun.Status = %q, want %q", scrapeRun.Status, models.ScrapeStatusCompleted)
	}
	if scrapeRun.PostsFound != 2 {
		t.Errorf("ScrapeRun.PostsFound = %d, want 2", scrapeRun.PostsFound)
	}
}

func TestScraperRunSkipsAlreadyDownloaded(t *testing.T) {
	callCount := 0
	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write([]byte("bytes"))
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{"data": {"id": "abc123", "subreddit": "golang", "url": "` + mediaSrv.URL + `/pic.png", "permalink": "/r/golang/comments/abc123/x/", "created_utc": 1700000000}}
			]
		}
	}`
	redditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listing))
	}))
	defer redditSrv.Close()

	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	redditClient := reddit.NewClient("test-agent/1.0")
	redditClient.BaseURL = redditSrv.URL

	s := scraper{
		db:              gormDB,
		reddit:          redditClient,
		downloadClient:  mediaSrv.Client(),
		downloadRootDir: filepath.Join(dir, "downloads"),
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if callCount != 1 {
		t.Errorf("media server was hit %d time(s), want 1 (second run should skip the already-downloaded post)", callCount)
	}
}

func TestScraperRunDownloadsConcurrently(t *testing.T) {
	const numPosts = 8

	var mu sync.Mutex
	current, maxConcurrent := 0, 0

	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current++
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()

		_, _ = w.Write([]byte("bytes"))
	}))
	defer mediaSrv.Close()

	var children strings.Builder
	for i := 0; i < numPosts; i++ {
		if i > 0 {
			children.WriteString(",")
		}
		fmt.Fprintf(&children, `{"data": {"id": "post%d", "subreddit": "golang", "url": "%s/pic%d.png", "permalink": "/r/golang/comments/post%d/x/", "created_utc": 1700000000}}`,
			i, mediaSrv.URL, i, i)
	}
	listing := `{"data": {"after": "", "children": [` + children.String() + `]}}`

	redditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listing))
	}))
	defer redditSrv.Close()

	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	redditClient := reddit.NewClient("test-agent/1.0")
	redditClient.BaseURL = redditSrv.URL

	s := scraper{
		db:                  gormDB,
		reddit:              redditClient,
		downloadClient:      mediaSrv.Client(),
		downloadRootDir:     filepath.Join(dir, "downloads"),
		downloadConcurrency: 4,
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	var scrapeRun models.ScrapeRun
	if err := gormDB.First(&scrapeRun).Error; err != nil {
		t.Fatalf("query scrape run: %v", err)
	}
	if scrapeRun.PostsFound != numPosts {
		t.Errorf("PostsFound = %d, want %d", scrapeRun.PostsFound, numPosts)
	}

	var downloadedCount int64
	if err := gormDB.Model(&models.Post{}).Where("local_path != ''").Count(&downloadedCount).Error; err != nil {
		t.Fatalf("count downloaded posts: %v", err)
	}
	if downloadedCount != numPosts {
		t.Errorf("downloaded posts = %d, want %d", downloadedCount, numPosts)
	}

	mu.Lock()
	got := maxConcurrent
	mu.Unlock()
	if got < 2 {
		t.Errorf("maxConcurrent = %d, want > 1 (downloads should overlap under the worker pool)", got)
	}
}

func TestScraperRunDefaultsConcurrencyWhenUnset(t *testing.T) {
	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("bytes"))
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{"data": {"id": "abc123", "subreddit": "golang", "url": "` + mediaSrv.URL + `/pic.png", "permalink": "/r/golang/comments/abc123/x/", "created_utc": 1700000000}}
			]
		}
	}`
	redditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listing))
	}))
	defer redditSrv.Close()

	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	redditClient := reddit.NewClient("test-agent/1.0")
	redditClient.BaseURL = redditSrv.URL

	// downloadConcurrency left at its zero value: scrapePages should fall
	// back to defaultDownloadConcurrency rather than deadlock or panic.
	s := scraper{
		db:              gormDB,
		reddit:          redditClient,
		downloadClient:  mediaSrv.Client(),
		downloadRootDir: filepath.Join(dir, "downloads"),
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	var post models.Post
	if err := gormDB.First(&post).Error; err != nil {
		t.Fatalf("query post: %v", err)
	}
	if post.LocalPath == "" {
		t.Error("LocalPath is empty, want a downloaded file path")
	}
}
