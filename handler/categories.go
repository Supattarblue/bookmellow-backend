
package handler

import (
	"net/http"
)

// ListCategories returns all available book categories.
func (h *Handler) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.Store.ListCategories(r.Context())
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": categories,
	})
}
