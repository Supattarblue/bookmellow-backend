package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Supattarblue/bookmellow-backend/auth"
	"github.com/Supattarblue/bookmellow-backend/handler"
	"github.com/Supattarblue/bookmellow-backend/mw"
	"github.com/Supattarblue/bookmellow-backend/store"
	"github.com/jackc/pgx/v5/pgxpool"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	jwtSecret := os.Getenv("JWT_SECRET")
	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	port := os.Getenv("PORT")

	if databaseURL == "" {
		return errConfig("DATABASE_URL is required")
	}

	if jwtSecret == "" {
		return errConfig("JWT_SECRET is required")
	}

	if allowedOrigins == "" {
		return errConfig("ALLOWED_ORIGINS is required")
	}

	if port == "" {
		port = "8080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return err
	}

	log.Println("Database connected successfully")

	st := store.New(db)
	issuer := auth.NewIssuer(jwtSecret)

	googleBooksAPIKey := os.Getenv("GOOGLE_BOOKS_API_KEY")

	h := handler.New(st, issuer, googleBooksAPIKey)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := st.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "unhealthy",
				"error":  "Database connection failed",
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// Authentication
	mux.HandleFunc("POST /api/auth/register", h.Register)
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.Handle("GET /api/auth/me",
		mw.RequireLogin(issuer, http.HandlerFunc(h.Me)),
	)

	// Books
	mux.HandleFunc("GET /api/books", h.ListBooks)
	mux.HandleFunc("GET /api/books/{id}", h.GetBook)

	// Categories
	mux.HandleFunc("GET /api/categories", h.ListCategories)

	// Member preferences
	mux.Handle("GET /api/me/preferences",
		mw.RequireLogin(issuer, http.HandlerFunc(h.GetPreferences)),
	)
	mux.Handle("PUT /api/me/preferences",
		mw.RequireLogin(issuer, http.HandlerFunc(h.UpdatePreferences)),
	)

	// Recommendations
	mux.Handle("GET /api/recommendations",
		mw.RequireLogin(issuer, http.HandlerFunc(h.GetRecommendations)),
	)

	// Favorites
	mux.Handle("GET /api/me/favorites",
		mw.RequireLogin(issuer, http.HandlerFunc(h.ListFavorites)),
	)
	mux.Handle("POST /api/me/favorites",
		mw.RequireLogin(issuer, http.HandlerFunc(h.AddFavorite)),
	)
	mux.Handle("DELETE /api/me/favorites/{id}",
		mw.RequireLogin(issuer, http.HandlerFunc(h.RemoveFavorite)),
	)

	// Reviews
	mux.HandleFunc("GET /api/books/{id}/reviews", h.ListReviews)
	mux.Handle("POST /api/books/{id}/reviews",
		mw.RequireLogin(issuer, http.HandlerFunc(h.CreateReview)),
	)
	mux.Handle("PUT /api/reviews/{id}",
		mw.RequireLogin(issuer, http.HandlerFunc(h.UpdateReview)),
	)
	mux.Handle("DELETE /api/reviews/{id}",
		mw.RequireLogin(issuer, http.HandlerFunc(h.DeleteReview)),
	)
	// Votes
	mux.HandleFunc("GET /api/books/{id}/vote", h.GetBookVote)
	mux.Handle("PUT /api/books/{id}/vote",
    mw.RequireLogin(issuer, http.HandlerFunc(h.SaveBookVote)),
)

	// Admin middleware
	adminOnly := func(next http.Handler) http.Handler {
		return mw.RequireLogin(
			issuer,
			mw.RequireRole("admin", next),
		)
	}

	// Admin dashboard
	mux.Handle("GET /api/admin/dashboard",
		adminOnly(http.HandlerFunc(h.AdminDashboard)),
	)

	// Admin members
	mux.Handle("GET /api/admin/members",
		adminOnly(http.HandlerFunc(h.AdminListMembers)),
	)
	mux.Handle("GET /api/admin/members/{id}",
		adminOnly(http.HandlerFunc(h.AdminGetMember)),
	)
	mux.Handle("PUT /api/admin/members/{id}/role",
		adminOnly(http.HandlerFunc(h.AdminSetMemberRole)),
	)
	mux.Handle("DELETE /api/admin/members/{id}",
		adminOnly(http.HandlerFunc(h.AdminDeleteMember)),
	)

	// Admin books
	mux.Handle("GET /api/admin/books",
		adminOnly(http.HandlerFunc(h.AdminListBooks)),
	)
	mux.Handle("POST /api/admin/books",
		adminOnly(http.HandlerFunc(h.AdminCreateBook)),
	)
	mux.Handle("GET /api/admin/books/{id}",
		adminOnly(http.HandlerFunc(h.AdminGetBook)),
	)
	mux.Handle("PUT /api/admin/books/{id}",
		adminOnly(http.HandlerFunc(h.AdminUpdateBook)),
	)
	mux.Handle("DELETE /api/admin/books/{id}",
		adminOnly(http.HandlerFunc(h.AdminDeleteBook)),
	)

	// Admin Google Books search
	mux.Handle("GET /api/admin/external-books",
		adminOnly(http.HandlerFunc(h.AdminSearchGoogleBooks)),
	)

	// Middleware
	var serverHandler http.Handler = mux
	serverHandler = mw.CORS(allowedOrigins, serverHandler)
	serverHandler = mw.Logging(serverHandler)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           serverHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("BookMellow API is running on port %s", port)

	return server.ListenAndServe()
}

type configError string

func (e configError) Error() string {
	return string(e)
}

func errConfig(message string) error {
	return configError(message)
}
