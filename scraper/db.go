package scraper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nyamimo-go/client"
)

// LogEntry represents a real-time scraping log message
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // INFO, SUCCESS, WARN, ERROR
	Message   string `json:"message"`
}

// ScrapedAnime is the complete representation of an anime in the database
type ScrapedAnime struct {
	Slug        string                  `json:"slug"`
	Title       string                  `json:"title"`
	Img         string                  `json:"img"`
	Poster      string                  `json:"poster"`
	Score       string                  `json:"score"`
	Status      string                  `json:"status"`
	Type        string                  `json:"type"`
	Episode     string                  `json:"episode"`
	Synopsis    string                  `json:"synopsis"`
	GenreNames  []string                `json:"genre_names"`
	Episodes    []client.EpisodeRef     `json:"episodes"`
	Downloads   []client.DownloadFormat `json:"downloads"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// ScraperStats holds statistics about the current database
type ScraperStats struct {
	TotalAnime     int       `json:"total_anime"`
	TotalOngoing   int       `json:"total_ongoing"`
	TotalCompleted int       `json:"total_completed"`
	TotalEpisodes  int       `json:"total_episodes"`
	IsRunning      bool      `json:"is_running"`
	CurrentTask    string    `json:"current_task"`
	LastScrapedAt  time.Time `json:"last_scraped_at"`
}

// AnimeStore is a high-performance in-memory and disk-persisted database for scraped anime
type AnimeStore struct {
	mu            sync.RWMutex
	dataFile      string
	Animes        map[string]*ScrapedAnime // keyed by Slug
	EpisodeCache  map[string]*client.EpisodeDetailResponse // keyed by Episode slug
	OngoingSlugs  []string
	CompleteSlugs []string
	Logs          []LogEntry
	maxLogs       int
	IsRunning     bool
	CurrentTask   string
	LastScraped   time.Time
	stopChan      chan struct{}
}

var (
	GlobalStore *AnimeStore
	storeOnce   sync.Once
)

func GetStore() *AnimeStore {
	storeOnce.Do(func() {
		dataDir := "data"
		_ = os.MkdirAll(dataDir, 0755)
		dataFile := filepath.Join(dataDir, "anime_database.json")

		GlobalStore = &AnimeStore{
			dataFile:      dataFile,
			Animes:        make(map[string]*ScrapedAnime),
			EpisodeCache:  make(map[string]*client.EpisodeDetailResponse),
			OngoingSlugs:  make([]string, 0),
			CompleteSlugs: make([]string, 0),
			Logs:          make([]LogEntry, 0),
			maxLogs:       300,
			stopChan:      make(chan struct{}),
		}
		GlobalStore.loadFromDisk()
	})
	return GlobalStore
}

func (s *AnimeStore) AddLog(level, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := time.Now().Format("15:04:05")
	entry := LogEntry{
		Timestamp: t,
		Level:     level,
		Message:   message,
	}

	s.Logs = append(s.Logs, entry)
	if len(s.Logs) > s.maxLogs {
		s.Logs = s.Logs[len(s.Logs)-s.maxLogs:]
	}
}

func (s *AnimeStore) GetLogs() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]LogEntry, len(s.Logs))
	copy(result, s.Logs)
	return result
}

func (s *AnimeStore) GetStats() ScraperStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalEps := 0
	for _, a := range s.Animes {
		totalEps += len(a.Episodes)
	}

	return ScraperStats{
		TotalAnime:     len(s.Animes),
		TotalOngoing:   len(s.OngoingSlugs),
		TotalCompleted: len(s.CompleteSlugs),
		TotalEpisodes:  totalEps,
		IsRunning:      s.IsRunning,
		CurrentTask:    s.CurrentTask,
		LastScrapedAt:  s.LastScraped,
	}
}

func (s *AnimeStore) IsStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.IsRunning {
		return true
	}
	if s.stopChan == nil {
		return true
	}
	select {
	case <-s.stopChan:
		return true
	default:
		return false
	}
}

func (s *AnimeStore) ResetCatalog() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Animes = make(map[string]*ScrapedAnime)
	s.EpisodeCache = make(map[string]*client.EpisodeDetailResponse)
	s.OngoingSlugs = make([]string, 0)
	s.CompleteSlugs = make([]string, 0)
	_ = os.Remove(s.dataFile)
}

func (s *AnimeStore) SaveAnime(anime *ScrapedAnime, isOngoing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if anime == nil || anime.Slug == "" {
		return
	}

	anime.UpdatedAt = time.Now()
	s.Animes[anime.Slug] = anime

	if isOngoing {
		if !contains(s.OngoingSlugs, anime.Slug) {
			s.OngoingSlugs = append([]string{anime.Slug}, s.OngoingSlugs...)
		}
		s.CompleteSlugs = remove(s.CompleteSlugs, anime.Slug)
	} else {
		if !contains(s.CompleteSlugs, anime.Slug) {
			s.CompleteSlugs = append([]string{anime.Slug}, s.CompleteSlugs...)
		}
		s.OngoingSlugs = remove(s.OngoingSlugs, anime.Slug)
	}
}

func (s *AnimeStore) GetOngoing(limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []client.AnimeItem
	for _, slug := range s.OngoingSlugs {
		if a, ok := s.Animes[slug]; ok {
			result = append(result, a.ToAnimeItem())
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result
}

func (s *AnimeStore) GetCompleted(limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []client.AnimeItem
	for _, slug := range s.CompleteSlugs {
		if a, ok := s.Animes[slug]; ok {
			result = append(result, a.ToAnimeItem())
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result
}

func (s *AnimeStore) GetAllAnime(limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []client.AnimeItem
	for _, a := range s.Animes {
		result = append(result, a.ToAnimeItem())
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func (s *AnimeStore) GetAnime(slug string) (*client.AnimeDetailData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a, ok := s.Animes[slug]
	if !ok {
		for sKey, item := range s.Animes {
			if strings.EqualFold(sKey, slug) || strings.EqualFold(item.Slug, slug) {
				detail := item.ToAnimeDetailData()
				return &detail, true
			}
		}
		return nil, false
	}
	detail := a.ToAnimeDetailData()
	return &detail, true
}

func normalizeSearchString(str string) string {
	str = strings.ToLower(str)
	var sb strings.Builder
	for _, r := range str {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func (s *AnimeStore) Search(query string, limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normQ := normalizeSearchString(query)
	if normQ == "" {
		return s.GetOngoing(limit)
	}

	qTokens := strings.Fields(normQ)
	compactQ := strings.ReplaceAll(normQ, " ", "")

	var result []client.AnimeItem
	for _, a := range s.Animes {
		normTitle := normalizeSearchString(a.Title)
		normSlug := normalizeSearchString(strings.ReplaceAll(a.Slug, "-", " "))
		normSynopsis := normalizeSearchString(a.Synopsis)
		normGenres := normalizeSearchString(strings.Join(a.GenreNames, " "))
		combinedText := normTitle + " " + normSlug + " " + normSynopsis + " " + normGenres
		compactTitle := strings.ReplaceAll(normTitle, " ", "")

		// Check 1: Exact normalized substring match in title
		matched := strings.Contains(normTitle, normQ) || strings.Contains(normSlug, normQ)

		// Check 2: Compact string match (e.g. "rezero" matches "re zero", "onepiece" matches "one piece")
		if !matched && compactQ != "" && (strings.Contains(compactTitle, compactQ) || strings.Contains(strings.ReplaceAll(normSlug, " ", ""), compactQ)) {
			matched = true
		}

		// Check 3: Multi-token match (all tokens in query exist in title/slug/genres/synopsis)
		if !matched && len(qTokens) > 0 {
			allTokensMatch := true
			for _, token := range qTokens {
				if !strings.Contains(combinedText, token) {
					allTokensMatch = false
					break
				}
			}
			if allTokensMatch {
				matched = true
			}
		}

		if matched {
			result = append(result, a.ToAnimeItem())
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result
}

func (s *AnimeStore) GetByGenre(genreID string, limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	gQuery := strings.ToLower(strings.TrimSpace(genreID))
	var result []client.AnimeItem
	for _, a := range s.Animes {
		for _, g := range a.GenreNames {
			cleanG := strings.ToLower(strings.ReplaceAll(g, " ", "-"))
			if cleanG == gQuery || strings.EqualFold(g, gQuery) {
				result = append(result, a.ToAnimeItem())
				if limit > 0 && len(result) >= limit {
					return result
				}
				break
			}
		}
	}
	return result
}

func (s *AnimeStore) GetByType(t string, limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tQuery := strings.ToLower(strings.TrimSpace(t))
	var result []client.AnimeItem
	for _, a := range s.Animes {
		if strings.EqualFold(a.Type, tQuery) || (tQuery == "tv" && (a.Type == "" || strings.Contains(strings.ToLower(a.Type), "tv"))) {
			result = append(result, a.ToAnimeItem())
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	if len(result) == 0 {
		return s.GetAllAnime(limit)
	}
	return result
}

func (s *AnimeStore) GetPopular(limit int) []client.AnimeItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var list []client.AnimeItem
	for _, slug := range s.CompleteSlugs {
		if a, ok := s.Animes[slug]; ok {
			list = append(list, a.ToAnimeItem())
		}
	}
	for _, slug := range s.OngoingSlugs {
		if a, ok := s.Animes[slug]; ok {
			list = append(list, a.ToAnimeItem())
		}
	}
	if len(list) == 0 {
		for _, a := range s.Animes {
			list = append(list, a.ToAnimeItem())
		}
	}
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

func (s *AnimeStore) SaveEpisode(slug string, eps *client.EpisodeDetailResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slug == "" || eps == nil {
		return
	}
	if s.EpisodeCache == nil {
		s.EpisodeCache = make(map[string]*client.EpisodeDetailResponse)
	}
	s.EpisodeCache[slug] = eps
}

func (s *AnimeStore) GetEpisode(slug string) (*client.EpisodeDetailResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.EpisodeCache == nil {
		return nil, false
	}
	eps, ok := s.EpisodeCache[slug]
	return eps, ok
}

func (a *ScrapedAnime) ToAnimeItem() client.AnimeItem {
	epNum := a.Episode
	if epNum == "" && len(a.Episodes) > 0 {
		epNum = a.Episodes[0].Title
	}
	if epNum == "" {
		epNum = a.Status
	}

	return client.AnimeItem{
		Title:     a.Title,
		Slug:      a.Slug,
		Img:       a.Img,
		Link:      "/anime/" + a.Slug,
		DetailURL: "/anime/" + a.Slug,
		Score:     a.Score,
		Type:      a.Type,
		Episode:   epNum,
	}
}

func (a *ScrapedAnime) ToAnimeDetailData() client.AnimeDetailData {
	genres := make([]client.Genre, 0)
	for _, g := range a.GenreNames {
		genres = append(genres, client.Genre{
			Title: g,
			ID:    strings.ToLower(strings.ReplaceAll(g, " ", "-")),
		})
	}

	return client.AnimeDetailData{
		Title:        a.Title,
		AltTitle:     a.Title,
		Img:          a.Img,
		Rating:       a.Score,
		Score:        a.Score,
		Type:         a.Type,
		Status:       a.Status,
		Synopsis:     a.Synopsis,
		Descriptions: []string{a.Synopsis},
		Genres:       genres,
		Episodes:     a.Episodes,
		Downloads:    a.Downloads,
	}
}

func (s *AnimeStore) FlushToDisk() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type diskFormat struct {
		Animes        map[string]*ScrapedAnime `json:"animes"`
		OngoingSlugs  []string                 `json:"ongoing_slugs"`
		CompleteSlugs []string                 `json:"complete_slugs"`
		LastScraped   time.Time                `json:"last_scraped"`
	}

	data := diskFormat{
		Animes:        s.Animes,
		OngoingSlugs:  s.OngoingSlugs,
		CompleteSlugs: s.CompleteSlugs,
		LastScraped:   s.LastScraped,
	}

	bytesData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := s.dataFile + ".tmp"
	if err := os.WriteFile(tmpFile, bytesData, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, s.dataFile)
}

func (s *AnimeStore) loadFromDisk() {
	if _, err := os.Stat(s.dataFile); os.IsNotExist(err) {
		return
	}

	bytesData, err := os.ReadFile(s.dataFile)
	if err != nil {
		return
	}

	type diskFormat struct {
		Animes        map[string]*ScrapedAnime `json:"animes"`
		OngoingSlugs  []string                 `json:"ongoing_slugs"`
		CompleteSlugs []string                 `json:"complete_slugs"`
		LastScraped   time.Time                `json:"last_scraped"`
	}

	var data diskFormat
	if err := json.Unmarshal(bytesData, &data); err != nil {
		return
	}

	s.Animes = data.Animes
	if s.Animes == nil {
		s.Animes = make(map[string]*ScrapedAnime)
	}
	s.OngoingSlugs = data.OngoingSlugs
	s.CompleteSlugs = data.CompleteSlugs
	s.LastScraped = data.LastScraped

	s.AddLog("INFO", fmt.Sprintf("Database lokal berhasil dimuat: %d anime, %d ongoing, %d tamat", len(s.Animes), len(s.OngoingSlugs), len(s.CompleteSlugs)))
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func remove(slice []string, val string) []string {
	var result []string
	for _, item := range slice {
		if item != val {
			result = append(result, item)
		}
	}
	return result
}
