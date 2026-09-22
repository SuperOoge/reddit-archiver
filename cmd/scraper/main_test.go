package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
	"github.com/corona10/goimagehash"
)

// checkerPNG renders a 16x16 checkerboard PNG. offset shifts the pattern by
// one cell; offset 0 vs 1 (a full inversion) empirically hashes to a
// difference-hash Hamming distance of 12 — safely above the default
// near-duplicate threshold of 8.
func checkerPNG(offset int) []byte {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			v := uint8(0)
			if (x+y+offset)%2 == 0 {
				v = 255
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// checkerPNGOnePixelChanged is checkerPNG(0) with a single pixel nudged to
// mid-gray. Empirically this hashes to a difference-hash Hamming distance of
// 4 from checkerPNG(0) — a near-duplicate under the default threshold of 8.
func checkerPNGOnePixelChanged() []byte {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			v := uint8(0)
			if (x+y)%2 == 0 {
				v = 255
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	img.SetGray(8, 8, color.Gray{Y: 128})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

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

func TestScraperRunDownloadsGalleryImages(t *testing.T) {
	const img1Body = "first gallery image"
	const img2Body = "second gallery image"

	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/img1.jpg":
			_, _ = w.Write([]byte(img1Body))
		case "/img2.jpg":
			_, _ = w.Write([]byte(img2Body))
		}
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{
					"data": {
						"id": "gallery1",
						"subreddit": "golang",
						"title": "a gallery post",
						"url": "https://www.reddit.com/gallery/gallery1",
						"permalink": "/r/golang/comments/gallery1/x/",
						"created_utc": 1700000000,
						"is_gallery": true,
						"gallery_data": {"items": [{"media_id": "a"}, {"media_id": "b"}]},
						"media_metadata": {
							"a": {"status": "valid", "e": "Image", "s": {"u": "` + mediaSrv.URL + `/img1.jpg"}},
							"b": {"status": "valid", "e": "Image", "s": {"u": "` + mediaSrv.URL + `/img2.jpg"}}
						}
					}
				}
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
		t.Fatalf("len(posts) = %d, want 2 (one row per gallery image)", len(posts))
	}

	for i, want := range []struct {
		externalID string
		body       string
	}{
		{"gallery1_a", img1Body},
		{"gallery1_b", img2Body},
	} {
		p := posts[i]
		if p.ExternalID != want.externalID {
			t.Errorf("posts[%d].ExternalID = %q, want %q", i, p.ExternalID, want.externalID)
		}
		if p.LocalPath == "" {
			t.Errorf("posts[%d]: LocalPath is empty, want a downloaded file path", i)
		}
		data, err := os.ReadFile(p.LocalPath)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", p.LocalPath, err)
		}
		if string(data) != want.body {
			t.Errorf("posts[%d] content = %q, want %q", i, data, want.body)
		}
	}

	var scrapeRun models.ScrapeRun
	if err := gormDB.First(&scrapeRun).Error; err != nil {
		t.Fatalf("query scrape run: %v", err)
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

func TestScraperMarksNearDuplicatePosts(t *testing.T) {
	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			_, _ = w.Write(checkerPNG(0))
		case "/b.png":
			_, _ = w.Write(checkerPNGOnePixelChanged())
		}
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{"data": {"id": "post-a", "subreddit": "golang", "url": "` + mediaSrv.URL + `/a.png", "permalink": "/r/golang/comments/post-a/x/", "created_utc": 1700000000}},
				{"data": {"id": "post-b", "subreddit": "golang", "url": "` + mediaSrv.URL + `/b.png", "permalink": "/r/golang/comments/post-b/x/", "created_utc": 1700000100}}
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
		// Concurrency 1 makes download order deterministic (post-a then
		// post-b), so post-b is guaranteed to see post-a's already-committed
		// hash when it looks for a near-duplicate.
		downloadConcurrency: 1,
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	var postA, postB models.Post
	if err := gormDB.Where("external_id = ?", "post-a").First(&postA).Error; err != nil {
		t.Fatalf("query post-a: %v", err)
	}
	if err := gormDB.Where("external_id = ?", "post-b").First(&postB).Error; err != nil {
		t.Fatalf("query post-b: %v", err)
	}

	if postA.PHash == "" || postB.PHash == "" {
		t.Fatal("expected both posts to have a computed PHash")
	}
	if postB.DuplicateOfID == nil || *postB.DuplicateOfID != postA.ID {
		t.Errorf("post-b.DuplicateOfID = %v, want %d (post-a)", postB.DuplicateOfID, postA.ID)
	}
	if postA.DuplicateOfID != nil {
		t.Errorf("post-a.DuplicateOfID = %v, want nil (downloaded first, nothing to compare against yet)", postA.DuplicateOfID)
	}
}

func TestScraperDoesNotMarkDissimilarPostsAsDuplicates(t *testing.T) {
	mediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			_, _ = w.Write(checkerPNG(0))
		case "/b.png":
			_, _ = w.Write(checkerPNG(1))
		}
	}))
	defer mediaSrv.Close()

	listing := `{
		"data": {
			"after": "",
			"children": [
				{"data": {"id": "post-a", "subreddit": "golang", "url": "` + mediaSrv.URL + `/a.png", "permalink": "/r/golang/comments/post-a/x/", "created_utc": 1700000000}},
				{"data": {"id": "post-b", "subreddit": "golang", "url": "` + mediaSrv.URL + `/b.png", "permalink": "/r/golang/comments/post-b/x/", "created_utc": 1700000100}}
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
		db:                  gormDB,
		reddit:              redditClient,
		downloadClient:      mediaSrv.Client(),
		downloadRootDir:     filepath.Join(dir, "downloads"),
		downloadConcurrency: 1,
	}

	if err := s.run(context.Background(), "golang", 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	var postB models.Post
	if err := gormDB.Where("external_id = ?", "post-b").First(&postB).Error; err != nil {
		t.Fatalf("query post-b: %v", err)
	}
	if postB.DuplicateOfID != nil {
		t.Errorf("post-b.DuplicateOfID = %v, want nil (images are too different to be near-duplicates)", postB.DuplicateOfID)
	}
}

func TestFindNearDuplicate(t *testing.T) {
	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	// Two 64-bit dHash values exactly 8 bits apart (differ in the low byte).
	const baseHash = "d:0000000000000000"
	const closeHash = "d:00000000000000ff" // Hamming distance 8 from baseHash
	const farHash = "d:ffffffffffffffff"   // Hamming distance 64 from baseHash

	seed := models.Post{Source: models.SourceReddit, ExternalID: "seed", URL: "https://example.com/seed.png", PHash: baseHash}
	if err := gormDB.Create(&seed).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}

	s := scraper{db: gormDB, perceptualHashDistance: 8}

	id, found := s.findNearDuplicate(seed.ID+1, closeHash)
	if !found || id != seed.ID {
		t.Errorf("closeHash: findNearDuplicate = (%d, %v), want (%d, true)", id, found, seed.ID)
	}

	if _, found := s.findNearDuplicate(seed.ID+1, farHash); found {
		t.Error("farHash: findNearDuplicate found a match, want none (distance exceeds threshold)")
	}

	if _, found := s.findNearDuplicate(seed.ID, closeHash); found {
		t.Error("querying with the seed's own ID excluded found a match, want none (it should never match itself)")
	}
}

func TestFindNearDuplicateDefaultsThresholdWhenUnset(t *testing.T) {
	dir := t.TempDir()
	gormDB, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}

	const baseHash = "d:0000000000000000"
	const closeHash = "d:00000000000000ff" // distance 8, within defaultPerceptualHashDistance

	seed := models.Post{Source: models.SourceReddit, ExternalID: "seed", URL: "https://example.com/seed.png", PHash: baseHash}
	if err := gormDB.Create(&seed).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}

	// perceptualHashDistance left at its zero value: should fall back to
	// defaultPerceptualHashDistance rather than matching nothing.
	s := scraper{db: gormDB}

	id, found := s.findNearDuplicate(seed.ID+1, closeHash)
	if !found || id != seed.ID {
		t.Errorf("findNearDuplicate = (%d, %v), want (%d, true)", id, found, seed.ID)
	}
}

func TestParseImageHash(t *testing.T) {
	hash, err := parseImageHash("d:00000000000000ff")
	if err != nil {
		t.Fatalf("parseImageHash: %v", err)
	}
	if hash.GetHash() != 0xff {
		t.Errorf("GetHash() = %#x, want 0xff", hash.GetHash())
	}
	if hash.GetKind() != goimagehash.DHash {
		t.Errorf("GetKind() = %v, want DHash", hash.GetKind())
	}
}

func TestParseImageHashRejectsMalformedInput(t *testing.T) {
	for _, s := range []string{"", "garbage", "d:notHex", "d:zz"} {
		if _, err := parseImageHash(s); err == nil {
			t.Errorf("parseImageHash(%q): expected an error, got nil", s)
		}
	}
}

func TestParseImageHashRejectsUnknownKind(t *testing.T) {
	if _, err := parseImageHash("z:00000000000000ff"); err == nil {
		t.Error("parseImageHash with kind \"z\": expected an error, got nil")
	}
}
