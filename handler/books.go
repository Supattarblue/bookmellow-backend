
package handler

import (
	"net/http"
	"strings"
)

// ListBooks returns books with optional search and category filters.
func (h *Handler) ListBooks(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	books, err := h.Store.ListBooks(r.Context(), search, category)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": books,
	})
}

// GetBook returns a book by ID.
func (h *Handler) GetBook(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	if id == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	book, err := h.Store.GetBook(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Book not found")
			return
		}

		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, book)
}
