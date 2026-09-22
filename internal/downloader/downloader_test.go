package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// testPNG renders a small checkerboard PNG whose difference-hash won't be
// all-zero (a solid-color image hashes to 0, which makes for a weak test
// fixture), offset so callers can produce two distinguishable images.
func testPNG(t *testing.T, offset int) []byte {
	t.Helper()
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
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

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

func TestDownloadNilClientUsesDefault(t *testing.T) {
	const body = "bytes fetched via http.DefaultClient"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), nil, srv.URL+"/image.png", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.SHA256 == "" {
		t.Error("SHA256 is empty")
	}
}

func TestDownloadMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	// A regular file where a directory component is expected: MkdirAll
	// can't create anything under it.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("unreachable"))
	}))
	defer srv.Close()

	destDir := filepath.Join(blocker, "sub")
	if _, err := Download(context.Background(), srv.Client(), srv.URL+"/image.png", destDir); err == nil {
		t.Fatal("Download: expected error when destDir can't be created, got nil")
	}
}

func TestDownloadComputesPerceptualHashForImages(t *testing.T) {
	body := testPNG(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), srv.Client(), srv.URL+"/image.png", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.PHash == "" {
		t.Fatal("PHash is empty, want a computed hash")
	}
	if matched, _ := regexp.MatchString(`^d:[0-9a-f]{16}$`, res.PHash); !matched {
		t.Errorf("PHash = %q, want goimagehash's \"d:<16 hex chars>\" format", res.PHash)
	}
}

func TestDownloadPerceptualHashDistinguishesImages(t *testing.T) {
	dir := t.TempDir()

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(testPNG(t, 0))
	}))
	defer srv1.Close()
	res1, err := Download(context.Background(), srv1.Client(), srv1.URL+"/a.png", dir)
	if err != nil {
		t.Fatalf("Download a: %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(testPNG(t, 1))
	}))
	defer srv2.Close()
	res2, err := Download(context.Background(), srv2.Client(), srv2.URL+"/b.png", dir)
	if err != nil {
		t.Fatalf("Download b: %v", err)
	}

	if res1.PHash == res2.PHash {
		t.Error("an inverted checkerboard hashed identically to the original, want distinct hashes")
	}
}

func TestDownloadNoPerceptualHashForVideo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not actually a video, doesn't matter"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), srv.Client(), srv.URL+"/clip.mp4", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.PHash != "" {
		t.Errorf("PHash = %q, want empty for a video extension", res.PHash)
	}
}

func TestDownloadCorruptImageYieldsEmptyHashNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not a valid png"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	res, err := Download(context.Background(), srv.Client(), srv.URL+"/broken.png", dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.PHash != "" {
		t.Errorf("PHash = %q, want empty for undecodable image data", res.PHash)
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
