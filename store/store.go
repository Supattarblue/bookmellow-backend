
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("record not found")

type Store struct {
	DB *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Store {
	return &Store{
		DB: db,
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.DB.Ping(ctx)
}
