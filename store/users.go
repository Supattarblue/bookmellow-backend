
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
}

func (s *Store) CreateUser(ctx context.Context, username, email, passwordHash string) (*User, error) {
	var user User

	err := s.DB.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, email, password_hash, role
	`, username, email, passwordHash).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
	)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User

	err := s.DB.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role
		FROM users
		WHERE email = $1
	`, email).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
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

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	var user User

	err := s.DB.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role
		FROM users
		WHERE id = $1
	`, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
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
