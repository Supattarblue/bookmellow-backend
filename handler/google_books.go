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
		"newsletter",
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
	title := strings.ToLower(strings.TrimSpace(result.Title))
	description := strings.ToLower(strings.TrimSpace(result.Description))
	text := title + " " + description

	if title == "" || result.GoogleBooksID == "" {
		return false
	}

	// ต้องเป็นชื่อหนังสือภาษาไทย
	if !hasThaiText(result.Title) {
		return false
	}

	if isPeriodical(title) || isLowQualityDiscoverBook(result) {
		return false
	}

	if isAdultRomance(text) {
		return false
	}

	category := ""
	if len(plan.Categories) > 0 {
		category = strings.ToLower(strings.TrimSpace(plan.Categories[0]))
	}

	switch category {
	case "romance":
		return containsAny(text, []string{
			"รัก", "โรแมนติก", "ความรัก", "หัวใจ",
			"romance", "love story", "ความสัมพันธ์",
		})

	case "fantasy":
		return containsAny(text, []string{
			"แฟนตาซี", "เวทมนตร์", "เวทมนต์",
			"มังกร", "อาณาจักร", "fantasy",
			"ทะลุมิติ", "ต่างโลก", "จอมยุทธ์",
		})

	case "mystery":
		return containsAny(text, []string{
			"สืบสวน", "สอบสวน", "ปริศนา", "ฆาตกรรม",
			"นักสืบ", "ลึกลับ", "mystery", "detective",
		})

	case "young adult":
		return containsAny(text, []string{
			"วัยรุ่น", "โรงเรียน", "มหาวิทยาลัย",
			"เติบโต", "มิตรภาพ", "young adult",
		})

	case "self-help":
		return containsAny(text, []string{
			"จิตวิทยา", "พัฒนาตนเอง", "พัฒนาตัวเอง",
			"เข้าใจตัวเอง", "ความสำเร็จ", "เป้าหมาย",
			"การใช้ชีวิต", "วินัย", "self-help",
			"กำลังใจ", "ฮีลใจ", "เยียวยาใจ",
		})

	case "children":
		return containsAny(text, []string{
			"นิทาน", "สำหรับเด็ก", "เด็กเล็ก",
			"วรรณกรรมเยาวชน", "เด็กและเยาวชน",
		})

	case "biography":
		return containsAny(text, []string{
			"ชีวประวัติ", "ประวัติชีวิต", "อัตชีวประวัติ",
			"บุคคลสำคัญ", "biography",
		})

	case "business":
		return containsAny(text, []string{
			"ธุรกิจ", "การเงิน", "การลงทุน",
			"ผู้ประกอบการ", "การตลาด", "บริหาร",
		})

	case "education":
		return containsAny(text, []string{
			"การศึกษา", "การเรียนรู้", "ความรู้",
			"ทักษะ", "พัฒนาการเรียน",
		})

	case "historical fiction":
		return containsAny(text, []string{
			"อิงประวัติศาสตร์", "ประวัติศาสตร์",
			"อยุธยา", "รัตนโกสินทร์",
			"สงครามโลก", "historical",
		})

	case "poetry":
		return containsAny(text, []string{
			"บทกวี", "กวีนิพนธ์", "กลอน",
			"โคลง", "ฉันท์", "ร้อยกรอง",
		})

	case "science fiction":
		return containsAny(text, []string{
			"วิทยาศาสตร์", "ไซไฟ", "โลกอนาคต",
			"หุ่นยนต์", "อวกาศ", "science fiction",
		})

	case "horror":
		return containsAny(text, []string{
			"สยองขวัญ", "ผี", "หลอน",
			"ปีศาจ", "วิญญาณ", "horror",
		})

	case "thriller":
		return containsAny(text, []string{
			"ระทึกขวัญ", "ไล่ล่า", "อันตราย",
			"ฆาตกรรม", "ลอบสังหาร", "thriller",
		})

	case "adventure":
		return containsAny(text, []string{
			"ผจญภัย", "เดินทาง", "ค้นหา",
			"เอาชีวิตรอด", "ภารกิจ", "adventure",
		})

	case "manga":
		return containsAny(text, []string{
			"มังงะ", "การ์ตูน", "comic",
			"manga", "graphic novel", "ไลท์โนเวล",
		})

	case "thai fiction", "translated fiction":
		// หมวดนี้เป็นหมวดค้นหา ไม่ใช่ชื่อหมวดหลักในฐานข้อมูล
		// กรองหนังสือกฎหมาย งานวิจัย และตำราออกก่อน
		return !containsAny(text, []string{
			"กฎหมาย", "ลิขสิทธิ์", "คดีศึกษา",
			"วิทยานิพนธ์", "งานวิจัย", "ตำราเรียน",
			"แบบเรียน", "คู่มือ",
		})
	}

	return true
}

