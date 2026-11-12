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
	"unicode"

	"github.com/Supattarblue/bookmellow-backend/store"
)

const (
	googleBooksURL = "https://www.googleapis.com/books/v1/volumes"
	bookTarget     = 100
	importPages    = 2
	importPageSize = 40
	minBookYear    = 2018
)

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

type bulkSearchPlan struct {
	Queries    []string
	Subject    string
	Kind       string
	Categories []string
	Target     int
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
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func isPeriodical(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))

	return containsAny(title, []string{
		"นิตยสาร", "หนังสือพิมพ์", "รายสัปดาห์", "รายเดือน",
		"วารสาร", "ฉบับที่", "ประจำวันที่", "magazine",
		"newspaper", "weekly", "monthly journal", "periodical",
		"newsletter",
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

func containsAny(value string, keywords []string) bool {
	value = strings.ToLower(value)

	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			return true
		}
	}

	return false
}

func hasThaiText(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Scripts["Thai"], r) {
			return true
		}
	}
	return false
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

func isLowQualityDiscoverBook(result GoogleBookResult) bool {
	text := strings.ToLower(result.Title + " " + result.Description)

	return containsAny(text, []string{
		"คู่มือเตรียมสอบ", "แนวข้อสอบ", "ข้อสอบ", "เตรียมสอบ",
		"หลักสูตร", "วิทยานิพนธ์", "การจัดการเรียนรู้",
		"สำหรับครู", "สําหรับครู", "ผู้ปกครอง",
		"รายงานการวิจัย", "งานวิจัย",
		"เอกสารประกอบการสอน", "เอกสารคำสอน",
		"academic journal", "bibliography",
		"exam preparation", "study guide", "teacher's guide",
		"research report", "dissertation", "thesis",
	})
}

func isAdultRomance(text string) bool {
	return containsAny(text, []string{
		"erotic", "erotica", "explicit sexual", "explicit sex",
		"sexually explicit", "sexual content", "steamy",
		"spicy romance", "dark erotic", "erotic romance",
		"dominant and possessive", "bdsm", "seduction",
		"hot and steamy", "graphic sex", "adult romance",
		"สำหรับผู้ใหญ่", "เนื้อหาทางเพศ", "ฉากร่วมเพศ",
		"อีโรติก", "อิโรติก", "เรท 18", "18+",
	})
}

func isRecentBook(result GoogleBookResult) bool {
	return result.PublishedYear >= minBookYear
}

func isRelevantDiscoverBook(result GoogleBookResult, plan bulkSearchPlan) bool {
	title := strings.ToLower(strings.TrimSpace(result.Title))
	description := strings.ToLower(strings.TrimSpace(result.Description))
	googleCategories := strings.ToLower(strings.Join(result.Categories, " "))
	text := title + " " + description + " " + googleCategories

	if title == "" || result.GoogleBooksID == "" || !hasThaiText(result.Title) {
		return false
	}

	if isPeriodical(title) || isLowQualityDiscoverBook(result) {
		return false
	}

	switch plan.Kind {
	case "self-help":
		return containsAny(text, []string{
			"จิตวิทยา", "พัฒนาตนเอง", "พัฒนาตัวเอง",
			"ความคิด", "อารมณ์", "ความมั่นใจ", "การใช้ชีวิต",
			"ความสำเร็จ", "เป้าหมาย", "วินัย", "เข้าใจตัวเอง",
			"พลังบวก", "ความสุข", "นิสัย", "แรงบันดาลใจ",
			"mindset", "ความสัมพันธ์", "ค้นหาตัวเอง",
		})

	case "manga":
		return containsAny(text, []string{
			"มังงะ", "การ์ตูน", "ไลท์โนเวล",
			"ไคจู", "สัตว์ประหลาด", "ภาพการ์ตูน",
			"comics", "graphic novel",
		})

	case "english":
		return containsAny(text, []string{
			"ภาษาอังกฤษ", "คำศัพท์", "ไวยากรณ์",
			"เรียนภาษาอังกฤษ", "ฝึกภาษาอังกฤษ",
			"สนทนาภาษาอังกฤษ", "อ่านภาษาอังกฤษ",
		})

	case "fantasy":
		return containsAny(text, []string{
			"แฟนตาซี", "เวทมนตร์", "เวทมนต์", "มังกร",
			"พ่อมด", "แม่มด", "อาณาจักร", "แฮร์รี่ พอตเตอร์",
			"ผจญภัย", "เวทมนตร์", "ต่างโลก",
		})

	case "mystery":
		return containsAny(text, []string{
			"สืบสวน", "สอบสวน", "ปริศนา", "ฆาตกรรม",
			"นักสืบ", "ลึกลับ", "ระทึกขวัญ", "คดี",
			"สืบสวนสอบสวน",
		})

	case "lifestyle":
		return containsAny(text, []string{
			"บ้าน", "ตกแต่งบ้าน", "จัดบ้าน", "ไลฟ์สไตล์",
			"แต่งบ้าน", "การใช้ชีวิต", "จัดระเบียบบ้าน",
			"ออกแบบบ้าน", "สวน", "minimal",
		})

	case "romance":
		return !isAdultRomance(text) && containsAny(text, []string{
			"รัก", "โรแมนติก", "ความรัก", "หัวใจ",
			"ความสัมพันธ์", "นิยายรัก",
		})

	case "healing":
		return containsAny(text, []string{
			"ฮีลใจ", "เยียวยา", "อบอุ่นหัวใจ",
			"กำลังใจ", "ความสุข", "มิตรภาพ",
			"วรรณกรรม", "ความทรงจำ", "เติบโต",
		})

	case "bl":
		return !isAdultRomance(text) && containsAny(text, []string{
			"วาย", "ชายรักชาย", "นิยายวาย",
			"บอยเลิฟ", "boy love", "boys love",
			"ความรักระหว่างผู้ชาย",
		})

	case "business":
		return containsAny(text, []string{
			"การเงิน", "ลงทุน", "ธุรกิจ", "การตลาด",
			"บริหาร", "ผู้ประกอบการ", "ออมเงิน",
			"วางแผนการเงิน", "การลงทุน",
		})
	}

	return false
}

