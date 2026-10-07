package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseAPIURL   = "https://api-mimo.neperone.id"
	DefaultWajikBaseURL = "https://wajik-anime-api.vercel.app"
	
	ProviderNyamimo       = "nyamimo"
	ProviderAnimeKuDesu   = "animekudesu"
	ProviderWajikOploverz = "wajik_oploverz"
	ProviderWajikOtakudesu = "wajik_otakudesu"
	ProviderWajikKuramanime = "wajik_kuramanime"
	ProviderCustom        = "custom"
)

var (
	BaseAPIURL     = DefaultBaseAPIURL
	ActiveProvider = ProviderNyamimo
)

type CacheItem struct {
	Data       []byte
	Expiration time.Time
}

type APIClient struct {
	httpClient *http.Client
	cache      sync.Map
	ttl        time.Duration
	baseURL    string
	provider   string
}

func NewAPIClient(ttl time.Duration) *APIClient {
	return &APIClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		ttl:      ttl,
		baseURL:  BaseAPIURL,
		provider: ActiveProvider,
	}
}

func (c *APIClient) SetProvider(provider string) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		provider = ProviderNyamimo
	}
	c.provider = provider
	ActiveProvider = provider
	c.ClearCache()
}

func (c *APIClient) GetProvider() string {
	if c.provider != "" {
		return c.provider
	}
	return ActiveProvider
}

func (c *APIClient) SetBaseURL(u string) {
	u = strings.TrimSpace(u)
	if u != "" {
		u = strings.TrimSuffix(u, "/")
		c.baseURL = u
		BaseAPIURL = u
		c.ClearCache()
	}
}

func (c *APIClient) GetBaseURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return BaseAPIURL
}

func (c *APIClient) ClearCache() {
	c.cache.Range(func(key, value interface{}) bool {
		c.cache.Delete(key)
		return true
	})
}

// Low-level HTTP GET with caching
func (c *APIClient) fetchRaw(fullURL string) ([]byte, error) {
	// Check in-memory cache
	if item, ok := c.cache.Load(fullURL); ok {
		cached := item.(CacheItem)
		if time.Now().Before(cached.Expiration) {
			return cached.Data, nil
		}
		c.cache.Delete(fullURL)
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API HTTP status %d for %s", resp.StatusCode, fullURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	c.cache.Store(fullURL, CacheItem{
		Data:       body,
		Expiration: time.Now().Add(c.ttl),
	})

	return body, nil
}

// Universal GetJSON with smart provider adaptation
func (c *APIClient) GetJSON(endpoint string, target interface{}) error {
	endpoint = strings.TrimSpace(endpoint)
	provider := c.GetProvider()
	baseURL := c.GetBaseURL()

	// If provider is Nyamimo or baseURL points to api-mimo / neperone.id
	if provider == ProviderNyamimo || strings.Contains(baseURL, "neperone.id") || strings.Contains(baseURL, "api-mimo") {
		handled, err := c.handleNyamimoEndpoint(endpoint, target)
		if handled {
			return err
		}
	}

	// If provider is Wajik (Oploverz / Otakudesu / Kuramanime), adapt common endpoints
	if strings.HasPrefix(provider, "wajik_") {
		handled, err := c.handleWajikEndpoint(provider, endpoint, target)
		if handled {
			return err
		}
	}

	// Default AnimeKuDesu / Custom direct call
	fullURL := endpoint
	if !strings.HasPrefix(fullURL, "http") {
		if !strings.HasPrefix(fullURL, "/") {
			fullURL = "/" + fullURL
		}
		fullURL = c.GetBaseURL() + fullURL
	}

	body, err := c.fetchRaw(fullURL)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, target)
}

