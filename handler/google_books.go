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
	googleBooksURL           = "https://www.googleapis.com/books/v1/volumes"
	minBookYear              = 2019
	importPageSize           = 40
	maxImportResultsPerQuery = 1000
)

var supportedBookLanguages = []string{"th", "en", "ja", "ko"}

type GoogleBooksResponse struct {
	TotalItems int              `json:"totalItems"`
	Items      []GoogleBookItem `json:"items"`
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

type bulkSearchPlan struct {
	ThaiQuery    string
	EnglishQuery string
	Subject      string
	Category     string
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

	plans := []bulkSearchPlan{
		{
			ThaiQuery:    "นิยายอิงประวัติศาสตร์ ภาษาไทย",
			EnglishQuery: "historical fiction novels",
			Subject:      "Historical Fiction",
			Category:     "Historical Fiction",
		},
		{
			ThaiQuery:    "หนังสือธุรกิจ การบริหาร การตลาด ผู้ประกอบการ ภาษาไทย",
			EnglishQuery: "business management marketing entrepreneurship books",
			Subject:      "Business & Economics",
			Category:     "Business",
		},
		{
			ThaiQuery:    "ชีวประวัติ อัตชีวประวัติ บุคคลสำคัญ ภาษาไทย",
			EnglishQuery: "biography autobiography memoir books",
			Subject:      "Biography & Autobiography",
			Category:     "Biography",
		},
		{
			ThaiQuery:    "นิทาน หนังสือเด็ก วรรณกรรมเยาวชน ภาษาไทย",
			EnglishQuery: "children's books juvenile fiction",
			Subject:      "Juvenile Fiction",
			Category:     "Children",
		},
		{
			ThaiQuery:    "หนังสือการศึกษา การเรียนรู้ การสอน ภาษาไทย",
			EnglishQuery: "education learning teaching books",
			Subject:      "Education",
			Category:     "Education",
		},
		{
			ThaiQuery:    "นิยายวิทยาศาสตร์ ไซไฟ ภาษาไทย",
			EnglishQuery: "science fiction novels",
			Subject:      "Science Fiction",
			Category:     "Science Fiction",
		},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()

	resultsByPlan := make([][]GoogleBookResult, len(plans))
	errorsByPlan := make([]string, len(plans))

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 6)

	for i, plan := range plans {
		wg.Add(1)

		go func(index int, currentPlan bulkSearchPlan) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				errorsByPlan[index] = ctx.Err().Error()
				return
			}
			defer func() {
				<-semaphore
			}()

			var thaiResults []GoogleBookResult
			var englishResults []GoogleBookResult
			var thaiErr error
			var englishErr error

			var searchWG sync.WaitGroup
			searchWG.Add(2)

			go func() {
				defer searchWG.Done()
				thaiResults, thaiErr = h.fetchGoogleBooksForImport(
					ctx,
					currentPlan.ThaiQuery,
					currentPlan.Subject,
					"th",
				)
			}()

			go func() {
				defer searchWG.Done()
				englishResults, englishErr = h.fetchGoogleBooksForImport(
					ctx,
					currentPlan.EnglishQuery,
					currentPlan.Subject,
					"en",
				)
			}()

			searchWG.Wait()

			combined := make(
				[]GoogleBookResult,
				0,
				len(thaiResults)+len(englishResults),
			)

			seenIDs := make(map[string]bool)

			for _, result := range thaiResults {
				if seenIDs[result.GoogleBooksID] {
					continue
				}

				seenIDs[result.GoogleBooksID] = true
				combined = append(combined, result)
			}

			for _, result := range englishResults {
				if seenIDs[result.GoogleBooksID] {
					continue
				}

				seenIDs[result.GoogleBooksID] = true
				combined = append(combined, result)
			}

			if len(combined) == 0 {
				if thaiErr != nil && englishErr != nil {
					errorsByPlan[index] = fmt.Sprintf(
						"Thai search: %v; English search: %v",
						thaiErr,
						englishErr,
					)
					return
				}

				if thaiErr != nil {
					errorsByPlan[index] = thaiErr.Error()
				} else if englishErr != nil {
					errorsByPlan[index] = englishErr.Error()
				}
			}

			resultsByPlan[index] = combined
		}(i, plan)
	}

	wg.Wait()

	if ctx.Err() != nil {
		writeError(
			w,
			http.StatusGatewayTimeout,
			"Google Books search timed out. Please try again.",
		)
		return
	}

	importBooks := make([]store.BulkImportBook, 0, 480)
	seenBooks := make(map[string]bool)
	candidateCounts := make(map[string]int)
	categoryErrors := make(map[string]string)

	for i, plan := range plans {
		if errorsByPlan[i] != "" {
			categoryErrors[plan.Category] = errorsByPlan[i]
			continue
		}

		for _, result := range resultsByPlan[i] {
			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			description := strings.TrimSpace(result.Description)
			coverURL := normalizeCoverURL(result.CoverURL)
			language := strings.ToLower(strings.TrimSpace(result.Language))

			if title == "" ||
				author == "" ||
				result.GoogleBooksID == "" ||
				result.PublishedYear < minBookYear ||
				!isSupportedBookLanguage(language) ||
				!isValidCoverURL(coverURL) ||
				isPeriodical(title) {
				continue
			}

			if isHorrorBook(result) {
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
				Categories:    []string{plan.Category},
				Language:      language,
			})

			candidateCounts[plan.Category]++
		}
	}

	if len(importBooks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"message":                "No eligible books found",
			"found":                  0,
			"candidate_counts":       candidateCounts,
			"category_errors":        categoryErrors,
			"minimum_published_year": minBookYear,
		})
		return
	}

	inserted, skipped, total, stats, err :=
		h.Store.AdminBulkImportBooksDetailed(ctx, importBooks)

	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":                 "Category book import completed",
		"found":                   len(importBooks),
		"inserted":                inserted,
		"skipped":                 skipped,
		"candidate_counts":        candidateCounts,
		"inserted_by_category":    stats.InsertedByCategory,
		"category_errors":         categoryErrors,
		"skip_reasons":            stats,
		"total_books_in_database": total,
		"minimum_published_year":  minBookYear,
	})
}

