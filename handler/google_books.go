
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Supattarblue/bookmellow-backend/store"
)

const googleBooksURL = "https://www.googleapis.com/books/v1/volumes"

type GoogleBooksResponse struct {
	Items []GoogleBookItem `json:"items"`
}

type GoogleBookItem struct {
	ID         string               `json:"id"`
	VolumeInfo GoogleBookVolumeInfo `json:"volumeInfo"`
}

type GoogleBookVolumeInfo struct {
	Title         string   `json:"title"`
	Authors       []string `json:"authors"`
	Description   string   `json:"description"`
	PublishedDate string   `json:"publishedDate"`
	Categories    []string `json:"categories"`
	ImageLinks    struct {
		Thumbnail      string `json:"thumbnail"`
		SmallThumbnail string `json:"smallThumbnail"`
	} `json:"imageLinks"`
}

type GoogleBookResult struct {
	GoogleBooksID string   `json:"google_books_id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Authors       []string `json:"authors"`
	Description   string   `json:"description"`
	CoverURL      string   `json:"cover_url"`
	PublishedDate string   `json:"published_date"`
	PublishedYear int      `json:"published_year"`
	Categories    []string `json:"google_categories"`
}

// AdminSearchGoogleBooks searches books from Google Books API.
func (h *Handler) AdminSearchGoogleBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	genre := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("genre")))

	if query == "" {
		writeError(w, http.StatusBadRequest, "Search query is required")
		return
	}

	if len(query) > 200 {
		writeError(w, http.StatusBadRequest, "Search query is too long")
		return
	}

	genreSubjects := map[string]string{
		"manga":      "Comics & Graphic Novels",
		"romance":    "Romance",
		"healing":    "Fiction",
		"fantasy":    "Fantasy",
		"mystery":    "Mystery",
		"psychology": "Psychology",
		"self-help":  "Self-Help",
	}

	searchQuery := query

	if subject, ok := genreSubjects[genre]; ok {
		searchQuery += " subject:" + subject
	}

	params := url.Values{}
	params.Set("q", searchQuery)
	params.Set("maxResults", "40")
	params.Set("printType", "books")
	params.Set("orderBy", "newest")
	params.Set("key", h.GoogleBooksAPIKey)

	if lang == "th" || lang == "en" {
		params.Set("langRestrict", lang)
	}

	requestURL := googleBooksURL + "?" + params.Encode()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		internal(w, err)
		return
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Unable to connect to Google Books")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "Google Books API request failed")
		return
	}

	var googleResponse GoogleBooksResponse

	if err := json.NewDecoder(resp.Body).Decode(&googleResponse); err != nil {
		writeError(w, http.StatusBadGateway, "Invalid response from Google Books")
		return
	}

	results := make([]GoogleBookResult, 0, len(googleResponse.Items))

	for _, item := range googleResponse.Items {
		info := item.VolumeInfo

		title := strings.TrimSpace(info.Title)
		author := strings.Join(info.Authors, ", ")

		if title == "" || strings.TrimSpace(author) == "" {
			continue
		}

		coverURL := info.ImageLinks.Thumbnail
		if coverURL == "" {
			coverURL = info.ImageLinks.SmallThumbnail
		}

		if coverURL == "" || isPeriodical(title) {
			continue
		}

		coverURL = strings.Replace(coverURL, "http://", "https://", 1)

		results = append(results, GoogleBookResult{
			GoogleBooksID: item.ID,
			Title:         title,
			Author:        author,
			Authors:       info.Authors,
			Description:   info.Description,
			CoverURL:      coverURL,
			PublishedDate: info.PublishedDate,
			PublishedYear: extractPublishedYear(info.PublishedDate),
			Categories:    info.Categories,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  results,
		"total": len(results),
	})
}

func isPeriodical(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))

	keywords := []string{
		"นิตยสาร",
		"หนังสือพิมพ์",
		"รายสัปดาห์",
		"รายเดือน",
		"วารสาร",
		"ฉบับที่",
		"ประจำวันที่",
		"magazine",
		"newspaper",
		"weekly",
		"monthly journal",
	}

	for _, keyword := range keywords {
		if strings.Contains(title, keyword) {
			return true
		}
	}

	return false
}

func extractPublishedYear(date string) int {
	date = strings.TrimSpace(date)

	if len(date) < 4 {
		return 0
	}

	var year int
	if _, err := fmt.Sscanf(date[:4], "%d", &year); err != nil {
		return 0
	}

	if year < 1 || year > 9999 {
		return 0
	}

	return year
}

// Bulk import

type bulkSearchPlan struct {
	Query      string
	Language   string
	Subject    string
	Categories []string
}

func (h *Handler) AdminBulkImportBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := []bulkSearchPlan{
		// Thai books
		{"มังงะ การ์ตูน", "th", "Comics & Graphic Novels", []string{"Young Adult", "Adventure"}},
		{"นิยายรัก", "th", "Romance", []string{"Romance"}},
		{"นิยายวาย BL", "th", "Romance", []string{"Romance", "Young Adult"}},
		{"นิยายยูริ GL", "th", "Romance", []string{"Romance", "Young Adult"}},
		{"หนังสือฮีลใจ feel good", "th", "Fiction", []string{"Young Adult"}},
		{"นิยายแฟนตาซี", "th", "Fantasy", []string{"Fantasy", "Adventure"}},
		{"นิยายสืบสวน", "th", "Mystery", []string{"Mystery", "Thriller"}},
		{"จิตวิทยา", "th", "Psychology", []string{"Self-Help"}},
		{"พัฒนาตนเอง", "th", "Self-Help", []string{"Self-Help"}},

		// English books
		{"manga graphic novels", "en", "Comics & Graphic Novels", []string{"Young Adult", "Adventure"}},
		{"romance novels", "en", "Romance", []string{"Romance"}},
		{"fantasy adventure novels", "en", "Fantasy", []string{"Fantasy", "Adventure"}},
		{"mystery thriller novels", "en", "Mystery", []string{"Mystery", "Thriller"}},
		{"healing feel good books", "en", "Fiction", []string{"Young Adult"}},
		{"psychology books", "en", "Psychology", []string{"Self-Help"}},
		{"self improvement books", "en", "Self-Help", []string{"Self-Help"}},
	}

	seen := make(map[string]bool)
	importBooks := make([]store.BulkImportBook, 0, 90)

	thaiCount := 0
	englishCount := 0

	for _, plan := range plans {
		if plan.Language == "th" && thaiCount >= 70 {
			continue
		}
		if plan.Language == "en" && englishCount >= 20 {
			continue
		}

		results, err := h.fetchGoogleBooksForImport(
			r.Context(),
			plan.Query,
			plan.Language,
			plan.Subject,
		)
		if err != nil {
			internal(w, err)
			return
		}

		for _, result := range results {
			if plan.Language == "th" && thaiCount >= 70 {
				break
			}
			if plan.Language == "en" && englishCount >= 20 {
				break
			}

			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			description := strings.TrimSpace(result.Description)
			coverURL := strings.TrimSpace(result.CoverURL)

			if title == "" || author == "" || coverURL == "" || description == "" {
				continue
			}

			if isPeriodical(title) {
				continue
			}

			key := strings.ToLower(title + "|" + author)
			if seen[key] {
				continue
			}
			seen[key] = true

			var publishedYear *int
			if result.PublishedYear > 0 {
				year := result.PublishedYear
				publishedYear = &year
			}

			importBooks = append(importBooks, store.BulkImportBook{
				Title:         title,
				Author:        author,
				Description:   description,
				CoverURL:      coverURL,
				PublishedYear: publishedYear,
				Categories:    plan.Categories,
			})

			if plan.Language == "th" {
				thaiCount++
			} else {
				englishCount++
			}
		}
	}

	if len(importBooks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"message":  "No suitable books were found",
			"inserted": 0,
			"skipped":  0,
			"total":    0,
			"remaining": 100,
		})
		return
	}

	inserted, skipped, total, err := h.Store.AdminBulkImportBooks(
		r.Context(),
		importBooks,
	)
	if err != nil {
		internal(w, err)
		return
	}

	remaining := 100 - total
	if remaining < 0 {
		remaining = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":   "Bulk import completed",
		"found":     len(importBooks),
		"inserted":  inserted,
		"skipped":   skipped,
		"total":     total,
		"remaining": remaining,
	})
}

func (h *Handler) fetchGoogleBooksForImport(
	ctx context.Context,
	query string,
	lang string,
	subject string,
) ([]GoogleBookResult, error) {
	searchQuery := query
	if subject != "" {
		searchQuery += " subject:" + subject
	}

	params := url.Values{}
	params.Set("q", searchQuery)
	params.Set("maxResults", "40")
	params.Set("printType", "books")
	params.Set("orderBy", "newest")
	params.Set("langRestrict", lang)
	params.Set("key", h.GoogleBooksAPIKey)

	requestURL := googleBooksURL + "?" + params.Encode()

	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Books API returned status %d", resp.StatusCode)
	}

	var googleResponse GoogleBooksResponse
	if err := json.NewDecoder(resp.Body).Decode(&googleResponse); err != nil {
		return nil, err
	}

	results := make([]GoogleBookResult, 0)

	for _, item := range googleResponse.Items {
		info := item.VolumeInfo

		title := strings.TrimSpace(info.Title)
		author := strings.Join(info.Authors, ", ")

		coverURL := info.ImageLinks.Thumbnail
		if coverURL == "" {
			coverURL = info.ImageLinks.SmallThumbnail
		}

		if title == "" || strings.TrimSpace(author) == "" || coverURL == "" {
			continue
		}

		if isPeriodical(title) {
			continue
		}

		coverURL = strings.Replace(coverURL, "http://", "https://", 1)

		results = append(results, GoogleBookResult{
			GoogleBooksID: item.ID,
			Title:         title,
			Author:        author,
			Authors:       info.Authors,
			Description:   info.Description,
			CoverURL:      coverURL,
			PublishedDate: info.PublishedDate,
			PublishedYear: extractPublishedYear(info.PublishedDate),
			Categories:    info.Categories,
		})
	}

	return results, nil
}
