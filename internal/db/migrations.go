package db

import "fmt"

// migrate runs all schema migrations. Every statement uses IF NOT EXISTS so
// it is safe to call on every startup.
func (db *DB) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			username    TEXT    NOT NULL UNIQUE,
			pin         TEXT    NOT NULL DEFAULT '',
			created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS games (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			source            TEXT    NOT NULL DEFAULT 'steam',
			store_product_id  TEXT,
			title             TEXT    NOT NULL,
			short_description TEXT    DEFAULT '',
			header_image_path TEXT    DEFAULT '',
			price_cents       INTEGER,
			currency          TEXT    DEFAULT 'USD',
			discount_percent  INTEGER DEFAULT 0,
			is_free           INTEGER DEFAULT 0,
			drm_free          INTEGER DEFAULT 0,
			release_date      TEXT    DEFAULT '',
			has_windows       INTEGER DEFAULT 1,
			has_linux         INTEGER DEFAULT 0,
			has_mac           INTEGER DEFAULT 0,
			store_url         TEXT    DEFAULT '',
			note              TEXT    DEFAULT '',
			added_by          INTEGER REFERENCES users(id),
			created_at        TEXT    NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE UNIQUE INDEX IF NOT EXISTS idx_games_store
			ON games(source, store_product_id)
			WHERE store_product_id IS NOT NULL AND store_product_id != ''`,

		`CREATE TABLE IF NOT EXISTS game_tags (
			game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			tag         TEXT    NOT NULL,
			tag_type    TEXT    NOT NULL DEFAULT 'genre',
			added_by    INTEGER REFERENCES users(id),
			PRIMARY KEY (game_id, tag)
		)`,

		`CREATE TABLE IF NOT EXISTS votes (
			user_id     INTEGER NOT NULL REFERENCES users(id),
			game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
			vote_type   TEXT    NOT NULL DEFAULT 'up',
			created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (user_id, game_id)
		)`,
	}

	for i, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration statement %d: %w", i, err)
		}
	}

	// Columns added after the initial schema. SQLite has no
	// "ADD COLUMN IF NOT EXISTS", so check table_info first. Existing rows
	// keep their data; new columns default to NULL / ''.
	columns := []struct{ table, name, def string }{
		{"games", "key_price_cents", "INTEGER"},
		{"games", "key_price_url", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range columns {
		if err := db.addColumnIfMissing(c.table, c.name, c.def); err != nil {
			return err
		}
	}

	return nil
}

// addColumnIfMissing adds a column to a table unless it already exists.
func (db *DB) addColumnIfMissing(table, column, def string) error {
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name = ?)`,
		table, column,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if exists {
		return nil
	}
	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, def)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