func (h *Handler) fetchGoogleBooksForImport(
	ctx context.Context,
	query string,
	subject string,
	language string,
) ([]GoogleBookResult, error) {
	searchQuery := strings.TrimSpace(query)

	if subject != "" {
		searchQuery += " subject:" + subject
	}

	client := &http.Client{Timeout: 12 * time.Second}

	results := make([]GoogleBookResult, 0)
	totalItems := maxImportResultsPerQuery

	for startIndex := 0; startIndex < maxImportResultsPerQuery; startIndex += importPageSize {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		params := url.Values{}
		params.Set("q", searchQuery)
		params.Set("maxResults", fmt.Sprintf("%d", importPageSize))
		params.Set("startIndex", fmt.Sprintf("%d", startIndex))
		params.Set("printType", "books")
		params.Set("orderBy", "newest")
		params.Set("langRestrict", language)
		params.Set("key", h.GoogleBooksAPIKey)

		requestURL := googleBooksURL + "?" + params.Encode()

		requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)

		req, err := http.NewRequestWithContext(
			requestCtx,
			http.MethodGet,
			requestURL,
			nil,
		)
		if err != nil {
			cancel()
			return results, err
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return results, err
		}

		if resp.StatusCode != http.StatusOK {
			statusCode := resp.StatusCode
			resp.Body.Close()
			cancel()

			return results, fmt.Errorf(
				"Google Books API returned status %d for language %s",
				statusCode,
				language,
			)
		}

		var googleResponse GoogleBooksResponse

		decodeErr := json.NewDecoder(resp.Body).Decode(&googleResponse)
		resp.Body.Close()
		cancel()

		if decodeErr != nil {
			return results, decodeErr
		}

		if startIndex == 0 {
			totalItems = googleResponse.TotalItems

			if totalItems > maxImportResultsPerQuery {
				totalItems = maxImportResultsPerQuery
			}
		}

		if len(googleResponse.Items) == 0 {
			break
		}

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
				bookLanguage != language ||
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

		if len(googleResponse.Items) < importPageSize {
			break
		}

		if startIndex+len(googleResponse.Items) >= totalItems {
			break
		}
	}

	return results, nil
}
