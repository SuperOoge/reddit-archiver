// Package models defines the database schema shared by every command.
package models

import "time"

// Source identifies which site a Post was scraped from. It's a distinct
// type (rather than folding "reddit" into every query) so a second source
// can be added later without touching existing rows or callers.
type Source string

// Recognized Source values.
const (
	SourceReddit Source = "reddit"
)

// Post is a single scraped item, e.g. a Reddit submission.
type Post struct {
	ID         uint   `gorm:"primaryKey"`
	Source     Source `gorm:"index;not null"`
	ExternalID string `gorm:"uniqueIndex:idx_source_external;not null"`
	Subreddit  string `gorm:"index"`
	Author     string
	Title      string
	URL        string `gorm:"not null"`
	Permalink  string
	CreatedAt  time.Time
	ScrapedAt  time.Time `gorm:"not null"`
}

// TableName pins the table name so it stays stable across GORM's
// pluralization rules regardless of the struct name.
func (Post) TableName() string { return "posts" }

// ScrapeStatus is the lifecycle state of a ScrapeRun.
type ScrapeStatus string

// Recognized ScrapeStatus values.
const (
	ScrapeStatusRunning   ScrapeStatus = "running"
	ScrapeStatusCompleted ScrapeStatus = "completed"
	ScrapeStatusFailed    ScrapeStatus = "failed"
)

// ScrapeRun records one invocation of a scrape against a target (a
// subreddit name), so history is auditable from the TUI.
type ScrapeRun struct {
	ID         uint         `gorm:"primaryKey"`
	Source     Source       `gorm:"index;not null"`
	Target     string       `gorm:"index;not null"`
	Status     ScrapeStatus `gorm:"not null"`
	PostsFound int
	StartedAt  time.Time `gorm:"not null"`
	FinishedAt *time.Time
	Error      string
}

// TableName pins the table name so it stays stable across GORM's
// pluralization rules regardless of the struct name.
func (ScrapeRun) TableName() string { return "scrape_runs" }
