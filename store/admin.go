package store

import (
	"context"
	"errors"

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
	`).Scan(&stats.Books, &stats.Members, &stats.Admins)

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

func (s *Store) AdminUpdateMember(
	ctx context.Context,
	id, username, email string,
) (*User, error) {
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
		SELECT role
		FROM users
		WHERE id = $1
		FOR UPDATE
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
		if errors.Is(err, pgx.ErrNoRows) {
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

// AdminCreateBook เพิ่มหนังสือทีละเล่มพร้อมหมวดหมู่
func (s *Store) AdminCreateBook(
	ctx context.Context,
	book *Book,
	categoryIDs []string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO books (
			title, author, description, cover_url, published_year
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

func (s *Store) AdminUpdateBook(
	ctx context.Context,
	book *Book,
	categoryIDs []string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
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

	if result.RowsAffected() == 0 {
		return ErrNotFound
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

// AdminBulkBook คงไว้เพื่อรองรับโค้ดเดิมที่อาจอ้างถึง type นี้
type AdminBulkBook struct {
	Book        Book
	CategoryIDs []string
}

// AdminBulkCreateBooks ปิดการเพิ่มหนังสือหลายเล่ม
func (s *Store) AdminBulkCreateBooks(
	ctx context.Context,
	items []AdminBulkBook,
	_ int,
) (int, int, error) {
	return 0, len(items), errors.New(
		"bulk book creation is disabled to preserve the current 88-book catalog",
	)
}

// BulkImportBook คงไว้เพื่อให้โค้ดเดิมที่อ้างถึง type นี้ยัง compile ได้
type BulkImportBook struct {
	Title         string
	Author        string
	Description   string
	CoverURL      string
	PublishedYear *int
	Categories    []string
	Language      string
}

// BulkImportStats คงโครงสร้างผลลัพธ์เดิมไว้
type BulkImportStats struct {
	SkippedLimit            int `json:"limit_reached"`
	SkippedNotThai          int `json:"not_thai"`
	SkippedMissingFields    int `json:"missing_required_fields"`
	SkippedInvalidCover     int `json:"invalid_cover_url"`
	SkippedDuplicate        int `json:"duplicate"`
	UnmatchedCategories     int `json:"unmatched_categories"`
	InsertedWithoutCategory int `json:"inserted_without_category"`
}

// AdminBulkImportBooks ปิดการนำเข้าหนังสือแบบกลุ่ม
func (s *Store) AdminBulkImportBooks(
	ctx context.Context,
	books []BulkImportBook,
) (int, int, int, error) {
	return 0, len(books), 0, errors.New(
		"bulk import is disabled to preserve the current 88-book catalog",
	)
}

// AdminBulkImportBooksDetailed ปิดการนำเข้าแบบกลุ่ม
func (s *Store) AdminBulkImportBooksDetailed(
	ctx context.Context,
	books []BulkImportBook,
) (int, int, int, BulkImportStats, error) {
	stats := BulkImportStats{}

	return 0, len(books), 0, stats, errors.New(
		"bulk import is disabled to preserve the current 88-book catalog",
	)
}

// AdminReplaceNonThaiBooks ปิดการแทนที่หนังสือเดิมทั้งหมด
func (s *Store) AdminReplaceNonThaiBooks(
	ctx context.Context,
	books []BulkImportBook,
) (
	replaced, skipped, total, remaining int,
	stats BulkImportStats,
	err error,
) {
	return 0, len(books), 0, 0, stats, errors.New(
		"book replacement is disabled to preserve the current 88-book catalog",
	)
}

// hasThaiScript ตรวจสอบว่าข้อความมีอักษรภาษาไทยหรือไม่
func hasThaiScript(value string) bool {
	for _, r := range value {
		if r >= '\u0E00' && r <= '\u0E7F' {
			return true
		}
	}

	return false
}
