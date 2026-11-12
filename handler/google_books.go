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
	importPages    = 2
	importPageSize = 40
	minBookYear    = 2018
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
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
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
		"นิตยสาร", "หนังสือพิมพ์", "รายสัปดาห์", "รายเดือน",
		"วารสาร", "ฉบับที่", "ประจำวันที่", "magazine",
		"newspaper", "weekly", "monthly journal", "periodical",
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
		"horror", "horrors", "horror fiction",
		"ghost horror", "supernatural horror",
		"สยองขวัญ", "นิยายสยองขวัญ", "เรื่องผี",
		"恐怖", "ホラー", "怪談",
		"공포", "호러",
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

func isLowQualityDiscoverBook(result GoogleBookResult) bool {
	text := strings.ToLower(result.Title + " " + result.Description)

	return containsAny(text, []string{
		"คู่มือเตรียมสอบ", "แนวข้อสอบ", "ข้อสอบ", "เตรียมสอบ",
		"หลักสูตร", "วิทยานิพนธ์", "การจัดการเรียนรู้",
		"สำหรับครู", "สําหรับครู", "ผู้ปกครอง",
		"รายงานการวิจัย", "งานวิจัย",
		"เอกสารประกอบการสอน", "เอกสารคำสอน",
		"academic journal", "bibliography",
		"exam preparation", "teacher's guide",
		"research report", "dissertation", "thesis",
		"受験対策", "論文", "학위논문", "수험서",
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

	if title == "" || result.GoogleBooksID == "" {
		return false
	}

	if !isSupportedBookLanguage(result.Language) {
		return false
	}

	if isPeriodical(title) || isLowQualityDiscoverBook(result) || isHorrorBook(result) {
		return false
	}

	switch plan.Kind {
	case "self-help":
		return containsAny(text, []string{
			"self-help", "self help", "psychology",
			"personal development", "mindset", "mental health",
			"จิตวิทยา", "พัฒนาตนเอง", "พัฒนาตัวเอง",
			"ความคิด", "อารมณ์", "ความมั่นใจ", "การใช้ชีวิต",
			"ความสำเร็จ", "เป้าหมาย", "วินัย", "เข้าใจตัวเอง",
			"พลังบวก", "ความสุข", "นิสัย", "แรงบันดาลใจ",
			"ความสัมพันธ์", "ค้นหาตัวเอง",
			"自己啓発", "心理学", "メンタルヘルス",
			"자기계발", "심리학", "마음",
		})

	case "manga":
		return containsAny(text, []string{
			"manga", "comic", "comics", "graphic novel",
			"มังงะ", "การ์ตูน", "ไลท์โนเวล",
			"マンガ", "漫画", "コミック", "ライトノベル",
			"만화", "웹툰", "라이트 노벨",
		})

	case "english":
		return containsAny(text, []string{
			"english learning", "english language",
			"english grammar", "vocabulary",
			"learn english", "ภาษาอังกฤษ", "คำศัพท์",
			"ไวยากรณ์", "เรียนภาษาอังกฤษ",
			"英語", "英会話", "英語学習",
			"영어", "영어회화", "영어학습",
		})

	case "fantasy":
		return containsAny(text, []string{
			"fantasy", "magic", "wizard", "dragon",
			"แฟนตาซี", "เวทมนตร์", "เวทมนต์", "มังกร",
			"พ่อมด", "แม่มด", "อาณาจักร", "ผจญภัย", "ต่างโลก",
			"ファンタジー", "魔法", "竜", "ドラゴン",
			"판타지", "마법", "드래곤",
		})

	case "mystery":
		return containsAny(text, []string{
			"mystery", "detective", "crime fiction",
			"thriller", "suspense", "สืบสวน", "สอบสวน",
			"ปริศนา", "ฆาตกรรม", "นักสืบ", "ลึกลับ", "คดี",
			"ミステリー", "推理", "探偵", "サスペンス",
			"추리", "미스터리", "탐정", "스릴러",
		})

	case "lifestyle":
		return containsAny(text, []string{
			"lifestyle", "minimalism", "home design",
			"interior design", "home organization",
			"บ้าน", "ตกแต่งบ้าน", "จัดบ้าน", "ไลฟ์สไตล์",
			"แต่งบ้าน", "การใช้ชีวิต", "จัดระเบียบบ้าน",
			"ออกแบบบ้าน", "สวน", "minimal",
			"ライフスタイル", "インテリア", "片付け",
			"라이프스타일", "인테리어", "정리",
		})

	case "romance":
		return !isAdultRomance(text) && containsAny(text, []string{
			"romance", "romantic", "love story",
			"contemporary romance", "โรแมนติก",
			"นิยายรัก", "ความรัก", "หัวใจ", "ความสัมพันธ์",
			"恋愛", "ロマンス", "ラブストーリー",
			"로맨스", "연애", "사랑",
		})

	case "healing":
		return containsAny(text, []string{
			"healing", "feel good", "friendship",
			"comfort", "inspirational fiction",
			"ฮีลใจ", "เยียวยา", "อบอุ่นหัวใจ",
			"กำลังใจ", "ความสุข", "มิตรภาพ",
			"วรรณกรรม", "ความทรงจำ", "เติบโต",
			"癒し", "心温まる", "友情",
			"힐링", "위로", "따뜻한",
		})

	case "bl":
		return !isAdultRomance(text) && containsAny(text, []string{
			"boys love", "boy love", "bl novel",
			"นิยายวาย", "ชายรักชาย", "บอยเลิฟ",
			"ボーイズラブ", "bl小説",
			"보이즈 러브", "bl 소설",
		})

	case "business":
		return containsAny(text, []string{
			"business", "finance", "investment",
			"marketing", "entrepreneurship", "personal finance",
			"การเงิน", "ลงทุน", "ธุรกิจ", "การตลาด",
			"บริหาร", "ผู้ประกอบการ", "ออมเงิน", "วางแผนการเงิน",
			"ビジネス", "投資", "金融", "マーケティング",
			"비즈니스", "투자", "금융", "마케팅",
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
		writeError(w, http.StatusBadRequest, "Supported languages are th, en, ja, ko")
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
		"business":   "Business & Economics",
		"lifestyle":  "House & Home",
		"english":    "Foreign Language Study",
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

	if language != "" {
		params.Set("langRestrict", language)
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
		coverURL := selectBestCover(info)
		bookLanguage := strings.ToLower(strings.TrimSpace(info.Language))

		if title == "" || author == "" || item.ID == "" ||
			!isSupportedBookLanguage(bookLanguage) ||
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
			Language:      bookLanguage,
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
				"จิตวิทยา พัฒนาตนเอง mindset psychology self help",
				"personal development psychology self improvement",
				"自己啓発 心理学",
				"자기계발 심리학",
			},
			Subject:    "Self-Help",
			Kind:       "self-help",
			Categories: []string{"Self-Help"},
			Target:     12,
		},
		{
			Queries: []string{
				"manga comics graphic novel",
				"มังงะ การ์ตูน ไลท์โนเวล",
				"漫画 マンガ コミック",
				"만화 웹툰 그래픽노블",
			},
			Subject:    "Comics & Graphic Novels",
			Kind:       "manga",
			Categories: []string{"Adventure"},
			Target:     12,
		},
		{
			Queries: []string{
				"English learning vocabulary grammar",
				"หนังสือภาษาอังกฤษ คำศัพท์ ไวยากรณ์",
				"英語学習 英会話",
				"영어학습 영어회화",
			},
			Subject:    "Foreign Language Study",
			Kind:       "english",
			Categories: []string{"Education"},
			Target:     8,
		},
		{
			Queries: []string{
				"fantasy fiction magic adventure",
				"นิยายแฟนตาซี เวทมนตร์ ผจญภัย",
				"ファンタジー小説 魔法 冒険",
				"판타지 소설 마법 모험",
			},
			Subject:    "Fantasy",
			Kind:       "fantasy",
			Categories: []string{"Fantasy"},
			Target:     14,
		},
		{
			Queries: []string{
				"mystery detective crime fiction thriller",
				"นิยายสืบสวนสอบสวน ปริศนา นักสืบ",
				"ミステリー 推理小説 探偵",
				"추리소설 미스터리 탐정",
			},
			Subject:    "Mystery",
			Kind:       "mystery",
			Categories: []string{"Mystery", "Thriller"},
			Target:     12,
		},
		{
			Queries: []string{
				"lifestyle minimalism home design organization",
				"หนังสือไลฟ์สไตล์ จัดบ้าน แต่งบ้าน",
				"ライフスタイル インテリア 片付け",
				"라이프스타일 인테리어 정리",
			},
			Subject:    "House & Home",
			Kind:       "lifestyle",
			Categories: []string{"Business"},
			Target:     8,
		},
		{
			Queries: []string{
				"romance fiction love story contemporary",
				"นิยายรักโรแมนติก ความสัมพันธ์",
				"恋愛小説 ロマンス",
				"로맨스 소설 사랑",
			},
			Subject:    "Romance",
			Kind:       "romance",
			Categories: []string{"Romance"},
			Target:     12,
		},
		{
			Queries: []string{
				"healing fiction feel good friendship",
				"หนังสือฮีลใจ วรรณกรรมอบอุ่นหัวใจ",
				"癒し小説 心温まる 友情",
				"힐링 소설 위로 우정",
			},
			Subject:    "Fiction",
			Kind:       "healing",
			Categories: []string{"Self-Help"},
			Target:     10,
		},
		{
			Queries: []string{
				"boys love romance BL novel",
				"นิยายวาย boy love ชายรักชาย",
				"ボーイズラブ BL小説",
				"보이즈 러브 BL 소설",
			},
			Subject:    "Romance",
			Kind:       "bl",
			Categories: []string{"Romance"},
			Target:     6,
		},
		{
			Queries: []string{
				"business finance investment marketing",
				"หนังสือการเงิน การลงทุน ธุรกิจ การตลาด",
				"ビジネス 投資 金融",
				"비즈니스 투자 금융 마케팅",
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

	candidatesByPlan := make([][]store.BulkImportBook, len(plans))
	rejectedByRelevance := 0
	candidateCounts := make(map[string]int)

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
			language := strings.ToLower(strings.TrimSpace(result.Language))

			if title == "" || author == "" ||
				result.GoogleBooksID == "" ||
				!isSupportedBookLanguage(language) ||
				!isValidCoverURL(coverURL) ||
				isPeriodical(title) {
				continue
			}

			if !isRecentBook(result) {
				rejectedByRelevance++
				continue
			}

			if isLowQualityDiscoverBook(result) ||
				isHorrorBook(result) ||
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

			year := result.PublishedYear
			publishedYear := &year

			candidatesByPlan[i] = append(
				candidatesByPlan[i],
				store.BulkImportBook{
					Title:         title,
					Author:        author,
					Description:   description,
					CoverURL:      coverURL,
					PublishedYear: publishedYear,
					Categories:    plan.Categories,
					Language:      language,
				},
			)
		}

		candidateCounts[plan.Kind] = len(candidatesByPlan[i])
	}

	// สลับหนังสือจากแต่ละหมวด เพื่อลดโอกาสที่หมวดเดียวจะครองรายการนำเข้า
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
			"message":                 "No recent books in the selected languages passed the import filters",
			"supported_languages":     supportedBookLanguages,
			"successful_searches":     successfulSearches,
			"search_plans":            len(plans),
			"candidate_target":        bookTarget,
			"found":                   0,
			"candidate_counts":        candidateCounts,
			"rejected_by_relevance":   rejectedByRelevance,
			"inserted":                0,
			"skipped":                 0,
			"total_books_in_database": 0,
			"visible_catalog_target":  bookTarget,
			"minimum_published_year":  minBookYear,
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
		"message":             "Book import completed",
		"supported_languages": supportedBookLanguages,
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
		"candidate_counts":        candidateCounts,
		"candidate_target":        bookTarget,
		"found":                   len(importBooks),
		"rejected_by_relevance":   rejectedByRelevance,
		"inserted":                inserted,
		"skipped":                 skipped,
		"skip_reasons":            stats,
		"total_books_in_database": total,
		"visible_catalog_target":  bookTarget,
		"minimum_published_year":  minBookYear,
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

	sortOrders := []string{"newest", "relevance"}

	for _, language := range supportedBookLanguages {
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
					params.Set("langRestrict", language)
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
							"Google Books API returned status %d for language %s",
							resp.StatusCode,
							language,
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
						bookLanguage := strings.ToLower(strings.TrimSpace(info.Language))

						if title == "" || item.ID == "" ||
							strings.TrimSpace(author) == "" ||
							!isSupportedBookLanguage(bookLanguage) ||
							bookLanguage != language ||
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
							Language:      bookLanguage,
							Categories:    info.Categories,
						})
					}
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
