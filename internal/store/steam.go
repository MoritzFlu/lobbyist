package store

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// steamAppIDRegex extracts the numeric app ID from a Steam store URL.
var steamAppIDRegex = regexp.MustCompile(`https?://store\.steampowered\.com/app/(\d+)`)

// usefulCategories is the set of Steam categories we keep as tags.
var usefulCategories = map[string]bool{
	"Multi-player":                true,
	"Co-op":                       true,
	"Online Co-op":                true,
	"LAN Co-op":                   true,
	"PvP":                         true,
	"Online PvP":                  true,
	"LAN PvP":                     true,
	"Shared/Split Screen Co-op":   true,
	"Shared/Split Screen PvP":     true,
	"Cross-Platform Multiplayer":  true,
	"Single-player":               true,
}

// ---------- JSON response structures ----------

type steamResponse map[string]steamAppResult

type steamAppResult struct {
	Success bool          `json:"success"`
	Data    steamAppData  `json:"data"`
}

type steamAppData struct {
	Name             string              `json:"name"`
	ShortDescription string              `json:"short_description"`
	HeaderImage      string              `json:"header_image"`
	IsFree           bool                `json:"is_free"`
	PriceOverview    *steamPriceOverview `json:"price_overview"`
	Genres           []steamGenre        `json:"genres"`
	Categories       []steamCategory     `json:"categories"`
	Platforms        steamPlatforms      `json:"platforms"`
	ReleaseDate      steamReleaseDate    `json:"release_date"`
}

type steamPriceOverview struct {
	Currency        string `json:"currency"`
	Initial         int    `json:"initial"`
	Final           int    `json:"final"`
	DiscountPercent int    `json:"discount_percent"`
}

type steamGenre struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type steamCategory struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

type steamPlatforms struct {
	Windows bool `json:"windows"`
	Mac     bool `json:"mac"`
	Linux   bool `json:"linux"`
}

type steamReleaseDate struct {
	Date string `json:"date"`
}

// ---------- Fetcher ----------

// fetchSteam retrieves game metadata from the Steam API.
func fetchSteam(rawURL, dataDir, countryCode string) (*GameMetadata, error) {
	// Extract app ID.
	matches := steamAppIDRegex.FindStringSubmatch(rawURL)
	if matches == nil || len(matches) < 2 {
		return nil, fmt.Errorf("unable to parse Steam app ID from URL: %s", rawURL)
	}
	appID := matches[1]

	// Build API URL.
	apiURL := fmt.Sprintf("https://store.steampowered.com/api/appdetails?appids=%s&cc=%s", appID, countryCode)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("requesting Steam API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Steam API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading Steam API response: %w", err)
	}

	var result steamResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding Steam API response: %w", err)
	}

	appResult, ok := result[appID]
	if !ok {
		return nil, fmt.Errorf("Steam API response missing app ID %s", appID)
	}
	if !appResult.Success {
		return nil, fmt.Errorf("Steam API returned success=false for app ID %s", appID)
	}

	data := appResult.Data

	// Map genres.
	genres := make([]string, 0, len(data.Genres))
	for _, g := range data.Genres {
		genres = append(genres, g.Description)
	}

	// Filter categories to useful ones.
	tags := make([]string, 0, len(data.Categories))
	for _, c := range data.Categories {
		if usefulCategories[c.Description] {
			tags = append(tags, c.Description)
		}
	}

	// Handle price.
	var priceCents *int
	var currency string
	var discountPercent int

	if data.PriceOverview != nil {
		p := data.PriceOverview.Final
		priceCents = &p
		currency = data.PriceOverview.Currency
		discountPercent = data.PriceOverview.DiscountPercent
	}

	// Download header image.
	imageFilename := fmt.Sprintf("steam_%s.jpg", appID)
	localImagePath := ""
	if data.HeaderImage != "" {
		path, err := DownloadImage(data.HeaderImage, dataDir, imageFilename)
		if err != nil {
			// Non-fatal: we still return metadata even if image download fails.
			localImagePath = ""
		} else {
			localImagePath = path
		}
	}

	meta := &GameMetadata{
		Title:            data.Name,
		ShortDescription: data.ShortDescription,
		HeaderImageURL:   localImagePath,
		PriceCents:       priceCents,
		Currency:         currency,
		DiscountPercent:  discountPercent,
		IsFree:           data.IsFree,
		ReleaseDate:      data.ReleaseDate.Date,
		HasWindows:       data.Platforms.Windows,
		HasLinux:         data.Platforms.Linux,
		HasMac:           data.Platforms.Mac,
		DRMFree:          false,
		Genres:           genres,
		Tags:             tags,
		StoreURL:         rawURL,
		Source:           "steam",
		StoreProductID:   appID,
	}

	return meta, nil
}
