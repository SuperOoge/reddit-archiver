package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	const body = "pretend this is image bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), srv.Client(), srv.URL+"/image.png", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	sum := sha256.Sum256([]byte(body))
	wantHash := hex.EncodeToString(sum[:])
	if res.SHA256 != wantHash {
		t.Errorf("SHA256 = %q, want %q", res.SHA256, wantHash)
	}
	if res.Deduped {
		t.Error("Deduped = true on first download, want false")
	}
	if filepath.Ext(res.Path) != ".png" {
		t.Errorf("Path = %q, want .png extension", res.Path)
	}

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != body {
		t.Errorf("file content = %q, want %q", data, body)
	}
}

func TestDownloadDedup(t *testing.T) {
	const body = "same bytes every time"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	first, err := Download(context.Background(), srv.Client(), srv.URL+"/a.jpg", dir)
	if err != nil {
		t.Fatalf("first Download: %v", err)
	}
	second, err := Download(context.Background(), srv.Client(), srv.URL+"/b.jpg", dir)
	if err != nil {
		t.Fatalf("second Download: %v", err)
	}

	if first.Path != second.Path {
		t.Errorf("paths differ: %q vs %q, want same (content-addressed)", first.Path, second.Path)
	}
	if !second.Deduped {
		t.Error("second.Deduped = false, want true")
	}
}

func TestDownloadErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, err := Download(context.Background(), srv.Client(), srv.URL+"/missing.png", dir); err == nil {
		t.Fatal("Download: expected error on 404, got nil")
	}
}

func TestDownloadQueryString(t *testing.T) {
	const body = "bytes behind a query string"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), srv.Client(), srv.URL+"/image.png?width=640&auto=webp", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if filepath.Ext(res.Path) != ".png" {
		t.Errorf("Path = %q, want .png extension (not the query string)", res.Path)
	}
}

func TestLooksLikeMedia(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://i.redd.it/abc123.jpg", true},
		{"https://i.redd.it/abc123.JPEG", true},
		{"https://i.imgur.com/abc123.png?width=640", true},
		{"https://example.com/video.mp4", true},
		{"https://www.reddit.com/r/golang/comments/abc123/some_title/", false},
		{"https://v.redd.it/abc123", false},
		{"https://imgur.com/a/abc123", false},
		{"not a url at all", false},
	}
	for _, tc := range cases {
		if got := LooksLikeMedia(tc.url); got != tc.want {
			t.Errorf("LooksLikeMedia(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
