package reddit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if gotQuery != "limit=50&raw_json=1" {
		t.Errorf("query = %q, want limit=50&raw_json=1", gotQuery)
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

func TestClientListingAllPaginatesUntilExhausted(t *testing.T) {
	afters := []string{"t3_page2", "t3_page3", ""}
	var requests []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query().Get("after"))
		page := len(requests) - 1
		w.Header().Set("Content-Type", "application/json")
		body := fmt.Sprintf(`{"data":{"after":%q,"children":[{"data":{"id":"post%d","subreddit":"golang","permalink":"/r/golang/comments/post%d/x/","created_utc":1700000000}}]}}`,
			afters[page], page, page)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, err := c.ListingAll(context.Background(), "golang", 0, 0)
	if err != nil {
		t.Fatalf("ListingAll: %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("len(posts) = %d, want 3", len(posts))
	}
	if want := []string{"", "t3_page2", "t3_page3"}; fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Errorf("requested after values = %v, want %v", requests, want)
	}
}

func TestClientListingAllRespectsMaxPages(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		body := fmt.Sprintf(`{"data":{"after":"t3_next","children":[{"data":{"id":"post%d","subreddit":"golang","permalink":"/x/","created_utc":1700000000}}]}}`, requestCount)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, err := c.ListingAll(context.Background(), "golang", 0, 2)
	if err != nil {
		t.Fatalf("ListingAll: %v", err)
	}
	if requestCount != 2 {
		t.Errorf("requestCount = %d, want 2", requestCount)
	}
	if len(posts) != 2 {
		t.Errorf("len(posts) = %d, want 2", len(posts))
	}
}

func TestClientListingAllBacksOffOnRateLimit(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Ratelimit-Remaining", "1")
		w.Header().Set("X-Ratelimit-Reset", "30")
		if requestCount >= 2 {
			_, _ = w.Write([]byte(`{"data":{"after":"","children":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"after":"t3_next","children":[]}}`))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	var slept time.Duration
	c.sleep = func(_ context.Context, d time.Duration) { slept = d }

	if _, err := c.ListingAll(context.Background(), "golang", 0, 0); err != nil {
		t.Fatalf("ListingAll: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}
	if slept != 30*time.Second {
		t.Errorf("slept = %v, want 30s", slept)
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

func TestClientListingDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	if _, _, err := c.Listing(context.Background(), "golang", "", 0); err == nil {
		t.Fatal("Listing: expected error on invalid JSON, got nil")
	}
}

func TestClientListingSendsCookie(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"after":"","children":[]}}`))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL
	c.Cookie = "session=abc123"

	if _, _, err := c.Listing(context.Background(), "golang", "", 0); err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if gotCookie != "session=abc123" {
		t.Errorf("Cookie header = %q, want session=abc123", gotCookie)
	}
}

func TestClientListingAllPropagatesFetchError(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount >= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"after":"t3_next","children":[]}}`))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	_, err := c.ListingAll(context.Background(), "golang", 0, 0)
	if err == nil {
		t.Fatal("ListingAll: expected error when a page fetch fails, got nil")
	}
	if !strings.Contains(err.Error(), "fetch page 1") {
		t.Errorf("error = %q, want it to mention \"fetch page 1\"", err.Error())
	}
}

func TestClientListingAllContextCanceledDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Ratelimit-Remaining", "1")
		w.Header().Set("X-Ratelimit-Reset", "1")
		_, _ = w.Write([]byte(`{"data":{"after":"t3_next","children":[]}}`))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.ListingAll(ctx, "golang", 0, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

const fixtureGalleryListing = `{
	"data": {
		"after": "",
		"children": [
			{
				"data": {
					"id": "normal1",
					"subreddit": "golang",
					"title": "an ordinary post",
					"url": "https://example.com/normal.png",
					"permalink": "/r/golang/comments/normal1/x/",
					"created_utc": 1700000000
				}
			},
			{
				"data": {
					"id": "gallery1",
					"subreddit": "golang",
					"title": "a gallery post",
					"url": "https://www.reddit.com/gallery/gallery1",
					"permalink": "/r/golang/comments/gallery1/x/",
					"created_utc": 1700000100,
					"is_gallery": true,
					"gallery_data": {
						"items": [
							{"media_id": "img1"},
							{"media_id": "img2"},
							{"media_id": "broken"}
						]
					},
					"media_metadata": {
						"img1": {
							"status": "valid",
							"e": "Image",
							"s": {"u": "https://i.redd.it/img1.jpg?width=1080&auto=webp&s=abc"}
						},
						"img2": {
							"status": "valid",
							"e": "AnimatedImage",
							"s": {"gif": "https://i.redd.it/img2.gif", "mp4": "https://i.redd.it/img2.mp4"}
						},
						"broken": {
							"status": "failed",
							"e": "Image",
							"s": {}
						}
					}
				}
			}
		]
	}
}`

func TestClientListingExpandsGalleryIntoMultiplePosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixtureGalleryListing))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, _, err := c.Listing(context.Background(), "golang", "", 0)
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}

	// 1 normal post + 2 usable gallery images (the "broken" item is skipped).
	if len(posts) != 3 {
		t.Fatalf("len(posts) = %d, want 3: %+v", len(posts), posts)
	}

	normal, img1, img2 := posts[0], posts[1], posts[2]

	if normal.ExternalID != "normal1" || normal.URL != "https://example.com/normal.png" {
		t.Errorf("posts[0] (normal) = %+v, unexpected", normal)
	}

	if img1.ExternalID != "gallery1_img1" {
		t.Errorf("img1.ExternalID = %q, want gallery1_img1", img1.ExternalID)
	}
	if img1.URL != "https://i.redd.it/img1.jpg?width=1080&auto=webp&s=abc" {
		t.Errorf("img1.URL = %q, unexpected", img1.URL)
	}
	if img1.Title != "a gallery post" || img1.Permalink != "https://www.reddit.com/r/golang/comments/gallery1/x/" {
		t.Errorf("img1 = %+v, want Title/Permalink inherited from the gallery post", img1)
	}

	if img2.ExternalID != "gallery1_img2" {
		t.Errorf("img2.ExternalID = %q, want gallery1_img2", img2.ExternalID)
	}
	if img2.URL != "https://i.redd.it/img2.gif" {
		t.Errorf("img2.URL = %q, want the gif URL preferred over mp4", img2.URL)
	}
}

func TestClientListingGalleryFallsBackWithoutMediaData(t *testing.T) {
	listing := `{
		"data": {
			"after": "",
			"children": [
				{
					"data": {
						"id": "gallery2",
						"subreddit": "golang",
						"url": "https://www.reddit.com/gallery/gallery2",
						"permalink": "/r/golang/comments/gallery2/x/",
						"created_utc": 1700000000,
						"is_gallery": true
					}
				}
			]
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listing))
	}))
	defer srv.Close()

	c := NewClient("test-agent/1.0")
	c.BaseURL = srv.URL

	posts, _, err := c.Listing(context.Background(), "golang", "", 0)
	if err != nil {
		t.Fatalf("Listing: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("len(posts) = %d, want 1 (fallback to a single row)", len(posts))
	}
	if posts[0].ExternalID != "gallery2" {
		t.Errorf("ExternalID = %q, want gallery2 (unchanged, not expanded)", posts[0].ExternalID)
	}
	if posts[0].URL != "https://www.reddit.com/gallery/gallery2" {
		t.Errorf("URL = %q, want the original gallery page URL", posts[0].URL)
	}
}