// Adapter for Nyamimo / api-mimo endpoints
func (c *APIClient) handleNyamimoEndpoint(endpoint string, target interface{}) (bool, error) {
	baseURL := c.GetBaseURL()
	if baseURL == "" {
		baseURL = DefaultBaseAPIURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	// 1. Home / Ongoing / Completed / New Anime / Popular / Genre / Type
	if endpoint == "/ongoing-anime" || endpoint == "/completed-anime" || endpoint == "/new-anime" ||
		endpoint == "/order-anime/popular" || endpoint == "/order-anime/latest-update" || endpoint == "/popular" ||
		strings.HasPrefix(endpoint, "/order-anime/") || strings.HasPrefix(endpoint, "/type-anime/") || strings.HasPrefix(endpoint, "/genre-anime/") {

		homeURL := baseURL + "/api/v1/home"
		if strings.HasSuffix(baseURL, "/api/v1") {
			homeURL = baseURL + "/home"
		}

		body, err := c.fetchRaw(homeURL)
		if err != nil {
			return true, err
		}

		var nyamimoHome struct {
			Status    string      `json:"status"`
			Banners   []AnimeItem `json:"banners"`
			Ongoing   []AnimeItem `json:"ongoing"`
			Completed []AnimeItem `json:"completed"`
			Genres    []Genre     `json:"genres"`
		}
		if err := json.Unmarshal(body, &nyamimoHome); err != nil {
			return true, err
		}

		var items []AnimeItem
		if endpoint == "/completed-anime" {
			items = nyamimoHome.Completed
		} else if endpoint == "/ongoing-anime" || endpoint == "/new-anime" || endpoint == "/order-anime/latest-update" {
			items = nyamimoHome.Ongoing
		} else if strings.HasPrefix(endpoint, "/genre-anime/") {
			genreID := strings.ToLower(strings.TrimPrefix(endpoint, "/genre-anime/"))
			genreID = strings.Trim(genreID, "/")

			// 1. Search VPS for this specific genre keyword
			searchURL := fmt.Sprintf("%s/api/v1/search?q=%s", baseURL, url.QueryEscape(genreID))
			if strings.HasSuffix(baseURL, "/api/v1") {
				searchURL = fmt.Sprintf("%s/search?q=%s", baseURL, url.QueryEscape(genreID))
			}

			if sBody, err := c.fetchRaw(searchURL); err == nil {
				var searchResp struct {
					Status  string      `json:"status"`
					Query   string      `json:"query"`
					Results []AnimeItem `json:"results"`
				}
				if err := json.Unmarshal(sBody, &searchResp); err == nil && len(searchResp.Results) > 0 {
					items = append(items, searchResp.Results...)
				}
			}

			// 2. Also search for common synonyms/aliases
			if genreID == "erotica" || genreID == "ecchi" || genreID == "hentai" {
				synonyms := []string{"ecchi", "erotica", "hentai"}
				for _, syn := range synonyms {
					if syn == genreID {
						continue
					}
					synURL := fmt.Sprintf("%s/api/v1/search?q=%s", baseURL, url.QueryEscape(syn))
					if strings.HasSuffix(baseURL, "/api/v1") {
						synURL = fmt.Sprintf("%s/search?q=%s", baseURL, url.QueryEscape(syn))
					}
					if sBody, err := c.fetchRaw(synURL); err == nil {
						var synResp struct {
							Status  string      `json:"status"`
							Results []AnimeItem `json:"results"`
						}
						if err := json.Unmarshal(sBody, &synResp); err == nil {
							for _, it := range synResp.Results {
								exists := false
								for _, existIt := range items {
									if existIt.Slug == it.Slug {
										exists = true
										break
									}
								}
								if !exists {
									items = append(items, it)
								}
							}
						}
					}
				}
			}

			// 3. If no search results found, filter from ongoing & completed
			if len(items) == 0 {
				all := append(nyamimoHome.Ongoing, nyamimoHome.Completed...)
				for _, it := range all {
					itLower := strings.ToLower(it.Title + " " + it.Type)
					if strings.Contains(itLower, genreID) {
						items = append(items, it)
					}
				}
			}

			// 4. Fallback to all if still empty
			if len(items) == 0 {
				items = append(nyamimoHome.Ongoing, nyamimoHome.Completed...)
			}
		} else {
			// Popular or latest
			items = append(nyamimoHome.Ongoing, nyamimoHome.Completed...)
		}

		switch t := target.(type) {
		case *AnimeListResponse:
			t.Status = "success"
			t.Data = items
			return true, nil
		case *struct {
			TotalPage int         `json:"total_page"`
			Data      []AnimeItem `json:"data"`
		}:
			t.TotalPage = 1
			t.Data = items
			return true, nil
		}
	}

	// 2. Genres list
	if endpoint == "/genres" {
		defaultGenres := GetDefaultGenres()
		if t, ok := target.(*GenreListResponse); ok {
			t.Status = "success"
			t.Data = defaultGenres
			return true, nil
		}
	}

	// 3. Anime Detail: /detail-anime/:slug
	if strings.HasPrefix(endpoint, "/detail-anime/") {
		slug := strings.TrimPrefix(endpoint, "/detail-anime/")
		slug = strings.Trim(slug, "/")

		detailURL := fmt.Sprintf("%s/api/v1/anime/%s", baseURL, slug)
		if strings.HasSuffix(baseURL, "/api/v1") {
			detailURL = fmt.Sprintf("%s/anime/%s", baseURL, slug)
		}

		body, err := c.fetchRaw(detailURL)
		if err != nil {
			return true, err
		}

		var resp struct {
			Status string          `json:"status"`
			Data   AnimeDetailData `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return true, err
		}

		switch t := target.(type) {
		case *AnimeDetailResponse:
			t.Status = "success"
			t.Data = resp.Data
			return true, nil
		case *AnimeDetailData:
			*t = resp.Data
			return true, nil
		}
	}

	// 4. Search: /search-anime?search=... or /search/...
	if strings.HasPrefix(endpoint, "/search-anime") || strings.HasPrefix(endpoint, "/search/") {
		var q string
		if strings.HasPrefix(endpoint, "/search-anime") {
			u, _ := url.Parse(endpoint)
			q = u.Query().Get("search")
			if q == "" {
				q = u.Query().Get("q")
			}
		} else {
			q = strings.TrimPrefix(endpoint, "/search/")
		}

		searchURL := fmt.Sprintf("%s/api/v1/search?q=%s", baseURL, url.QueryEscape(q))
		if strings.HasSuffix(baseURL, "/api/v1") {
			searchURL = fmt.Sprintf("%s/search?q=%s", baseURL, url.QueryEscape(q))
		}

		body, err := c.fetchRaw(searchURL)
		if err != nil {
			return true, err
		}

		var searchResp struct {
			Status  string      `json:"status"`
			Query   string      `json:"query"`
			Results []AnimeItem `json:"results"`
		}
		if err := json.Unmarshal(body, &searchResp); err != nil {
			return true, err
		}

		if t, ok := target.(*AnimeListResponse); ok {
			t.Status = "success"
			t.Data = searchResp.Results
			return true, nil
		}
	}

	// 5. Episode Detail: any call where target is *EpisodeDetailResponse OR endpoint contains episode
	_, isEpisodeTarget := target.(*EpisodeDetailResponse)
	if isEpisodeTarget || strings.Contains(endpoint, "episode") || strings.HasPrefix(endpoint, "/episode") || strings.HasPrefix(endpoint, "/api/v1/episode") {
		slug := endpoint
		if strings.HasPrefix(slug, "/api/v1/episode") || strings.HasPrefix(slug, "/episode") {
			u, err := url.Parse(slug)
			if err == nil {
				qSlug := u.Query().Get("slug")
				if qSlug == "" {
					qSlug = u.Query().Get("detail_eps")
				}
				if qSlug != "" {
					slug = qSlug
				}
			}
		}
		slug = strings.TrimPrefix(slug, "/episode/")
		slug = strings.TrimPrefix(slug, "/api/v1/episode/")
		slug = strings.TrimPrefix(slug, "/api/episode/")
		slug = strings.TrimPrefix(slug, "/")

		if slug != "" {
			epURL := fmt.Sprintf("%s/api/v1/episode?slug=%s", baseURL, url.QueryEscape(slug))
			if strings.HasSuffix(baseURL, "/api/v1") {
				epURL = fmt.Sprintf("%s/episode?slug=%s", baseURL, url.QueryEscape(slug))
			}

			body, err := c.fetchRaw(epURL)
			if err != nil {
				return true, err
			}

			var vpsEp struct {
				Status            string           `json:"status"`
				Title             string           `json:"title"`
				EpisodeNum        string           `json:"episodeNum"`
				VideoURL          string           `json:"videoURL"`
				RawVideoURL       string           `json:"video_url"`
				RawIframe         string           `json:"rawIframe"`
				IsDirectVideo     bool             `json:"isDirectVideo"`
				Videos            []PlayerOption   `json:"videos"`
				ActiveServerTitle string           `json:"activeServerTitle"`
				Downloads         []DownloadFormat `json:"downloads"`
			}
			if err := json.Unmarshal(body, &vpsEp); err == nil {
				vURL := vpsEp.VideoURL
				if vURL == "" {
					vURL = vpsEp.RawVideoURL
				}
				if vURL == "" && len(vpsEp.Videos) > 0 {
					vURL = vpsEp.Videos[0].Video
				}

				if t, ok := target.(*EpisodeDetailResponse); ok {
					t.Title = vpsEp.Title
					t.EpisodeNumber = vpsEp.EpisodeNum
					t.VideoURL = vURL
					t.Videos = vpsEp.Videos
					t.Downloads = vpsEp.Downloads
					return true, nil
				}
			}

			return true, json.Unmarshal(body, target)
		}
	}

	return false, nil
}

// Adapter for Wajik endpoints
func (c *APIClient) handleWajikEndpoint(provider, endpoint string, target interface{}) (bool, error) {
	source := "oploverz"
	if provider == ProviderWajikOtakudesu {
		source = "otakudesu"
	} else if provider == ProviderWajikKuramanime {
		source = "kuramanime"
	}

	baseURL := c.GetBaseURL()
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = DefaultWajikBaseURL
	}

	// 1. Home / New Anime / Ongoing / Completed
	if endpoint == "/new-anime" || endpoint == "/ongoing-anime" || endpoint == "/completed-anime" || endpoint == "/order-anime/popular" || endpoint == "/order-anime/latest-update" || endpoint == "/popular" {
		homeURL := fmt.Sprintf("%s/%s/home", baseURL, source)
		body, err := c.fetchRaw(homeURL)
		if err != nil {
			return true, err
		}

		var wajikHome struct {
			Data struct {
				PopularList []WajikHomeItem `json:"popularList"`
				LatestList  []WajikHomeItem `json:"latestList"`
				Ongoing     []WajikHomeItem `json:"ongoing"`
				Completed   []WajikHomeItem `json:"completed"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wajikHome); err != nil {
			return true, err
		}

		var items []AnimeItem
		if endpoint == "/new-anime" || endpoint == "/order-anime/latest-update" {
			for _, it := range wajikHome.Data.LatestList {
				items = append(items, it.ToAnimeItem(source))
			}
			if len(items) == 0 {
				for _, it := range wajikHome.Data.Ongoing {
					items = append(items, it.ToAnimeItem(source))
				}
			}
		} else if endpoint == "/completed-anime" {
			for _, it := range wajikHome.Data.Completed {
				items = append(items, it.ToAnimeItem(source))
			}
			if len(items) == 0 {
				for _, it := range wajikHome.Data.LatestList {
					if strings.EqualFold(it.Status, "Completed") {
						items = append(items, it.ToAnimeItem(source))
					}
				}
			}
			if len(items) == 0 {
				for _, it := range wajikHome.Data.PopularList {
					if strings.EqualFold(it.Status, "Completed") {
						items = append(items, it.ToAnimeItem(source))
					}
				}
			}
		} else {
			// Popular or ongoing
			for _, it := range wajikHome.Data.PopularList {
				items = append(items, it.ToAnimeItem(source))
			}
			if len(items) == 0 {
				for _, it := range wajikHome.Data.LatestList {
					items = append(items, it.ToAnimeItem(source))
				}
			}
		}

		// Fill target
		switch t := target.(type) {
		case *AnimeListResponse:
			t.Status = "success"
			t.Data = items
			return true, nil
		case *struct {
			TotalPage int         `json:"total_page"`
			Data      []AnimeItem `json:"data"`
		}:
			t.TotalPage = 10
			t.Data = items
			return true, nil
		}
	}

	// 2. Genres List
	if endpoint == "/genres" {
		defaultGenres := GetDefaultGenres()
		if t, ok := target.(*GenreListResponse); ok {
			t.Status = "success"
			t.Data = defaultGenres
			return true, nil
		}
	}

	// 3. Available Orders
	if endpoint == "/available-orders" {
		if t, ok := target.(*OrderListResponse); ok {
			t.Data = []OrderOption{
				{Title: "Terpopuler", Order: "popular"},
				{Title: "Rilis Terbaru", Order: "latest-update"},
			}
			return true, nil
		}
	}

	// 4. Anime Detail: /detail-anime/:slug
	if strings.HasPrefix(endpoint, "/detail-anime/") {
		slug := strings.TrimPrefix(endpoint, "/detail-anime/")
		slug = strings.Trim(slug, "/")
		detailURL := fmt.Sprintf("%s/%s/anime/%s", baseURL, source, slug)

		body, err := c.fetchRaw(detailURL)
		if err != nil {
			return true, err
		}

		var wajikDetail WajikDetailResponse
		if err := json.Unmarshal(body, &wajikDetail); err != nil {
			return true, err
		}

		normDetail := wajikDetail.ToAnimeDetailData(source, slug)

		switch t := target.(type) {
		case *AnimeDetailResponse:
			t.Status = "success"
			t.Data = normDetail
			return true, nil
		case *AnimeDetailData:
			*t = normDetail
			return true, nil
		}
	}

	// 5. Anime Downloads: /download-anime/:slug
	if strings.HasPrefix(endpoint, "/download-anime/") {
		// In Wajik, downloads are attached to episodes or batches
		slug := strings.TrimPrefix(endpoint, "/download-anime/")
		slug = strings.Trim(slug, "/")
		
		// Fallback empty list or extract from first episode
		if t, ok := target.(*struct {
			Status string           `json:"status"`
			Data   []DownloadFormat `json:"data"`
		}); ok {
			t.Status = "success"
			t.Data = []DownloadFormat{}
			return true, nil
		}
	}

	// 6. Episode Detail: /oploverz/episode/... or /episode/... or direct endpoint
	if strings.Contains(endpoint, "/episode/") || strings.HasPrefix(endpoint, "/api/episode/") {
		slug := endpoint
		if idx := strings.LastIndex(endpoint, "/"); idx != -1 {
			slug = endpoint[idx+1:]
		}
		epURL := fmt.Sprintf("%s/%s/episode/%s", baseURL, source, slug)
		body, err := c.fetchRaw(epURL)
		if err != nil {
			return true, err
		}

		var wajikEp WajikEpisodeResponse
		if err := json.Unmarshal(body, &wajikEp); err != nil {
			return true, err
		}

		normEp := wajikEp.ToEpisodeDetailResponse()
		if t, ok := target.(*EpisodeDetailResponse); ok {
			*t = normEp
			return true, nil
		}
	}

	// 7. Search: /search-anime?search=...
	if strings.HasPrefix(endpoint, "/search-anime") {
		u, _ := url.Parse(endpoint)
		q := u.Query().Get("search")
		if q == "" {
			q = u.Query().Get("q")
		}
		searchURL := fmt.Sprintf("%s/%s/search?q=%s", baseURL, source, url.QueryEscape(q))
		body, err := c.fetchRaw(searchURL)
		if err != nil {
			return true, err
		}

		var wajikSearch struct {
			Data struct {
				AnimeList []WajikHomeItem `json:"animeList"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &wajikSearch); err != nil {
			return true, err
		}

		var items []AnimeItem
		for _, it := range wajikSearch.Data.AnimeList {
			items = append(items, it.ToAnimeItem(source))
		}

		if t, ok := target.(*AnimeListResponse); ok {
			t.Status = "success"
			t.Data = items
			return true, nil
		}
	}

	// 8. Schedule: /release-schedule?day=...
	if strings.HasPrefix(endpoint, "/release-schedule") {
		schedURL := fmt.Sprintf("%s/%s/schedule", baseURL, source)
		body, err := c.fetchRaw(schedURL)
		if err != nil {
			return true, err
		}

		var wajikSched struct {
			Data struct {
				ScheduleList []struct {
					Day       string          `json:"day"`
					AnimeList []WajikHomeItem `json:"animeList"`
				} `json:"scheduleList"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &wajikSched)

		u, _ := url.Parse(endpoint)
		targetDay := strings.ToLower(u.Query().Get("day"))

		var items []AnimeItem
		for _, s := range wajikSched.Data.ScheduleList {
			if targetDay == "" || strings.EqualFold(s.Day, targetDay) || strings.Contains(strings.ToLower(s.Day), targetDay) {
				for _, it := range s.AnimeList {
					items = append(items, it.ToAnimeItem(source))
				}
			}
		}

		if t, ok := target.(*ReleaseScheduleResponse); ok {
			t.Message = "success"
			t.Day = targetDay
			t.TotalAnime = len(items)
			t.Data = items
			return true, nil
		}
	}

	// Fallback to direct raw call
	fullURL := endpoint
	if !strings.HasPrefix(fullURL, "http") {
		if !strings.HasPrefix(fullURL, "/") {
			fullURL = "/" + fullURL
		}
		fullURL = baseURL + fullURL
	}
	body, err := c.fetchRaw(fullURL)
	if err != nil {
		return true, err
	}
	return true, json.Unmarshal(body, target)
}

func (c *APIClient) GetTotalAnimeCount() int {
	if strings.HasPrefix(c.GetProvider(), "wajik_") {
		return 850
	}
	var resp struct {
		TotalPage int         `json:"total_page"`
		Data      []AnimeItem `json:"data"`
	}
	err := c.GetJSON("/order-anime/popular?page=1", &resp)
	if err == nil && resp.TotalPage > 0 {
		itemsPerPage := len(resp.Data)
		if itemsPerPage == 0 {
			itemsPerPage = 30
		}
		return resp.TotalPage * itemsPerPage
	}
	return 780
}

// Standard Data Models
type AnimeItem struct {
	Link        string      `json:"link"`
	DetailURL   string      `json:"detail_url"`
	Slug        string      `json:"slug"`
	Img         string      `json:"img"`
	Alt         string      `json:"alt"`
	Title       string      `json:"title"`
	Episode     string      `json:"episode"`
	Released    string      `json:"released"`
	Time        string      `json:"time"`
	Type        string      `json:"type"`
	Score       interface{} `json:"score"`
	TotalViews  interface{} `json:"total_views"`
	SeasonBadge string      `json:"-"`
}

type AnimeListResponse struct {
	Status string      `json:"status"`
	Data   []AnimeItem `json:"data"`
}

type Genre struct {
	Title string `json:"title"`
	ID    string `json:"id"`
	Link  string `json:"link"`
	Tag   string `json:"tag"`
}

type GenreListResponse struct {
	Status string  `json:"status"`
	Data   []Genre `json:"data"`
}

type EpisodeRef struct {
	Title     string      `json:"title"`
	DetailEps string      `json:"detail_eps"`
	Episode   interface{} `json:"episode"`
	Number    string      `json:"-"`
}

type DownloadLink struct {
	Title string `json:"title"`
	Link  string `json:"link"`
}

type DownloadResolution struct {
	Resolution string         `json:"resolution"`
	Links      []DownloadLink `json:"links"`
}

type DownloadFormat struct {
	Format string               `json:"format"`
	List   []DownloadResolution `json:"list"`
}

type AnimeDetailData struct {
	Title           string           `json:"title"`
	AltTitle        string           `json:"alt_title"`
	EnglishTitle    string           `json:"english_title"`
	JapaneseTitle   string           `json:"japanese_title"`
	Img             string           `json:"img"`
	Rating          string           `json:"rating"`
	Score           interface{}      `json:"score"`
	Type            string           `json:"type"`
	Status          string           `json:"status"`
	Duration        string           `json:"duration"`
	Release         string           `json:"release"`
	Released        string           `json:"released"`
	Studio          string           `json:"studio"`
	Descriptions    []string         `json:"descriptions"`
	Synopsis        string           `json:"synopsis"`
	Genres          []Genre          `json:"genres"`
	Episodes        []EpisodeRef     `json:"episodes"`
	Downloads       []DownloadFormat `json:"downloads"`
	BatchLink       string           `json:"batch_link"`
	Recommendations []AnimeItem      `json:"recommendations"`
}

type AnimeDetailResponse struct {
	Status string          `json:"status"`
	Data   AnimeDetailData `json:"data"`
}

type PlayerOption struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Post   string `json:"post"`
	Action string `json:"action"`
	Nume   string `json:"nume"`
	Type   string `json:"type"`
	Video  string `json:"video"`
}

type EpisodeDetailResponse struct {
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	EpisodeNumber interface{}      `json:"episode_number"`
	VideoURL      string           `json:"video_url"`
	Videos        []PlayerOption   `json:"videos"`
	Downloads     []DownloadFormat `json:"downloads"`
}

type VideoURLResponse struct {
	URL string `json:"url"`
}

type OrderOption struct {
	Title string `json:"title"`
	Order string `json:"order"`
}

type OrderListResponse struct {
	Data []OrderOption `json:"data"`
}

type ScheduleDay struct {
	Day   string      `json:"day"`
	Anime []AnimeItem `json:"anime"`
}

type ScheduleResponse struct {
	Data []ScheduleDay `json:"data"`
}

type ReleaseScheduleResponse struct {
	Message    string      `json:"message"`
	Day        string      `json:"day"`
	DayValue   string      `json:"day_value"`
	TotalAnime int         `json:"total_anime"`
	Data       []AnimeItem `json:"data"`
}

// ----------------------------------------------------
// Wajik API Adapter Structs & Helpers
// ----------------------------------------------------

type WajikHomeItem struct {
	Title       string      `json:"title"`
	Poster      string      `json:"poster"`
	Type        string      `json:"type"`
	Episode     string      `json:"episode"`
	Status      string      `json:"status"`
	Score       interface{} `json:"score"`
	Genres      string      `json:"genres"`
	ReleaseTime string      `json:"releaseTime"`
	SeriesName  string      `json:"seriesName"`
	Href        string      `json:"href"`
	Slug        string      `json:"slug"`
}

func (w WajikHomeItem) ExtractSlug() string {
	if w.Slug != "" {
		return strings.Trim(w.Slug, "/")
	}
	if w.Href != "" {
		u := strings.Trim(w.Href, "/")
		parts := strings.Split(u, "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			return last
		}
	}
	if w.SeriesName != "" {
		slug := strings.ToLower(w.SeriesName)
		slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
		return strings.Trim(slug, "-")
	}
	return "anime"
}

func (w WajikHomeItem) ToAnimeItem(source string) AnimeItem {
	slug := w.ExtractSlug()
	title := w.Title
	if title == "" {
		title = w.SeriesName
	}
	return AnimeItem{
		Title:       CleanAnimeTitle(title),
		Alt:         title,
		Slug:        slug,
		Link:        fmt.Sprintf("/anime/%s", slug),
		DetailURL:   fmt.Sprintf("/detail-anime/%s", slug),
		Img:         GetCleanHDImage(w.Poster),
		Episode:     w.Episode,
		Type:        w.Type,
		Score:       w.Score,
		Released:    w.ReleaseTime,
		SeasonBadge: ExtractSeasonBadge(title),
	}
}

type WajikDetailResponse struct {
	Data struct {
		Details struct {
			Title       string `json:"title"`
			Poster      string `json:"poster"`
			Rating      string `json:"rating"`
			Score       string `json:"score"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Duration    string `json:"duration"`
			ReleasedOn  string `json:"releasedOn"`
			Studio      string `json:"studio"`
			Synopsis    string `json:"synopsis"`
			Season      string `json:"season"`
			EpisodeList []struct {
				Episode string `json:"episode"`
				Title   string `json:"title"`
				Date    string `json:"date"`
				Href    string `json:"href"`
			} `json:"episodeList"`
		} `json:"details"`
	} `json:"data"`
}

func (w WajikDetailResponse) ToAnimeDetailData(source, slug string) AnimeDetailData {
	d := w.Data.Details
	scoreVal := d.Score
	if scoreVal == "" {
		scoreVal = d.Rating
	}

	var episodes []EpisodeRef
	for _, ep := range d.EpisodeList {
		epSlug := ""
		if ep.Href != "" {
			parts := strings.Split(strings.Trim(ep.Href, "/"), "/")
			if len(parts) > 0 {
				epSlug = parts[len(parts)-1]
			}
		}
		if epSlug == "" {
			epSlug = fmt.Sprintf("%s-episode-%s", slug, ep.Episode)
		}

		episodes = append(episodes, EpisodeRef{
			Title:     ep.Title,
			DetailEps: fmt.Sprintf("/%s/episode/%s", source, epSlug),
			Episode:   ep.Episode,
			Number:    ep.Episode,
		})
	}

	return AnimeDetailData{
		Title:        d.Title,
		JapaneseTitle: d.Title,
		Img:          GetCleanHDImage(d.Poster),
		Rating:       d.Rating,
		Score:        scoreVal,
		Type:         d.Type,
		Status:       d.Status,
		Duration:     d.Duration,
		Released:     d.ReleasedOn,
		Release:      d.ReleasedOn,
		Studio:       d.Studio,
		Synopsis:     d.Synopsis,
		Episodes:     episodes,
	}
}

type WajikEpisodeResponse struct {
	Data struct {
		Details struct {
			Title         string `json:"title"`
			EpisodeNumber string `json:"episodeNumber"`
			SeriesName    string `json:"seriesName"`
			StreamingURL  string `json:"streamingUrl"`
			Download      []struct {
				Title       string `json:"title"`
				QualityList []struct {
					Title   string `json:"title"`
					UrlList []struct {
						Title string `json:"title"`
						URL   string `json:"url"`
					} `json:"urlList"`
				} `json:"qualityList"`
			} `json:"download"`
		} `json:"details"`
	} `json:"data"`
}

func (w WajikEpisodeResponse) ToEpisodeDetailResponse() EpisodeDetailResponse {
	d := w.Data.Details
	var videos []PlayerOption
	if d.StreamingURL != "" {
		videos = append(videos, PlayerOption{
			ID:    "wajik-stream-main",
			Title: "Server Utama (HD Stream)",
			Video: d.StreamingURL,
			Type:  "embed",
		})
	}

	var downloads []DownloadFormat
	for _, dl := range d.Download {
		var resList []DownloadResolution
		for _, q := range dl.QualityList {
			var linkList []DownloadLink
			for _, u := range q.UrlList {
				linkList = append(linkList, DownloadLink{
					Title: u.Title,
					Link:  u.URL,
				})
			}
			resList = append(resList, DownloadResolution{
				Resolution: q.Title,
				Links:      linkList,
			})
		}
		downloads = append(downloads, DownloadFormat{
			Format: strings.ToUpper(dl.Title),
			List:   resList,
		})
	}

	return EpisodeDetailResponse{
		Title:         d.Title,
		EpisodeNumber: d.EpisodeNumber,
		VideoURL:      d.StreamingURL,
		Videos:        videos,
		Downloads:     downloads,
	}
}

func GetDefaultGenres() []Genre {
	genreNames := []string{
		"Action", "Adult", "Adventure", "Comedy", "Demons", "Drama", "Ecchi", "Erotica",
		"Fantasy", "Game", "Harem", "Hentai", "Historical", "Horror", "Isekai",
		"Josei", "Kids", "Magic", "Martial Arts", "Mecha", "Military", "Music",
		"Mystery", "Parody", "Police", "Psychological", "Romance", "Samurai", "School",
		"Sci-Fi", "Seinen", "Shoujo", "Shoujo Ai", "Shounen", "Shounen Ai",
		"Slice of Life", "Space", "Sports", "Super Power", "Supernatural",
		"Suspense", "Thriller", "Vampire",
	}
	var res []Genre
	for _, g := range genreNames {
		slug := strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(g, "-"))
		res = append(res, Genre{
			Title: g,
			ID:    slug,
			Link:  fmt.Sprintf("/genres/%s", slug),
			Tag:   g,
		})
	}
	return res
}

// ----------------------------------------------------
// Utility Functions
// ----------------------------------------------------

func GetAnimeSlug(item AnimeItem) string {
	if item.Slug != "" {
		return strings.TrimPrefix(item.Slug, "/")
	}
	u := item.DetailURL
	if u == "" {
		u = item.Link
	}
	u = strings.Split(u, "?")[0]
	parts := strings.Split(strings.Trim(u, "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func FormatScore(score interface{}) string {
	if score == nil {
		return "N/A"
	}
	switch v := score.(type) {
	case float64:
		if v <= 0 {
			return "N/A"
		}
		return fmt.Sprintf("%.1f", v)
	case string:
		if v == "" || v == "0" || v == "N/A" {
			return "N/A"
		}
		return v
	default:
		return "N/A"
	}
}

func FormatEpisodeNum(ep interface{}) string {
	if ep == nil {
		return "-"
	}
	switch v := ep.(type) {
	case float64:
		return fmt.Sprintf("%.0f", v)
	case string:
		if v == "" {
			return "-"
		}
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func FormatSearchQuery(q string) string {
	return url.QueryEscape(q)
}

func CleanAnimeTitle(t string) string {
	t = strings.TrimSpace(t)
	if idx := strings.Index(t, "Sub Indo"); idx != -1 {
		cleaned := strings.TrimSpace(t[:idx])
		if cleaned != "" {
			return cleaned
		}
	}
	return t
}

func GetCleanHDImage(img string) string {
	if img == "" {
		return ""
	}
	idx := strings.LastIndex(img, ".")
	if idx == -1 {
		return img
	}
	ext := img[idx:]
	base := img[:idx]
	dashIdx := strings.LastIndex(base, "-")
	if dashIdx != -1 {
		dim := base[dashIdx+1:]
		if strings.Contains(dim, "x") {
			parts := strings.Split(dim, "x")
			if len(parts) == 2 {
				return base[:dashIdx] + ext
			}
		}
	}
	return img
}

func ExtractSeasonBadge(title string) string {
	t := strings.TrimSpace(title)
	lower := strings.ToLower(t)

	if strings.Contains(lower, "movie") || strings.Contains(lower, "gekijouban") {
		return "Movie"
	}
	if strings.Contains(lower, "ova") || strings.Contains(lower, "oad") || strings.Contains(lower, "special") {
		return "OVA / Special"
	}

	reSeasonPart := regexp.MustCompile(`(?i)(season\s*\d+|1st\s*season|2nd\s*season|3rd\s*season|\d+th\s*season|s\d+)\s*(?:-?\s*|\s+)(part\s*\d+|cour\s*\d+)`)
	if match := reSeasonPart.FindString(t); match != "" {
		return strings.Title(strings.ToLower(match))
	}

	reSeason := regexp.MustCompile(`(?i)(season\s*\d+|1st\s*season|2nd\s*season|3rd\s*season|\d+th\s*season|season\s+[ivx]+)`)
	if match := reSeason.FindString(t); match != "" {
		return strings.Title(strings.ToLower(match))
	}

	rePart := regexp.MustCompile(`(?i)(part\s*\d+|cour\s*\d+)`)
	if match := rePart.FindString(t); match != "" {
		return strings.Title(strings.ToLower(match))
	}

	return "Season 1"
}
