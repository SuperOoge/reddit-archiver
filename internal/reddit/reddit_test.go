package reddit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const fixtureListing = `{
	"data": {
		"after": "t3_next",
		"children": [
			{
				"data": {
					"id": "abc123",
					"subreddit": "golang",
					"author": "gopher",
					"title": "first post",
					"url": "https://example.com/one.png",
					"permalink": "/r/golang/comments/abc123/first_post/",
					"created_utc": 1700000000
				}
			},
			{
				"data": {
					"id": "def456",
					"subreddit": "golang",
					"author": "gopher2",
					"title": "second post",
					"url": "https://example.com/two.png",
					"permalink": "/r/golang/comments/def456/second_post/",
					"created_utc": 1700000100
				}
			}
		]
	}
}`

func TestClientListing(t *testing.T) {
	var gotUserAgent, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureListing))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, after, err := c.Listing(context.Background(), "golang", "", 50)
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}

	if gotUserAgent != "test-agent/1.0" {
		t.Errorf("User-Agent = %q, want test-agent/1.0", gotUserAgent)
	}
	if gotPath != "/r/golang/new.json" {
		t.Errorf("path = %q, want /r/golang/new.json", gotPath)
	}
	if gotQuery != "limit=50" {
		t.Errorf("query = %q, want limit=50", gotQuery)
	}
	if after != "t3_next" {
		t.Errorf("after = %q, want t3_next", after)
	}
	if len(posts) != 2 {
		t.Fatalf("len(posts) = %d, want 2", len(posts))
	}
	if posts[0].ExternalID != "abc123" || posts[0].Subreddit != "golang" {
		t.Errorf("posts[0] = %+v, unexpected", posts[0])
	}
	if posts[1].Permalink != "https://www.reddit.com/r/golang/comments/def456/second_post/" {
		t.Errorf("posts[1].Permalink = %q, unexpected", posts[1].Permalink)
	}
}

func TestClientListingPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "t3_cursor" {
			t.Errorf("after param = %q, want t3_cursor", r.URL.Query().Get("after"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"after":"","children":[]}}`))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, after, err := c.Listing(context.Background(), "golang", "t3_cursor", 0)
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if after != "" {
		t.Errorf("after = %q, want empty", after)
	}
	if len(posts) != 0 {
		t.Errorf("len(posts) = %d, want 0", len(posts))
	}
}

func TestClientListingErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	if _, _, err := c.Listing(context.Background(), "golang", "", 0); err == nil {
		t.Fatal("Listing: expected error on non-200 status, got nil")
	}
}
