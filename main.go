package main

import (
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/MoritzFlu/lobbyist/internal/db"
	"github.com/MoritzFlu/lobbyist/internal/handlers"
	"github.com/MoritzFlu/lobbyist/internal/hub"
	"github.com/MoritzFlu/lobbyist/web"
)

func main() {
	// Parse flags with environment variable fallbacks.
	addr := flag.String("addr", getenv("LPP_ADDR", ":8080"), "listen address")
	dataDir := flag.String("data", getenv("LPP_DATA_DIR", "./data"), "data directory")
	steamCC := flag.String("cc", getenv("LPP_STEAM_CC", "de"), "Steam country code")
	users := flag.String("users", getenv("LPP_USERS", ""), "comma-separated initial usernames")
	sitePass := flag.String("password", getenv("LPP_PASSWORD", ""), "site-wide password (leave empty to disable)")
	basePath := flag.String("base-path", getenv("LPP_BASE_PATH", ""), "URL base path (e.g. /lan)")
	flag.Parse()

	// Normalize base path: strip trailing slash, ensure leading slash if non-empty.
	bp := strings.TrimRight(*basePath, "/")
	if bp != "" && !strings.HasPrefix(bp, "/") {
		bp = "/" + bp
	}

	// Initialise the database.
	database, err := db.New(*dataDir)
	if err != nil {
		log.Fatalf("failed to initialise database: %v", err)
	}
	defer database.Close()

	// Seed initial users from LPP_USERS if no users exist yet.
	if *users != "" {
		existingUsers, err := database.ListUsers()
		if err != nil {
			log.Fatalf("failed to list users: %v", err)
		}
		if len(existingUsers) == 0 {
			for _, name := range strings.Split(*users, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				id, err := database.CreateUser(name, "")
				if err != nil {
					log.Printf("warning: failed to create user %q: %v", name, err)
				} else {
					log.Printf("created user %q (id=%d)", name, id)
				}
			}
		}
	}

	// Initialise the SSE broadcast hub.
	h := hub.New()

	// Template function map shared across all template sets.
	funcMap := handlers.TemplateFuncs(bp)

	// Parse the base template set: layout + partials. This set does NOT
	// include page-specific templates (index.html, login.html) because they
	// both define the "content" block and would conflict.
	baseTmpl := template.Must(
		template.New("").Funcs(funcMap).ParseFS(
			web.Files, "templates/layout.html", "templates/partials/*.html",
		),
	)

	// Build per-page template sets by cloning the base and parsing each page
	// template into its own copy. This avoids the conflicting "content"
	// definitions problem.
	pages := make(map[string]*template.Template)
	for _, page := range []string{"index.html", "overview.html", "login.html", "gate.html"} {
		clone := template.Must(baseTmpl.Clone())
		template.Must(clone.ParseFS(web.Files, "templates/"+page))
		pages[page] = clone
	}

	// Initialise handlers.
	hand := handlers.New(database, h, baseTmpl, pages, *dataDir, *steamCC, bp)

	// Set up routes using Go 1.22+ pattern routing.
	mux := http.NewServeMux()

	// Serve static assets from the embedded filesystem (no auth needed).
	staticFS, err := fs.Sub(web.Files, "static")
	if err != nil {
		log.Fatalf("failed to create static sub-filesystem: %v", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Serve cached game images from the data directory.
	mux.Handle("GET /images/", http.StripPrefix("/images/", http.FileServer(http.Dir(*dataDir))))

	// Site password gate endpoint.
	mux.HandleFunc("GET /gate", hand.HandleGatePage)
	mux.HandleFunc("POST /gate", hand.HandleGateSubmit)

	// Page routes.
	mux.HandleFunc("GET /", hand.HandleIndex)
	mux.HandleFunc("GET /overview", hand.HandleOverview)
	mux.HandleFunc("GET /login", hand.HandleLoginPage)

	// User API routes.
	mux.HandleFunc("POST /api/users/login", hand.HandleLogin)
	mux.HandleFunc("POST /api/users/logout", hand.HandleLogout)
	mux.HandleFunc("GET /api/users/{id}/pin-form", hand.HandlePinForm)

	// Game API routes.
	mux.HandleFunc("POST /api/games", hand.HandleFetchGame)
	mux.HandleFunc("POST /api/games/confirm", hand.HandleConfirmGame)
	mux.HandleFunc("GET /api/games/custom", hand.HandleCustomGameForm)
	mux.HandleFunc("POST /api/games/custom", hand.HandleCreateCustomGame)
	mux.HandleFunc("GET /api/games/{id}/edit", hand.HandleEditGameForm)
	mux.HandleFunc("POST /api/games/{id}", hand.HandleUpdateGame)
	mux.HandleFunc("DELETE /api/games/{id}", hand.HandleDeleteGame)

	// Vote API route.
	mux.HandleFunc("POST /api/games/{id}/vote", hand.HandleVote)

	// SSE route.
	mux.HandleFunc("GET /events", hand.HandleSSE)

	// Wrap with site password middleware if a password is set.
	var handler http.Handler = mux
	if *sitePass != "" {
		log.Printf("site password protection enabled")
		handler = sitePasswordMiddleware(*sitePass, bp, mux)
	}

	// If a base path is set, strip it before the mux sees the request,
	// and redirect requests to the base path itself to base path + "/".
	if bp != "" {
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Redirect /lan to /lan/
			if r.URL.Path == bp {
				http.Redirect(w, r, bp+"/", http.StatusMovedPermanently)
				return
			}
			// Strip the base path prefix before routing.
			if strings.HasPrefix(r.URL.Path, bp+"/") {
				r.URL.Path = strings.TrimPrefix(r.URL.Path, bp)
				r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, bp)
				inner.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
		})
		log.Printf("base path: %s", bp)
	}

	log.Printf("Lobbyist listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}

// getenv returns the environment variable value or a default if unset.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// sitePasswordMiddleware checks for a "lpp_gate" cookie whose value matches the
// site password. If missing or wrong, it redirects to /gate. Static assets and
// the /gate endpoint itself are excluded from the check.
func sitePasswordMiddleware(password, basePath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always allow static assets, images, and the gate page itself.
		if strings.HasPrefix(r.URL.Path, "/static/") ||
			strings.HasPrefix(r.URL.Path, "/images/") ||
			r.URL.Path == "/gate" {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("lpp_gate")
		if err != nil || cookie.Value != password {
			http.Redirect(w, r, basePath+"/gate", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: lobbyist [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
}
