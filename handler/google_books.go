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
	Query      string
	Language   string
	Subject    string
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
	if err != nil {
		return false
	}

	return parsed.Scheme == "https" && parsed.Host != ""
}

func isPeriodical(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))

	keywords := []string{
		"นิตยสาร", "หนังสือพิมพ์", "รายสัปดาห์", "รายเดือน",
		"วารสาร", "ฉบับที่", "ประจำวันที่", "magazine",
		"newspaper", "weekly", "monthly journal", "periodical",
		"newsletter", "catalogue", "catalog",
	}

	return containsAny(title, keywords)
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

func isLowQualityDiscoverBook(result GoogleBookResult) bool {
	text := strings.ToLower(result.Title + " " + result.Description)

	excluded := []string{
		"คู่มือเตรียมสอบ", "แนวข้อสอบ", "ข้อสอบ", "เตรียมสอบ",
		"หลักสูตร", "วิทยานิพนธ์", "การจัดการเรียนรู้",
		"สำหรับครู", "สําหรับครู", "ผู้ปกครอง",
		"รายงานการวิจัย", "งานวิจัย", "ตำราเรียน",
		"ตําราเรียน", "แบบเรียน", "แบบฝึกหัด",
		"เอกสารประกอบการสอน", "เอกสารคำสอน",
		"academic journal", "bibliography", "textbook",
		"exam preparation", "study guide", "teacher's guide",
		"research report", "dissertation", "thesis",
		"political campaign", "politics", "political science",
		"พรรคการเมือง", "หาเสียง", "การเลือกตั้ง",
		"นโยบายพรรค", "การเมืองไทย", "การเมือง",
	}

	return containsAny(text, excluded)
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

func isRelevantDiscoverBook(result GoogleBookResult, plan bulkSearchPlan) bool {
	if strings.TrimSpace(result.Title) == "" ||
		strings.TrimSpace(result.Author) == "" ||
		strings.TrimSpace(result.GoogleBooksID) == "" ||
		strings.TrimSpace(result.Description) == "" ||
		!strings.EqualFold(strings.TrimSpace(result.Language), "th") {
		return false
	}

	text := strings.ToLower(strings.TrimSpace(
		result.Title + " " + result.Description + " " + strings.Join(result.Categories, " "),
	))

	if !hasThaiText(result.Title+" "+result.Description) ||
		isPeriodical(result.Title) ||
		isLowQualityDiscoverBook(result) ||
		isAdultRomance(text) {
		return false
	}

	category := ""
	if len(plan.Categories) > 0 {
		category = strings.ToLower(strings.TrimSpace(plan.Categories[0]))
	}

	keywordsByCategory := map[string][]string{
		"romance": {
			"รัก", "โรแมนติก", "ความรัก", "หัวใจ",
			"romance", "love story", "ความสัมพันธ์",
		},
		"fantasy": {
			"แฟนตาซี", "เวทมนตร์", "เวทมนต์",
			"มังกร", "อาณาจักร", "fantasy",
		},
		"mystery": {
			"สืบสวน", "สอบสวน", "ปริศนา", "ฆาตกรรม",
			"นักสืบ", "ลึกลับ", "mystery", "detective",
		},
		"science fiction": {
			"วิทยาศาสตร์", "ไซไฟ", "โลกอนาคต",
			"อวกาศ", "หุ่นยนต์", "science fiction", "sci-fi",
		},
		"thriller": {
			"ระทึกขวัญ", "ลุ้นระทึก", "อันตราย",
			"ไล่ล่า", "thriller", "suspense",
		},
		"horror": {
			"สยองขวัญ", "ผี", "วิญญาณ",
			"หลอน", "ปีศาจ", "horror", "ghost",
		},
		"historical fiction": {
			"ย้อนยุค", "ประวัติศาสตร์", "สมัย",
			"ราชวงศ์", "historical fiction",
		},
		"self-help": {
			"พัฒนาตนเอง", "พัฒนาตัวเอง", "จิตวิทยา",
			"ความสำเร็จ", "เป้าหมาย", "การใช้ชีวิต",
			"วินัย", "self-help",
		},
		"business": {
			"ธุรกิจ", "การตลาด", "ผู้ประกอบการ",
			"การเงิน", "การลงทุน", "บริหาร",
			"business", "entrepreneur",
		},
		"education": {
			"การเรียนรู้", "ความรู้", "วิทยาศาสตร์",
			"ภาษา", "การศึกษา", "เรียนรู้", "education",
		},
		"biography": {
			"ชีวประวัติ", "ประวัติชีวิต", "บันทึกชีวิต",
			"อัตชีวประวัติ", "memoir", "biography",
		},
		"young adult": {
			"วัยรุ่น", "โรงเรียน", "มหาวิทยาลัย",
			"เติบโต", "มิตรภาพ", "young adult",
		},
		"children": {
			"เด็ก", "เยาวชน", "นิทาน",
			"ภาพประกอบ", "children", "juvenile",
		},
		"poetry": {
			"บทกวี", "กวีนิพนธ์", "กลอน", "poetry", "poems",
		},
		"adventure": {
			"ผจญภัย", "เดินทาง", "สำรวจ",
			"เอาชีวิตรอด", "adventure", "quest",
		},
	}

	keywords, ok := keywordsByCategory[category]
	if !ok {
		return false
	}

	return containsAny(text, keywords)
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

		coverURL := selectBestCover(info)
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
			Language:      info.Language,
			Categories:    info.Categories,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  results,
		"total": len(results),
	})
}

