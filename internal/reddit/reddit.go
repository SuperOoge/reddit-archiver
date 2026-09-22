// Package reddit fetches subreddit listings from Reddit's public JSON API.
package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/SuperOoge/reddit-archiver/internal/models"
)

// DefaultBaseURL is Reddit's JSON API root. Tests override this via
// Client.BaseURL to point at a local httptest server.
const DefaultBaseURL = "https://www.reddit.com"

// Client fetches subreddit listings.
type Client struct {
	HTTPClient *http.Client
	UserAgent  string
	BaseURL    string
	Cookie     string

	// sleep is used by ListingAll to wait out Reddit's rate limit between
	// pages. Tests override it to avoid real delays; nil means time.Sleep.
	sleep func(context.Context, time.Duration)
}

// NewClient returns a Client configured with sane defaults.
func NewClient(userAgent string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		UserAgent:  userAgent,
		BaseURL:    DefaultBaseURL,
	}
}

// listingResponse mirrors the subset of Reddit's Thing/Listing JSON shape
// this client needs. See https://www.reddit.com/dev/api for the full shape.
type listingResponse struct {
	Data struct {
		After    string `json:"after"`
		Children []struct {
			Data childData `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// childData is one post's fields from a listing response.
type childData struct {
	ID        string  `json:"id"`
	Subreddit string  `json:"subreddit"`
	Author    string  `json:"author"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Permalink string  `json:"permalink"`
	CreatedUT float64 `json:"created_utc"`

	// Gallery posts (reddit.com/gallery/...) hold multiple images; the
	// direct URL for each is in MediaMetadata, ordered by GalleryData.
	// See expandPost.
	IsGallery   bool `json:"is_gallery"`
	GalleryData *struct {
		Items []struct {
			MediaID string `json:"media_id"`
		} `json:"items"`
	} `json:"gallery_data"`
	MediaMetadata map[string]struct {
		Status string `json:"status"`
		E      string `json:"e"` // "Image", "AnimatedImage", ...
		S      struct {
			U   string `json:"u"`   // direct URL, Image items
			GIF string `json:"gif"` // direct URL, AnimatedImage items
			MP4 string `json:"mp4"` // direct URL, AnimatedImage items
		} `json:"s"`
	} `json:"media_metadata"`
}

// Listing fetches one page of a subreddit's "new" feed. after is the
// pagination cursor from a previous call's return value; pass "" for the
// first page. It returns the posts on the page and the cursor for the next
// page (empty when there are no more pages).
func (c *Client) Listing(ctx context.Context, subreddit, after string, limit int) ([]models.Post, string, error) {
	posts, next, _, err := c.listingPage(ctx, subreddit, after, limit)
	return posts, next, err
}

// ListingAll fetches every page of a subreddit's "new" feed, following the
// after cursor itself until Reddit returns none or maxPages pages have been
// fetched (maxPages <= 0 means no page limit). Between pages it backs off
// according to Reddit's X-Ratelimit-* response headers so a large scrape
// doesn't get rate-limited.
func (c *Client) ListingAll(ctx context.Context, subreddit string, limit, maxPages int) ([]models.Post, error) {
	var all []models.Post
	after := ""

	for page := 0; maxPages <= 0 || page < maxPages; page++ {
		posts, next, header, err := c.listingPage(ctx, subreddit, after, limit)
		if err != nil {
			return all, fmt.Errorf("fetch page %d: %w", page, err)
		}
		all = append(all, posts...)

		if next == "" {
			break
		}
		after = next

		if delay := rateLimitDelay(header); delay > 0 {
			if c.sleep != nil {
				c.sleep(ctx, delay)
			} else {
				t := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					t.Stop()
				case <-t.C:
				}
			}
			if err := ctx.Err(); err != nil {
				return all, err
			}
		}
	}

	return all, nil
}

// minRateLimitRemaining is the X-Ratelimit-Remaining threshold below which
// ListingAll pauses until the window resets, rather than hitting Reddit's
// rate limit outright.
const minRateLimitRemaining = 2

// rateLimitDelay inspects Reddit's X-Ratelimit-Remaining and
// X-Ratelimit-Reset response headers and returns how long to wait before
// the next request, or 0 if there's no need to back off.
func rateLimitDelay(header http.Header) time.Duration {
	remaining, err := strconv.ParseFloat(header.Get("X-Ratelimit-Remaining"), 64)
	if err != nil || remaining > minRateLimitRemaining {
		return 0
	}

	reset, err := strconv.Atoi(header.Get("X-Ratelimit-Reset"))
	if err != nil || reset <= 0 {
		return 0
	}

	return time.Duration(reset) * time.Second
}

// listingPage fetches one page of a subreddit's "new" feed and returns the
// raw response headers alongside the parsed result, so callers can inspect
// rate-limit info that Listing's public signature doesn't expose.
func (c *Client) listingPage(ctx context.Context, subreddit, after string, limit int) ([]models.Post, string, http.Header, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	// raw_json=1 asks Reddit not to HTML-entity-escape string values —
	// otherwise gallery image URLs (which contain query strings) come
	// back with "&" as "&amp;", breaking them as URLs.
	u := fmt.Sprintf("%s/r/%s/new.json?limit=%d&raw_json=1", c.BaseURL, url.PathEscape(subreddit), limit)
	if after != "" {
		u += "&after=" + url.QueryEscape(after)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", nil, fmt.Errorf("fetch %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("fetch %s: unexpected status %s", u, resp.Status)
	}

	var listing listingResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, "", nil, fmt.Errorf("decode response from %s: %w", u, err)
	}

	now := time.Now().UTC()
	posts := make([]models.Post, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		posts = append(posts, expandPost(child.Data, now)...)
	}

	return posts, listing.Data.After, resp.Header, nil
}

