package db

import "fmt"

// SetVote upserts a vote for a user on a game. If voteType is "none", the
// existing vote is deleted. Otherwise the vote ("must_play", "up", "neutral",
// "down" or "veto") is inserted or replaced. vote_type is plain TEXT, so new
// vote types need no schema migration.
func (db *DB) SetVote(userID, gameID int64, voteType string) error {
	if voteType == "none" {
		_, err := db.Exec(
			`DELETE FROM votes WHERE user_id = ? AND game_id = ?`,
			userID, gameID,
		)
		if err != nil {
			return fmt.Errorf("delete vote: %w", err)
		}
		return nil
	}

	_, err := db.Exec(
		`INSERT OR REPLACE INTO votes (user_id, game_id, vote_type)
		 VALUES (?, ?, ?)`,
		userID, gameID, voteType,
	)
	if err != nil {
		return fmt.Errorf("upsert vote: %w", err)
	}
	return nil
}

// GetVotesForGame returns all votes for a game with voter usernames.
func (db *DB) GetVotesForGame(gameID int64) ([]Vote, error) {
	rows, err := db.Query(`
		SELECT v.user_id, u.username, v.vote_type
		FROM votes v
		JOIN users u ON u.id = v.user_id
		WHERE v.game_id = ?
		ORDER BY v.created_at`, gameID)
	if err != nil {
		return nil, fmt.Errorf("query votes for game %d: %w", gameID, err)
	}
	defer rows.Close()

	var votes []Vote
	for rows.Next() {
		var v Vote
		if err := rows.Scan(&v.UserID, &v.Username, &v.VoteType); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		votes = append(votes, v)
	}
	return votes, rows.Err()
}

// UserVoteSummary holds per-user vote totals for the overview page.
type UserVoteSummary struct {
	UserID     int64
	Username   string
	MustPlays  int
	Ups        int
	Neutrals   int
	Downs      int
	Vetoes     int
	GamesAdded int
}

// ListUserVoteSummaries returns vote totals for every user, ordered by
// username. Users without any votes are included with zero counts.
func (db *DB) ListUserVoteSummaries() ([]UserVoteSummary, error) {
	rows, err := db.Query(`
		SELECT
			u.id, u.username,
			COALESCE(SUM(v.vote_type = 'must_play'), 0),
			COALESCE(SUM(v.vote_type = 'up'), 0),
			COALESCE(SUM(v.vote_type = 'neutral'), 0),
			COALESCE(SUM(v.vote_type = 'down'), 0),
			COALESCE(SUM(v.vote_type = 'veto'), 0),
			(SELECT COUNT(*) FROM games g WHERE g.added_by = u.id)
		FROM users u
		LEFT JOIN votes v ON v.user_id = u.id
		GROUP BY u.id
		ORDER BY u.username`)
	if err != nil {
		return nil, fmt.Errorf("query user vote summaries: %w", err)
	}
	defer rows.Close()

	var summaries []UserVoteSummary
	for rows.Next() {
		var s UserVoteSummary
		if err := rows.Scan(&s.UserID, &s.Username, &s.MustPlays, &s.Ups, &s.Neutrals, &s.Downs, &s.Vetoes, &s.GamesAdded); err != nil {
			return nil, fmt.Errorf("scan user vote summary: %w", err)
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}
