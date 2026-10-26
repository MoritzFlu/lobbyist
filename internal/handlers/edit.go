package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoritzFlu/lobbyist/internal/db"
	"github.com/MoritzFlu/lobbyist/internal/store"
)

// HandleCustomGameForm returns an empty game_form for adding a game that is
// not on Steam or GOG.
// GET /api/games/custom
func (h *Handlers) HandleCustomGameForm(w http.ResponseWriter, r *http.Request) {
	h.renderGameForm(w, &db.Game{Source: "custom"}, "")
}

// HandleCreateCustomGame creates a game from the custom game_form.
// POST /api/games/custom
func (h *Handlers) HandleCreateCustomGame(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	game := &db.Game{
		Source:   "custom",
		Currency: "EUR",
		AddedBy:  sql.NullInt64{Int64: user.ID, Valid: true},
	}
	tagNames, err := h.applyGameForm(r, game)
	if err != nil {
		h.renderGameForm(w, game, err.Error())
		return
	}

	tags := make([]db.Tag, 0, len(tagNames))
	for _, t := range tagNames {
		tags = append(tags, db.Tag{Tag: t, TagType: "tag"})
	}
	if _, err := h.DB.CreateGame(game, tags); err != nil {
		log.Printf("error creating custom game: %v", err)
		http.Error(w, "Failed to save game", http.StatusInternalServerError)
		return
	}

	h.broadcastUpdate()
	w.Header().Set("HX-Trigger", "games-changed")
	w.WriteHeader(http.StatusOK) // empty body removes the form
}

// HandleEditGameForm returns the game_form prefilled with an existing game.
// GET /api/games/{id}/edit
func (h *Handlers) HandleEditGameForm(w http.ResponseWriter, r *http.Request) {
	game := h.gameFromPath(w, r)
	if game == nil {
		return
	}
	h.renderGameForm(w, game, "")
}

// HandleUpdateGame saves the edit game_form.
// POST /api/games/{id}
func (h *Handlers) HandleUpdateGame(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	game := h.gameFromPath(w, r)
	if game == nil {
		return
	}

	tagNames, err := h.applyGameForm(r, game)
	if err != nil {
		h.renderGameForm(w, game, err.Error())
		return
	}
	if err := h.DB.UpdateGame(game, tagNames, user.ID); err != nil {
		log.Printf("error updating game %d: %v", game.ID, err)
		http.Error(w, "Failed to save game", http.StatusInternalServerError)
		return
	}

	h.broadcastUpdate()
	w.Header().Set("HX-Trigger", "games-changed")
	w.WriteHeader(http.StatusOK) // empty body removes the form
}

// gameFromPath loads the game named by the {id} path value, writing an error
// response and returning nil if that fails.
func (h *Handlers) gameFromPath(w http.ResponseWriter, r *http.Request) *db.Game {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid game ID", http.StatusBadRequest)
		return nil
	}
	game, err := h.DB.GetGame(id)
	if err != nil {
		log.Printf("error fetching game %d: %v", id, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}
	if game == nil {
		http.Error(w, "Game not found", http.StatusNotFound)
		return nil
	}
	return game
}

// applyGameForm copies the game_form fields onto game and returns the tag
// list. Store price fields are only editable for custom games; Steam/GOG
// prices come from the store. On a validation error the game still carries
// the submitted values so the form can be re-rendered.
func (h *Handlers) applyGameForm(r *http.Request, game *db.Game) ([]string, error) {
	if err := r.ParseForm(); err != nil {
		return nil, errors.New("invalid form data")
	}

	game.Title = strings.TrimSpace(r.FormValue("title"))
	game.ShortDescription = strings.TrimSpace(r.FormValue("short_description"))
	game.Note = strings.TrimSpace(r.FormValue("note"))
	game.StoreURL = strings.TrimSpace(r.FormValue("store_url"))
	game.KeyPriceURL = strings.TrimSpace(r.FormValue("key_price_url"))
	tags := parseTagList(r.FormValue("tags"))

	if game.Title == "" {
		return tags, errors.New("title is required")
	}

	keyPrice, err := parsePrice(r.FormValue("key_price"))
	if err != nil {
		return tags, fmt.Errorf("key price: %w", err)
	}
	game.KeyPriceCents = keyPrice

	if game.Source == "custom" {
		game.IsFree = r.FormValue("is_free") == "on"
		price, err := parsePrice(r.FormValue("price"))
		if err != nil {
			return tags, fmt.Errorf("price: %w", err)
		}
		game.PriceCents = price
		game.DiscountPercent = 0
		if game.IsFree {
			game.PriceCents = sql.NullInt64{}
		}
	}

	if imageURL := strings.TrimSpace(r.FormValue("image_url")); imageURL != "" {
		// Unique name so a replaced image is not served from the old file.
		filename := fmt.Sprintf("%s_%d.jpg", game.Source, time.Now().UnixNano())
		path, err := store.DownloadImage(imageURL, h.DataDir, filename)
		if err != nil {
			log.Printf("error downloading image: %v", err)
			return tags, errors.New("could not download image from that URL")
		}
		game.HeaderImagePath = path
	}

	return tags, nil
}

// renderGameForm renders the add/edit game_form partial.
func (h *Handlers) renderGameForm(w http.ResponseWriter, game *db.Game, errMsg string) {
	data := map[string]interface{}{
		"Game":  game,
		"Error": errMsg,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Tmpl.ExecuteTemplate(w, "game_form", data); err != nil {
		log.Printf("error rendering game_form: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// parsePrice parses a user-entered euro amount ("4.99", "4,99", "5") into
// cents. An empty string yields an invalid (NULL) value.
func parsePrice(s string) (sql.NullInt64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "€"))
	if s == "" {
		return sql.NullInt64{}, nil
	}
	f, err := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
	if err != nil || f < 0 {
		return sql.NullInt64{}, fmt.Errorf("%q is not a valid price", s)
	}
	return sql.NullInt64{Int64: int64(math.Round(f * 100)), Valid: true}, nil
}

// parseTagList splits a comma-separated tag string, trimming blanks and
// dropping duplicates.
func parseTagList(s string) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}

// tagListFunc joins a game's tags into the comma-separated form used by
// game_form.
func tagListFunc(tags []db.Tag) string {
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = t.Tag
	}
	return strings.Join(names, ", ")
}

// priceInputFunc formats cents for a price input, or "" if unset.
func priceInputFunc(cents sql.NullInt64) string {
	if !cents.Valid {
		return ""
	}
	return fmt.Sprintf("%.2f", float64(cents.Int64)/100)
}
