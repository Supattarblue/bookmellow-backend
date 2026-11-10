
package mw

import (
	"net/http"
	"strings"
)

func CORS(allowedOrigins string, next http.Handler) http.Handler {
	origins := strings.Split(allowedOrigins, ",")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigin := false

		for _, allowed := range origins {
			if origin != "" && origin == strings.TrimSpace(allowed) {
				allowedOrigin = true
				break
			}
		}

		if origin != "" && !allowedOrigin {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}

		if allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
