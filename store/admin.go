package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type AdminStats struct {
	Books   int `json:"books"`
	Members int `json:"members"`
	Admins  int `json:"admins"`
}

func (s *Store) AdminDashboard(ctx context.Context) (*AdminStats, error) {
	var stats AdminStats

	err := s.DB.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM books),
			(SELECT COUNT(*) FROM users WHERE role = 'member'),
			(SELECT COUNT(*) FROM users WHERE role = 'admin')
	`).Scan(
		&stats.Books,
		&stats.Members,
		&stats.Admins,
	)
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

func (s *Store) AdminListMembers(ctx context.Context, search string) ([]User, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, username, email, role
		FROM users
		WHERE username ILIKE '%' || $1 || '%'
		   OR email ILIKE '%' || $1 || '%'
		ORDER BY created_at DESC
	`, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]User, 0)

	for rows.Next() {
		var user User

		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.Role,
		); err != nil {
			return nil, err
		}

		members = append(members, user)
	}

	return members, rows.Err()
}

func (s *Store) AdminGetMember(ctx context.Context, id string) (*User, error) {
	return s.GetUserByID(ctx, id)
}

func (s *Store) AdminUpdateMember(ctx context.Context, id, username, email string) (*User, error) {
	var user User

	err := s.DB.QueryRow(ctx, `
		UPDATE users
		SET username = $1, email = $2
		WHERE id = $3
		RETURNING id, username, email, role
	`, username, email, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Role,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &user, nil
}

func (s *Store) AdminSetRole(ctx context.Context, id, role string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentRole string

	err = tx.QueryRow(ctx, `
		SELECT role
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&currentRole)

	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}

	if currentRole == "admin" && role == "member" {
		var adminCount int

		err = tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM users
			WHERE role = 'admin'
		`).Scan(&adminCount)

		if err != nil {
			return err
		}

		if adminCount <= 1 {
			return errors.New("cannot change the role of the last admin")
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE users
		SET role = $1
		WHERE id = $2
	`, role, id)

	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminDeleteMember(ctx context.Context, id string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var role string

	err = tx.QueryRow(ctx, `
		SELECT role
		FROM users
		WHERE id = $1
		FOR UPDATE
	`, id).Scan(&role)

	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		return err
	}

	if role == "admin" {
		var adminCount int

		err = tx.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM users
			WHERE role = 'admin'
		`).Scan(&adminCount)

		if err != nil {
			return err
		}

		if adminCount <= 1 {
			return errors.New("cannot delete the last admin")
		}
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM users
		WHERE id = $1
	`, id)

	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminCreateBook(ctx context.Context, book *Book, categoryIDs []string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO books (title, author, description, cover_url, published_year)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`,
		book.Title,
		book.Author,
		book.Description,
		book.CoverURL,
		book.PublishedYear,
	).Scan(&book.ID)
	if err != nil {
		return err
	}

	for _, categoryID := range categoryIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO book_categories (book_id, category_id)
			VALUES ($1, $2)
		`, book.ID, categoryID)
		if err != nil {
			return err
		}
	}

	book.Categories = make([]string, 0, len(categoryIDs))

	rows, err := tx.Query(ctx, `
		SELECT c.name
		FROM categories c
		JOIN book_categories bc ON bc.category_id = c.id
		WHERE bc.book_id = $1
		ORDER BY c.name
	`, book.ID)
	if err != nil {
		return err
	}

	for rows.Next() {
		var categoryName string

		if err := rows.Scan(&categoryName); err != nil {
			rows.Close()
			return err
		}

		book.Categories = append(book.Categories, categoryName)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminUpdateBook(ctx context.Context, book *Book, categoryIDs []string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE books
		SET title = $1,
		    author = $2,
		    description = $3,
		    cover_url = $4,
		    published_year = $5,
		    updated_at = NOW()
		WHERE id = $6
	`,
		book.Title,
		book.Author,
		book.Description,
		book.CoverURL,
		book.PublishedYear,
		book.ID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM book_categories
		WHERE book_id = $1
	`, book.ID)
	if err != nil {
		return err
	}

	for _, categoryID := range categoryIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO book_categories (book_id, category_id)
			VALUES ($1, $2)
		`, book.ID, categoryID)
		if err != nil {
			return err
		}
	}

	book.Categories = make([]string, 0, len(categoryIDs))

	rows, err := tx.Query(ctx, `
		SELECT c.name
		FROM categories c
		JOIN book_categories bc ON bc.category_id = c.id
		WHERE bc.book_id = $1
		ORDER BY c.name
	`, book.ID)
	if err != nil {
		return err
	}

	for rows.Next() {
		var categoryName string

		if err := rows.Scan(&categoryName); err != nil {
			rows.Close()
			return err
		}

		book.Categories = append(book.Categories, categoryName)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminDeleteBook(ctx context.Context, id string) error {
	result, err := s.DB.Exec(ctx, `
		DELETE FROM books
		WHERE id = $1
	`, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// AdminBulkBook represents one book and its categories for bulk import.
type AdminBulkBook struct {
	Book        Book
	CategoryIDs []string
}

// AdminBulkCreateBooks imports multiple books in one transaction.
// Duplicate books are skipped and the total catalog is capped at maxBooks.
func (s *Store) AdminBulkCreateBooks(
	ctx context.Context,
	items []AdminBulkBook,
	maxBooks int,
) (int, int, error) {
	if maxBooks <= 0 {
		maxBooks = 100
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// Prevent concurrent inserts from exceeding the catalog limit.
	_, err = tx.Exec(ctx, `
		LOCK TABLE books IN SHARE ROW EXCLUSIVE MODE
	`)
	if err != nil {
		return 0, 0, err
	}

	var currentCount int

	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM books
	`).Scan(&currentCount)
	if err != nil {
		return 0, 0, err
	}

	added := 0
	skipped := 0

	for _, item := range items {
		book := item.Book

		book.Title = strings.TrimSpace(book.Title)
		book.Author = strings.TrimSpace(book.Author)

		if book.Title == "" || book.Author == "" {
			skipped++
			continue
		}

		if currentCount >= maxBooks {
			skipped++
			continue
		}

		var exists bool

		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM books
				WHERE LOWER(BTRIM(title)) = LOWER(BTRIM($1))
				  AND LOWER(BTRIM(author)) = LOWER(BTRIM($2))
			)
		`, book.Title, book.Author).Scan(&exists)

		if err != nil {
			return 0, 0, err
		}

		if exists {
			skipped++
			continue
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO books (
				title,
				author,
				description,
				cover_url,
				published_year
			)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`,
			book.Title,
			book.Author,
			book.Description,
			book.CoverURL,
			book.PublishedYear,
		).Scan(&book.ID)

		if err != nil {
			return 0, 0, err
		}

		for _, categoryID := range item.CategoryIDs {
			_, err = tx.Exec(ctx, `
				INSERT INTO book_categories (book_id, category_id)
				VALUES ($1, $2)
			`, book.ID, categoryID)

			if err != nil {
				return 0, 0, err
			}
		}

		added++
		currentCount++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}

	return added, skipped, nil
}

type BulkImportBook struct {
	Title         string
	Author        string
	Description   string
	CoverURL      string
	PublishedYear *int
	Categories    []string
}

func (s *Store) AdminBulkImportBooks(
	ctx context.Context,
	books []BulkImportBook,
) (int, int, int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback(ctx)

	// ป้องกันการนำเข้าหลายชุดพร้อมกัน
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(100100)`)
	if err != nil {
		return 0, 0, 0, err
	}

	var total int

	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM books
	`).Scan(&total)
	if err != nil {
		return 0, 0, 0, err
	}

	inserted := 0
	skipped := 0

	for _, book := range books {
		if total >= 100 {
			skipped++
			continue
		}

		book.Title = strings.TrimSpace(book.Title)
		book.Author = strings.TrimSpace(book.Author)
		book.CoverURL = strings.TrimSpace(book.CoverURL)

		if book.Title == "" ||
			book.Author == "" ||
			book.CoverURL == "" {
			skipped++
			continue
		}

		var exists bool

		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM books
				WHERE LOWER(BTRIM(title)) = LOWER(BTRIM($1))
				  AND LOWER(BTRIM(author)) = LOWER(BTRIM($2))
			)
		`, book.Title, book.Author).Scan(&exists)

		if err != nil {
			return 0, 0, 0, err
		}

		if exists {
			skipped++
			continue
		}

		var bookID string

		err = tx.QueryRow(ctx, `
			INSERT INTO books (
				title,
				author,
				description,
				cover_url,
				published_year
			)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`,
			book.Title,
			book.Author,
			book.Description,
			book.CoverURL,
			book.PublishedYear,
		).Scan(&bookID)

		if err != nil {
			return 0, 0, 0, err
		}

		for _, categoryName := range book.Categories {
			var categoryID string

			err = tx.QueryRow(ctx, `
				SELECT id
				FROM categories
				WHERE LOWER(name) = LOWER($1)
				LIMIT 1
			`, strings.TrimSpace(categoryName)).Scan(&categoryID)

			if err != nil {
				if err == pgx.ErrNoRows {
					continue
				}
				return 0, 0, 0, err
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO book_categories (book_id, category_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, bookID, categoryID)

			if err != nil {
				return 0, 0, 0, err
			}
		}

		inserted++
		total++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, err
	}

	return inserted, skipped, total, nil
}
