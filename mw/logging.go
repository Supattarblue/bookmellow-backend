package mw

import (
	"log"
	"net/http"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.written {
		return
	}

	rw.statusCode = code
	rw.written = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}

	return rw.ResponseWriter.Write(b)
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(wrapped, r)

		log.Printf(
			"%s %s %d %s",
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			time.Since(start),
		)
	})
}
