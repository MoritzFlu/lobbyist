package handlers

import (
	"net/http"
)

// HandleGatePage renders the site password form.
func (h *Handlers) HandleGatePage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Error": r.URL.Query().Get("error"),
	}
	h.renderPage(w, "gate.html", data)
}

// HandleGateSubmit checks the submitted password and sets the gate cookie.
func (h *Handlers) HandleGateSubmit(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if password == "" {
		http.Redirect(w, r, h.url("/gate?error=empty"), http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "lpp_gate",
		Value:    password,
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.url("/"), http.StatusSeeOther)
}
