
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Supattarblue/bookmellow-backend/mw"
)

func (h *Handler) GetBookVote(w http.ResponseWriter, r *http.Request) {
	bookID := r.PathValue("id")

	if bookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	summary, err := h.Store.GetVoteSummary(r.Context(), bookID)
	if err != nil {
		internal(w, err)
		return
	}

	var userVote any

	claims, ok := mw.GetClaims(r)
	if ok {
		vote, err := h.Store.GetUserVote(
			r.Context(),
			claims.UserID,
			bookID,
		)
		if err != nil {
			internal(w, err)
			return
		}
		userVote = vote
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary":   summary,
		"user_vote": userVote,
	})
}

func (h *Handler) SaveBookVote(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var input struct {
		Rating int `json:"rating"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if input.Rating < 1 || input.Rating > 5 {
		writeError(w, http.StatusBadRequest, "Rating must be between 1 and 5")
		return
	}

	bookID := r.PathValue("id")
	if bookID == "" {
		writeError(w, http.StatusBadRequest, "Book ID is required")
		return
	}

	vote, err := h.Store.UpsertVote(
		r.Context(),
		claims.UserID,
		bookID,
		input.Rating,
	)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"vote": vote,
	})
}
