
package store

import (
	"context"
	"strings"
)

type Recommendation struct {
	Book
	Reason string `json:"reason"`
}

func (s *Store) GetRecommendations(ctx context.Context, userID string) ([]Recommendation, error) {
	rows, err := s.DB.Query(ctx, `
		WITH matched_books AS (
			SELECT
				bc.book_id,
				ARRAY_AGG(DISTINCT c.name) AS matched_categories,
				COUNT(DISTINCT up.category_id) AS match_count
			FROM user_preferences up
			JOIN book_categories bc
				ON bc.category_id = up.category_id
			JOIN categories c
				ON c.id = up.category_id
			WHERE up.user_id = $1
			GROUP BY bc.book_id
		)
		SELECT
			b.id,
			b.title,
			b.author,
			COALESCE(b.description, ''),
			b.cover_url,
			b.published_year,
			COALESCE(
				(
					SELECT ARRAY_AGG(DISTINCT c2.name)
					FROM book_categories bc2
					JOIN categories c2 ON c2.id = bc2.category_id
					WHERE bc2.book_id = b.id
				),
				'{}'
			),
			mb.matched_categories
		FROM matched_books mb
		JOIN books b ON b.id = mb.book_id
		ORDER BY mb.match_count DESC, b.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recommendations := make([]Recommendation, 0)

	for rows.Next() {
		var item Recommendation
		var matchedCategories []string

		err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Author,
			&item.Description,
			&item.CoverURL,
			&item.PublishedYear,
			&item.Categories,
			&matchedCategories,
		)
		if err != nil {
			return nil, err
		}

		item.Reason = "ตรงกับหมวดหมู่ที่คุณสนใจ: " +
			strings.Join(matchedCategories, ", ")

		recommendations = append(recommendations, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return recommendations, nil
}