func (h *Handler) AdminSearchGoogleBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
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
	params.Set("langRestrict", "th")
	params.Set("key", h.GoogleBooksAPIKey)

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
		coverURL := selectBestCover(info)

		if title == "" || author == "" || item.ID == "" ||
			!hasThaiText(title) ||
			strings.ToLower(strings.TrimSpace(info.Language)) != "th" ||
			!isValidCoverURL(coverURL) ||
			isPeriodical(title) {
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
			Language:      "th",
			Categories:    info.Categories,
		})
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
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := []bulkSearchPlan{
		{
			Queries: []string{
				"หนังสือจิตวิทยา พัฒนาตัวเอง mindset ความคิด",
				"หนังสือพัฒนาตนเอง ความมั่นใจ ความสัมพันธ์",
			},
			Subject:    "Self-Help",
			Kind:       "self-help",
			Categories: []string{"Self-Help"},
			Target:     12,
		},
		{
			Queries: []string{
				"มังงะไทย ไลท์โนเวล การ์ตูนญี่ปุ่น ฉบับภาษาไทย",
				"การ์ตูนญี่ปุ่น มังงะ แฟนตาซี ภาษาไทย",
			},
			Subject:    "Comics & Graphic Novels",
			Kind:       "manga",
			Categories: []string{"Adventure"},
			Target:     12,
		},
		{
			Queries: []string{
				"หนังสือภาษาอังกฤษ คำศัพท์ สนทนา ภาษาไทย",
				"เรียนภาษาอังกฤษด้วยตัวเอง คำศัพท์ ไวยากรณ์",
			},
			Subject:    "Foreign Language Study",
			Kind:       "english",
			Categories: []string{"Education"},
			Target:     8,
		},
		{
			Queries: []string{
				"นิยายแฟนตาซี เวทมนตร์ ผจญภัย ภาษาไทย",
				"นิยายแฟนตาซีแปล อาณาจักร เวทมนตร์",
			},
			Subject:    "Fantasy",
			Kind:       "fantasy",
			Categories: []string{"Fantasy"},
			Target:     14,
		},
		{
			Queries: []string{
				"นิยายสืบสวนสอบสวน ปริศนา นักสืบ ภาษาไทย",
				"นิยายแปล สืบสวน ฆาตกรรม ระทึกขวัญ",
			},
			Subject:    "Mystery",
			Kind:       "mystery",
			Categories: []string{"Mystery", "Thriller"},
			Target:     12,
		},
		{
			Queries: []string{
				"หนังสือไลฟ์สไตล์ จัดบ้าน แต่งบ้าน ภาษาไทย",
				"หนังสือ lifestyle minimal living ภาษาไทย",
			},
			Subject:    "House & Home",
			Kind:       "lifestyle",
			Categories: []string{"Business"},
			Target:     8,
		},
		{
			Queries: []string{
				"นิยายรักโรแมนติก ฉบับภาษาไทย",
				"นิยายรักแปล ความสัมพันธ์ โรแมนติก",
			},
			Subject:    "Romance",
			Kind:       "romance",
			Categories: []string{"Romance"},
			Target:     12,
		},
		{
			Queries: []string{
				"หนังสือฮีลใจ วรรณกรรมอบอุ่นหัวใจ ภาษาไทย",
				"นิยาย feel good มิตรภาพ การเติบโต ภาษาไทย",
			},
			Subject:    "Fiction",
			Kind:       "healing",
			Categories: []string{"Self-Help"},
			Target:     10,
		},
		{
			Queries: []string{
				"นิยายวาย boy love ฉบับภาษาไทย",
				"นิยายชายรักชาย ความรัก ภาษาไทย",
			},
			Subject:    "Romance",
			Kind:       "bl",
			Categories: []string{"Romance"},
			Target:     6,
		},
		{
			Queries: []string{
				"หนังสือการเงิน การลงทุน ภาษาไทย",
				"หนังสือธุรกิจ การตลาด ผู้ประกอบการ ภาษาไทย",
			},
			Subject:    "Business & Economics",
			Kind:       "business",
			Categories: []string{"Business"},
			Target:     6,
		},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 85*time.Second)
	defer cancel()

	resultsByPlan := make([][]GoogleBookResult, len(plans))
	errorsByPlan := make([]error, len(plans))

	semaphore := make(chan struct{}, 5)
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
			defer func() { <-semaphore }()

			results, err := h.fetchGoogleBooksForImport(
				ctx,
				currentPlan.Queries,
				currentPlan.Subject,
			)

			resultsByPlan[index] = results
			errorsByPlan[index] = err
		}(i, plan)
	}

	wg.Wait()

	successfulSearches := 0
	for _, searchErr := range errorsByPlan {
		if searchErr == nil {
			successfulSearches++
		}
	}

	if successfulSearches == 0 {
		if ctx.Err() != nil {
			writeError(w, http.StatusGatewayTimeout, "Google Books search timed out")
			return
		}
		writeError(w, http.StatusBadGateway, "All Google Books searches failed")
		return
	}

	// สร้างรายการสำรองของแต่ละหมวดก่อนนำเข้า
	// จำกัดจำนวนสำรองเพื่อไม่ให้หมวดเดียวครองรายการทั้งหมด
	candidatesByPlan := make([][]store.BulkImportBook, len(plans))
	rejectedByRelevance := 0

	for i, plan := range plans {
		if errorsByPlan[i] != nil {
			continue
		}

		seenIDs := make(map[string]bool)
		seenBooks := make(map[string]bool)
		maxCandidates := plan.Target * 6

		for _, result := range resultsByPlan[i] {
			if len(candidatesByPlan[i]) >= maxCandidates {
				break
			}

			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			description := strings.TrimSpace(result.Description)
			coverURL := normalizeCoverURL(result.CoverURL)

			if title == "" || author == "" ||
				result.GoogleBooksID == "" ||
				!hasThaiText(title) ||
				result.Language != "th" ||
				!isValidCoverURL(coverURL) ||
				isPeriodical(title) {
				continue
			}

			// เน้นหนังสือฉบับใหม่ตั้งแต่ปี 2018 เป็นต้นไป
			if !isRecentBook(result) {
				rejectedByRelevance++
				continue
			}

			if isLowQualityDiscoverBook(result) ||
				isAdultRomance(title+" "+description) ||
				!isRelevantDiscoverBook(result, plan) {
				rejectedByRelevance++
				continue
			}

			duplicateKey := makeBookDuplicateKey(title, author)
			if seenIDs[result.GoogleBooksID] || seenBooks[duplicateKey] {
				continue
			}

			seenIDs[result.GoogleBooksID] = true
			seenBooks[duplicateKey] = true

			var publishedYear *int
			year := result.PublishedYear
			publishedYear = &year

			candidatesByPlan[i] = append(
				candidatesByPlan[i],
				store.BulkImportBook{
					Title:         title,
					Author:        author,
					Description:   description,
					CoverURL:      coverURL,
					PublishedYear: publishedYear,
					Categories:    plan.Categories,
					Language:      "th",
				},
			)
		}
	}

	// สลับหยิบรายการจากแต่ละหมวดทีละเล่ม
	// เพื่อให้หมวดต่าง ๆ ได้โอกาสเติมครบตามสัดส่วน
	importBooks := make([]store.BulkImportBook, 0, bookTarget*4)
	maxRound := 0

	for _, candidates := range candidatesByPlan {
		if len(candidates) > maxRound {
			maxRound = len(candidates)
		}
	}

	for round := 0; round < maxRound; round++ {
		for i := range candidatesByPlan {
			if round < len(candidatesByPlan[i]) {
				importBooks = append(importBooks, candidatesByPlan[i][round])
			}
		}
	}

	if len(importBooks) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"message":                 "No recent Thai books passed the import filters",
			"successful_searches":     successfulSearches,
			"search_plans":            len(plans),
			"candidate_target":        bookTarget,
			"found":                   0,
			"rejected_by_relevance":   rejectedByRelevance,
			"inserted":                0,
			"skipped":                 0,
			"total_books_in_database": 0,
			"visible_catalog_target":  bookTarget,
		})
		return
	}

	inserted, skipped, total, stats, err :=
		h.Store.AdminBulkImportBooksDetailed(r.Context(), importBooks)

	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":             "Thai book import completed",
		"successful_searches": successfulSearches,
		"search_plans":        len(plans),
		"category_targets": map[string]int{
			"self_help": 12,
			"manga":     12,
			"english":   8,
			"fantasy":   14,
			"mystery":   12,
			"lifestyle": 8,
			"romance":   12,
			"healing":   10,
			"bl":        6,
			"business":  6,
		},
		"candidate_target":        bookTarget,
		"found":                   len(importBooks),
		"rejected_by_relevance":   rejectedByRelevance,
		"inserted":                inserted,
		"skipped":                 skipped,
		"skip_reasons":            stats,
		"total_books_in_database": total,
		"visible_catalog_target":  bookTarget,
	})
}

