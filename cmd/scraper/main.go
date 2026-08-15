// Command scraper scrapes a subreddit's "new" listing and stores the
// results in SQLite.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/SuperOoge/reddit-archiver/internal/config"
	"github.com/SuperOoge/reddit-archiver/internal/db"
	"github.com/SuperOoge/reddit-archiver/internal/models"
	"github.com/SuperOoge/reddit-archiver/internal/reddit"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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

	client := reddit.NewClient(cfg.UserAgent)
	client.Cookie = cfg.RedditCookie

	if err := scrape(context.Background(), gormDB, client, *subreddit, *pages); err != nil {
		log.Printf("scrape %s: %v", *subreddit, err)
		return 1
	}

	return 0
}

func scrape(ctx context.Context, gormDB *gorm.DB, client *reddit.Client, subreddit string, pages int) error {
	run := models.ScrapeRun{
		Source:    models.SourceReddit,
		Target:    subreddit,
		Status:    models.ScrapeStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	if err := gormDB.Create(&run).Error; err != nil {
		return fmt.Errorf("record scrape start: %w", err)
	}

	found, scrapeErr := scrapePages(ctx, gormDB, client, subreddit, pages)

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
	if err := gormDB.Model(&models.ScrapeRun{}).Where("id = ?", run.ID).Updates(updates).Error; err != nil {
		return errors.Join(scrapeErr, fmt.Errorf("record scrape finish: %w", err))
	}

	if scrapeErr != nil {
		return scrapeErr
	}
	log.Printf("scraped %d post(s) from r/%s", found, subreddit)
	return nil
}

func scrapePages(ctx context.Context, gormDB *gorm.DB, client *reddit.Client, subreddit string, pages int) (int, error) {
	total := 0
	after := ""
	for page := 0; page < pages; page++ {
		posts, next, err := client.Listing(ctx, subreddit, after, 100)
		if err != nil {
			return total, fmt.Errorf("fetch page %d: %w", page, err)
		}

		if len(posts) > 0 {
			// Ignore rows whose (source, external_id) already exists so
			// re-running a scrape is safe.
			result := gormDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&posts)
			if result.Error != nil {
				return total, fmt.Errorf("store page %d: %w", page, result.Error)
			}
		}

		total += len(posts)
		if next == "" {
			break
		}
		after = next
	}
	return total, nil
}