// expandPost converts one listing item into the Post row(s) it represents.
// Almost every post is one row; a gallery post (IsGallery, with resolvable
// GalleryData/MediaMetadata) becomes one row per image, each with its own
// direct URL and an ExternalID of "<post ID>_<media ID>" so they coexist
// under the (source, external_id) uniqueness constraint. A gallery post
// whose media data is missing, or that has no resolvable items, falls back
// to a single row using its own (gallery-page, non-direct) URL — same as
// any other post type LooksLikeMedia doesn't recognize — so the post is
// still recorded rather than silently dropped.
func expandPost(d childData, scrapedAt time.Time) []models.Post {
	base := models.Post{
		Source:     models.SourceReddit,
		ExternalID: d.ID,
		Subreddit:  d.Subreddit,
		Author:     d.Author,
		Title:      d.Title,
		URL:        d.URL,
		Permalink:  "https://www.reddit.com" + d.Permalink,
		CreatedAt:  time.Unix(int64(d.CreatedUT), 0).UTC(),
		ScrapedAt:  scrapedAt,
	}

	if !d.IsGallery || d.GalleryData == nil || len(d.GalleryData.Items) == 0 {
		return []models.Post{base}
	}

	var posts []models.Post
	for _, item := range d.GalleryData.Items {
		meta, ok := d.MediaMetadata[item.MediaID]
		if !ok || meta.Status != "valid" {
			continue
		}

		var mediaURL string
		switch meta.E {
		case "Image":
			mediaURL = meta.S.U
		case "AnimatedImage":
			mediaURL = meta.S.GIF
			if mediaURL == "" {
				mediaURL = meta.S.MP4
			}
		}
		if mediaURL == "" {
			continue
		}

		post := base
		post.ExternalID = d.ID + "_" + item.MediaID
		post.URL = mediaURL
		posts = append(posts, post)
	}

	if len(posts) == 0 {
		return []models.Post{base}
	}
	return posts
}