func (h *Handler) fetchGoogleBooksForImport(
	ctx context.Context,
	queries []string,
	subject string,
) ([]GoogleBookResult, error) {
	results := make([]GoogleBookResult, 0)
	seenIDs := make(map[string]bool)
	successfulRequests := 0
	var lastErr error

	client := &http.Client{
		Timeout: 7 * time.Second,
	}

	// ค้นทั้ง newest และ relevance และเปลี่ยนคำค้นเมื่อผลไม่พอ
	sortOrders := []string{"newest", "relevance"}

	for _, query := range queries {
		searchQuery := strings.TrimSpace(query)
		if subject != "" {
			searchQuery += " subject:" + subject
		}

		for _, orderBy := range sortOrders {
			for page := 0; page < importPages; page++ {
				if err := ctx.Err(); err != nil {
					if successfulRequests > 0 {
						return results, nil
					}
					return nil, err
				}

				params := url.Values{}
				params.Set("q", searchQuery)
				params.Set("maxResults", fmt.Sprintf("%d", importPageSize))
				params.Set("startIndex", fmt.Sprintf("%d", page*importPageSize))
				params.Set("printType", "books")
				params.Set("orderBy", orderBy)
				params.Set("langRestrict", "th")
				params.Set("key", h.GoogleBooksAPIKey)

				requestURL := googleBooksURL + "?" + params.Encode()
				requestCtx, cancel := context.WithTimeout(ctx, 7*time.Second)

				req, err := http.NewRequestWithContext(
					requestCtx,
					http.MethodGet,
					requestURL,
					nil,
				)
				if err != nil {
					cancel()
					lastErr = err
					continue
				}

				resp, err := client.Do(req)
				if err != nil {
					cancel()
					lastErr = err
					continue
				}

				if resp.StatusCode != http.StatusOK {
					lastErr = fmt.Errorf(
						"Google Books API returned status %d",
						resp.StatusCode,
					)
					resp.Body.Close()
					cancel()
					continue
				}

				var googleResponse GoogleBooksResponse
				decodeErr := json.NewDecoder(resp.Body).Decode(&googleResponse)
				resp.Body.Close()
				cancel()

				if decodeErr != nil {
					lastErr = decodeErr
					continue
				}

				successfulRequests++

				if len(googleResponse.Items) == 0 {
					break
				}

				for _, item := range googleResponse.Items {
					info := item.VolumeInfo
					title := strings.TrimSpace(info.Title)
					author := strings.Join(info.Authors, ", ")
					coverURL := selectBestCover(info)
					language := strings.ToLower(strings.TrimSpace(info.Language))

					if title == "" || item.ID == "" ||
						strings.TrimSpace(author) == "" ||
						language != "th" ||
						!hasThaiText(title) ||
						!isValidCoverURL(coverURL) ||
						isPeriodical(title) {
						continue
					}

					if seenIDs[item.ID] {
						continue
					}
					seenIDs[item.ID] = true

					results = append(results, GoogleBookResult{
						GoogleBooksID: item.ID,
						Title:         title,
						Author:        author,
						Authors:       info.Authors,
						Description:   strings.TrimSpace(info.Description),
						CoverURL:      coverURL,
						PublishedDate: info.PublishedDate,
						PublishedYear: extractPublishedYear(info.PublishedDate),
						Language:      "th",
						Categories:    info.Categories,
					})
				}
			}
		}
	}

	if successfulRequests == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no successful Google Books search")
	}

	return results, nil
}
