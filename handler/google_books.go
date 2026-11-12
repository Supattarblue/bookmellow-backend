
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	params.Set("orderBy", "relevance")
	params.Set("key", h.GoogleBooksAPIKey)

	if lang == "th" || lang == "en" {
		params.Set("langRestrict", lang)
	}

	requestURL := googleBooksURL + "?" + params.Encode()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		internal(w, err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}

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

		coverURL = normalizeCoverURL(coverURL)

		if !isValidCoverURL(coverURL) || isPeriodical(title) {
			continue
		}

		results = append(results, GoogleBookResult{
			GoogleBooksID: item.ID,
			Title:         title,
			Author:        author,
			Authors:       info.Authors,
			Description:   strings.TrimSpace(info.Description),
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

func normalizeCoverURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Replace(value, "http://", "https://", 1)
	value = strings.ReplaceAll(value, "&amp;", "&")
	return value
}

func isValidCoverURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" && parsed.Host != ""
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
		"periodical",
		"newsletter",
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
		{
			Query:      "มังงะ การ์ตูน",
			Language:   "th",
			Subject:    "Comics & Graphic Novels",
			Categories: []string{"Young Adult", "Adventure"},
		},
		{
			Query:      "นิยายรัก",
			Language:   "th",
			Subject:    "Romance",
			Categories: []string{"Romance"},
		},
		{
			Query:      "นิยายวาย BL",
			Language:   "th",
			Subject:    "Romance",
			Categories: []string{"Romance", "Young Adult"},
		},
		{
			Query:      "นิยายยูริ GL",
			Language:   "th",
			Subject:    "Romance",
			Categories: []string{"Romance", "Young Adult"},
		},
		{
			Query:      "หนังสือฮีลใจ feel good",
			Language:   "th",
			Subject:    "Fiction",
			Categories: []string{"Young Adult"},
		},
		{
			Query:      "นิยายแฟนตาซี",
			Language:   "th",
			Subject:    "Fantasy",
			Categories: []string{"Fantasy", "Adventure"},
		},
		{
			Query:      "นิยายสืบสวน",
			Language:   "th",
			Subject:    "Mystery",
			Categories: []string{"Mystery", "Thriller"},
		},
		{
			Query:      "จิตวิทยา",
			Language:   "th",
			Subject:    "Psychology",
			Categories: []string{"Self-Help"},
		},
		{
			Query:      "พัฒนาตนเอง",
			Language:   "th",
			Subject:    "Self-Help",
			Categories: []string{"Self-Help"},
		},
		{
			Query:      "manga graphic novels",
			Language:   "en",
			Subject:    "Comics & Graphic Novels",
			Categories: []string{"Young Adult", "Adventure"},
		},
		{
			Query:      "romance novels",
			Language:   "en",
			Subject:    "Romance",
			Categories: []string{"Romance"},
		},
		{
			Query:      "fantasy adventure novels",
			Language:   "en",
			Subject:    "Fantasy",
			Categories: []string{"Fantasy", "Adventure"},
		},
		{
			Query:      "mystery thriller novels",
			Language:   "en",
			Subject:    "Mystery",
			Categories: []string{"Mystery", "Thriller"},
		},
		{
			Query:      "healing feel good books",
			Language:   "en",
			Subject:    "Fiction",
			Categories: []string{"Young Adult"},
		},
		{
			Query:      "psychology books",
			Language:   "en",
			Subject:    "Psychology",
			Categories: []string{"Self-Help"},
		},
		{
			Query:      "self improvement books",
			Language:   "en",
			Subject:    "Self-Help",
			Categories: []string{"Self-Help"},
		},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	resultsByPlan := make([][]GoogleBookResult, len(plans))
	errorsByPlan := make([]error, len(plans))

	semaphore := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for i, plan := range plans {
		wg.Add(1)

		go func(index int, currentPlan bulkSearchPlan) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				errorsByPlan[index] = ctx.Err()
				return
			}

			defer func() {
				<-semaphore
			}()

			results, err := h.fetchGoogleBooksForImport(
				ctx,
				currentPlan.Query,
				currentPlan.Language,
				currentPlan.Subject,
			)

			resultsByPlan[index] = results
			errorsByPlan[index] = err
		}(i, plan)
	}

	wg.Wait()

	if ctx.Err() != nil {
		writeError(w, http.StatusGatewayTimeout, "Google Books search timed out")
		return
	}

	successfulSearches := 0

	for _, searchErr := range errorsByPlan {
		if searchErr == nil {
			successfulSearches++
		}
	}

	if successfulSearches == 0 {
		writeError(w, http.StatusBadGateway, "All Google Books searches failed")
		return
	}

	seen := make(map[string]bool)
	importBooks := make([]store.BulkImportBook, 0, 300)

	for i, plan := range plans {
		if errorsByPlan[i] != nil {
			continue
		}

		for _, result := range resultsByPlan[i] {
			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			description := strings.TrimSpace(result.Description)
			coverURL := normalizeCoverURL(result.CoverURL)

			if title == "" || author == "" || description == "" || !isValidCoverURL(coverURL) {
				continue
			}

			if isPeriodical(title) {
				continue
			}

			key := strings.ToLower(
				strings.Join(strings.Fields(title), " ") + "|" +
					strings.Join(strings.Fields(author), " "),
			)

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
				Language:      plan.Language,
			})
		}
	}

	// นับจำนวนหนังสือที่ผ่านการกรอง แยกตามภาษาที่ใช้ค้นหา
	thaiCandidates := 0
	foreignCandidates := 0

	for _, book := range importBooks {
		switch strings.ToLower(book.Language) {
		case "th":
			thaiCandidates++
		case "en":
			foreignCandidates++
		}
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
		"message":             "Bulk import completed",
		"successful_searches": successfulSearches,
		"found":               len(importBooks),
		"thai_candidates":     thaiCandidates,
		"foreign_candidates":  foreignCandidates,
		"inserted":            inserted,
		"skipped":             skipped,
		"total":               total,
		"remaining":           remaining,
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
	params.Set("orderBy", "relevance")
	params.Set("langRestrict", lang)
	params.Set("key", h.GoogleBooksAPIKey)

	requestURL := googleBooksURL + "?" + params.Encode()

	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 12 * time.Second}

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

	results := make([]GoogleBookResult, 0, len(googleResponse.Items))

	for _, item := range googleResponse.Items {
		info := item.VolumeInfo
		title := strings.TrimSpace(info.Title)
		author := strings.Join(info.Authors, ", ")

		coverURL := info.ImageLinks.Thumbnail
		if coverURL == "" {
			coverURL = info.ImageLinks.SmallThumbnail
		}

		coverURL = normalizeCoverURL(coverURL)

		if title == "" || strings.TrimSpace(author) == "" || !isValidCoverURL(coverURL) {
			continue
		}

		if isPeriodical(title) {
			continue
		}

		results = append(results, GoogleBookResult{
			GoogleBooksID: item.ID,
			Title:         title,
			Author:        author,
			Authors:       info.Authors,
			Description:   strings.TrimSpace(info.Description),
			CoverURL:      coverURL,
			PublishedDate: info.PublishedDate,
			PublishedYear: extractPublishedYear(info.PublishedDate),
			Categories:    info.Categories,
		})
	}

	return results, nil
}