func normalizeDiscoverCategory(categories []string, result GoogleBookResult) []string {
	if len(categories) == 0 {
		return []string{"Young Adult"}
	}

	category := strings.TrimSpace(categories[0])

	// ใช้ชื่อหมวดหมู่ที่มีอยู่จริงใน BookMellow
	allowed := map[string]bool{
		"Adventure":          true,
		"Biography":          true,
		"Business":           true,
		"Children":           true,
		"Education":          true,
		"Fantasy":            true,
		"Historical Fiction": true,
		"Horror":             true,
		"Mystery":            true,
		"Poetry":             true,
		"Romance":            true,
		"Science Fiction":    true,
		"Self-Help":          true,
		"Thriller":           true,
		"Young Adult":        true,
	}

	if allowed[category] {
		return []string{category}
	}

	text := strings.ToLower(result.Title + " " + result.Description)

	switch strings.ToLower(category) {
	case "bl", "gl", "feel-good":
		return []string{"Romance"}
	case "manga":
		return []string{"Young Adult"}
	case "psychology":
		return []string{"Self-Help"}
	case "thai fiction", "translated fiction":
		switch {
		case containsAny(text, []string{"สืบสวน", "ฆาตกรรม", "นักสืบ", "ปริศนา"}):
			return []string{"Mystery"}
		case containsAny(text, []string{"แฟนตาซี", "เวทมนตร์", "มังกร", "ทะลุมิติ"}):
			return []string{"Fantasy"}
		case containsAny(text, []string{"สยองขวัญ", "ผี", "ปีศาจ"}):
			return []string{"Horror"}
		case containsAny(text, []string{"ระทึกขวัญ", "ไล่ล่า"}):
			return []string{"Thriller"}
		case containsAny(text, []string{"ผจญภัย", "เดินทาง", "เอาชีวิตรอด"}):
			return []string{"Adventure"}
		case containsAny(text, []string{"รัก", "โรแมนติก", "ความรัก", "หัวใจ"}):
			return []string{"Romance"}
		default:
			return []string{"Young Adult"}
		}
	}

	return []string{"Young Adult"}
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
			Language:      strings.ToLower(strings.TrimSpace(info.Language)),
			Categories:    info.Categories,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  results,
		"total": len(results),
	})
}

