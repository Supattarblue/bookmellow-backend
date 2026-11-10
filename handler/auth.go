package handler

import (
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/auth"
	"github.com/Supattarblue/bookmellow-backend/mw"
)

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)

	if input.Username == "" || input.Email == "" || len(input.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Username, email and password (at least 8 characters) are required")
		return
	}

	passwordHash, err := auth.HashPassword(input.Password)
	if err != nil {
		internal(w, err)
		return
	}

	user, err := h.Store.CreateUser(
		r.Context(),
		input.Username,
		input.Email,
		passwordHash,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "Email may already be registered")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "Registration successful",
		"user":    user,
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	email := strings.TrimSpace(input.Email)

	if email == "" || input.Password == "" {
		writeError(w, http.StatusBadRequest, "Email and password are required")
		return
	}

	user, err := h.Store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusUnauthorized, "Invalid email or password")
			return
		}

		internal(w, err)
		return
	}

	if !auth.CheckPassword(user.PasswordHash, input.Password) {
		writeError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	token, err := h.Tickets.Issue(user.ID, user.Role)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  user,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := h.Store.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusUnauthorized, "User not found")
			return
		}

		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
	})
}
