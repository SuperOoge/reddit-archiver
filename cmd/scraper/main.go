// Command scraper scrapes a subreddit's "new" listing, stores the results
// in SQLite, and downloads any post that links directly to a media file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SuperOoge/reddit-archiver/internal/config"
	"github.com/SuperOoge/reddit-archiver/internal/db"
	"github.com/SuperOoge/reddit-archiver/internal/downloader"
	"github.com/SuperOoge/reddit-archiver/internal/models"
	"github.com/SuperOoge/reddit-archiver/internal/reddit"
	"gorm.io/gorm"
)

// defaultDownloadConcurrency is used when Config.DownloadConcurrency isn't
// set to a positive value.
const defaultDownloadConcurrency = 4

func main() {
	os.Exit(run())
}

func run() int {
	subreddit := flag.String("subreddit", "", "subreddit to scrape (required)")
	configPath := flag.String("config", "config.json", "path to config file")
	envPath := flag.String("env", ".env", "path to .env file")
	pages := flag.Int("pages", 1, "number of listing pages to fetch")
	flag.Parse()

	if *subreddit == "" {
		fmt.Fprintln(os.Stderr, "error: -subreddit is required")
		flag.Usage()
		return 2
	}

	cfg, err := config.Load(*configPath, *envPath)
	if err != nil {
		log.Printf("load config: %v", err)
		return 1
	}

	gormDB, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Printf("open database: %v", err)
		return 1
	}

	redditClient := reddit.NewClient(cfg.UserAgent)
	redditClient.Cookie = cfg.RedditCookie
	downloadClient := &http.Client{Timeout: 2 * time.Minute}

	s := scraper{
		db:                  gormDB,
		reddit:              redditClient,
		downloadClient:      downloadClient,
		downloadRootDir:     cfg.DownloadPath,
		downloadConcurrency: cfg.DownloadConcurrency,
	}
	if err := s.run(context.Background(), *subreddit, *pages); err != nil {
		log.Printf("scrape %s: %v", *subreddit, err)
		return 1
	}

	return 0
}

type scraper struct {
	db              *gorm.DB
	reddit          *reddit.Client
	downloadClient  *http.Client
	downloadRootDir string

	// downloadConcurrency caps how many downloadPost calls scrapePages runs
	// at once. <= 0 falls back to defaultDownloadConcurrency.
	downloadConcurrency int
}

func (s scraper) run(ctx context.Context, subreddit string, pages int) error {
	scrapeRun := models.ScrapeRun{
		Source:    models.SourceReddit,
		Target:    subreddit,
		Status:    models.ScrapeStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	if err := s.db.Create(&scrapeRun).Error; err != nil {
		return fmt.Errorf("record scrape start: %w", err)
	}

	found, downloaded, scrapeErr := s.scrapePages(ctx, subreddit, pages)

	finishedAt := time.Now().UTC()
	status := models.ScrapeStatusCompleted
	errMsg := ""
	if scrapeErr != nil {
		status = models.ScrapeStatusFailed
		errMsg = scrapeErr.Error()
	}
	updates := map[string]interface{}{
		"status":      status,
		"posts_found": found,
		"finished_at": finishedAt,
		"error":       errMsg,
	}
	if err := s.db.Model(&models.ScrapeRun{}).Where("id = ?", scrapeRun.ID).Updates(updates).Error; err != nil {
		return errors.Join(scrapeErr, fmt.Errorf("record scrape finish: %w", err))
	}

	if scrapeErr != nil {
		return scrapeErr
	}
	log.Printf("scraped %d post(s), downloaded %d new file(s) from r/%s", found, downloaded, subreddit)
	return nil
}

// scrapePages walks the subreddit's listing, storing every post it sees, and
// fans out media downloads to a bounded pool of workers so a scrape with a
// lot of direct-media posts doesn't download them one at a time.
func (s scraper) scrapePages(ctx context.Context, subreddit string, pages int) (found, downloaded int, err error) {
	destDir := filepath.Join(s.downloadRootDir, subreddit)

	concurrency := s.downloadConcurrency
	if concurrency <= 0 {
		concurrency = defaultDownloadConcurrency
	}

	jobs := make(chan models.Post)
	var downloadedCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if s.downloadPost(ctx, p, destDir) {
					downloadedCount.Add(1)
				}
			}
		}()
	}

	found, err = s.enqueuePages(ctx, subreddit, pages, jobs)

	close(jobs)
	wg.Wait()

	return found, int(downloadedCount.Load()), err
}

// enqueuePages walks the listing and sends every media post's stored row on
// jobs for a worker to download. It returns however many posts it stored
// before stopping, even on error, so the caller can still report progress.
func (s scraper) enqueuePages(ctx context.Context, subreddit string, pages int, jobs chan<- models.Post) (found int, err error) {
	after := ""

	for page := 0; page < pages; page++ {
		posts, next, err := s.reddit.Listing(ctx, subreddit, after, 100)
		if err != nil {
			return found, fmt.Errorf("fetch page %d: %w", page, err)
		}

		for _, p := range posts {
			stored, err := s.upsertPost(p)
			if err != nil {
				return found, fmt.Errorf("store post %s: %w", p.ExternalID, err)
			}
			found++

			if stored.LocalPath != "" || !downloader.LooksLikeMedia(stored.URL) {
				continue
			}
			select {
			case jobs <- stored:
			case <-ctx.Done():
				return found, ctx.Err()
			}
		}

		if next == "" {
			break
		}
		after = next
	}

	return found, nil
}

// upsertPost returns the stored row for p, creating it if this
// (source, external_id) pair hasn't been seen before. Re-running a scrape
// is safe: existing posts are returned as-is rather than overwritten, so
// download state already recorded on them is preserved.
func (s scraper) upsertPost(p models.Post) (models.Post, error) {
	var existing models.Post
	err := s.db.Where("source = ? AND external_id = ?", p.Source, p.ExternalID).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := s.db.Create(&p).Error; err != nil {
			return models.Post{}, err
		}
		return p, nil
	case err != nil:
		return models.Post{}, err
	default:
		return existing, nil
	}
}

// downloadPost fetches p's media to destDir and records the result on its
// row. Failures are logged and treated as non-fatal — one broken link
// shouldn't abort the rest of the scrape — so the caller learns only
// whether the download succeeded.
func (s scraper) downloadPost(ctx context.Context, p models.Post, destDir string) bool {
	result, err := downloader.Download(ctx, s.downloadClient, p.URL, destDir)
	if err != nil {
		log.Printf("download %s (post %s): %v", p.URL, p.ExternalID, err)
		return false
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{
		"local_path":    result.Path,
		"sha256":        result.SHA256,
		"downloaded_at": now,
	}
	if err := s.db.Model(&models.Post{}).Where("id = ?", p.ID).Updates(updates).Error; err != nil {
		log.Printf("record download for post %s: %v", p.ExternalID, err)
		return false
	}
	return true
}