// DiscoverBooks ค้นหาหนังสือภาษาไทยจาก Google Books
// ไม่บันทึกข้อมูลลง Supabase
func (h *Handler) DiscoverBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := []bulkSearchPlan{
		{Query: "นิยายรักโรแมนติก ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance"}, Target: 15},
		{Query: "นิยายวาย BL ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance"}, Target: 10},
		{Query: "นิยายยูริ GL ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance"}, Target: 5},
		{Query: "นิยายแฟนตาซี ภาษาไทย", Language: "th", Subject: "Fantasy", Categories: []string{"Fantasy"}, Target: 12},
		{Query: "นิยายสืบสวนสอบสวน ภาษาไทย", Language: "th", Subject: "Mystery", Categories: []string{"Mystery"}, Target: 10},
		{Query: "นิยายวัยรุ่น ภาษาไทย", Language: "th", Categories: []string{"Young Adult"}, Target: 8},
		{Query: "หนังสือจิตวิทยา พัฒนาตนเอง ภาษาไทย", Language: "th", Subject: "Psychology", Categories: []string{"Self-Help"}, Target: 8},
		{Query: "มังงะ การ์ตูนญี่ปุ่น ภาษาไทย", Language: "th", Subject: "Comics & Graphic Novels", Categories: []string{"Young Adult"}, Target: 5},
		{Query: "หนังสือธุรกิจ การเงิน ภาษาไทย", Language: "th", Subject: "Business & Economics", Categories: []string{"Business"}, Target: 5},
		{Query: "หนังสือชีวประวัติ ภาษาไทย", Language: "th", Subject: "Biography & Autobiography", Categories: []string{"Biography"}, Target: 4},
		{Query: "หนังสือเด็ก นิทาน ภาษาไทย", Language: "th", Subject: "Juvenile Fiction", Categories: []string{"Children"}, Target: 4},
		{Query: "นิยายสยองขวัญ ภาษาไทย", Language: "th", Subject: "Horror", Categories: []string{"Horror"}, Target: 4},
		{Query: "นิยายวิทยาศาสตร์ ภาษาไทย", Language: "th", Subject: "Science Fiction", Categories: []string{"Science Fiction"}, Target: 4},
		{Query: "นิยายผจญภัย ภาษาไทย", Language: "th", Subject: "Fiction", Categories: []string{"Adventure"}, Target: 4},
		{Query: "นิยายอิงประวัติศาสตร์ ภาษาไทย", Language: "th", Subject: "Historical Fiction", Categories: []string{"Historical Fiction"}, Target: 4},
		{Query: "บทกวี กวีนิพนธ์ ภาษาไทย", Language: "th", Subject: "Poetry", Categories: []string{"Poetry"}, Target: 3},
		{Query: "นิยายระทึกขวัญ ภาษาไทย", Language: "th", Subject: "Fiction", Categories: []string{"Thriller"}, Target: 5},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
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

			results, err := h.fetchGoogleBooksForDiscover(
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
				!hasThaiText(title) || !isValidCoverURL(coverURL) {
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
				Categories:    normalizeDiscoverCategory(plan.Categories, result),
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

// ค้นหาหลายหน้า เพื่อเพิ่มโอกาสได้หนังสือครบตามจำนวนเป้าหมาย
func (h *Handler) fetchGoogleBooksForDiscover(
	ctx context.Context,
	query string,
	lang string,
	subject string,
) ([]GoogleBookResult, error) {
	searchQuery := query

	if subject != "" {
		searchQuery += " subject:" + subject
	}

	results := make([]GoogleBookResult, 0, 120)

	for page := 0; page < 3; page++ {
		params := url.Values{}
		params.Set("q", searchQuery)
		params.Set("maxResults", "40")
		params.Set("startIndex", fmt.Sprintf("%d", page*40))
		params.Set("printType", "books")
		params.Set("orderBy", "relevance")
		params.Set("langRestrict", lang)
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

		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			return results, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cancel()
			return results, fmt.Errorf(
				"Google Books API returned status %d",
				resp.StatusCode,
			)
		}

		var googleResponse GoogleBooksResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&googleResponse)
		resp.Body.Close()
		cancel()

		if decodeErr != nil {
			return results, decodeErr
		}

		if len(googleResponse.Items) == 0 {
			break
		}

		for _, item := range googleResponse.Items {
			info := item.VolumeInfo
			title := strings.TrimSpace(info.Title)
			author := strings.Join(info.Authors, ", ")
			description := strings.TrimSpace(info.Description)
			coverURL := selectBestCover(info)

			if title == "" || strings.TrimSpace(author) == "" ||
				!hasThaiText(title) ||
				!isValidCoverURL(coverURL) ||
				isPeriodical(title) {
				continue
			}

			language := strings.ToLower(strings.TrimSpace(info.Language))

			// Google Books บางรายการระบุ language ไม่ครบ
			// ยอมรับเมื่อชื่อหนังสือเป็นภาษาไทยจริง
			if language == "" {
				language = "th"
			}

			if language != "th" {
				continue
			}

			results = append(results, GoogleBookResult{
				GoogleBooksID: item.ID,
				Title:         title,
				Author:        author,
				Authors:       info.Authors,
				Description:   description,
				CoverURL:      coverURL,
				PublishedDate: info.PublishedDate,
				PublishedYear: extractPublishedYear(info.PublishedDate),
				Language:      language,
				Categories:    info.Categories,
			})
		}

		if len(googleResponse.Items) < 40 {
			break
		}
	}

	return results, nil
}

