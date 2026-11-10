
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Review struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	BookID    string `json:"book_id"`
	Rating    int    `json:"rating"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Store) ListReviews(ctx context.Context, bookID string) ([]Review, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT
			r.id,
			r.user_id,
			u.username,
			r.book_id,
			r.rating,
			COALESCE(r.comment, ''),
			r.created_at::text,
			r.updated_at::text
		FROM reviews r
		JOIN users u ON u.id = r.user_id
		WHERE r.book_id = $1
		ORDER BY r.created_at DESC
	`, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reviews := make([]Review, 0)

	for rows.Next() {
		var review Review

		err := rows.Scan(
			&review.ID,
			&review.UserID,
			&review.Username,
			&review.BookID,
			&review.Rating,
			&review.Comment,
			&review.CreatedAt,
			&review.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		reviews = append(reviews, review)
	}

	return reviews, rows.Err()
}

func (s *Store) CreateReview(ctx context.Context, userID, bookID string, rating int, comment string) (*Review, error) {
	var review Review

	err := s.DB.QueryRow(ctx, `
		INSERT INTO reviews (user_id, book_id, rating, comment)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, book_id, rating,
			COALESCE(comment, ''), created_at::text, updated_at::text
	`, userID, bookID, rating, comment).Scan(
		&review.ID,
		&review.UserID,
		&review.BookID,
		&review.Rating,
		&review.Comment,
		&review.CreatedAt,
		&review.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &review, nil
}

func (s *Store) UpdateReview(ctx context.Context, reviewID, userID string, rating int, comment string) (*Review, error) {
	var review Review

	err := s.DB.QueryRow(ctx, `
		UPDATE reviews
		SET rating = $1, comment = $2, updated_at = NOW()
		WHERE id = $3 AND user_id = $4
		RETURNING id, user_id, book_id, rating,
			COALESCE(comment, ''), created_at::text, updated_at::text
	`, rating, comment, reviewID, userID).Scan(
		&review.ID,
		&review.UserID,
		&review.BookID,
		&review.Rating,
		&review.Comment,
		&review.CreatedAt,
		&review.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &review, nil
}

func (s *Store) DeleteReview(ctx context.Context, reviewID, userID string) error {
	result, err := s.DB.Exec(ctx, `
		DELETE FROM reviews
		WHERE id = $1 AND user_id = $2
	`, reviewID, userID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
