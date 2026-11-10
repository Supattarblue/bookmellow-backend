package handler

import (
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/mw"
	"github.com/Supattarblue/bookmellow-backend/store"
)

// AdminDashboard returns dashboard statistics.
func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Store.AdminDashboard(r.Context())
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// AdminListMembers returns all members matching the search query.
func (h *Handler) AdminListMembers(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	members, err := h.Store.AdminListMembers(r.Context(), search)
	if err != nil {
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": members,
	})
}

// AdminGetMember returns one member.
func (h *Handler) AdminGetMember(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	member, err := h.Store.AdminGetMember(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, member)
}

// AdminUpdateMember updates a member's username and email.
func (h *Handler) AdminUpdateMember(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	var input struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)

	if input.Username == "" || input.Email == "" {
		writeError(w, http.StatusBadRequest, "Username and email are required")
		return
	}

	member, err := h.Store.AdminUpdateMember(
		r.Context(),
		id,
		input.Username,
		input.Email,
	)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		writeError(w, http.StatusConflict, "Email may already be in use")
		return
	}

	writeJSON(w, http.StatusOK, member)
}

// AdminSetMemberRole changes a member's role.
func (h *Handler) AdminSetMemberRole(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	var input struct {
		Role string `json:"role"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Role = strings.TrimSpace(input.Role)

	if input.Role != "member" && input.Role != "admin" {
		writeError(w, http.StatusBadRequest, "Role must be member or admin")
		return
	}

	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if claims.UserID == id {
		writeError(w, http.StatusForbidden, "You cannot change your own role")
		return
	}

	err := h.Store.AdminSetRole(r.Context(), id, input.Role)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Member role updated successfully",
		"role":    input.Role,
	})
}

// AdminDeleteMember deletes a member.
func (h *Handler) AdminDeleteMember(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	claims, ok := mw.GetClaims(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if claims.UserID == id {
		writeError(w, http.StatusForbidden, "You cannot delete your own account")
		return
	}

	err := h.Store.AdminDeleteMember(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Member deleted successfully",
	})
}

// AdminListBooks returns books for the admin page.
func (h *Handler) AdminListBooks(w http.ResponseWriter, r *http.Request) {
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

// AdminGetBook returns one book for the admin page.
func (h *Handler) AdminGetBook(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

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

// AdminCreateBook creates a book.
func (h *Handler) AdminCreateBook(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title         string   `json:"title"`
		Author        string   `json:"author"`
		Description   string   `json:"description"`
		CoverURL      *string  `json:"cover_url"`
		PublishedYear *int     `json:"published_year"`
		CategoryIDs   []string `json:"category_ids"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)

	if input.Title == "" || input.Author == "" {
		writeError(w, http.StatusBadRequest, "Title and author are required")
		return
	}

	book := &store.Book{
		Title:         input.Title,
		Author:        input.Author,
		Description:   input.Description,
		CoverURL:      input.CoverURL,
		PublishedYear: input.PublishedYear,
	}

	err := h.Store.AdminCreateBook(r.Context(), book, input.CategoryIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not create book. Please check category IDs")
		return
	}

	writeJSON(w, http.StatusCreated, book)
}

// AdminUpdateBook updates a book.
func (h *Handler) AdminUpdateBook(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	var input struct {
		Title         string   `json:"title"`
		Author        string   `json:"author"`
		Description   string   `json:"description"`
		CoverURL      *string  `json:"cover_url"`
		PublishedYear *int     `json:"published_year"`
		CategoryIDs   []string `json:"category_ids"`
	}

	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	input.Title = strings.TrimSpace(input.Title)
	input.Author = strings.TrimSpace(input.Author)

	if input.Title == "" || input.Author == "" {
		writeError(w, http.StatusBadRequest, "Title and author are required")
		return
	}

	_, err := h.Store.GetBook(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Book not found")
			return
		}
		internal(w, err)
		return
	}

	book := &store.Book{
		ID:            id,
		Title:         input.Title,
		Author:        input.Author,
		Description:   input.Description,
		CoverURL:      input.CoverURL,
		PublishedYear: input.PublishedYear,
	}

	err = h.Store.AdminUpdateBook(r.Context(), book, input.CategoryIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not update book. Please check category IDs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "Book updated successfully",
		"book":    book,
	})
}

// AdminDeleteBook deletes a book.
func (h *Handler) AdminDeleteBook(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)

	err := h.Store.AdminDeleteBook(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "Book not found")
			return
		}
		internal(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Book deleted successfully",
	})
}
