package db

import (
	"database/sql"
	"fmt"
)

// Game represents a row in the games table along with associated tags, votes,
// a computed score, and the username of the person who added it.
type Game struct {
	ID               int64
	Source           string
	StoreProductID   sql.NullString
	Title            string
	ShortDescription string
	HeaderImagePath  string
	PriceCents       sql.NullInt64
	Currency         string
	DiscountPercent  int
	IsFree           bool
	DRMFree          bool
	ReleaseDate      string
	HasWindows       bool
	HasLinux         bool
	HasMac           bool
	StoreURL         string
	Note             string
	AddedBy          sql.NullInt64
	CreatedAt        string
	KeyPriceCents    sql.NullInt64 // manual override, e.g. a cheaper key shop price
	KeyPriceURL      string

	Tags        []Tag
	Votes       []Vote
	AddedByName string
	Score       int
	Vetoes      int // number of users who vetoed this game
}

// Tag represents a row in the game_tags table.
type Tag struct {
	Tag         string
	TagType     string
	AddedByName string
}

// Vote represents a row in the votes table with the voter's username.
type Vote struct {
	UserID   int64
	Username string
	VoteType string
}

// StorePriceCents returns the store price after discount.
func (g Game) StorePriceCents() int64 {
	return g.PriceCents.Int64 * int64(100-g.DiscountPercent) / 100
}

// HasKeyDeal reports whether the key price is set and beats the store price
// (or the game has no store price at all).
func (g Game) HasKeyDeal() bool {
	if !g.KeyPriceCents.Valid {
		return false
	}
	return !g.PriceCents.Valid || g.KeyPriceCents.Int64 < g.StorePriceCents()
}

// effectivePriceSQL is the cheapest known price: the key price if it beats
// the discounted store price, else the discounted store price.
const effectivePriceSQL = `CASE
	WHEN g.key_price_cents IS NOT NULL AND (g.price_cents IS NULL
		OR g.key_price_cents < g.price_cents * (100 - g.discount_percent) / 100)
	THEN g.key_price_cents
	ELSE g.price_cents * (100 - g.discount_percent) / 100
END`

