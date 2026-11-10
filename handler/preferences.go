
package handler

import (
	"net/http"

	"github.com/Supattarblue/bookmellow-backend/mw"
)

// GetPreferences returns the logged-in user's preferred categories.
func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	categories, err := h.Store.GetPreferences(r.Context(), claims.UserID)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": categories,
	})
}

// UpdatePreferences updates the logged-in user's preferred categories.
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		CategoryIDs []string `json:"category_ids"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.CategoryIDs == nil {
		writeError(w, http.StatusBadRequest, "Category IDs are required")
		return
	}

	err := h.Store.UpdatePreferences(
		r.Context(),
		claims.UserID,
		input.CategoryIDs,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not update preferences. Please check category IDs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Preferences updated successfully",
	})
}
