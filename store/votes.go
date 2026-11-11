
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Vote struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	BookID    string `json:"book_id"`
	Rating    int    `json:"rating"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type VoteSummary struct {
	AverageRating float64 `json:"average_rating"`
	TotalVotes    int     `json:"total_votes"`
}

func (s *Store) GetVoteSummary(ctx context.Context, bookID string) (*VoteSummary, error) {
	var summary VoteSummary

	err := s.DB.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(rating), 0)::float8,
			COUNT(*)::int
		FROM votes
		WHERE book_id = $1
	`, bookID).Scan(
		&summary.AverageRating,
		&summary.TotalVotes,
	)

	if err != nil {
		return nil, err
	}

	return &summary, nil
}

func (s *Store) GetUserVote(ctx context.Context, userID, bookID string) (*Vote, error) {
	var vote Vote

	err := s.DB.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			book_id,
			rating,
			created_at::text,
			updated_at::text
		FROM votes
		WHERE user_id = $1 AND book_id = $2
	`, userID, bookID).Scan(
		&vote.ID,
		&vote.UserID,
		&vote.BookID,
		&vote.Rating,
		&vote.CreatedAt,
		&vote.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &vote, nil
}

func (s *Store) UpsertVote(ctx context.Context, userID, bookID string, rating int) (*Vote, error) {
	var vote Vote

	err := s.DB.QueryRow(ctx, `
		INSERT INTO votes (user_id, book_id, rating)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, book_id)
		DO UPDATE SET
			rating = EXCLUDED.rating,
			updated_at = NOW()
		RETURNING
			id,
			user_id,
			book_id,
			rating,
			created_at::text,
			updated_at::text
	`, userID, bookID, rating).Scan(
		&vote.ID,
		&vote.UserID,
		&vote.BookID,
		&vote.Rating,
		&vote.CreatedAt,
		&vote.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &vote, nil
}
