// Package downloader fetches media files to disk, naming them by content
// hash so a file already on disk is never fetched twice.
package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/corona10/goimagehash"
	"golang.org/x/image/webp"
)

// mediaExtensions are file extensions this downloader knows how to save as
// direct media. It intentionally doesn't cover indirect media — Reddit
// gallery pages, v.redd.it DASH manifests, imgur album pages — which need
// their own resolution step before there's a direct URL to fetch; see
// CONTRIBUTING.md.
var mediaExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".mp4":  true,
	".webm": true,
	".mov":  true,
}

// LooksLikeMedia reports whether rawURL points directly at a file this
// downloader can save, based on its extension.
func LooksLikeMedia(rawURL string) bool {
	return mediaExtensions[mediaExt(rawURL)]
}

// mediaExt returns the lowercased file extension from rawURL's path,
// ignoring any query string or fragment.
func mediaExt(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		return strings.ToLower(path.Ext(u.Path))
	}
	return strings.ToLower(path.Ext(rawURL))
}

// Result describes a completed (or deduplicated) download.
type Result struct {
	// Path is where the file ended up on disk.
	Path string
	// SHA256 is the hex-encoded content hash.
	SHA256 string
	// Deduped is true when a file with this hash already existed and the
	// download was skipped.
	Deduped bool
	// PHash is a difference-hash of the image content, serialized via
	// goimagehash's ToString, for near-duplicate detection by the caller.
	// Empty when sourceURL isn't a format we can decode as an image (e.g.
	// video) or decoding/hashing failed — neither case fails the download.
	PHash string
}

// Download fetches sourceURL and saves it under destDir, named by the
// SHA256 hash of its content plus the extension from sourceURL. If a file
// with that hash already exists, the fetch is short-circuited and Deduped
// is set to true — but since the hash is only known after downloading, a
// full fetch to a temp file happens either way; only the final rename is
// skipped when a duplicate is found.
func Download(ctx context.Context, client *http.Client, sourceURL, destDir string) (Result, error) {
	if client == nil {
		client = http.DefaultClient
	}

	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return Result{}, fmt.Errorf("create download dir %q: %w", destDir, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request for %q: %w", sourceURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("fetch %q: %w", sourceURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("fetch %q: unexpected status %s", sourceURL, resp.Status)
	}

	tmp, err := os.CreateTemp(destDir, ".download-*.tmp")
	if err != nil {
		return Result{}, fmt.Errorf("create temp file in %q: %w", destDir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once renamed

	hasher := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(resp.Body, hasher)); err != nil {
		_ = tmp.Close()
		return Result{}, fmt.Errorf("write %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return Result{}, fmt.Errorf("close %q: %w", tmpPath, err)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	finalPath := filepath.Join(destDir, sum+mediaExt(sourceURL))
	phash := perceptualHash(tmpPath, mediaExt(sourceURL))

	if _, err := os.Stat(finalPath); err == nil {
		return Result{Path: finalPath, SHA256: sum, Deduped: true, PHash: phash}, nil
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("stat %q: %w", finalPath, err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return Result{}, fmt.Errorf("rename %q to %q: %w", tmpPath, finalPath, err)
	}

	return Result{Path: finalPath, SHA256: sum, PHash: phash}, nil
}

// imageDecoders maps a lowercased file extension to the stdlib (or
// golang.org/x/image) decoder for that format. Video extensions are
// intentionally absent — there's no perceptual-hash equivalent for them.
var imageDecoders = map[string]func(io.Reader) (image.Image, error){
	".jpg":  jpeg.Decode,
	".jpeg": jpeg.Decode,
	".png":  png.Decode,
	".gif":  gif.Decode,
	".webp": webp.Decode,
}

// perceptualHash computes a difference-hash of the image at path, for
// near-duplicate detection by the caller. It returns "" if ext isn't a
// format imageDecoders knows, or if opening, decoding, or hashing fails —
// this is best-effort extra data, never a reason to fail the download.
func perceptualHash(path, ext string) string {
	decode, ok := imageDecoders[ext]
	if !ok {
		return ""
	}

	f, err := os.Open(path) // #nosec G304 -- path is our own just-written temp file, not user input
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	img, err := decode(f)
	if err != nil {
		return ""
	}

	hash, err := goimagehash.DifferenceHash(img)
	if err != nil {
		return ""
	}
	return hash.ToString()
}
