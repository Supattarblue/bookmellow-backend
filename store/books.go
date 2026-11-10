
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type Book struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Description   string   `json:"description"`
	CoverURL      *string  `json:"cover_url"`
	PublishedYear *int     `json:"published_year"`
	Categories    []string `json:"categories"`
}

func (s *Store) ListBooks(ctx context.Context, search, category string) ([]Book, error) {
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
		FROM books b
		LEFT JOIN book_categories bc ON bc.book_id = b.id
		LEFT JOIN categories c ON c.id = bc.category_id
		WHERE
			($1 = '' OR b.title ILIKE '%' || $1 || '%' OR b.author ILIKE '%' || $1 || '%')
			AND ($2 = '' OR EXISTS (
				SELECT 1
				FROM book_categories bc2
				JOIN categories c2 ON c2.id = bc2.category_id
				WHERE bc2.book_id = b.id AND c2.name = $2
			))
		GROUP BY b.id
		ORDER BY b.created_at DESC
	`, search, category)
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

func (s *Store) GetBook(ctx context.Context, id string) (*Book, error) {
	var book Book

	err := s.DB.QueryRow(ctx, `
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
		FROM books b
		LEFT JOIN book_categories bc ON bc.book_id = b.id
		LEFT JOIN categories c ON c.id = bc.category_id
		WHERE b.id = $1
		GROUP BY b.id
	`, id).Scan(
		&book.ID,
		&book.Title,
		&book.Author,
		&book.Description,
		&book.CoverURL,
		&book.PublishedYear,
		&book.Categories,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &book, nil
}
