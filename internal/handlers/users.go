package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
)

// HandleLoginPage renders the login page with all users.
// GET /login
func (h *Handlers) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	users, err := h.DB.ListUsers()
	if err != nil {
		log.Printf("error listing users: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Users": users,
	}

	if err := h.renderPage(w, "login.html", data); err != nil {
		log.Printf("error rendering login page: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleLogin authenticates a user and sets the session cookie.
// POST /api/users/login
func (h *Handlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.FormValue("user_id")
	if userIDStr == "" {
		http.Error(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user_id", http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByID(userID)
	if err != nil {
		log.Printf("error looking up user %d: %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// If the user has a PIN, validate it.
	if user.Pin != "" {
		pin := r.FormValue("pin")
		if pin != user.Pin {
			http.Error(w, "Incorrect PIN", http.StatusUnauthorized)
			return
		}
	}

	// Set the session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "lobbyist_user",
		Value:    strconv.FormatInt(user.ID, 10),
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60, // 30 days
		HttpOnly: true,
	})

	// Tell HTMX to redirect.
	w.Header().Set("HX-Redirect", h.url("/"))
	w.WriteHeader(http.StatusOK)
}

// HandleLogout clears the session cookie and redirects to the login page.
// POST /api/users/logout
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "lobbyist_user",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, h.url("/login"), http.StatusSeeOther)
}

// HandlePinForm returns a small HTML fragment with a PIN input form.
// GET /api/users/{id}/pin-form
func (h *Handlers) HandlePinForm(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByID(userID)
	if err != nil {
		log.Printf("error looking up user %d: %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<form hx-post="%s/api/users/login" hx-swap="none" hx-on::after-request="if(event.detail.successful) window.location.href='%s/'">
  <input type="hidden" name="user_id" value="%d">
  <p style="margin-bottom:10px;">Enter PIN for <strong>%s</strong></p>
  <input type="password" name="pin" class="pin-input" placeholder="PIN" autofocus>
  <button type="submit" class="btn-confirm" style="margin-top:10px;">Login</button>
</form>`, h.BasePath, h.BasePath, user.ID, user.Username)
}
