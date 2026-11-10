
package store

import (
	"context"

	
)

func (s *Store) GetPreferences(ctx context.Context, userID string) ([]Category, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT c.id, c.name
		FROM user_preferences up
		JOIN categories c ON c.id = up.category_id
		WHERE up.user_id = $1
		ORDER BY c.name ASC
	`, userID)
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

func (s *Store) UpdatePreferences(ctx context.Context, userID string, categoryIDs []string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		DELETE FROM user_preferences
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return err
	}

	for _, categoryID := range categoryIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO user_preferences (user_id, category_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, userID, categoryID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}


