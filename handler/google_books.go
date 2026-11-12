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

const (
	googleBooksURL = "https://www.googleapis.com/books/v1/volumes"
	bookTarget     = 100
	minBookYear    = 2019
	importPages    = 2
	importPageSize = 40
)

var supportedBookLanguages = []string{"th", "en", "ja", "ko"}

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
	Language      string   `json:"language"`
	Categories    []string `json:"categories"`
	ImageLinks    struct {
		SmallThumbnail string `json:"smallThumbnail"`
		Thumbnail      string `json:"thumbnail"`
		Small          string `json:"small"`
		Medium         string `json:"medium"`
		Large          string `json:"large"`
		ExtraLarge     string `json:"extraLarge"`
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
	Language      string   `json:"language"`
	Categories    []string `json:"google_categories"`
}

type HomeExternalBook struct {
	ID            string   `json:"id"`
	Source        string   `json:"source"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Description   string   `json:"description"`
	CoverURL      string   `json:"cover_url"`
	PublishedYear int      `json:"published_year"`
	Categories    []string `json:"categories"`
}

func isSupportedBookLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "th", "en", "ja", "ko":
		return true
	default:
		return false
	}
}

func normalizeCoverURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Replace(value, "http://", "https://", 1)
	value = strings.ReplaceAll(value, "&amp;", "&")
	return value
}

func selectBestCover(info GoogleBookVolumeInfo) string {
	candidates := []string{
		info.ImageLinks.ExtraLarge,
		info.ImageLinks.Large,
		info.ImageLinks.Medium,
		info.ImageLinks.Small,
		info.ImageLinks.Thumbnail,
		info.ImageLinks.SmallThumbnail,
	}

	for _, candidate := range candidates {
		candidate = normalizeCoverURL(candidate)
		if isValidCoverURL(candidate) {
			return candidate
		}
	}

	return ""
}

func isValidCoverURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil &&
		parsed.Scheme == "https" &&
		parsed.Host != ""
}

func containsAny(value string, keywords []string) bool {
	value = strings.ToLower(value)

	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

func isPeriodical(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))

	return containsAny(title, []string{
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
	})
}

func isHorrorBook(result GoogleBookResult) bool {
	text := strings.ToLower(
		result.Title + " " +
			result.Description + " " +
			strings.Join(result.Categories, " "),
	)

	return containsAny(text, []string{
		"horror",
		"horrors",
		"horror fiction",
		"ghost horror",
		"supernatural horror",
		"สยองขวัญ",
		"นิยายสยองขวัญ",
		"เรื่องผี",
		"恐怖",
		"ホラー",
		"怪談",
		"공포",
		"호러",
	})
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

func normalizeBookText(value string) string {
	return strings.Join(
		strings.Fields(strings.ToLower(strings.TrimSpace(value))),
		" ",
	)
}

func makeBookDuplicateKey(title, author string) string {
	return normalizeBookText(title) + "|" + normalizeBookText(author)
}

func (h *Handler) AdminSearchGoogleBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"Google Books API key is not configured",
		)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	language := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))

	if query == "" {
		writeError(w, http.StatusBadRequest, "Search query is required")
		return
	}

	if len(query) > 200 {
		writeError(w, http.StatusBadRequest, "Search query is too long")
		return
	}

	if language != "" && !isSupportedBookLanguage(language) {
		writeError(
			w,
			http.StatusBadRequest,
			"Supported languages are th, en, ja, ko",
		)
		return
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("maxResults", "40")
	params.Set("printType", "books")
	params.Set("orderBy", "newest")
	params.Set("key", h.GoogleBooksAPIKey)

	if language != "" {
		params.Set("langRestrict", language)
	}

	requestURL := googleBooksURL + "?" + params.Encode()

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
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

	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"Unable to connect to Google Books",
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeError(
			w,
			http.StatusBadGateway,
			"Google Books API request failed",
		)
		return
	}

	var googleResponse GoogleBooksResponse

	if err := json.NewDecoder(resp.Body).Decode(&googleResponse); err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"Invalid response from Google Books",
		)
		return
	}

	results := make([]GoogleBookResult, 0, len(googleResponse.Items))

	for _, item := range googleResponse.Items {
		info := item.VolumeInfo

		title := strings.TrimSpace(info.Title)
		author := strings.Join(info.Authors, ", ")
		coverURL := selectBestCover(info)
		bookLanguage := strings.ToLower(strings.TrimSpace(info.Language))
		year := extractPublishedYear(info.PublishedDate)

		if title == "" ||
			author == "" ||
			item.ID == "" ||
			!isSupportedBookLanguage(bookLanguage) ||
			!isValidCoverURL(coverURL) ||
			isPeriodical(title) {
			continue
		}

		result := GoogleBookResult{
			GoogleBooksID: item.ID,
			Title:         title,
			Author:        author,
			Authors:       info.Authors,
			Description:   strings.TrimSpace(info.Description),
			CoverURL:      coverURL,
			PublishedDate: info.PublishedDate,
			PublishedYear: year,
			Language:      bookLanguage,
			Categories:    info.Categories,
		}

		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  results,
		"total": len(results),
	})
}

func (h *Handler) DiscoverBooks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data":              []HomeExternalBook{},
		"discovery_enabled": false,
		"message":           "Book discovery is currently disabled.",
		"target":            bookTarget,
		"total":             bookTarget,
	})
}

func (h *Handler) AdminBulkImportBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"Google Books API key is not configured",
		)
		return
	}

	// จำกัดเวลารวมให้สั้นกว่าเวลาที่ Render มักตัดการเชื่อมต่อ
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	results, err := h.fetchGoogleBooksForImport(ctx)

	if err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"Unable to find books from Google Books: "+err.Error(),
		)
		return
	}

	importBooks := make([]store.BulkImportBook, 0, len(results))
	seenBooks := make(map[string]bool)

	rejected := 0

	for _, result := range results {
		title := strings.TrimSpace(result.Title)
		author := strings.TrimSpace(result.Author)
		description := strings.TrimSpace(result.Description)
		coverURL := normalizeCoverURL(result.CoverURL)
		language := strings.ToLower(strings.TrimSpace(result.Language))

		if title == "" ||
			author == "" ||
			result.GoogleBooksID == "" ||
			!isSupportedBookLanguage(language) ||
			!isValidCoverURL(coverURL) ||
			isPeriodical(title) {
			rejected++
			continue
		}

		if result.PublishedYear < minBookYear {
			rejected++
			continue
		}

		if isHorrorBook(result) {
			rejected++
			continue
		}

		duplicateKey := makeBookDuplicateKey(title, author)

		if seenBooks[duplicateKey] {
			continue
		}

		seenBooks[duplicateKey] = true

		year := result.PublishedYear

		importBooks = append(importBooks, store.BulkImportBook{
			Title:         title,
			Author:        author,
			Description:   description,
			CoverURL:      coverURL,
			PublishedYear: &year,
			Categories:    []string{"Other"},
			Language:      language,
		})
	}

	if len(importBooks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"message":                "No books published after 2018 passed the basic filters",
			"found":                  0,
			"rejected":               rejected,
			"minimum_published_year": minBookYear,
			"supported_languages":    supportedBookLanguages,
		})
		return
	}

	// ใช้ ctx เดียวกัน เพื่อให้การบันทึกอยู่ภายใต้เวลาที่กำหนดด้วย
	inserted, skipped, total, stats, err :=
		h.Store.AdminBulkImportBooksDetailed(ctx, importBooks)

	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":                 "Book import completed",
		"found":                   len(importBooks),
		"rejected":                rejected,
		"inserted":                inserted,
		"skipped":                 skipped,
		"skip_reasons":            stats,
		"total_books_in_database": total,
		"visible_catalog_target":  bookTarget,
		"minimum_published_year":  minBookYear,
		"supported_languages":     supportedBookLanguages,
	})
}

func (h *Handler) fetchGoogleBooksForImport(
	ctx context.Context,
) ([]GoogleBookResult, error) {
	type searchTask struct {
		language string
		query    string
		page     int
	}

	languageQueries := map[string]string{
		"th": "หนังสือ",
		"en": "book",
		"ja": "本",
		"ko": "책",
	}

	tasks := make([]searchTask, 0, len(supportedBookLanguages)*importPages)

	for _, language := range supportedBookLanguages {
		for page := 0; page < importPages; page++ {
			tasks = append(tasks, searchTask{
				language: language,
				query:    languageQueries[language],
				page:     page,
			})
		}
	}

	// แต่ละคำขอเขียนผลลง index ของตัวเอง
	pageResults := make([][]GoogleBookResult, len(tasks))

	client := &http.Client{
		Timeout: 8 * time.Second,
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	successfulRequests := 0
	var lastErr error

	for i, task := range tasks {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)

		go func(index int, task searchTask) {
			defer wg.Done()

			requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()

			params := url.Values{}
			params.Set("q", task.query)
			params.Set("maxResults", fmt.Sprintf("%d", importPageSize))
			params.Set("startIndex", fmt.Sprintf("%d", task.page*importPageSize))
			params.Set("printType", "books")
			params.Set("orderBy", "newest")
			params.Set("langRestrict", task.language)
			params.Set("key", h.GoogleBooksAPIKey)

			requestURL := googleBooksURL + "?" + params.Encode()

			req, err := http.NewRequestWithContext(
				requestCtx,
				http.MethodGet,
				requestURL,
				nil,
			)
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				mu.Lock()
				lastErr = fmt.Errorf(
					"Google Books API returned status %d for language %s page %d",
					resp.StatusCode,
					task.language,
					task.page,
				)
				mu.Unlock()
				return
			}

			var googleResponse GoogleBooksResponse

			if err := json.NewDecoder(resp.Body).Decode(&googleResponse); err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			results := make([]GoogleBookResult, 0, len(googleResponse.Items))

			for _, item := range googleResponse.Items {
				info := item.VolumeInfo

				title := strings.TrimSpace(info.Title)
				author := strings.Join(info.Authors, ", ")
				coverURL := selectBestCover(info)
				bookLanguage := strings.ToLower(strings.TrimSpace(info.Language))
				year := extractPublishedYear(info.PublishedDate)

				if title == "" ||
					author == "" ||
					item.ID == "" ||
					bookLanguage != task.language ||
					year < minBookYear ||
					!isValidCoverURL(coverURL) ||
					isPeriodical(title) {
					continue
				}

				result := GoogleBookResult{
					GoogleBooksID: item.ID,
					Title:         title,
					Author:        author,
					Authors:       info.Authors,
					Description:   strings.TrimSpace(info.Description),
					CoverURL:      coverURL,
					PublishedDate: info.PublishedDate,
					PublishedYear: year,
					Language:      bookLanguage,
					Categories:    info.Categories,
				}

				if isHorrorBook(result) {
					continue
				}

				results = append(results, result)
			}

			pageResults[index] = results

			mu.Lock()
			successfulRequests++
			mu.Unlock()
		}(i, task)
	}

	wg.Wait()

	allResults := make([]GoogleBookResult, 0)
	seenIDs := make(map[string]bool)

	for _, page := range pageResults {
		for _, result := range page {
			if seenIDs[result.GoogleBooksID] {
				continue
			}

			seenIDs[result.GoogleBooksID] = true
			allResults = append(allResults, result)
		}
	}

	if successfulRequests == 0 {
		if lastErr != nil {
			return nil, lastErr
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, fmt.Errorf("no successful Google Books search")
	}

	return allResults, nil
}