// thaiBookSearchPlans กำหนดสัดส่วนการค้นหาหนังสือภาษาไทยให้ครบ 15 หมวดหมู่
// Target รวมกัน 100 เล่ม แต่ผลค้นหาจริงอาจน้อยกว่านี้หลังผ่านการคัดกรอง
func thaiBookSearchPlans() []bulkSearchPlan {
	return []bulkSearchPlan{
		{Query: "นิยายรักโรแมนติก ฉบับภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance"}, Target: 12},
		{Query: "นิยายแฟนตาซี ภาษาไทย", Language: "th", Subject: "Fantasy", Categories: []string{"Fantasy"}, Target: 10},
		{Query: "นิยายสืบสวนสอบสวน ปริศนา ภาษาไทย", Language: "th", Subject: "Mystery", Categories: []string{"Mystery"}, Target: 9},
		{Query: "นิยายผจญภัย ภาษาไทย", Language: "th", Subject: "Adventure", Categories: []string{"Adventure"}, Target: 8},
		{Query: "หนังสือพัฒนาตนเอง จิตวิทยา ภาษาไทย", Language: "th", Subject: "Self-Help", Categories: []string{"Self-Help"}, Target: 8},
		{Query: "นิยายวิทยาศาสตร์ ไซไฟ ภาษาไทย", Language: "th", Subject: "Science Fiction", Categories: []string{"Science Fiction"}, Target: 7},
		{Query: "นิยายระทึกขวัญ ภาษาไทย", Language: "th", Subject: "Thrillers", Categories: []string{"Thriller"}, Target: 7},
		{Query: "นิยายสยองขวัญ ผี ภาษาไทย", Language: "th", Subject: "Horror", Categories: []string{"Horror"}, Target: 6},
		{Query: "วรรณกรรมเยาวชน นิยายวัยรุ่น ภาษาไทย", Language: "th", Subject: "Young Adult Fiction", Categories: []string{"Young Adult"}, Target: 8},
		{Query: "หนังสือเด็ก นิทาน ภาษาไทย", Language: "th", Subject: "Juvenile Fiction", Categories: []string{"Children"}, Target: 5},
		{Query: "ชีวประวัติ บุคคลสร้างแรงบันดาลใจ ภาษาไทย", Language: "th", Subject: "Biography & Autobiography", Categories: []string{"Biography"}, Target: 5},
		{Query: "หนังสือความรู้ การเรียนรู้ ภาษาไทย", Language: "th", Subject: "Education", Categories: []string{"Education"}, Target: 5},
		{Query: "ธุรกิจ การตลาด ผู้ประกอบการ ภาษาไทย", Language: "th", Subject: "Business & Economics", Categories: []string{"Business"}, Target: 4},
		{Query: "นิยายอิงประวัติศาสตร์ ภาษาไทย", Language: "th", Subject: "Historical Fiction", Categories: []string{"Historical Fiction"}, Target: 3},
		{Query: "บทกวี กวีนิพนธ์ ภาษาไทย", Language: "th", Subject: "Poetry", Categories: []string{"Poetry"}, Target: 3},
	}
}

