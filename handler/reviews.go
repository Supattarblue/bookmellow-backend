
package handler

import (
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/mw"
)

func (h *Handler) ListReviews(w http.ResponseWriter, r *http.Request) {
	bookID := pathID(r)

	if bookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	reviews, err := h.Store.ListReviews(r.Context(), bookID)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"reviews": reviews,
	})
}

func (h *Handler) CreateReview(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.Rating < 1 || input.Rating > 5 {
		writeError(w, http.StatusBadRequest, "Rating must be between 1 and 5")
		return
	}

	bookID := pathID(r)
	if bookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	review, err := h.Store.CreateReview(
		r.Context(),
		claims.UserID,
		bookID,
		input.Rating,
		strings.TrimSpace(input.Comment),
	)
	if err != nil {
		writeError(w, http.StatusConflict, "Could not create review. You may have already reviewed this book")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"review": review,
	})
}

func (h *Handler) UpdateReview(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.Rating < 1 || input.Rating > 5 {
		writeError(w, http.StatusBadRequest, "Rating must be between 1 and 5")
		return
	}

	reviewID := pathID(r)
	if reviewID == "" {
		writeError(w, http.StatusBadRequest, "Review ID is required")
		return
	}

	review, err := h.Store.UpdateReview(
		r.Context(),
		reviewID,
		claims.UserID,
		input.Rating,
		strings.TrimSpace(input.Comment),
	)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Review not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"review": review,
	})
}

func (h *Handler) DeleteReview(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	reviewID := pathID(r)
	if reviewID == "" {
		writeError(w, http.StatusBadRequest, "Review ID is required")
		return
	}

	err := h.Store.DeleteReview(r.Context(), reviewID, claims.UserID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Review not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Review deleted successfully",
	})
}
func (h *Handler) AdminListAllReviews(w http.ResponseWriter, r *http.Request) {
	reviews, err := h.Store.AdminListAllReviews(r.Context())
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": reviews,
	})
}