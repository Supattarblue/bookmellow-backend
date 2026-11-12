package store

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

const BookCatalogTarget = 100

type AdminStats struct {
	Books   int `json:"books"`
	Members int `json:"members"`
	Admins  int `json:"admins"`
}

func (s *Store) AdminDashboard(ctx context.Context) (*AdminStats, error) {
	var stats AdminStats

	err := s.DB.QueryRow(ctx, `
		SELECT
			(
				SELECT COUNT(*)
				FROM books b
				INNER JOIN book_catalog_visibility v
					ON v.book_id = b.id
				WHERE v.is_visible = TRUE
			),
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
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role); err != nil {
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
		&user.ID, &user.Username, &user.Email, &user.Role,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
		SELECT role FROM users WHERE id = $1 FOR UPDATE
	`, id).Scan(&currentRole)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if currentRole == "admin" && role == "member" {
		var adminCount int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM users WHERE role = 'admin'
		`).Scan(&adminCount)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return errors.New("cannot change the role of the last admin")
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE users SET role = $1 WHERE id = $2
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
		SELECT role FROM users WHERE id = $1 FOR UPDATE
	`, id).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if role == "admin" {
		var adminCount int
		err = tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM users WHERE role = 'admin'
		`).Scan(&adminCount)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return errors.New("cannot delete the last admin")
		}
	}

	_, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminCreateBook(ctx context.Context, book *Book, categoryIDs []string) error {
	book.Title = strings.TrimSpace(book.Title)
	book.Author = strings.TrimSpace(book.Author)

	if book.Title == "" || book.Author == "" {
		return errors.New("book title and author are required")
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var visible int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM book_catalog_visibility
		WHERE is_visible = TRUE
	`).Scan(&visible)
	if err != nil {
		return err
	}

	if visible >= BookCatalogTarget {
		return errors.New("book catalog has reached the 100-book limit")
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO books (title, author, description, cover_url, published_year)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`,
		book.Title, book.Author, book.Description, book.CoverURL, book.PublishedYear,
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

	_, err = tx.Exec(ctx, `
		INSERT INTO book_catalog_visibility (book_id, is_visible)
		VALUES ($1, TRUE)
		ON CONFLICT (book_id) DO UPDATE SET is_visible = TRUE
	`, book.ID)
	if err != nil {
		return err
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
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		book.Categories = append(book.Categories, name)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) AdminUpdateBook(ctx context.Context, book *Book, categoryIDs []string) error {
	book.Title = strings.TrimSpace(book.Title)
	book.Author = strings.TrimSpace(book.Author)

	if book.Title == "" || book.Author == "" {
		return errors.New("book title and author are required")
	}

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
		book.Title, book.Author, book.Description,
		book.CoverURL, book.PublishedYear, book.ID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM book_categories WHERE book_id = $1
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
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		book.Categories = append(book.Categories, name)
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
		DELETE FROM books WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type AdminBulkBook struct {
	Book        Book
	CategoryIDs []string
}

func (s *Store) AdminBulkCreateBooks(
	ctx context.Context,
	items []AdminBulkBook,
	_ int,
) (int, int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		LOCK TABLE books IN SHARE ROW EXCLUSIVE MODE
	`)
	if err != nil {
		return 0, 0, err
	}

	var visible int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM book_catalog_visibility
		WHERE is_visible = TRUE
	`).Scan(&visible)
	if err != nil {
		return 0, 0, err
	}

	remaining := BookCatalogTarget - visible
	if remaining <= 0 {
		return 0, len(items), nil
	}

	added, skipped := 0, 0

	for _, item := range items {
		if added >= remaining {
			skipped++
			continue
		}

		book := item.Book
		book.Title = strings.TrimSpace(book.Title)
		book.Author = strings.TrimSpace(book.Author)

		if book.Title == "" || book.Author == "" {
			skipped++
			continue
		}

		var exists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM books
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

		var bookID string
		err = tx.QueryRow(ctx, `
			INSERT INTO books (title, author, description, cover_url, published_year)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`,
			book.Title, book.Author, book.Description,
			book.CoverURL, book.PublishedYear,
		).Scan(&bookID)
		if err != nil {
			return 0, 0, err
		}

		for _, categoryID := range item.CategoryIDs {
			_, err = tx.Exec(ctx, `
				INSERT INTO book_categories (book_id, category_id)
				VALUES ($1, $2)
			`, bookID, categoryID)
			if err != nil {
				return 0, 0, err
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO book_catalog_visibility (book_id, is_visible)
			VALUES ($1, TRUE)
			ON CONFLICT (book_id) DO UPDATE SET is_visible = TRUE
		`, bookID)
		if err != nil {
			return 0, 0, err
		}

		added++
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
	Language      string
}

type BulkImportStats struct {
	SkippedLimit            int `json:"limit_reached"`
	SkippedNotThai          int `json:"unsupported_language_or_title"`
	SkippedMissingFields    int `json:"missing_required_fields"`
	SkippedInvalidCover     int `json:"invalid_cover_url"`
	SkippedDuplicate        int `json:"duplicate"`
	UnmatchedCategories     int `json:"unmatched_categories"`
	InsertedWithoutCategory int `json:"inserted_without_category"`
}