// DiscoverBooks ค้นหาหนังสือจาก Google Books สำหรับหน้า Home
// ฟังก์ชันนี้ไม่บันทึกข้อมูลลง Supabase
func (h *Handler) DiscoverBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := thaiBookSearchPlans()

	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
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
			defer func() { <-semaphore }()

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

	books := make([]HomeExternalBook, 0, 100)
	seenIDs := make(map[string]bool)
	seenTitles := make(map[string]bool)
	successfulSearches := 0

	for i, plan := range plans {
		if errorsByPlan[i] != nil {
			continue
		}

		successfulSearches++
		addedFromPlan := 0

		for _, result := range resultsByPlan[i] {
			if !isRelevantDiscoverBook(result, plan) {
				continue
			}

			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			coverURL := normalizeCoverURL(result.CoverURL)
			googleID := strings.TrimSpace(result.GoogleBooksID)

			if title == "" || author == "" || googleID == "" ||
				!isValidCoverURL(coverURL) {
				continue
			}

			if seenIDs[googleID] {
				continue
			}

			key := strings.ToLower(
				strings.Join(strings.Fields(title), " ") + "|" +
					strings.Join(strings.Fields(author), " "),
			)

			if seenTitles[key] {
				continue
			}

			seenIDs[googleID] = true
			seenTitles[key] = true

			books = append(books, HomeExternalBook{
				ID:            "google_" + googleID,
				Source:        "google_books",
				Title:         title,
				Author:        author,
				Description:   strings.TrimSpace(result.Description),
				CoverURL:      coverURL,
				PublishedYear: result.PublishedYear,
				Categories:    plan.Categories,
			})

			addedFromPlan++

			if addedFromPlan >= plan.Target || len(books) >= 100 {
				break
			}
		}

		if len(books) >= 100 {
			break
		}
	}

	if successfulSearches == 0 {
		writeError(w, http.StatusBadGateway, "Unable to retrieve books from Google Books")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":     books,
		"total":    len(books),
		"target":   100,
		"language": "th",
	})
}

func (h *Handler) AdminBulkImportBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := thaiBookSearchPlans()

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
			defer func() { <-semaphore }()

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
	importBooks := make([]store.BulkImportBook, 0, 100)

	for i, plan := range plans {
		if errorsByPlan[i] != nil {
			continue
		}

		addedFromPlan := 0

		for _, result := range resultsByPlan[i] {
			if addedFromPlan >= plan.Target {
				break
			}

			title := strings.TrimSpace(result.Title)
			author := strings.TrimSpace(result.Author)
			description := strings.TrimSpace(result.Description)
			coverURL := normalizeCoverURL(result.CoverURL)

			if title == "" || author == "" || description == "" ||
				!isValidCoverURL(coverURL) ||
				!isRelevantDiscoverBook(result, plan) {
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
				Language:      "th",
			})

			addedFromPlan++
		}
	}

	thaiCandidates := len(importBooks)

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
		"foreign_candidates":  0,
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

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodGet,
		requestURL,
		nil,
	)
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
		return nil, fmt.Errorf(
			"Google Books API returned status %d",
			resp.StatusCode,
		)
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
		coverURL := selectBestCover(info)

		if title == "" || strings.TrimSpace(author) == "" ||
			!isValidCoverURL(coverURL) || isPeriodical(title) ||
			!strings.EqualFold(strings.TrimSpace(info.Language), "th") ||
			!hasThaiText(title+" "+info.Description) {
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
			Language:      info.Language,
			Categories:    info.Categories,
		})
	}

	return results, nil
}
