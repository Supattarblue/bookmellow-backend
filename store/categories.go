
package store

import (
	"context"
)

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, name
		FROM categories
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := make([]Category, 0)

	for rows.Next() {
		var category Category

		if err := rows.Scan(&category.ID, &category.Name); err != nil {
			return nil, err
		}

		categories = append(categories, category)
	}

	return categories, rows.Err()
}
