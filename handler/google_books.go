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

// ตรวจคุณภาพพื้นฐานโดยไม่บังคับว่าต้องมีคำตรงกับชื่อหมวดหมู่
// เพราะคำอธิบายหนังสือจริงอาจไม่ได้ใช้ keyword ตามหมวดที่ค้นหา
func isRelevantDiscoverBook(result GoogleBookResult, plan bulkSearchPlan) bool {
	title := strings.TrimSpace(result.Title)
	author := strings.TrimSpace(result.Author)
	description := strings.TrimSpace(result.Description)

	if title == "" || author == "" || result.GoogleBooksID == "" {
		return false
	}

	if !hasThaiText(title) && !hasThaiText(description) {
		return false
	}

	if isPeriodical(title) || isLowQualityDiscoverBook(result) {
		return false
	}

	if isAdultRomance(title + " " + description) {
		return false
	}

	// ต้องมีคำอธิบายที่มีเนื้อหาจริง ไม่ใช่ keyword อย่างเดียว
	description = cleanBookDescription(description)
	if len([]rune(description)) < 35 || !hasThaiText(description) {
		return false
	}

	if !isValidCoverURL(result.CoverURL) {
		return false
	}

	return true
}

func cleanBookDescription(description string) string {
	description = strings.TrimSpace(description)

	lower := strings.ToLower(description)
	if index := strings.Index(lower, "keyword:"); index >= 0 {
		description = strings.TrimSpace(description[:index])
	}

	if index := strings.Index(description, "++++++++++++++++++++"); index >= 0 {
		description = strings.TrimSpace(description[:index])
	}

	return description
}