func (s *Store) AdminBulkImportBooks(
	ctx context.Context,
	books []BulkImportBook,
) (int, int, int, error) {
	inserted, skipped, total, _, err :=
		s.AdminBulkImportBooksDetailed(ctx, books)
	return inserted, skipped, total, err
}

func (s *Store) AdminBulkImportBooksDetailed(
	ctx context.Context,
	books []BulkImportBook,
) (int, int, int, BulkImportStats, error) {
	stats := BulkImportStats{}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, 0, 0, stats, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		LOCK TABLE books IN SHARE ROW EXCLUSIVE MODE
	`)
	if err != nil {
		return 0, 0, 0, stats, err
	}

	var total, visible int

	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM books`).Scan(&total)
	if err != nil {
		return 0, 0, 0, stats, err
	}

	err = tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM book_catalog_visibility
		WHERE is_visible = TRUE
	`).Scan(&visible)
	if err != nil {
		return 0, 0, 0, stats, err
	}

	remaining := BookCatalogTarget - visible
	if remaining <= 0 {
		if err := tx.Commit(ctx); err != nil {
			return 0, 0, 0, stats, err
		}
		return 0, len(books), total, stats, nil
	}

	inserted, skipped := 0, 0

	allowedLanguages := map[string]bool{
		"th": true,
		"en": true,
		"ja": true,
		"ko": true,
	}

	for _, candidate := range books {
		if inserted >= remaining {
			stats.SkippedLimit++
			skipped++
			continue
		}

		book := candidate
		book.Title = strings.TrimSpace(book.Title)
		book.Author = strings.TrimSpace(book.Author)
		book.Description = strings.TrimSpace(book.Description)
		book.CoverURL = strings.TrimSpace(book.CoverURL)
		book.Language = strings.ToLower(strings.TrimSpace(book.Language))

		if book.Title == "" || book.Author == "" || book.CoverURL == "" {
			stats.SkippedMissingFields++
			skipped++
			continue
		}

		if !allowedLanguages[book.Language] {
			stats.SkippedNotThai++
			skipped++
			continue
		}

		parsedURL, parseErr := url.ParseRequestURI(book.CoverURL)
		if parseErr != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
			stats.SkippedInvalidCover++
			skipped++
			continue
		}

		var exists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM books
				WHERE LOWER(BTRIM(title)) = LOWER(BTRIM($1))
				  AND LOWER(BTRIM(author)) = LOWER(BTRIM($2))
			)
		`, book.Title, book.Author).Scan(&exists)
		if err != nil {
			return 0, 0, 0, stats, err
		}

		if exists {
			stats.SkippedDuplicate++
			skipped++
			continue
		}

		var bookID string
		err = tx.QueryRow(ctx, `
			INSERT INTO books (
				title, author, description, cover_url, published_year
			)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`,
			book.Title, book.Author, book.Description,
			book.CoverURL, book.PublishedYear,
		).Scan(&bookID)
		if err != nil {
			return 0, 0, 0, stats, err
		}

		matchedCategories := 0

		for _, categoryName := range book.Categories {
			categoryName = strings.TrimSpace(categoryName)
			if categoryName == "" {
				continue
			}

			var categoryID string
			err = tx.QueryRow(ctx, `
				SELECT id FROM categories
				WHERE LOWER(BTRIM(name)) = LOWER(BTRIM($1))
				LIMIT 1
			`, categoryName).Scan(&categoryID)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					stats.UnmatchedCategories++
					continue
				}
				return 0, 0, 0, stats, err
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO book_categories (book_id, category_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, bookID, categoryID)
			if err != nil {
				return 0, 0, 0, stats, err
			}

			matchedCategories++
		}

		if matchedCategories == 0 {
			var otherID string
			fallbackErr := tx.QueryRow(ctx, `
				SELECT id FROM categories
				WHERE LOWER(BTRIM(name)) = 'other'
				LIMIT 1
			`).Scan(&otherID)

			if fallbackErr == nil {
				_, err = tx.Exec(ctx, `
					INSERT INTO book_categories (book_id, category_id)
					VALUES ($1, $2)
					ON CONFLICT DO NOTHING
				`, bookID, otherID)
				if err != nil {
					return 0, 0, 0, stats, err
				}
			} else if errors.Is(fallbackErr, pgx.ErrNoRows) {
				stats.InsertedWithoutCategory++
			} else {
				return 0, 0, 0, stats, fallbackErr
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO book_catalog_visibility (book_id, is_visible)
			VALUES ($1, TRUE)
			ON CONFLICT (book_id)
			DO UPDATE SET is_visible = TRUE
		`, bookID)
		if err != nil {
			return 0, 0, 0, stats, err
		}

		inserted++
		total++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, 0, stats, err
	}

	return inserted, skipped, total, stats, nil
}

// ปิดการแทนที่หนังสือเดิม เพื่อป้องกันรีวิวและรายการโปรดผูกผิดเล่ม
func (s *Store) AdminReplaceNonThaiBooks(
	ctx context.Context,
	books []BulkImportBook,
) (replaced, skipped, total, remaining int, stats BulkImportStats, err error) {
	return 0, 0, 0, 0, stats,
		errors.New("replacing existing books is disabled; use AdminBulkImportBooksDetailed")
}

func hasThaiScript(value string) bool {
	for _, r := range value {
		if r >= '\u0E00' && r <= '\u0E7F' {
			return true
		}
	}
	return false
}