// CreateGame inserts a game and its tags in a single transaction. Returns the
// new game row ID.
func (db *DB) CreateGame(game *Game, tags []Tag) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO games (
			source, store_product_id, title, short_description,
			header_image_path, price_cents, currency, discount_percent,
			is_free, drm_free, release_date, has_windows, has_linux, has_mac,
			store_url, note, added_by, key_price_cents, key_price_url
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		game.Source,
		game.StoreProductID,
		game.Title,
		game.ShortDescription,
		game.HeaderImagePath,
		game.PriceCents,
		game.Currency,
		game.DiscountPercent,
		game.IsFree,
		game.DRMFree,
		game.ReleaseDate,
		game.HasWindows,
		game.HasLinux,
		game.HasMac,
		game.StoreURL,
		game.Note,
		game.AddedBy,
		game.KeyPriceCents,
		game.KeyPriceURL,
	)
	if err != nil {
		return 0, fmt.Errorf("insert game: %w", err)
	}

	gameID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	for _, t := range tags {
		_, err := tx.Exec(
			`INSERT INTO game_tags (game_id, tag, tag_type, added_by)
			 VALUES (?, ?, ?, ?)`,
			gameID, t.Tag, t.TagType, game.AddedBy,
		)
		if err != nil {
			return 0, fmt.Errorf("insert tag %q: %w", t.Tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return gameID, nil
}

// GetGame returns a single game by ID with its tags and votes fully populated.
func (db *DB) GetGame(id int64) (*Game, error) {
	g, err := db.scanGame(db.QueryRow(`
		SELECT
			g.id, g.source, g.store_product_id, g.title,
			g.short_description, g.header_image_path, g.price_cents,
			g.currency, g.discount_percent, g.is_free, g.drm_free,
			g.release_date, g.has_windows, g.has_linux, g.has_mac,
			g.store_url, g.note, g.added_by, g.created_at,
			g.key_price_cents, g.key_price_url,
			COALESCE(u.username, '') AS added_by_name,
			COALESCE(
				(SELECT SUM(CASE v.vote_type WHEN 'must_play' THEN 2 WHEN 'up' THEN 1 WHEN 'down' THEN -1 WHEN 'veto' THEN -2 ELSE 0 END)
				 FROM votes v WHERE v.game_id = g.id), 0
			) AS score,
			(SELECT COUNT(*) FROM votes v
			 WHERE v.game_id = g.id AND v.vote_type = 'veto') AS vetoes
		FROM games g
		LEFT JOIN users u ON u.id = g.added_by
		WHERE g.id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get game %d: %w", id, err)
	}

	g.Tags, err = db.getTagsForGame(g.ID)
	if err != nil {
		return nil, err
	}

	g.Votes, err = db.GetVotesForGame(g.ID)
	if err != nil {
		return nil, err
	}

	return g, nil
}

// ListGames returns all games with tags, votes, and score.
// sortBy: "votes" (default, score desc with vetoed games last), "recent" (created_at desc),
// "price_asc" / "price_desc" (cheapest of key price and discounted store price).
func (db *DB) ListGames(sortBy string) ([]Game, error) {
	var orderClause string
	switch sortBy {
	case "recent":
		orderClause = "g.created_at DESC"
	case "price_asc":
		orderClause = effectivePriceSQL + " ASC"
	case "price_desc":
		orderClause = effectivePriceSQL + " DESC"
	default: // "votes"
		orderClause = "vetoes > 0, score DESC"
	}

	query := fmt.Sprintf(`
		SELECT
			g.id, g.source, g.store_product_id, g.title,
			g.short_description, g.header_image_path, g.price_cents,
			g.currency, g.discount_percent, g.is_free, g.drm_free,
			g.release_date, g.has_windows, g.has_linux, g.has_mac,
			g.store_url, g.note, g.added_by, g.created_at,
			g.key_price_cents, g.key_price_url,
			COALESCE(u.username, '') AS added_by_name,
			COALESCE(
				(SELECT SUM(CASE v.vote_type WHEN 'must_play' THEN 2 WHEN 'up' THEN 1 WHEN 'down' THEN -1 WHEN 'veto' THEN -2 ELSE 0 END)
				 FROM votes v WHERE v.game_id = g.id), 0
			) AS score,
			(SELECT COUNT(*) FROM votes v
			 WHERE v.game_id = g.id AND v.vote_type = 'veto') AS vetoes
		FROM games g
		LEFT JOIN users u ON u.id = g.added_by
		ORDER BY %s`, orderClause)

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list games: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		g, err := db.scanGameRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan game: %w", err)
		}
		games = append(games, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	// Populate tags and votes for each game.
	for i := range games {
		games[i].Tags, err = db.getTagsForGame(games[i].ID)
		if err != nil {
			return nil, err
		}
		games[i].Votes, err = db.GetVotesForGame(games[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return games, nil
}

// UpdateGame saves a game's editable fields and replaces its tags with the
// given list. Tags that remain keep their type and author; new tags are
// recorded as type "tag" added by editorID.
func (db *DB) UpdateGame(g *Game, tags []string, editorID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE games SET
			title = ?, short_description = ?, note = ?, store_url = ?,
			header_image_path = ?, price_cents = ?, discount_percent = ?,
			is_free = ?, key_price_cents = ?, key_price_url = ?
		WHERE id = ?`,
		g.Title, g.ShortDescription, g.Note, g.StoreURL,
		g.HeaderImagePath, g.PriceCents, g.DiscountPercent,
		g.IsFree, g.KeyPriceCents, g.KeyPriceURL,
		g.ID,
	)
	if err != nil {
		return fmt.Errorf("update game %d: %w", g.ID, err)
	}

	keep := make(map[string]bool, len(tags))
	for _, t := range tags {
		keep[t] = true
	}
	for _, t := range g.Tags {
		if !keep[t.Tag] {
			if _, err := tx.Exec(`DELETE FROM game_tags WHERE game_id = ? AND tag = ?`, g.ID, t.Tag); err != nil {
				return fmt.Errorf("delete tag %q: %w", t.Tag, err)
			}
		}
	}
	for _, t := range tags {
		_, err := tx.Exec(
			`INSERT OR IGNORE INTO game_tags (game_id, tag, tag_type, added_by)
			 VALUES (?, ?, 'tag', ?)`,
			g.ID, t, editorID,
		)
		if err != nil {
			return fmt.Errorf("insert tag %q: %w", t, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// DeleteGame removes a game by ID. Tags and votes are removed via ON DELETE CASCADE.
func (db *DB) DeleteGame(id int64) error {
	_, err := db.Exec(`DELETE FROM games WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete game %d: %w", id, err)
	}
	return nil
}

// GameExistsByStoreID checks whether a game already exists for the given
// source and store product ID.
func (db *DB) GameExistsByStoreID(source, storeProductID string) (bool, error) {
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS(
			SELECT 1 FROM games WHERE source = ? AND store_product_id = ?
		)`, source, storeProductID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check game exists: %w", err)
	}
	return exists, nil
}

// getTagsForGame returns all tags for a game, joined with the user who added
// them.
func (db *DB) getTagsForGame(gameID int64) ([]Tag, error) {
	rows, err := db.Query(`
		SELECT gt.tag, gt.tag_type, COALESCE(u.username, '') AS added_by_name
		FROM game_tags gt
		LEFT JOIN users u ON u.id = gt.added_by
		WHERE gt.game_id = ?
		ORDER BY gt.tag`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query tags for game %d: %w", gameID, err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.Tag, &t.TagType, &t.AddedByName); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// scanGame scans a single *sql.Row into a Game (used by GetGame).
func (db *DB) scanGame(row *sql.Row) (*Game, error) {
	var g Game
	err := row.Scan(
		&g.ID, &g.Source, &g.StoreProductID, &g.Title,
		&g.ShortDescription, &g.HeaderImagePath, &g.PriceCents,
		&g.Currency, &g.DiscountPercent, &g.IsFree, &g.DRMFree,
		&g.ReleaseDate, &g.HasWindows, &g.HasLinux, &g.HasMac,
		&g.StoreURL, &g.Note, &g.AddedBy, &g.CreatedAt,
		&g.KeyPriceCents, &g.KeyPriceURL,
		&g.AddedByName, &g.Score, &g.Vetoes,
	)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// scanGameRow scans a single row from *sql.Rows into a Game (used by ListGames).
func (db *DB) scanGameRow(rows *sql.Rows) (*Game, error) {
	var g Game
	err := rows.Scan(
		&g.ID, &g.Source, &g.StoreProductID, &g.Title,
		&g.ShortDescription, &g.HeaderImagePath, &g.PriceCents,
		&g.Currency, &g.DiscountPercent, &g.IsFree, &g.DRMFree,
		&g.ReleaseDate, &g.HasWindows, &g.HasLinux, &g.HasMac,
		&g.StoreURL, &g.Note, &g.AddedBy, &g.CreatedAt,
		&g.KeyPriceCents, &g.KeyPriceURL,
		&g.AddedByName, &g.Score, &g.Vetoes,
	)
	if err != nil {
		return nil, err
	}
	return &g, nil
}
