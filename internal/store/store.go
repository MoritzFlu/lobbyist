package store

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// httpClient is the shared HTTP client with a 15-second timeout.
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

// DetectStore inspects a raw URL and returns "steam", "gog", or "unknown".
func DetectStore(rawURL string) string {
	lower := strings.ToLower(rawURL)
	if strings.Contains(lower, "store.steampowered.com") {
		return "steam"
	}
	if strings.Contains(lower, "gog.com") {
		return "gog"
	}
	return "unknown"
}

// FetchGame detects whether the URL points to Steam or GOG and dispatches to
// the appropriate fetcher. It returns an error for unrecognised URLs.
func FetchGame(rawURL, dataDir, countryCode string) (*GameMetadata, error) {
	store := DetectStore(rawURL)
	switch store {
	case "steam":
		return fetchSteam(rawURL, dataDir, countryCode)
	case "gog":
		return fetchGOG(rawURL, dataDir, countryCode)
	default:
		return nil, fmt.Errorf("unrecognised store URL: %s", rawURL)
	}
}

// DownloadImage downloads the image at imageURL into dataDir/images/filename
// and returns the relative path "images/<filename>". If the destination file
// already exists the download is skipped and the path is returned immediately.
func DownloadImage(imageURL, dataDir, filename string) (string, error) {
	relPath := filepath.Join("images", filename)
	fullPath := filepath.Join(dataDir, relPath)

	// Skip if the file already exists.
	if _, err := os.Stat(fullPath); err == nil {
		return relPath, nil
	}

	// Ensure the images directory exists.
	imagesDir := filepath.Join(dataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return "", fmt.Errorf("creating images directory: %w", err)
	}

	resp, err := httpClient.Get(imageURL)
	if err != nil {
		return "", fmt.Errorf("downloading image %s: %w", imageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading image %s: HTTP %d", imageURL, resp.StatusCode)
	}

	out, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("creating image file %s: %w", fullPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("writing image file %s: %w", fullPath, err)
	}

	return relPath, nil
}