func normalizeDiscoverCategory(categories []string, result GoogleBookResult) []string {
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

	if len(categories) > 0 {
		category := strings.TrimSpace(categories[0])
		if allowed[category] {
			return []string{category}
		}

		switch strings.ToLower(category) {
		case "bl", "gl", "feel-good":
			return []string{"Romance"}
		case "manga", "comics & graphic novels":
			return []string{"Young Adult"}
		case "psychology":
			return []string{"Self-Help"}
		}
	}

	text := strings.ToLower(result.Title + " " + result.Description)

	switch {
	case containsAny(text, []string{"ชีวประวัติ", "อัตชีวประวัติ", "ประวัติชีวิต"}):
		return []string{"Biography"}
	case containsAny(text, []string{"ธุรกิจ", "การเงิน", "การลงทุน", "ผู้ประกอบการ"}):
		return []string{"Business"}
	case containsAny(text, []string{"บทกวี", "กวีนิพนธ์", "ร้อยกรอง", "กลอน"}):
		return []string{"Poetry"}
	case containsAny(text, []string{"นิทาน", "สำหรับเด็ก", "วรรณกรรมเยาวชน"}):
		return []string{"Children"}
	case containsAny(text, []string{"สยองขวัญ", "ผี", "ปีศาจ", "วิญญาณ"}):
		return []string{"Horror"}
	case containsAny(text, []string{"สืบสวน", "นักสืบ", "ปริศนา", "ฆาตกรรม"}):
		return []string{"Mystery"}
	case containsAny(text, []string{"ระทึกขวัญ", "ไล่ล่า", "เอาชีวิตรอด"}):
		return []string{"Thriller"}
	case containsAny(text, []string{"แฟนตาซี", "เวทมนตร์", "มังกร", "ทะลุมิติ"}):
		return []string{"Fantasy"}
	case containsAny(text, []string{"ผจญภัย", "เดินทาง", "ภารกิจ"}):
		return []string{"Adventure"}
	case containsAny(text, []string{"บทเรียน", "การศึกษา", "การเรียนรู้"}):
		return []string{"Education"}
	case containsAny(text, []string{"จิตวิทยา", "พัฒนาตนเอง", "พัฒนาตัวเอง"}):
		return []string{"Self-Help"}
	case containsAny(text, []string{"วิทยาศาสตร์", "ไซไฟ", "อวกาศ", "หุ่นยนต์"}):
		return []string{"Science Fiction"}
	case containsAny(text, []string{"ประวัติศาสตร์", "อยุธยา", "รัตนโกสินทร์"}):
		return []string{"Historical Fiction"}
	case containsAny(text, []string{"รัก", "โรแมนติก", "ความรัก", "หัวใจ"}):
		return []string{"Romance"}
	default:
		return []string{"Young Adult"}
	}
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

	// ใช้คำค้นหลายรูปแบบ และไม่ใส่ subject จำกัดผลลัพธ์
	plans := []bulkSearchPlan{
		{Query: "นิยายรักไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายโรแมนติกไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายความรัก ภาษาไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายวายไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายแฟนตาซีไทย", Language: "th", Categories: []string{"Fantasy"}},
		{Query: "นิยายกำลังภายใน แฟนตาซี", Language: "th", Categories: []string{"Fantasy"}},
		{Query: "นิยายสืบสวนไทย", Language: "th", Categories: []string{"Mystery"}},
		{Query: "นิยายฆาตกรรม ปริศนา", Language: "th", Categories: []string{"Mystery"}},
		{Query: "นิยายวัยรุ่นไทย", Language: "th", Categories: []string{"Young Adult"}},
		{Query: "วรรณกรรมเยาวชนไทย", Language: "th", Categories: []string{"Young Adult"}},
		{Query: "หนังสือจิตวิทยาไทย", Language: "th", Categories: []string{"Self-Help"}},
		{Query: "หนังสือพัฒนาตนเองไทย", Language: "th", Categories: []string{"Self-Help"}},
		{Query: "หนังสือธุรกิจไทย", Language: "th", Categories: []string{"Business"}},
		{Query: "หนังสือการเงิน การลงทุน ภาษาไทย", Language: "th", Categories: []string{"Business"}},
		{Query: "ชีวประวัติบุคคลสำคัญไทย", Language: "th", Categories: []string{"Biography"}},
		{Query: "อัตชีวประวัติ ภาษาไทย", Language: "th", Categories: []string{"Biography"}},
		{Query: "นิทานเด็กไทย", Language: "th", Categories: []string{"Children"}},
		{Query: "หนังสือสำหรับเด็ก ภาษาไทย", Language: "th", Categories: []string{"Children"}},
		{Query: "นิยายสยองขวัญไทย", Language: "th", Categories: []string{"Horror"}},
		{Query: "เรื่องผีไทย หนังสือ", Language: "th", Categories: []string{"Horror"}},
		{Query: "นิยายวิทยาศาสตร์ไทย", Language: "th", Categories: []string{"Science Fiction"}},
		{Query: "นิยายไซไฟ ภาษาไทย", Language: "th", Categories: []string{"Science Fiction"}},
		{Query: "นิยายผจญภัยไทย", Language: "th", Categories: []string{"Adventure"}},
		{Query: "วรรณกรรมผจญภัย ภาษาไทย", Language: "th", Categories: []string{"Adventure"}},
		{Query: "นิยายอิงประวัติศาสตร์ไทย", Language: "th", Categories: []string{"Historical Fiction"}},
		{Query: "วรรณกรรมประวัติศาสตร์ ภาษาไทย", Language: "th", Categories: []string{"Historical Fiction"}},
		{Query: "บทกวีไทย กวีนิพนธ์", Language: "th", Categories: []string{"Poetry"}},
		{Query: "รวมบทกลอน ภาษาไทย", Language: "th", Categories: []string{"Poetry"}},
		{Query: "หนังสือการศึกษา ภาษาไทย", Language: "th", Categories: []string{"Education"}},
		{Query: "หนังสือความรู้ ภาษาไทย", Language: "th", Categories: []string{"Education"}},
		{Query: "นิยายระทึกขวัญไทย", Language: "th", Categories: []string{"Thriller"}},
		{Query: "หนังสือ thriller ภาษาไทย", Language: "th", Categories: []string{"Thriller"}},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
	defer cancel()

	resultsByPlan := make([][]GoogleBookResult, len(plans))
	errorsByPlan := make([]error, len(plans))

	semaphore := make(chan struct{}, 8)
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

	successfulSearches := 0
	for _, searchErr := range errorsByPlan {
		if searchErr == nil {
			successfulSearches++
		}
	}

	if successfulSearches == 0 {
		writeError(w, http.StatusBadGateway, "Unable to retrieve books from Google Books")
		return
	}

	books := make([]HomeExternalBook, 0, 100)
	seenIDs := make(map[string]bool)
	seenTitles := make(map[string]bool)

	// เป้าหมายต่อหมวดหมู่ เพื่อไม่ให้ผลลัพธ์กระจุกอยู่หมวดเดียว
	categoryTargets := map[string]int{
		"Romance":            12,
		"Fantasy":            8,
		"Mystery":            8,
		"Young Adult":        7,
		"Self-Help":          8,
		"Business":           6,
		"Biography":          5,
		"Children":           6,
		"Horror":             6,
		"Science Fiction":    6,
		"Adventure":          7,
		"Historical Fiction": 5,
		"Poetry":             5,
		"Education":          4,
		"Thriller":           7,
	}

	categoryCounts := make(map[string]int)

	// เก็บผู้สมัครที่ผ่านการตรวจพื้นฐานก่อน
	type discoverCandidate struct {
		result   GoogleBookResult
		category string
	}

	candidates := make([]discoverCandidate, 0, 300)

	for i, plan := range plans {
		if errorsByPlan[i] != nil {
			continue
		}

		for _, result := range resultsByPlan[i] {
			if !isRelevantDiscoverBook(result, plan) {
				continue
			}

			categoryList := normalizeDiscoverCategory(plan.Categories, result)
			category := "Young Adult"
			if len(categoryList) > 0 {
				category = categoryList[0]
			}

			candidates = append(candidates, discoverCandidate{
				result:   result,
				category: category,
			})
		}
	}

	addCandidate := func(candidate discoverCandidate) {
		if len(books) >= 100 {
			return
		}

		result := candidate.result
		title := strings.TrimSpace(result.Title)
		author := strings.TrimSpace(result.Author)
		description := cleanBookDescription(result.Description)
		coverURL := normalizeCoverURL(result.CoverURL)
		googleID := strings.TrimSpace(result.GoogleBooksID)

		if title == "" || author == "" || googleID == "" ||
			len([]rune(description)) < 35 ||
			!hasThaiText(description) ||
			!isValidCoverURL(coverURL) {
			return
		}

		if seenIDs[googleID] {
			return
		}

		key := strings.ToLower(
			strings.Join(strings.Fields(title), " ") + "|" +
				strings.Join(strings.Fields(author), " "),
		)

		if seenTitles[key] {
			return
		}

		category := candidate.category
		if categoryTargets[category] == 0 {
			category = "Young Adult"
		}

		seenIDs[googleID] = true
		seenTitles[key] = true
		categoryCounts[category]++

		books = append(books, HomeExternalBook{
			ID:            "google_" + googleID,
			Source:        "google_books",
			Title:         title,
			Author:        author,
			Description:   description,
			CoverURL:      coverURL,
			PublishedYear: result.PublishedYear,
			Categories:    []string{category},
		})
	}

	// รอบแรก เติมตามเป้าหมายของแต่ละหมวด
	for _, candidate := range candidates {
		if len(books) >= 100 {
			break
		}

		if categoryCounts[candidate.category] >= categoryTargets[candidate.category] {
			continue
		}

		addCandidate(candidate)
	}

	// รอบสอง หากบางหมวดค้นหาได้น้อย ให้ใช้หนังสือที่ผ่านเกณฑ์จากหมวดอื่นเติม
	for _, candidate := range candidates {
		if len(books) >= 100 {
			break
		}

		addCandidate(candidate)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":                books,
		"total":               len(books),
		"target":              100,
		"language":            "th",
		"successful_searches": successfulSearches,
		"category_counts":     categoryCounts,
	})
}

