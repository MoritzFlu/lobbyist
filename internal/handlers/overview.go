package handlers

import (
	"log"
	"net/http"
)

// HandleOverview renders a compact table of all games with every user's vote,
// plus per-user vote totals.
// GET /overview
func (h *Handlers) HandleOverview(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		http.Redirect(w, r, h.url("/login"), http.StatusSeeOther)
		return
	}

	users, err := h.DB.ListUsers()
	if err != nil {
		log.Printf("error listing users: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	games, err := h.DB.ListGames("votes")
	if err != nil {
		log.Printf("error listing games: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	summaries, err := h.DB.ListUserVoteSummaries()
	if err != nil {
		log.Printf("error listing vote summaries: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"CurrentUser": user,
		"Games":       games,
		"AllUsers":    users,
		"Summaries":   summaries,
		"Page":        "overview",
	}

	// HTMX refreshes (triggered by SSE updates) only need the body partial.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := h.Tmpl.ExecuteTemplate(w, "overview_body", data); err != nil {
			log.Printf("error rendering overview partial: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	if err := h.renderPage(w, "overview.html", data); err != nil {
		log.Printf("error rendering overview: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
