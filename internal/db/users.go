package db

import (
	"database/sql"
	"fmt"
)

// User represents a row in the users table.
type User struct {
	ID        int64
	Username  string
	Pin       string
	CreatedAt string
}

// CreateUser inserts a new user and returns the new row ID.
func (db *DB) CreateUser(username, pin string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO users (username, pin) VALUES (?, ?)`,
		username, pin,
	)
	if err != nil {
		return 0, fmt.Errorf("insert user: %w", err)
	}
	return res.LastInsertId()
}

// GetUserByName returns a single user by username, or nil if not found.
func (db *DB) GetUserByName(username string) (*User, error) {
	row := db.QueryRow(
		`SELECT id, username, pin, created_at FROM users WHERE username = ?`,
		username,
	)

	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Pin, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

// GetUserByID returns a single user by ID, or nil if not found.
func (db *DB) GetUserByID(id int64) (*User, error) {
	row := db.QueryRow(
		`SELECT id, username, pin, created_at FROM users WHERE id = ?`,
		id,
	)

	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Pin, &u.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

// ListUsers returns all users ordered by username.
func (db *DB) ListUsers() ([]User, error) {
	rows, err := db.Query(
		`SELECT id, username, pin, created_at FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Pin, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