func (h *Handler) AdminBulkImportBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := []bulkSearchPlan{
		{Query: "นิยายรักโรแมนติก ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance"}},
		{Query: "นิยายวาย BL ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance", "Young Adult"}},
		{Query: "นิยายยูริ GL ภาษาไทย", Language: "th", Subject: "Romance", Categories: []string{"Romance", "Young Adult"}},
		{Query: "นิยายแฟนตาซี ภาษาไทย", Language: "th", Subject: "Fantasy", Categories: []string{"Fantasy"}},
		{Query: "นิยายผจญภัย ภาษาไทย", Language: "th", Subject: "Fiction", Categories: []string{"Adventure"}},
		{Query: "นิยายสืบสวนสอบสวน ภาษาไทย", Language: "th", Subject: "Mystery", Categories: []string{"Mystery"}},
		{Query: "นิยายระทึกขวัญ ภาษาไทย", Language: "th", Subject: "Fiction", Categories: []string{"Thriller"}},
		{Query: "นิยายวัยรุ่น ภาษาไทย", Language: "th", Subject: "Young Adult", Categories: []string{"Young Adult"}},
		{Query: "นิยายวิทยาศาสตร์ ภาษาไทย", Language: "th", Subject: "Science Fiction", Categories: []string{"Science Fiction"}},
		{Query: "หนังสือสยองขวัญ ภาษาไทย", Language: "th", Subject: "Horror", Categories: []string{"Horror"}},
		{Query: "จิตวิทยา พัฒนาตนเอง ภาษาไทย", Language: "th", Subject: "Self-Help", Categories: []string{"Self-Help"}},
		{Query: "ชีวประวัติ บุคคลสำคัญ ภาษาไทย", Language: "th", Subject: "Biography & Autobiography", Categories: []string{"Biography"}},
		{Query: "หนังสือความรู้สำหรับเด็ก ภาษาไทย", Language: "th", Subject: "Juvenile Nonfiction", Categories: []string{"Children"}},
		{Query: "หนังสือความรู้ การเรียนรู้ ภาษาไทย", Language: "th", Subject: "Education", Categories: []string{"Education"}},
		{Query: "ธุรกิจ การเงิน การทำงาน ภาษาไทย", Language: "th", Subject: "Business & Economics", Categories: []string{"Business"}},
		{Query: "นิยายอิงประวัติศาสตร์ ภาษาไทย", Language: "th", Subject: "Historical Fiction", Categories: []string{"Historical Fiction"}},
		{Query: "บทกวี กวีนิพนธ์ ภาษาไทย", Language: "th", Subject: "Poetry", Categories: []string{"Poetry"}},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()

	resultsByPlan := make([][]GoogleBookResult, len(plans))
	errorsByPlan := make([]error, len(plans))
	semaphore := make(chan struct{}, 6)
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

			if title == "" || author == "" ||
				!hasThaiText(title) ||
				!isValidCoverURL(coverURL) ||
				isPeriodical(title) ||
				!strings.EqualFold(result.Language, "th") {
				continue
			}

			if isLowQualityDiscoverBook(result) ||
				isAdultRomance(title+" "+description) {
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
		}
	}

	replaced, skipped, total, remaining, stats, err :=
		h.Store.AdminReplaceNonThaiBooks(r.Context(), importBooks)

	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":             "Thai book replacement completed",
		"successful_searches": successfulSearches,
		"found":               len(importBooks),
		"thai_candidates":     len(importBooks),
		"foreign_candidates":  0,
		"replaced":            replaced,
		"skipped":             skipped,
		"skip_reasons":        stats,
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

		language := strings.ToLower(strings.TrimSpace(info.Language))

		if language == "" && hasThaiText(title) {
			language = "th"
		}

		if title == "" || strings.TrimSpace(author) == "" ||
			!hasThaiText(title) ||
			!isValidCoverURL(coverURL) ||
			isPeriodical(title) ||
			language != "th" {
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
			Language:      language,
			Categories:    info.Categories,
		})
	}

	return results, nil
}
