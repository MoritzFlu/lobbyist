package handlers

import (
	"log"
	"net/http"
	"strconv"
)

// HandleVote processes a vote on a game and returns the updated game card.
// POST /api/games/{id}/vote
func (h *Handlers) HandleVote(w http.ResponseWriter, r *http.Request) {
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

	voteType := r.FormValue("type")
	switch voteType {
	case "must_play", "up", "neutral", "down", "veto", "none":
	default:
		http.Error(w, "Invalid vote type", http.StatusBadRequest)
		return
	}

	if err := h.DB.SetVote(user.ID, gameID, voteType); err != nil {
		log.Printf("error setting vote: %v", err)
		http.Error(w, "Failed to save vote", http.StatusInternalServerError)
		return
	}

	// Re-fetch the game with updated votes and score.
	game, err := h.DB.GetGame(gameID)
	if err != nil {
		log.Printf("error fetching game %d: %v", gameID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if game == nil {
		http.Error(w, "Game not found", http.StatusNotFound)
		return
	}

	// Render the updated game card and return it.
	cardHTML, err := h.renderGameCard(game, user)
	if err != nil {
		log.Printf("error rendering game card: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Tell the voting client to refresh its games section (the vote table
	// buttons swap nothing themselves), even if its SSE connection is down.
	w.Header().Set("HX-Trigger", "games-changed")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(cardHTML))

	// Also notify all SSE clients so everyone stays in sync.
	h.broadcastUpdate()
}
