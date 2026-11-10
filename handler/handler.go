
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/auth"
	"github.com/Supattarblue/bookmellow-backend/store"
)

type Handler struct {
	Store   *store.Store
	Tickets *auth.Issuer
}

func New(s *store.Store, issuer *auth.Issuer) *Handler {
	return &Handler{
		Store:   s,
		Tickets: issuer,
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func internal(w http.ResponseWriter, err error) {
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func pathID(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

func decodeJSON(r *http.Request, data any) error {
	return json.NewDecoder(r.Body).Decode(data)
}
