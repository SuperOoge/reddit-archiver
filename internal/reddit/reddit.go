// Package reddit fetches subreddit listings from Reddit's public JSON API.
package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
			Data struct {
				ID        string  `json:"id"`
				Subreddit string  `json:"subreddit"`
				Author    string  `json:"author"`
				Title     string  `json:"title"`
				URL       string  `json:"url"`
				Permalink string  `json:"permalink"`
				CreatedUT float64 `json:"created_utc"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// Listing fetches one page of a subreddit's "new" feed. after is the
// pagination cursor from a previous call's return value; pass "" for the
// first page. It returns the posts on the page and the cursor for the next
// page (empty when there are no more pages).
func (c *Client) Listing(ctx context.Context, subreddit, after string, limit int) ([]models.Post, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	u := fmt.Sprintf("%s/r/%s/new.json?limit=%d", c.BaseURL, url.PathEscape(subreddit), limit)
	if after != "" {
		u += "&after=" + url.QueryEscape(after)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: unexpected status %s", u, resp.Status)
	}

	var listing listingResponse
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, "", fmt.Errorf("decode response from %s: %w", u, err)
	}

	now := time.Now().UTC()
	posts := make([]models.Post, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		d := child.Data
		posts = append(posts, models.Post{
			Source:     models.SourceReddit,
			ExternalID: d.ID,
			Subreddit:  d.Subreddit,
			Author:     d.Author,
			Title:      d.Title,
			URL:        d.URL,
			Permalink:  "https://www.reddit.com" + d.Permalink,
			CreatedAt:  time.Unix(int64(d.CreatedUT), 0).UTC(),
			ScrapedAt:  now,
		})
	}

	return posts, listing.Data.After, nil
}
