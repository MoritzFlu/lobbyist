package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/MoritzFlu/lobbyist/internal/db"
	"github.com/MoritzFlu/lobbyist/internal/store"
)

// HandleIndex renders the main page with the game list.
// GET /
func (h *Handlers) HandleIndex(w http.ResponseWriter, r *http.Request) {
	// Only handle the exact root path — Go 1.22 "GET /" also catches all
	// unmatched GETs, so we reject anything that is not "/" exactly.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	user := h.currentUser(r)
	if user == nil {
		http.Redirect(w, r, h.url("/login"), http.StatusSeeOther)
		return
	}

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "votes"
	}
	tagFilter := r.URL.Query().Get("tag")

	data, err := h.gamesBodyData(sortBy, tagFilter, user)
	if err != nil {
		log.Printf("error loading games: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	data["Page"] = "games"

	// HTMX refreshes (triggered by SSE updates or the client's own votes)
	// only need the games body partial.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := h.Tmpl.ExecuteTemplate(w, "games_body", data); err != nil {
			log.Printf("error rendering games body partial: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	if err := h.renderPage(w, "index.html", data); err != nil {
		log.Printf("error rendering index: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// gamesBodyData builds the template data for the games_body partial: the
// score-ranked vote table of all games plus the sorted/filtered card list.
func (h *Handlers) gamesBodyData(sortBy, tagFilter string, user *db.User) (map[string]interface{}, error) {
	users, err := h.DB.ListUsers()
	if err != nil {
		return nil, err
	}

	ranked, err := h.DB.ListGames("votes")
	if err != nil {
		return nil, err
	}

	games := ranked
	if sortBy != "votes" {
		games, err = h.DB.ListGames(sortBy)
		if err != nil {
			return nil, err
		}
	}

	// Collect all unique tags before filtering.
	allTags := collectTags(games)

	// Filter by tag if specified.
	if tagFilter != "" {
		games = filterByTag(games, tagFilter)
	}

	unvoted := 0
	for _, g := range ranked {
		if voteOfFunc(g.Votes, user.ID) == "" {
			unvoted++
		}
	}

	return map[string]interface{}{
		"CurrentUser": user,
		"Games":       games,
		"Ranked":      ranked,
		"Unvoted":     unvoted,
		"AllTags":     allTags,
		"AllUsers":    users,
		"Sort":        sortBy,
		"ActiveTag":   tagFilter,
	}, nil
}

// HandleFetchGame fetches game metadata from a store URL and returns the
// add_game_form preview partial.
// POST /api/games
func (h *Handlers) HandleFetchGame(w http.ResponseWriter, r *http.Request) {
	rawURL := r.FormValue("url")
	if rawURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	meta, err := store.FetchGame(rawURL, h.DataDir, h.SteamCC)
	if err != nil {
		log.Printf("error fetching game from %s: %v", rawURL, err)
		http.Error(w, fmt.Sprintf("Failed to fetch game: %v", err), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Tmpl.ExecuteTemplate(w, "add_game_form", meta); err != nil {
		log.Printf("error rendering add_game_form: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleConfirmGame creates a new game from the preview form data, broadcasts
// the update, and returns the refreshed game list.
// POST /api/games/confirm
func (h *Handlers) HandleConfirmGame(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse form fields.
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")
	shortDesc := r.FormValue("short_description")
	note := r.FormValue("note")
	source := r.FormValue("source")
	storeProductID := r.FormValue("store_product_id")
	headerImageURL := r.FormValue("header_image_url")
	storeURL := r.FormValue("store_url")
	currency := r.FormValue("currency")
	releaseDate := r.FormValue("release_date")

	isFree := r.FormValue("is_free") == "true"
	drmFree := r.FormValue("drm_free") == "true"
	hasWindows := r.FormValue("has_windows") == "true"
	hasLinux := r.FormValue("has_linux") == "true"
	hasMac := r.FormValue("has_mac") == "true"

	discountPercent, _ := strconv.Atoi(r.FormValue("discount_percent"))

	var priceCents sql.NullInt64
	if pc := r.FormValue("price_cents"); pc != "" {
		val, err := strconv.ParseInt(pc, 10, 64)
		if err == nil {
			priceCents = sql.NullInt64{Int64: val, Valid: true}
		}
	}

	var storeProductIDNull sql.NullString
	if storeProductID != "" {
		storeProductIDNull = sql.NullString{String: storeProductID, Valid: true}
	}

	// Download the header image if a URL was provided.
	var headerImagePath string
	if headerImageURL != "" {
		filename := fmt.Sprintf("%s_%s.jpg", source, storeProductID)
		path, err := store.DownloadImage(headerImageURL, h.DataDir, filename)
		if err != nil {
			log.Printf("error downloading image: %v", err)
			// Continue without image — not a fatal error.
		} else {
			headerImagePath = path
		}
	}

	game := &db.Game{
		Source:           source,
		StoreProductID:   storeProductIDNull,
		Title:            title,
		ShortDescription: shortDesc,
		HeaderImagePath:  headerImagePath,
		PriceCents:       priceCents,
		Currency:         currency,
		DiscountPercent:  discountPercent,
		IsFree:           isFree,
		DRMFree:          drmFree,
		ReleaseDate:      releaseDate,
		HasWindows:       hasWindows,
		HasLinux:         hasLinux,
		HasMac:           hasMac,
		StoreURL:         storeURL,
		Note:             note,
		AddedBy:          sql.NullInt64{Int64: user.ID, Valid: true},
		KeyPriceURL:      strings.TrimSpace(r.FormValue("key_price_url")),
	}

	keyPrice, err := parsePrice(r.FormValue("key_price"))
	if err != nil {
		http.Error(w, "Invalid key price: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	game.KeyPriceCents = keyPrice

	// Build tags from genres and tags form fields.
	var tags []db.Tag
	for _, g := range r.Form["genres"] {
		if g != "" {
			tags = append(tags, db.Tag{Tag: g, TagType: "genre"})
		}
	}
	for _, t := range r.Form["tags"] {
		if t != "" {
			tags = append(tags, db.Tag{Tag: t, TagType: "tag"})
		}
	}

	_, err = h.DB.CreateGame(game, tags)
	if err != nil {
		log.Printf("error creating game: %v", err)
		http.Error(w, "Failed to save game", http.StatusInternalServerError)
		return
	}

	// Notify all SSE clients, and have the requesting client refresh its
	// games section. The empty body removes the preview card.
	h.broadcastUpdate()
	w.Header().Set("HX-Trigger", "games-changed")
	w.WriteHeader(http.StatusOK)
}

// HandleDeleteGame removes a game by ID and broadcasts the update.
// DELETE /api/games/{id}
func (h *Handlers) HandleDeleteGame(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	gameID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteGame(gameID); err != nil {
		log.Printf("error deleting game %d: %v", gameID, err)
		http.Error(w, "Failed to delete game", http.StatusInternalServerError)
		return
	}

	// Notify all SSE clients.
	h.broadcastUpdate()
	w.Header().Set("HX-Trigger", "games-changed")

	// Return empty body — HTMX will remove the card via outerHTML swap.
	w.WriteHeader(http.StatusOK)
}

// broadcastUpdate tells all SSE clients that games or votes changed. The
// content is irrelevant: each page re-fetches its own view, because the
// rendered HTML depends on who is logged in.
func (h *Handlers) broadcastUpdate() {
	h.Hub.Broadcast("refresh")
}
