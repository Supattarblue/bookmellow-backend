
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// CreatePasswordResetToken สร้าง Token ใหม่และยกเลิก Token เก่าที่ยังใช้ได้
func (s *Store) CreatePasswordResetToken(
	ctx context.Context,
	userID string,
	tokenHash string,
	expiresAt time.Time,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE password_reset_tokens
		SET used_at = NOW()
		WHERE user_id = $1
		  AND used_at IS NULL
	`, userID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO password_reset_tokens
			(user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, tokenHash, expiresAt)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ResetPassword ตรวจสอบ Token และเปลี่ยนรหัสผ่านแบบใช้ครั้งเดียว
func (s *Store) ResetPassword(
	ctx context.Context,
	tokenHash string,
	newPasswordHash string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string

	err = tx.QueryRow(ctx, `
		UPDATE password_reset_tokens
		SET used_at = NOW()
		WHERE token_hash = $1
		  AND used_at IS NULL
		  AND expires_at > NOW()
		RETURNING user_id
	`, tokenHash).Scan(&userID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2
	`, newPasswordHash, userID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
