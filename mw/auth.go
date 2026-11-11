package mw

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Supattarblue/bookmellow-backend/auth"
)

type contextKey string

const userClaimsKey contextKey = "userClaims"

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

func RequireLogin(issuer *auth.Issuer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		parts := strings.SplitN(header, " ", 2)

		if len(parts) != 2 ||
			!strings.EqualFold(parts[0], "Bearer") ||
			strings.TrimSpace(parts[1]) == "" {
			writeAuthError(w, http.StatusUnauthorized, "Authorization token is required")
			return
		}

		claims, err := issuer.Parse(strings.TrimSpace(parts[1]))
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), userClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalLogin allows both guests and logged-in members.
func OptionalLogin(issuer *auth.Issuer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		parts := strings.SplitN(header, " ", 2)

		if len(parts) == 2 &&
			strings.EqualFold(parts[0], "Bearer") &&
			strings.TrimSpace(parts[1]) != "" {

			claims, err := issuer.Parse(strings.TrimSpace(parts[1]))
			if err == nil {
				ctx := context.WithValue(r.Context(), userClaimsKey, claims)
				r = r.WithContext(ctx)
			}
		}

		next.ServeHTTP(w, r)
	})
}

func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(userClaimsKey).(*auth.Claims)
		if !ok || claims == nil {
			writeAuthError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}

		if claims.Role != role {
			writeAuthError(w, http.StatusForbidden, "Forbidden")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GetClaims(r *http.Request) (*auth.Claims, bool) {
	claims, ok := r.Context().Value(userClaimsKey).(*auth.Claims)
	return claims, ok && claims != nil
}
