
package store

import (
	"context"
)

func (s *Store) ListFavorites(ctx context.Context, userID string) ([]Book, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT
			b.id,
			b.title,
			b.author,
			COALESCE(b.description, ''),
			b.cover_url,
			b.published_year,
			COALESCE(
				ARRAY_AGG(c.name) FILTER (WHERE c.name IS NOT NULL),
				'{}'
			)
		FROM favorites f
		JOIN books b ON b.id = f.book_id
		LEFT JOIN book_categories bc ON bc.book_id = b.id
		LEFT JOIN categories c ON c.id = bc.category_id
		WHERE f.user_id = $1
		GROUP BY b.id, f.created_at
		ORDER BY f.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	books := make([]Book, 0)

	for rows.Next() {
		var book Book

		err := rows.Scan(
			&book.ID,
			&book.Title,
			&book.Author,
			&book.Description,
			&book.CoverURL,
			&book.PublishedYear,
			&book.Categories,
		)
		if err != nil {
			return nil, err
		}

		books = append(books, book)
	}

	return books, rows.Err()
}

func (s *Store) AddFavorite(ctx context.Context, userID, bookID string) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO favorites (user_id, book_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, book_id) DO NOTHING
	`, userID, bookID)

	return err
}

func (s *Store) RemoveFavorite(ctx context.Context, userID, bookID string) error {
	_, err := s.DB.Exec(ctx, `
		DELETE FROM favorites
		WHERE user_id = $1 AND book_id = $2
	`, userID, bookID)

	return err
}
