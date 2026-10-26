package store

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// gogSlugRegex extracts the game slug from a GOG store URL.
var gogSlugRegex = regexp.MustCompile(`https?://(?:www\.)?gog\.com/(?:\w+/)?game/([a-z0-9_]+)`)

// ---------- JSON response structures ----------

type gogCatalogResponse struct {
	Products []gogProduct `json:"products"`
}

type gogProduct struct {
	ID               string             `json:"id"`
	Slug             string             `json:"slug"`
	Title            string             `json:"title"`
	CoverHorizontal  string             `json:"coverHorizontal"`
	Price            gogPrice           `json:"price"`
	Genres           []gogNamedItem     `json:"genres"`
	Tags             []gogNamedItem     `json:"tags"`
	Features         []gogNamedItem     `json:"features"`
	OperatingSystems []string           `json:"operatingSystems"`
	ReleaseDate      string             `json:"releaseDate"`
	StoreLink        string             `json:"storeLink"`
}

type gogPrice struct {
	Final      string    `json:"final"`
	Base       string    `json:"base"`
	FinalMoney gogMoney  `json:"finalMoney"`
	BaseMoney  gogMoney  `json:"baseMoney"`
}

type gogMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Discount string `json:"discount"`
}

type gogNamedItem struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// ---------- Fetcher ----------

// fetchGOG retrieves game metadata from the GOG catalog API.
func fetchGOG(rawURL, dataDir, countryCode string) (*GameMetadata, error) {
	// Extract slug from URL.
	matches := gogSlugRegex.FindStringSubmatch(rawURL)
	if matches == nil || len(matches) < 2 {
		return nil, fmt.Errorf("unable to parse GOG slug from URL: %s", rawURL)
	}
	slug := matches[1]

	// Build API URL.
	apiURL := fmt.Sprintf(
		"https://catalog.gog.com/v1/catalog?query=%s&limit=5",
		url.QueryEscape(slug),
	)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("requesting GOG catalog API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GOG catalog API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading GOG catalog API response: %w", err)
	}

	var catalog gogCatalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("decoding GOG catalog API response: %w", err)
	}

	if len(catalog.Products) == 0 {
		return nil, fmt.Errorf("no products found on GOG for slug %q", slug)
	}

	// Find best matching product by slug similarity.
	product := findBestSlugMatch(slug, catalog.Products)

	// Parse prices.
	priceCents, currency, discountPercent, err := parseGOGPrice(product.Price)
	if err != nil {
		return nil, fmt.Errorf("parsing GOG price: %w", err)
	}

	// Determine if free.
	isFree := priceCents != nil && *priceCents == 0

	// Map genres.
	genres := make([]string, 0, len(product.Genres))
	for _, g := range product.Genres {
		genres = append(genres, g.Name)
	}

	// Merge tags and features into tags.
	tags := make([]string, 0, len(product.Tags)+len(product.Features))
	for _, t := range product.Tags {
		tags = append(tags, t.Name)
	}
	for _, f := range product.Features {
		tags = append(tags, f.Name)
	}

	// Map operating systems.
	hasWindows, hasLinux, hasMac := false, false, false
	for _, os := range product.OperatingSystems {
		switch strings.ToLower(os) {
		case "windows":
			hasWindows = true
		case "linux":
			hasLinux = true
		case "osx":
			hasMac = true
		}
	}

	// Download cover image.
	imageFilename := fmt.Sprintf("gog_%s.jpg", product.ID)
	localImagePath := ""
	if product.CoverHorizontal != "" {
		path, err := DownloadImage(product.CoverHorizontal, dataDir, imageFilename)
		if err != nil {
			localImagePath = ""
		} else {
			localImagePath = path
		}
	}

	// Use the store link from the API response, or fall back to the raw URL.
	storeURL := product.StoreLink
	if storeURL == "" {
		storeURL = rawURL
	}

	meta := &GameMetadata{
		Title:            product.Title,
		ShortDescription: "",
		HeaderImageURL:   localImagePath,
		PriceCents:       priceCents,
		Currency:         currency,
		DiscountPercent:  discountPercent,
		IsFree:           isFree,
		ReleaseDate:      product.ReleaseDate,
		HasWindows:       hasWindows,
		HasLinux:         hasLinux,
		HasMac:           hasMac,
		DRMFree:          true,
		Genres:           genres,
		Tags:             tags,
		StoreURL:         storeURL,
		Source:           "gog",
		StoreProductID:   product.ID,
	}

	return meta, nil
}

// findBestSlugMatch selects the product whose slug is the closest match
// to the target slug. It prefers an exact match, then a prefix match,
// then falls back to the product with the longest common prefix.
func findBestSlugMatch(target string, products []gogProduct) gogProduct {
	bestIdx := 0
	bestScore := -1

	for i, p := range products {
		score := slugSimilarity(target, p.Slug)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return products[bestIdx]
}

// slugSimilarity returns a score for how similar two slugs are.
// Higher is better. Exact match gets the maximum score.
func slugSimilarity(target, candidate string) int {
	if target == candidate {
		return math.MaxInt32
	}

	// Prefix match in either direction is strong.
	if strings.HasPrefix(candidate, target) || strings.HasPrefix(target, candidate) {
		longer := len(target)
		if len(candidate) > longer {
			longer = len(candidate)
		}
		shorter := len(target)
		if len(candidate) < shorter {
			shorter = len(candidate)
		}
		// Score based on how much of the longer string is covered.
		return shorter * 1000 / longer
	}

	// Fall back to longest common prefix length.
	commonLen := 0
	for i := 0; i < len(target) && i < len(candidate); i++ {
		if target[i] != candidate[i] {
			break
		}
		commonLen++
	}

	return commonLen
}

// parseGOGPrice extracts price-in-cents, currency, and discount percent
// from GOG price data.
func parseGOGPrice(price gogPrice) (*int, string, int, error) {
	// Parse final amount.
	finalAmount, err := strconv.ParseFloat(price.FinalMoney.Amount, 64)
	if err != nil {
		return nil, "", 0, fmt.Errorf("parsing final amount %q: %w", price.FinalMoney.Amount, err)
	}
	cents := int(math.Round(finalAmount * 100))

	currency := price.FinalMoney.Currency

	// Calculate discount by comparing base and final.
	discountPercent := 0
	baseAmount, err := strconv.ParseFloat(price.BaseMoney.Amount, 64)
	if err == nil && baseAmount > 0 {
		discountPercent = int(math.Round((1.0 - finalAmount/baseAmount) * 100))
		if discountPercent < 0 {
			discountPercent = 0
		}
	}

	return &cents, currency, discountPercent, nil
}
