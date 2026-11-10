
package handler

import (
	"net/http"

	"github.com/Supattarblue/bookmellow-backend/mw"
)

// ListFavorites returns the logged-in user's favorite books.
func (h *Handler) ListFavorites(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	books, err := h.Store.ListFavorites(r.Context(), claims.UserID)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": books,
	})
}

// AddFavorite adds a book to the user's favorites.
func (h *Handler) AddFavorite(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		BookID string `json:"book_id"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.BookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	err := h.Store.AddFavorite(r.Context(), claims.UserID, input.BookID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not add favorite")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"message": "Book added to favorites",
	})
}

// RemoveFavorite removes a book from the user's favorites.
func (h *Handler) RemoveFavorite(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	bookID := pathID(r)
	if bookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	err := h.Store.RemoveFavorite(r.Context(), claims.UserID, bookID)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Book removed from favorites",
	})
}
