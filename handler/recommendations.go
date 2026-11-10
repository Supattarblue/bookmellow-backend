
package handler

import (
	"net/http"

	"github.com/Supattarblue/bookmellow-backend/mw"
)

// GetRecommendations returns books recommended for the logged-in user.
func (h *Handler) GetRecommendations(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	recommendations, err := h.Store.GetRecommendations(
		r.Context(),
		claims.UserID,
	)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": recommendations,
	})
}