// ค้นหาหนึ่งคำค้นจาก Google Books
func (h *Handler) fetchGoogleBooksForDiscover(
	ctx context.Context,
	query string,
	lang string,
	subject string,
) ([]GoogleBookResult, error) {
	searchQuery := query

	// Discover ใช้ subject ว่างเพื่อไม่จำกัดผลลัพธ์มากเกินไป
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

	client := &http.Client{Timeout: 15 * time.Second}
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
		description := strings.TrimSpace(info.Description)
		coverURL := selectBestCover(info)
		language := strings.ToLower(strings.TrimSpace(info.Language))

		if title == "" || strings.TrimSpace(author) == "" ||
			item.ID == "" ||
			!isValidCoverURL(coverURL) ||
			isPeriodical(title) {
			continue
		}

		// ยอมรับหนังสือที่ Google ระบุภาษาไทย
		// แม้ชื่อเรื่องเป็นภาษาอังกฤษ หากคำอธิบายเป็นภาษาไทย
		if language != "" && language != "th" {
			continue
		}

		if language == "" {
			if !hasThaiText(title) && !hasThaiText(description) {
				continue
			}
			language = "th"
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

	return results, nil
}

func (h *Handler) AdminBulkImportBooks(w http.ResponseWriter, r *http.Request) {
	if h.GoogleBooksAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "Google Books API key is not configured")
		return
	}

	plans := []bulkSearchPlan{
		{Query: "นิยายรักโรแมนติก ภาษาไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายวาย BL ภาษาไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายยูริ GL ภาษาไทย", Language: "th", Categories: []string{"Romance"}},
		{Query: "นิยายแฟนตาซี ภาษาไทย", Language: "th", Categories: []string{"Fantasy"}},
		{Query: "นิยายผจญภัย ภาษาไทย", Language: "th", Categories: []string{"Adventure"}},
		{Query: "นิยายสืบสวนสอบสวน ภาษาไทย", Language: "th", Categories: []string{"Mystery"}},
		{Query: "นิยายระทึกขวัญ ภาษาไทย", Language: "th", Categories: []string{"Thriller"}},
		{Query: "นิยายวัยรุ่น ภาษาไทย", Language: "th", Categories: []string{"Young Adult"}},
		{Query: "นิยายวิทยาศาสตร์ ภาษาไทย", Language: "th", Categories: []string{"Science Fiction"}},
		{Query: "หนังสือสยองขวัญ ภาษาไทย", Language: "th", Categories: []string{"Horror"}},
		{Query: "จิตวิทยา พัฒนาตนเอง ภาษาไทย", Language: "th", Categories: []string{"Self-Help"}},
		{Query: "ชีวประวัติ บุคคลสำคัญ ภาษาไทย", Language: "th", Categories: []string{"Biography"}},
		{Query: "หนังสือความรู้สำหรับเด็ก ภาษาไทย", Language: "th", Categories: []string{"Children"}},
		{Query: "หนังสือความรู้ การเรียนรู้ ภาษาไทย", Language: "th", Categories: []string{"Education"}},
		{Query: "ธุรกิจ การเงิน การทำงาน ภาษาไทย", Language: "th", Categories: []string{"Business"}},
		{Query: "นิยายอิงประวัติศาสตร์ ภาษาไทย", Language: "th", Categories: []string{"Historical Fiction"}},
		{Query: "บทกวี กวีนิพนธ์ ภาษาไทย", Language: "th", Categories: []string{"Poetry"}},
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
			description := cleanBookDescription(result.Description)
			coverURL := normalizeCoverURL(result.CoverURL)

			if title == "" || author == "" ||
				!hasThaiText(title) ||
				len([]rune(description)) < 35 ||
				!hasThaiText(description) ||
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
