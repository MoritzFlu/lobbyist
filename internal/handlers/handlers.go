package handlers

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"

	"lan-party-planner/internal/db"
	"lan-party-planner/internal/hub"
)

// Handlers holds shared dependencies for all HTTP handlers.
type Handlers struct {
	DB       *db.DB
	Hub      *hub.Hub
	Tmpl     *template.Template
	Pages    map[string]*template.Template
	DataDir  string
	SteamCC  string
	BasePath string // e.g. "/lan" or "" for root
}

// New creates a new Handlers instance.
func New(database *db.DB, h *hub.Hub, tmpl *template.Template, pages map[string]*template.Template, dataDir, steamCC, basePath string) *Handlers {
	return &Handlers{
		DB:       database,
		Hub:      h,
		Tmpl:     tmpl,
		Pages:    pages,
		DataDir:  dataDir,
		SteamCC:  steamCC,
		BasePath: basePath,
	}
}

// url prefixes a path with the base path.
func (h *Handlers) url(path string) string {
	return h.BasePath + path
}

// currentUser reads the "lpp_user" cookie, parses the user ID, and looks up
// the user in the database. Returns nil if the cookie is missing, invalid, or
// the user is not found.
func (h *Handlers) currentUser(r *http.Request) *db.User {
	cookie, err := r.Cookie("lpp_user")
	if err != nil {
		return nil
	}
	id, err := strconv.ParseInt(cookie.Value, 10, 64)
	if err != nil {
		return nil
	}
	user, err := h.DB.GetUserByID(id)
	if err != nil {
		log.Printf("error looking up user %d: %v", id, err)
		return nil
	}
	return user
}

// renderPage executes a page template by name (e.g., "index.html" or
// "login.html"). Each page template is a clone of the base layout with its
// own "content" block defined.
func (h *Handlers) renderPage(w http.ResponseWriter, page string, data interface{}) error {
	t, ok := h.Pages[page]
	if !ok {
		log.Printf("unknown page template: %s", page)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}
	return t.ExecuteTemplate(w, "layout.html", data)
}

// renderGameCard renders a single game_card partial into a string for SSE
// broadcast. It fetches all users to populate the vote breakdown.
func (h *Handlers) renderGameCard(game *db.Game, currentUser *db.User) (string, error) {
	users, err := h.DB.ListUsers()
	if err != nil {
		return "", err
	}

	data := map[string]interface{}{
		"Game":        *game,
		"CurrentUser": currentUser,
		"AllUsers":    users,
	}

	var buf bytes.Buffer
	if err := h.Tmpl.ExecuteTemplate(&buf, "game_card", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// collectTags returns a sorted, deduplicated list of tag strings from the
// given games.
func collectTags(games []db.Game) []string {
	seen := make(map[string]struct{})
	for _, g := range games {
		for _, t := range g.Tags {
			seen[t.Tag] = struct{}{}
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// filterByTag returns only games that have at least one tag matching the given
// filter string.
func filterByTag(games []db.Game, tag string) []db.Game {
	var filtered []db.Game
	for _, g := range games {
		for _, t := range g.Tags {
			if t.Tag == tag {
				filtered = append(filtered, g)
				break
			}
		}
	}
	return filtered
}

// TemplateFuncs returns the custom template function map used by both the
// handlers and main.go for template parsing. basePath is the URL prefix
// (e.g. "/lan" or "").
func TemplateFuncs(basePath string) template.FuncMap {
	return template.FuncMap{
		"dict":            dictFunc,
		"divf":            divfFunc,
		"discountedPrice": discountedPriceFunc,
		"vetoers":         vetoersFunc,
		"voteOf":          voteOfFunc,
		"voteOptions":     voteOptionsFunc,
		"voteIcon":        voteIconFunc,
		"inc":             func(i int) int { return i + 1 },
		"tagList":         tagListFunc,
		"priceInput":      priceInputFunc,
		"base": func() string {
			return basePath
		},
	}
}

// dictFunc creates a map[string]interface{} from alternating key-value pairs.
func dictFunc(pairs ...interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			continue
		}
		m[key] = pairs[i+1]
	}
	return m
}

// divfFunc divides an integer value by a divisor and returns a float64.
// It handles int64, int, and *int types for the numerator.
func divfFunc(a interface{}, b int) float64 {
	switch v := a.(type) {
	case int64:
		return float64(v) / float64(b)
	case int:
		return float64(v) / float64(b)
	case *int:
		if v == nil {
			return 0
		}
		return float64(*v) / float64(b)
	default:
		return 0
	}
}

// discountedPriceFunc calculates the discounted price in cents.
// originalCents * (100 - discountPercent) / 100
func discountedPriceFunc(originalCents int64, discountPercent int) int64 {
	return originalCents * int64(100-discountPercent) / 100
}

// vetoersFunc returns the usernames of everyone who vetoed, in vote order.
func vetoersFunc(votes []db.Vote) []string {
	var names []string
	for _, v := range votes {
		if v.VoteType == "veto" {
			names = append(names, v.Username)
		}
	}
	return names
}

// voteOfFunc returns the vote type a user cast on a game, or "" if none.
func voteOfFunc(votes []db.Vote, userID int64) string {
	for _, v := range votes {
		if v.UserID == userID {
			return v.VoteType
		}
	}
	return ""
}

// VoteOption describes one vote button: its stored type, icon and tooltip.
type VoteOption struct {
	Type  string
	Icon  string
	Title string
}

// voteOptions lists the vote types from best to worst. Scores must match the
// CASE expression in db.ListGames / db.GetGame.
var voteOptions = []VoteOption{
	{"must_play", "\u2B50", "Must play (+2)"},
	{"up", "\U0001F44D", "Thumbs up (+1)"},
	{"neutral", "\U0001F610", "Neutral \u2014 seen it (0)"},
	{"down", "\U0001F44E", "Thumbs down (\u22121)"},
	{"veto", "\U0001F6AB", "Veto (\u22122, sinks the game)"},
}

// voteOptionsFunc returns all vote options in display order.
func voteOptionsFunc() []VoteOption {
	return voteOptions
}

// voteIconFunc returns the icon for a vote type, or "" if unknown/empty.
func voteIconFunc(voteType string) string {
	for _, o := range voteOptions {
		if o.Type == voteType {
			return o.Icon
		}
	}
	return ""
}
