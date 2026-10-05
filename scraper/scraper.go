package scraper

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"nyamimo-go/client"
)

type ScraperEngine struct {
	client            *http.Client
	baseURL           string
	samehadakuBaseURL string
}

func NewScraperEngine() *ScraperEngine {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &ScraperEngine{
		baseURL:           "https://otakudesu.blog",
		samehadakuBaseURL: "https://samehadaku.li",
		client: &http.Client{
			Transport: tr,
			Timeout:   15 * time.Second,
		},
	}
}

// Fetch raw HTML with realistic browser headers
func (e *ScraperEngine) fetchHTML(targetURL string) (string, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7")
	if strings.Contains(targetURL, "samehadaku") {
		req.Header.Set("Referer", "https://samehadaku.li/")
	} else {
		req.Header.Set("Referer", "https://google.com/")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// StartJob runs scraper asynchronously in background
func (e *ScraperEngine) StartJob(mode string) error {
	store := GetStore()
	store.mu.Lock()
	if store.IsRunning {
		store.mu.Unlock()
		return fmt.Errorf("bot scraper sedang berjalan")
	}
	store.IsRunning = true
	store.stopChan = make(chan struct{})
	store.mu.Unlock()

	go func() {
		defer func() {
			store.mu.Lock()
			store.IsRunning = false
			store.CurrentTask = ""
			store.mu.Unlock()
			_ = store.FlushToDisk()
			store.AddLog("INFO", "🏁 Background scraper task selesai.")
		}()

		switch mode {
		case "samehadaku_all", "samehadaku_az":
			store.mu.Lock()
			store.CurrentTask = "Scraping Seluruh Anime Samehadaku.li (A - Z)"
			store.mu.Unlock()
			_ = e.ScrapeSamehadakuCatalogAZ()

		case "samehadaku_ongoing":
			store.mu.Lock()
			store.CurrentTask = "Scraping Anime Ongoing Samehadaku.li"
			store.mu.Unlock()
			_, _ = e.ScrapeSamehadakuOngoing()

		case "samehadaku_completed":
			store.mu.Lock()
			store.CurrentTask = "Scraping Anime Tamat Samehadaku.li"
			store.mu.Unlock()
			_, _ = e.ScrapeSamehadakuCompleted()

		case "all_sources", "scrape_all_sources":
			store.mu.Lock()
			store.CurrentTask = "Scraping Semua Sumber (Otakudesu + Samehadaku A - Z)"
			store.mu.Unlock()
			store.AddLog("INFO", "🌟 MEMULAI SCRAPING MULTI-SUMBER: Otakudesu + Samehadaku...")
			_, _ = e.ScrapeSamehadakuOngoing()
			if store.IsStopped() {
				return
			}
			time.Sleep(200 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_ = e.ScrapeFullCatalogAZ()
			if store.IsStopped() {
				return
			}
			time.Sleep(300 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_ = e.ScrapeSamehadakuCatalogAZ()

		case "rescrape", "rescrape_all":
			store.AddLog("INFO", "🧹 Mereset database lama untuk memulai re-scraping bersih dari Otakudesu + Samehadaku...")
			store.ResetCatalog()
			store.mu.Lock()
			store.CurrentTask = "Re-scraping Bersih (Otakudesu + Samehadaku A - Z)"
			store.mu.Unlock()
			_ = e.ScrapeFullCatalogAZ()
			if store.IsStopped() {
				return
			}
			time.Sleep(300 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_ = e.ScrapeSamehadakuCatalogAZ()

		case "ongoing":
			store.mu.Lock()
			store.CurrentTask = "Scraping Anime Ongoing (Otakudesu + Samehadaku)"
			store.mu.Unlock()
			_, _ = e.ScrapeOngoing()
			if store.IsStopped() {
				return
			}
			time.Sleep(200 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_, _ = e.ScrapeSamehadakuOngoing()

		case "completed":
			store.mu.Lock()
			store.CurrentTask = "Scraping Anime Tamat (Otakudesu + Samehadaku)"
			store.mu.Unlock()
			_, _ = e.ScrapeCompleted()
			if store.IsStopped() {
				return
			}
			time.Sleep(200 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_, _ = e.ScrapeSamehadakuCompleted()

		case "all", "full", "otakudesu_all":
			store.mu.Lock()
			store.CurrentTask = "Scraping Seluruh Katalog Otakudesu (A - Z)"
			store.mu.Unlock()
			_ = e.ScrapeFullCatalogAZ()

		case "quick":
			fallthrough
		default:
			store.mu.Lock()
			store.CurrentTask = "Scraping Cepat Ongoing & Tamat"
			store.mu.Unlock()
			_, _ = e.ScrapeOngoing()
			if store.IsStopped() {
				return
			}
			time.Sleep(200 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_, _ = e.ScrapeSamehadakuOngoing()
			if store.IsStopped() {
				return
			}
			time.Sleep(200 * time.Millisecond)
			if store.IsStopped() {
				return
			}
			_, _ = e.ScrapeCompleted()
		}
	}()

	return nil
}

// Stop halts any active scraping process immediately
func (e *ScraperEngine) Stop() {
	store := GetStore()
	store.mu.Lock()
	if store.stopChan != nil {
		select {
		case <-store.stopChan:
		default:
			close(store.stopChan)
		}
	}
	store.IsRunning = false
	store.CurrentTask = "Dihentikan"
	store.mu.Unlock()
	store.AddLog("WARN", "🛑 Bot scraper berhasil dihentikan pengguna.")
}

// ==========================================
// 1. OTAKUDESU SCRAPING METHODS
// ==========================================

// Scrape Ongoing Anime with all episodes
func (e *ScraperEngine) ScrapeOngoing() ([]*ScrapedAnime, error) {
	store := GetStore()
	store.AddLog("INFO", "🌐 Menghubungkan ke sumber Otakudesu.blog (Ongoing)...")

	html, err := e.fetchHTML(e.baseURL + "/ongoing-anime/")
	if err != nil {
		html, err = e.fetchHTML(e.baseURL + "/")
		if err != nil {
			store.AddLog("ERROR", "❌ Gagal menghubungi server Otakudesu: "+err.Error())
			return nil, err
		}
	}

	itemRegex := regexp.MustCompile(`(?s)<div class=['"]detpost['"]>.*?<div class=['"]epz['"][^>]*>(?:<[^>]+>)*\s*([^<]+)</div>.*?<div class=['"]thumb['"]>\s*<a\s+href="([^"]+)".*?<img[^>]+src="([^"]+)".*?<h2 class=['"]jdlflm['"]>([^<]+)</h2>`)
	matches := itemRegex.FindAllStringSubmatch(html, -1)

	if len(matches) == 0 {
		itemRegex = regexp.MustCompile(`(?s)<div class=['"]thumb['"]>\s*<a\s+href="([^"]+)".*?<img[^>]+src="([^"]+)".*?<h2 class=['"]jdlflm['"]>([^<]+)</h2>.*?<div class=['"]epz['"][^>]*>(?:<[^>]+>)*\s*([^<]+)</div>`)
		rawMatches := itemRegex.FindAllStringSubmatch(html, -1)
		for _, rm := range rawMatches {
			matches = append(matches, []string{rm[0], rm[4], rm[1], rm[2], rm[3]})
		}
	}

	var results []*ScrapedAnime
	store.AddLog("INFO", fmt.Sprintf("📋 Ditemukan %d anime Ongoing di Otakudesu. Memproses detail & seluruh episode...", len(matches)))

	for idx, m := range matches {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping Ongoing dihentikan oleh pengguna.")
			return results, nil
		default:
		}

		episode := strings.TrimSpace(m[1])
		rawURL := strings.TrimSpace(m[2])
		imgURL := strings.TrimSpace(m[3])
		title := strings.TrimSpace(m[4])

		slug := extractSlugFromURL(rawURL)
		if slug == "" {
			continue
		}

		store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⏳ Mengikis Otakudesu: %s...", idx+1, len(matches), title))

		detail, _ := e.ScrapeAnimeDetail(slug)

		anime := &ScrapedAnime{
			Title:    title,
			Slug:     slug,
			Img:      imgURL,
			Poster:   imgURL,
			Score:    "8.6",
			Status:   "Ongoing",
			Type:     "TV Series",
			Episode:  episode,
			Synopsis: fmt.Sprintf("Nonton streaming anime %s subtitle Indonesia gratis kualitas jernih full HD di Nyamimo.", title),
		}

		if detail != nil && len(detail.Episodes) > 0 {
			anime.Episodes = detail.Episodes
			if detail.Synopsis != "" {
				anime.Synopsis = detail.Synopsis
			}
			if detail.Rating != "" {
				anime.Score = detail.Rating
			}
			if len(detail.Genres) > 0 {
				var gn []string
				for _, g := range detail.Genres {
					gn = append(gn, g.Title)
				}
				anime.GenreNames = gn
			}
		} else {
			anime.Episodes = []client.EpisodeRef{
				{
					Title:     "Episode " + cleanEpisodeNumber(episode),
					DetailEps: slug + "-episode-" + cleanEpisodeNumber(episode),
					Episode:   cleanEpisodeNumber(episode),
				},
			}
		}

		store.SaveAnime(anime, true)
		results = append(results, anime)
		store.AddLog("SUCCESS", fmt.Sprintf("[%d/%d] ✅ Tersimpan: %s (%d Episode)", idx+1, len(matches), title, len(anime.Episodes)))
		time.Sleep(30 * time.Millisecond)
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 Scraping Ongoing Otakudesu selesai! Total %d anime tersimpan.", len(results)))
	return results, nil
}

// Scrape Completed Anime from Otakudesu
func (e *ScraperEngine) ScrapeCompleted() ([]*ScrapedAnime, error) {
	store := GetStore()
	store.AddLog("INFO", "🌐 Menghubungkan ke sumber Otakudesu.blog (Completed/Tamat)...")

	html, err := e.fetchHTML(e.baseURL + "/complete-anime/")
	if err != nil {
		store.AddLog("ERROR", "❌ Gagal menghubungi sumber anime tamat Otakudesu: "+err.Error())
		return nil, err
	}

	itemRegex := regexp.MustCompile(`(?s)<div class=['"]detpost['"]>.*?<div class=['"]epz['"][^>]*>(?:<[^>]+>)*\s*([^<]+)</div>.*?<div class=['"]epztipe['"][^>]*>(?:<[^>]+>)*\s*([^<]+)</div>.*?<div class=['"]thumb['"]>\s*<a\s+href="([^"]+)".*?<img[^>]+src="([^"]+)".*?<h2 class=['"]jdlflm['"]>([^<]+)</h2>`)
	matches := itemRegex.FindAllStringSubmatch(html, -1)

	var results []*ScrapedAnime
	store.AddLog("INFO", fmt.Sprintf("📋 Ditemukan %d anime Tamat di Otakudesu. Memproses detail & seluruh episode...", len(matches)))

	for idx, m := range matches {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping Tamat dihentikan oleh pengguna.")
			return results, nil
		default:
		}

		episode := strings.TrimSpace(m[1])
		score := strings.TrimSpace(m[2])
		rawURL := strings.TrimSpace(m[3])
		imgURL := strings.TrimSpace(m[4])
		title := strings.TrimSpace(m[5])

		slug := extractSlugFromURL(rawURL)
		if slug == "" {
			continue
		}

		store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⏳ Mengikis Otakudesu Tamat: %s...", idx+1, len(matches), title))

		detail, _ := e.ScrapeAnimeDetail(slug)

		anime := &ScrapedAnime{
			Title:    title,
			Slug:     slug,
			Img:      imgURL,
			Poster:   imgURL,
			Score:    cleanScore(score),
			Status:   "Completed",
			Type:     "TV Series",
			Episode:  episode,
			Synopsis: fmt.Sprintf("Nonton streaming anime %s Sub Indo full tamat kualitas HD tanpa iklan di Nyamimo.", title),
		}

		if detail != nil && len(detail.Episodes) > 0 {
			anime.Episodes = detail.Episodes
			if detail.Synopsis != "" {
				anime.Synopsis = detail.Synopsis
			}
			if len(detail.Genres) > 0 {
				var gn []string
				for _, g := range detail.Genres {
					gn = append(gn, g.Title)
				}
				anime.GenreNames = gn
			}
		} else {
			anime.Episodes = []client.EpisodeRef{
				{
					Title:     "Full Episode (" + cleanEpisodeNumber(episode) + ")",
					DetailEps: slug + "-complete",
					Episode:   cleanEpisodeNumber(episode),
				},
			}
		}

		store.SaveAnime(anime, false)
		results = append(results, anime)
		store.AddLog("SUCCESS", fmt.Sprintf("[%d/%d] ✅ Tersimpan Tamat: %s (%d Episode)", idx+1, len(matches), title, len(anime.Episodes)))
		time.Sleep(30 * time.Millisecond)
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 Scraping Tamat Otakudesu selesai! Total %d anime tersimpan.", len(results)))
	return results, nil
}

// Scrape Full Anime Catalog A-Z from Otakudesu
func (e *ScraperEngine) ScrapeFullCatalogAZ() error {
	store := GetStore()

	store.AddLog("INFO", "🚀 MEMULAI SCRAPING KATALOG OTAKUDESU DARI A - Z...")

	_, _ = e.ScrapeOngoing()
	time.Sleep(300 * time.Millisecond)
	_, _ = e.ScrapeCompleted()
	time.Sleep(300 * time.Millisecond)

	store.AddLog("INFO", "🌐 Menghubungkan ke indeks A-Z Otakudesu.blog...")
	html, err := e.fetchHTML(e.baseURL + "/anime-list/")
	if err != nil {
		store.AddLog("ERROR", "❌ Gagal mengambil daftar anime A-Z Otakudesu: "+err.Error())
		return err
	}

	linkRegex := regexp.MustCompile(`<a\s+[^>]*href=["']https?://otakudesu\.[^/]+/anime/([^/"']+)/?["'][^>]*>(.*?)</a>`)
	matches := linkRegex.FindAllStringSubmatch(html, -1)

	if len(matches) == 0 {
		linkRegex = regexp.MustCompile(`<a\s+[^>]*href=["'](/anime/([^/"']+)/?)["'][^>]*>(.*?)</a>`)
		matches = linkRegex.FindAllStringSubmatch(html, -1)
	}

	store.AddLog("INFO", fmt.Sprintf("🎯 Total %d anime ditemukan di katalog A-Z Otakudesu! Memulai scraping...", len(matches)))

	cleanTagRegex := regexp.MustCompile(`<[^>]+>`)
	savedCount := 0

	for i, m := range matches {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping dihentikan oleh pengguna.")
			return nil
		default:
		}

		slug := strings.TrimSpace(m[1])
		rawTitle := strings.TrimSpace(m[2])
		cleanTitle := strings.TrimSpace(cleanTagRegex.ReplaceAllString(rawTitle, ""))
		if idx := strings.Index(cleanTitle, "<color"); idx != -1 {
			cleanTitle = strings.TrimSpace(cleanTitle[:idx])
		}
		if cleanTitle == "" {
			cleanTitle = slug
		}

		if existing, ok := store.Animes[slug]; ok && len(existing.Episodes) > 0 && existing.Img != "" {
			store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⚡ Sudah ada di database: %s (%d Episode)", i+1, len(matches), cleanTitle, len(existing.Episodes)))
			continue
		}

		store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⏳ Mengikis anime: %s...", i+1, len(matches), cleanTitle))

		detail, err := e.ScrapeAnimeDetail(slug)
		if err != nil || detail == nil || detail.Title == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		savedCount++
		store.AddLog("SUCCESS", fmt.Sprintf("[%d/%d] ✅ Tersimpan: %s (%d Episode)", i+1, len(matches), detail.Title, len(detail.Episodes)))

		if (i+1)%5 == 0 || i == len(matches)-1 {
			_ = store.FlushToDisk()
		}

		time.Sleep(80 * time.Millisecond)
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 Selesai! Database Nyamimo sekarang memiliki %d anime lengkap!", len(store.Animes)))
	return nil
}

// Scrape Detail Anime from Otakudesu (with fallback to Samehadaku)
func (e *ScraperEngine) ScrapeAnimeDetail(slug string) (*client.AnimeDetailData, error) {
	store := GetStore()

	if existing, ok := store.GetAnime(slug); ok && len(existing.Episodes) > 1 && existing.Img != "" {
		return existing, nil
	}

	targetURL := fmt.Sprintf("%s/anime/%s/", e.baseURL, slug)
	html, err := e.fetchHTML(targetURL)
	if err != nil || strings.Contains(html, "404 Not Found") || strings.Contains(html, "Halaman Tidak Ditemukan") {
		// Fallback to Samehadaku!
		return e.ScrapeSamehadakuAnimeDetail(slug)
	}

	titleRegex := regexp.MustCompile(`(?s)<div class="infozingle">.*?<span><b>Judul</b>:\s*([^<]+)</span>`)
	statusRegex := regexp.MustCompile(`<span><b>Status</b>:\s*([^<]+)</span>`)
	scoreRegex := regexp.MustCompile(`<span><b>Skor</b>:\s*([^<]+)</span>`)
	imgRegex := regexp.MustCompile(`(?s)<div\s+class=['"]fotoanime['"][^>]*>.*?<img[^>]+src=['"]([^'"]+)['"]`)
	synRegex := regexp.MustCompile(`(?s)<div class=['"]sinopc['"][^>]*>.*?<p>(.*?)</p>`)

	titleMatch := titleRegex.FindStringSubmatch(html)
	statusMatch := statusRegex.FindStringSubmatch(html)
	scoreMatch := scoreRegex.FindStringSubmatch(html)
	imgMatch := imgRegex.FindStringSubmatch(html)
	synMatch := synRegex.FindStringSubmatch(html)

	title := slug
	if len(titleMatch) > 1 {
		title = strings.TrimSpace(titleMatch[1])
	}
	status := "Completed"
	if len(statusMatch) > 1 {
		status = strings.TrimSpace(statusMatch[1])
	}
	score := "8.5"
	if len(scoreMatch) > 1 {
		score = strings.TrimSpace(scoreMatch[1])
	}
	img := ""
	if len(imgMatch) > 1 {
		img = strings.TrimSpace(imgMatch[1])
	}
	synopsis := fmt.Sprintf("Nonton streaming anime %s subtitle Indonesia gratis kualitas HD di Nyamimo.", title)
	if len(synMatch) > 1 {
		synopsis = strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(synMatch[1], ""))
	}

	epRegex := regexp.MustCompile(`<a\s+href=["'](https?://otakudesu\.[^/]+/episode/([^/"']+)/?)["'][^>]*>(.*?)</a>`)
	epMatches := epRegex.FindAllStringSubmatch(html, -1)

	if len(epMatches) == 0 {
		epRegex = regexp.MustCompile(`(?s)<li><span><a\s+href=["']([^"']+)["'][^>]*>(.*?)</a></span>`)
		epMatches = epRegex.FindAllStringSubmatch(html, -1)
	}

	var episodes []client.EpisodeRef
	for _, em := range epMatches {
		epURL := strings.TrimSpace(em[1])
		epTitle := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(em[len(em)-1], ""))
		epSlug := extractSlugFromURL(epURL)

		if epSlug != "" {
			epepNum := cleanEpisodeNumber(epTitle)
			if epepNum == "" {
				epepNum = fmt.Sprintf("%d", len(episodes)+1)
			}
			episodes = append(episodes, client.EpisodeRef{
				Title:     epTitle,
				DetailEps: epSlug,
				Episode:   epepNum,
			})
		}
	}

	if len(episodes) == 0 {
		// Try Samehadaku for richer episode lists!
		if samDetail, err := e.ScrapeSamehadakuAnimeDetail(slug); err == nil && samDetail != nil && len(samDetail.Episodes) > 0 {
			return samDetail, nil
		}
	}

	var genreNames []string
	genreRegex := regexp.MustCompile(`<a\s+href=["']https?://otakudesu\.[^/]+/genres/([^/"']+)/?["'][^>]*>(.*?)</a>`)
	genreMatches := genreRegex.FindAllStringSubmatch(html, -1)
	for _, gm := range genreMatches {
		genreNames = append(genreNames, strings.TrimSpace(gm[2]))
	}

	anime := &ScrapedAnime{
		Title:      title,
		Slug:       slug,
		Img:        img,
		Poster:     img,
		Score:      score,
		Synopsis:   synopsis,
		Status:     status,
		Type:       "TV Series",
		Episode:    fmt.Sprintf("%d Episode", len(episodes)),
		GenreNames: genreNames,
		Episodes:   episodes,
	}

	store.SaveAnime(anime, strings.EqualFold(status, "Ongoing"))
	detail := anime.ToAnimeDetailData()
	return &detail, nil
}

// ScrapeEpisodeDetail extracts playable video streams (DesuStream, Blogger, VidHide, Mega, etc.)
func (e *ScraperEngine) ScrapeEpisodeDetail(slug string) (*client.EpisodeDetailResponse, error) {
	store := GetStore()

	if cached, ok := store.GetEpisode(slug); ok && (len(cached.Videos) > 0 || cached.VideoURL != "") {
		return cached, nil
	}

	targetURL := fmt.Sprintf("%s/episode/%s/", e.baseURL, slug)
	html, err := e.fetchHTML(targetURL)
	if err != nil || strings.Contains(html, "404 Not Found") {
		// Fallback to Samehadaku!
		return e.ScrapeSamehadakuEpisodeDetail(slug)
	}

	titleRegex := regexp.MustCompile(`<h1[^>]*class="posttl"[^>]*>([^<]+)</h1>`)
	titleMatch := titleRegex.FindStringSubmatch(html)
	epTitle := slug
	if len(titleMatch) > 1 {
		epTitle = strings.TrimSpace(titleMatch[1])
	}
	epNum := cleanEpisodeNumber(epTitle)

	var defaultVideoURL string
	var videos []client.PlayerOption

	pembedRegex := regexp.MustCompile(`(?s)<div[^>]*(?:id=['"]pembed['"]|class=['"][^'"]*responsive-embed-stream[^'"]*['"])[^>]*>.*?<iframe[^>]+(?:src|data-src|data-litespeed-src)=['"]([^'"]+)['"]`)
	if m := pembedRegex.FindStringSubmatch(html); len(m) > 1 && strings.HasPrefix(m[1], "http") {
		streamURL := m[1]
		if !strings.Contains(streamURL, "maintenance") {
			defaultVideoURL = streamURL
			videos = append(videos, client.PlayerOption{
				ID:    "server-1",
				Title: "DesuStream HD (Utama)",
				Type:  "embed",
				Video: streamURL,
			})
		}
	} else {
		desuRegex := regexp.MustCompile(`(?i)<iframe[^>]+(?:src|data-src|data-litespeed-src)=["'](https?://[^"']*(?:desustream|playdesu|blogger|streamwish|vidhide|mega)[^"']*)["']`)
		if m := desuRegex.FindStringSubmatch(html); len(m) > 1 {
			streamURL := m[1]
			if !strings.Contains(streamURL, "maintenance") {
				defaultVideoURL = streamURL
				videos = append(videos, client.PlayerOption{
					ID:    "server-1",
					Title: "DesuStream HD (Utama)",
					Type:  "embed",
					Video: streamURL,
				})
			}
		}
	}

	actionRegex := regexp.MustCompile(`action:\s*["']([a-f0-9]{32})["']`)
	actionMatches := actionRegex.FindAllStringSubmatch(html, -1)

	var nonceAction, embedAction string
	if len(actionMatches) >= 2 {
		embedAction = actionMatches[0][1]
		nonceAction = actionMatches[1][1]
	}

	mirrorRegex := regexp.MustCompile(`(?s)<li[^>]*>\s*<a\s+[^>]*data-content="([^"]+)"[^>]*>([^<]+)</a>`)
	mirrorMatches := mirrorRegex.FindAllStringSubmatch(html, -1)

	var nonce string
	if nonceAction != "" {
		form := url.Values{}
		form.Set("action", nonceAction)
		ajaxReq, _ := http.NewRequest("POST", e.baseURL+"/wp-admin/admin-ajax.php", strings.NewReader(form.Encode()))
		ajaxReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		ajaxReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		ajaxReq.Header.Set("Referer", targetURL)
		ajaxReq.Header.Set("X-Requested-With", "XMLHttpRequest")

		if ajaxResp, err := e.client.Do(ajaxReq); err == nil {
			ajaxBody, _ := io.ReadAll(ajaxResp.Body)
			ajaxResp.Body.Close()
			var nonceRes struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(ajaxBody, &nonceRes)
			nonce = nonceRes.Data
		}
	}

	if nonce == "" && embedAction != "" {
		form := url.Values{}
		form.Set("action", embedAction)
		ajaxReq, _ := http.NewRequest("POST", e.baseURL+"/wp-admin/admin-ajax.php", strings.NewReader(form.Encode()))
		ajaxReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		ajaxReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		ajaxReq.Header.Set("Referer", targetURL)
		ajaxReq.Header.Set("X-Requested-With", "XMLHttpRequest")

		if ajaxResp, err := e.client.Do(ajaxReq); err == nil {
			ajaxBody, _ := io.ReadAll(ajaxResp.Body)
			ajaxResp.Body.Close()
			var nonceRes struct {
				Data string `json:"data"`
			}
			_ = json.Unmarshal(ajaxBody, &nonceRes)
			if nonceRes.Data != "" {
				nonce = nonceRes.Data
				nonceAction, embedAction = embedAction, nonceAction
			}
		}
	}

	for _, mm := range mirrorMatches {
		rawContent := mm[1]
		serverName := strings.TrimSpace(mm[2])

		decodedJson, err := base64.StdEncoding.DecodeString(rawContent)
		if err != nil {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(decodedJson, &payload); err == nil && nonce != "" && embedAction != "" {
			embedForm := url.Values{}
			embedForm.Set("action", embedAction)
			embedForm.Set("nonce", nonce)
			for k, v := range payload {
				embedForm.Set(k, fmt.Sprintf("%v", v))
			}

			eReq, _ := http.NewRequest("POST", e.baseURL+"/wp-admin/admin-ajax.php", strings.NewReader(embedForm.Encode()))
			eReq.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
			eReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			eReq.Header.Set("Referer", targetURL)
			eReq.Header.Set("X-Requested-With", "XMLHttpRequest")

			if eResp, err := e.client.Do(eReq); err == nil {
				eBody, _ := io.ReadAll(eResp.Body)
				eResp.Body.Close()

				var embedRes struct {
					Data string `json:"data"`
				}
				_ = json.Unmarshal(eBody, &embedRes)
				if embedRes.Data != "" {
					decodedIframe, err := base64.StdEncoding.DecodeString(embedRes.Data)
					if err == nil {
						iframeHTML := string(decodedIframe)
						srcRegex := regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)
						if srcMatch := srcRegex.FindStringSubmatch(iframeHTML); len(srcMatch) > 1 {
							streamURL := srcMatch[1]
							if !strings.Contains(streamURL, "maintenance") {
								quality := ""
								if qVal, ok := payload["q"].(string); ok && qVal != "" {
									quality = " " + qVal
								}

								playerTitle := fmt.Sprintf("%s%s", serverName, quality)
								videos = append(videos, client.PlayerOption{
									ID:    fmt.Sprintf("server-%d", len(videos)+1),
									Title: playerTitle,
									Type:  "embed",
									Video: streamURL,
								})

								if defaultVideoURL == "" {
									defaultVideoURL = streamURL
								}
							}
						}
					}
				}
			}
		}

		if len(videos) >= 8 {
			break
		}
	}

	if len(videos) == 0 {
		// Try Samehadaku
		if samEp, err := e.ScrapeSamehadakuEpisodeDetail(slug); err == nil && samEp != nil && (len(samEp.Videos) > 0 || samEp.VideoURL != "") {
			return samEp, nil
		}
	}

	if defaultVideoURL == "" && len(videos) > 0 {
		defaultVideoURL = videos[0].Video
	}

	// Download links extraction
	var downloads []client.DownloadFormat
	dlSecRegex := regexp.MustCompile(`(?s)<div class="download">(.*?)<div class="clear">`)
	if dlSecMatch := dlSecRegex.FindStringSubmatch(html); len(dlSecMatch) > 1 {
		rowRegex := regexp.MustCompile(`(?s)<li>\s*<h4>([^<]+)</h4>\s*(.*?)\s*</li>`)
		rows := rowRegex.FindAllStringSubmatch(dlSecMatch[1], -1)

		for _, row := range rows {
			formatTitle := strings.TrimSpace(row[1])
			linksHtml := row[2]

			linkTagRegex := regexp.MustCompile(`<a\s+href="([^"]+)"[^>]*>([^<]+)</a>`)
			linkMatches := linkTagRegex.FindAllStringSubmatch(linksHtml, -1)

			var links []client.DownloadLink
			for _, lm := range linkMatches {
				dURL := strings.TrimSpace(lm[1])
				dName := strings.TrimSpace(lm[2])
				if dURL != "" && dName != "" {
					links = append(links, client.DownloadLink{
						Title: dName,
						Link:  dURL,
					})

					if defaultVideoURL == "" && strings.Contains(dURL, "pixeldrain") {
						defaultVideoURL = dURL
					}
				}
			}

			if len(links) > 0 {
				downloads = append(downloads, client.DownloadFormat{
					Format: formatTitle,
					List: []client.DownloadResolution{
						{
							Resolution: formatTitle,
							Links:      links,
						},
					},
				})
			}
		}
	}

	// Extract Mega & embeddable links from downloads into streamable videos
	for _, df := range downloads {
		for _, dlRes := range df.List {
			resLabel := dlRes.Resolution
			if resLabel == "" {
				resLabel = df.Format
			}
			for _, link := range dlRes.Links {
				lURL := link.Link
				lName := strings.ToLower(link.Title + " " + df.Format + " " + dlRes.Resolution)
				if strings.Contains(lURL, "mega.nz") || strings.Contains(lURL, "mega.co.nz") || strings.Contains(lName, "mega") {
					embedMega := lURL
					if strings.Contains(embedMega, "/file/") {
						embedMega = strings.Replace(embedMega, "/file/", "/embed/", 1)
					} else if strings.Contains(embedMega, "/#!") {
						embedMega = strings.Replace(embedMega, "/#!", "/embed/", 1)
					} else if strings.Contains(embedMega, "/#") {
						embedMega = strings.Replace(embedMega, "/#", "/embed/", 1)
					}
					megaTitle := "Mega HD"
					if strings.Contains(resLabel, "1080") || strings.Contains(df.Format, "1080") {
						megaTitle = "Mega 1080p (Full HD)"
					} else if strings.Contains(resLabel, "720") || strings.Contains(df.Format, "720") {
						megaTitle = "Mega 720p (HD)"
					} else if strings.Contains(resLabel, "480") || strings.Contains(df.Format, "480") {
						megaTitle = "Mega 480p (SD)"
					} else if strings.Contains(resLabel, "360") || strings.Contains(df.Format, "360") {
						megaTitle = "Mega 360p (SD)"
					} else if resLabel != "" {
						megaTitle = fmt.Sprintf("Mega %s", resLabel)
					}

					videos = append(videos, client.PlayerOption{
						ID:    fmt.Sprintf("mega-%d", len(videos)+1),
						Title: megaTitle,
						Type:  "embed",
						Video: embedMega,
					})
				}
			}
		}
	}

	res := &client.EpisodeDetailResponse{
		Title:         epTitle,
		EpisodeNumber: epNum,
		VideoURL:      defaultVideoURL,
		Videos:        videos,
		Downloads:     downloads,
	}

	store.SaveEpisode(slug, res)
	return res, nil
}

// ==========================================
// 2. SAMEHADAKU SCRAPING METHODS (https://samehadaku.li)
// ==========================================

// Scrape Samehadaku Ongoing Anime with all episodes
func (e *ScraperEngine) ScrapeSamehadakuOngoing() ([]*ScrapedAnime, error) {
	store := GetStore()
	store.AddLog("INFO", "🌐 Menghubungkan ke sumber Samehadaku.li (Ongoing & Latest Releases)...")

	html, err := e.fetchHTML(e.samehadakuBaseURL + "/")
	if err != nil {
		store.AddLog("ERROR", "❌ Gagal menghubungi server Samehadaku: "+err.Error())
		return nil, err
	}

	// Match all anime cards from homepage latest release section
	itemRegex := regexp.MustCompile(`(?s)<article class="bs"[^>]*>.*?<a\s+href="https?://samehadaku\.[^/]+/([^/"]+)/"[^>]*title="([^"]+)"[^>]*>.*?<img[^>]+(?:data-src|src)="([^"]+)".*?<h2[^>]*>([^<]+)</h2>`)
	matches := itemRegex.FindAllStringSubmatch(html, -1)

	var results []*ScrapedAnime
	store.AddLog("INFO", fmt.Sprintf("📋 Ditemukan %d rilis anime di Samehadaku.li. Memproses seluruh episode & player...", len(matches)))

	cleanTagRegex := regexp.MustCompile(`<[^>]+>`)

	for idx, m := range matches {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping Samehadaku Ongoing dihentikan oleh pengguna.")
			return results, nil
		default:
		}

		epSlug := strings.TrimSpace(m[1])
		fullTitle := strings.TrimSpace(cleanTagRegex.ReplaceAllString(m[4], ""))
		if fullTitle == "" {
			fullTitle = strings.TrimSpace(m[2])
		}
		imgURL := strings.TrimSpace(m[3])

		animeSlug := extractSamehadakuAnimeSlugFromEp(epSlug, fullTitle)
		if animeSlug == "" {
			continue
		}

		store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⏳ Mengikis Samehadaku: %s...", idx+1, len(matches), fullTitle))

		detail, err := e.ScrapeSamehadakuAnimeDetail(animeSlug)
		if err != nil || detail == nil || detail.Title == "" {
			// Fallback with minimal data
			anime := &ScrapedAnime{
				Title:    cleanAnimeTitleSamehadaku(fullTitle),
				Slug:     animeSlug,
				Img:      imgURL,
				Poster:   imgURL,
				Score:    "8.7",
				Status:   "Ongoing",
				Type:     "TV",
				Episode:  cleanEpisodeNumber(fullTitle),
				Synopsis: fmt.Sprintf("Nonton streaming anime %s subtitle Indonesia gratis kualitas jernih full HD di Nyamimo.", fullTitle),
				Episodes: []client.EpisodeRef{
					{
						Title:     fullTitle,
						DetailEps: epSlug,
						Episode:   cleanEpisodeNumber(fullTitle),
					},
				},
			}
			store.SaveAnime(anime, true)
			results = append(results, anime)
		} else {
			anime := &ScrapedAnime{
				Title:      detail.Title,
				Slug:       animeSlug,
				Img:        detail.Img,
				Poster:     detail.Img,
				Score:      cleanScore(fmt.Sprintf("%v", detail.Score)),
				Status:     detail.Status,
				Type:       detail.Type,
				Episode:    fmt.Sprintf("%d Episode", len(detail.Episodes)),
				Synopsis:   detail.Synopsis,
				Episodes:   detail.Episodes,
				Downloads:  detail.Downloads,
			}
			for _, g := range detail.Genres {
				anime.GenreNames = append(anime.GenreNames, g.Title)
			}
			store.SaveAnime(anime, strings.EqualFold(detail.Status, "Ongoing"))
			results = append(results, anime)
		}

		store.AddLog("SUCCESS", fmt.Sprintf("[%d/%d] ✅ Tersimpan dari Samehadaku: %s", idx+1, len(matches), fullTitle))
		time.Sleep(40 * time.Millisecond)
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 Scraping Samehadaku selesai! Total %d anime tersimpan di database lokal.", len(results)))
	return results, nil
}

// Scrape Completed Anime from Samehadaku
func (e *ScraperEngine) ScrapeSamehadakuCompleted() ([]*ScrapedAnime, error) {
	store := GetStore()
	store.AddLog("INFO", "🌐 Menghubungkan ke sumber Samehadaku.li (Completed/Tamat)...")

	html, err := e.fetchHTML(e.samehadakuBaseURL + "/anime/?status=Completed&type=&order=update")
	if err != nil {
		store.AddLog("ERROR", "❌ Gagal menghubungi server Samehadaku: "+err.Error())
		return nil, err
	}

	itemRegex := regexp.MustCompile(`(?s)<article class="bs"[^>]*>.*?<a\s+href="https?://samehadaku\.[^/]+/anime/([^/"]+)/"[^>]*title="([^"]+)"[^>]*>.*?<img[^>]+(?:data-src|src)="([^"]+)"`)
	matches := itemRegex.FindAllStringSubmatch(html, -1)

	var results []*ScrapedAnime
	store.AddLog("INFO", fmt.Sprintf("📋 Ditemukan %d anime Tamat di Samehadaku. Memproses detail & seluruh episode...", len(matches)))

	for idx, m := range matches {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping Tamat Samehadaku dihentikan.")
			return results, nil
		default:
		}

		slug := strings.TrimSpace(m[1])
		title := strings.TrimSpace(m[2])

		store.AddLog("INFO", fmt.Sprintf("[%d/%d] ⏳ Mengikis Tamat: %s...", idx+1, len(matches), title))

		detail, err := e.ScrapeSamehadakuAnimeDetail(slug)
		if err != nil || detail == nil || detail.Title == "" {
			continue
		}

		anime := &ScrapedAnime{
			Title:      detail.Title,
			Slug:       slug,
			Img:        detail.Img,
			Poster:     detail.Img,
			Score:      cleanScore(fmt.Sprintf("%v", detail.Score)),
			Status:     "Completed",
			Type:       detail.Type,
			Episode:    fmt.Sprintf("%d Episode", len(detail.Episodes)),
			Synopsis:   detail.Synopsis,
			Episodes:   detail.Episodes,
			Downloads:  detail.Downloads,
		}
		for _, g := range detail.Genres {
			anime.GenreNames = append(anime.GenreNames, g.Title)
		}

		store.SaveAnime(anime, false)
		results = append(results, anime)
		store.AddLog("SUCCESS", fmt.Sprintf("[%d/%d] ✅ Tersimpan Tamat: %s (%d Episode)", idx+1, len(matches), detail.Title, len(detail.Episodes)))
		time.Sleep(40 * time.Millisecond)
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 Scraping Tamat Samehadaku selesai! Total %d anime tersimpan.", len(results)))
	return results, nil
}

// Scrape Complete A-Z Catalog from Samehadaku.li
func (e *ScraperEngine) ScrapeSamehadakuCatalogAZ() error {
	store := GetStore()
	store.AddLog("INFO", "🚀 MEMULAI SCRAPING SELURUH KATALOG SAMEHADAKU DARI A - Z...")

	letters := []string{".", "0-9", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}

	totalDiscovered := 0

	for _, letter := range letters {
		select {
		case <-store.stopChan:
			store.AddLog("WARN", "Scraping A-Z Samehadaku dihentikan oleh pengguna.")
			return nil
		default:
		}

		targetURL := fmt.Sprintf("%s/az-list/?show=%s", e.samehadakuBaseURL, url.QueryEscape(letter))
		store.AddLog("INFO", fmt.Sprintf("📖 Menjelajahi Abjad [%s] di Samehadaku.li...", letter))

		html, err := e.fetchHTML(targetURL)
		if err != nil {
			store.AddLog("WARN", fmt.Sprintf("⚠️ Gagal mengambil daftar abjad [%s]: %s", letter, err.Error()))
			continue
		}

		itemRegex := regexp.MustCompile(`(?s)<article class="bs"[^>]*>.*?<a\s+href="https?://samehadaku\.[^/]+/anime/([^/"]+)/"[^>]*title="([^"]+)"[^>]*>.*?<img[^>]+(?:data-src|src)="([^"]+)"`)
		matches := itemRegex.FindAllStringSubmatch(html, -1)

		if len(matches) == 0 {
			itemRegex = regexp.MustCompile(`<a class="series" href="https?://samehadaku\.[^/]+/anime/([^/"]+)/"[^>]*>(.*?)</a>`)
			matches = itemRegex.FindAllStringSubmatch(html, -1)
		}

		store.AddLog("INFO", fmt.Sprintf("🎯 Abjad [%s]: Ditemukan %d anime.", letter, len(matches)))
		totalDiscovered += len(matches)

		for i, m := range matches {
			select {
			case <-store.stopChan:
				store.AddLog("WARN", "Scraping A-Z Samehadaku dihentikan oleh pengguna.")
				return nil
			default:
			}

			slug := strings.TrimSpace(m[1])
			title := slug
			if len(m) > 2 {
				title = strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[2], ""))
			}

			// If already cached with full episodes, skip
			if existing, ok := store.Animes[slug]; ok && len(existing.Episodes) > 1 && existing.Img != "" {
				store.AddLog("INFO", fmt.Sprintf("[%s %d/%d] ⚡ Sudah ada di database: %s (%d Episode)", letter, i+1, len(matches), existing.Title, len(existing.Episodes)))
				continue
			}

			store.AddLog("INFO", fmt.Sprintf("[%s %d/%d] ⏳ Mengikis detail anime: %s...", letter, i+1, len(matches), title))

			detail, err := e.ScrapeSamehadakuAnimeDetail(slug)
			if err != nil || detail == nil || detail.Title == "" {
				time.Sleep(30 * time.Millisecond)
				continue
			}

			anime := &ScrapedAnime{
				Title:      detail.Title,
				Slug:       slug,
				Img:        detail.Img,
				Poster:     detail.Img,
				Score:      cleanScore(fmt.Sprintf("%v", detail.Score)),
				Status:     detail.Status,
				Type:       detail.Type,
				Episode:    fmt.Sprintf("%d Episode", len(detail.Episodes)),
				Synopsis:   detail.Synopsis,
				Episodes:   detail.Episodes,
				Downloads:  detail.Downloads,
			}
			for _, g := range detail.Genres {
				anime.GenreNames = append(anime.GenreNames, g.Title)
			}

			store.SaveAnime(anime, strings.EqualFold(detail.Status, "Ongoing"))
			store.AddLog("SUCCESS", fmt.Sprintf("[%s %d/%d] ✅ Tersimpan: %s (%d Episode)", letter, i+1, len(matches), detail.Title, len(detail.Episodes)))

			if (i+1)%5 == 0 {
				_ = store.FlushToDisk()
			}

			time.Sleep(50 * time.Millisecond)
		}
	}

	store.LastScraped = time.Now()
	_ = store.FlushToDisk()
	store.AddLog("SUCCESS", fmt.Sprintf("🎉 SELESAI SCRAPING SAMEHADAKU! Total %d anime di seluruh katalog sekarang tersedia di Nyamimo!", len(store.Animes)))
	return nil
}

// Scrape Anime Detail & All Episodes from Samehadaku.li
func (e *ScraperEngine) ScrapeSamehadakuAnimeDetail(slug string) (*client.AnimeDetailData, error) {
	store := GetStore()

	if existing, ok := store.GetAnime(slug); ok && len(existing.Episodes) > 1 && existing.Img != "" {
		return existing, nil
	}

	targetURL := fmt.Sprintf("%s/anime/%s/", e.samehadakuBaseURL, slug)
	html, err := e.fetchHTML(targetURL)
	if err != nil {
		return nil, err
	}

	// Extract Title
	titleRegex := regexp.MustCompile(`<h1 class="entry-title"[^>]*>([^<]+)</h1>`)
	titleMatch := titleRegex.FindStringSubmatch(html)
	title := slug
	if len(titleMatch) > 1 {
		title = strings.TrimSpace(titleMatch[1])
	}

	// Extract Poster Image
	imgRegex := regexp.MustCompile(`(?s)<div class="thumb"[^>]*>.*?<img[^>]+(?:data-src|src)="([^"]+)"`)
	imgMatch := imgRegex.FindStringSubmatch(html)
	img := ""
	if len(imgMatch) > 1 {
		img = strings.TrimSpace(imgMatch[1])
	}

	// Extract Rating
	scoreRegex := regexp.MustCompile(`(?i)(?:Rating|ratingValue["']\s*content=["'])\s*([0-9.]+)`)
	scoreMatch := scoreRegex.FindStringSubmatch(html)
	score := "8.7"
	if len(scoreMatch) > 1 {
		score = strings.TrimSpace(scoreMatch[1])
	}

	// Extract Status
	statusRegex := regexp.MustCompile(`(?i)<b>Status:</b>\s*([^<]+)`)
	statusMatch := statusRegex.FindStringSubmatch(html)
	status := "Completed"
	if len(statusMatch) > 1 {
		status = strings.TrimSpace(statusMatch[1])
	}

	// Extract Type
	typeRegex := regexp.MustCompile(`(?i)<b>Type:</b>\s*([^<]+)`)
	typeMatch := typeRegex.FindStringSubmatch(html)
	animeType := "TV"
	if len(typeMatch) > 1 {
		animeType = strings.TrimSpace(typeMatch[1])
	}

	// Extract Synopsis
	synRegex := regexp.MustCompile(`(?s)<div class="entry-content"[^>]*itemprop="description">(.*?)</div>`)
	synMatch := synRegex.FindStringSubmatch(html)
	synopsis := fmt.Sprintf("Nonton streaming anime %s subtitle Indonesia gratis kualitas HD di Nyamimo.", title)
	if len(synMatch) > 1 {
		cleanSyn := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(synMatch[1], "")
		cleanSyn = strings.TrimSpace(cleanSyn)
		if cleanSyn != "" {
			synopsis = cleanSyn
		}
	}

	// Extract Genres
	var genres []client.Genre
	var genreNames []string
	genreSectionRegex := regexp.MustCompile(`(?s)<div class="genxed">(.*?)</div>`)
	if gSecMatch := genreSectionRegex.FindStringSubmatch(html); len(gSecMatch) > 1 {
		gItemRegex := regexp.MustCompile(`<a\s+href="https?://samehadaku\.[^/]+/genres/([^/"]+)/"[^>]*>([^<]+)</a>`)
		gMatches := gItemRegex.FindAllStringSubmatch(gSecMatch[1], -1)
		for _, gm := range gMatches {
			gID := strings.TrimSpace(gm[1])
			gTitle := strings.TrimSpace(gm[2])
			if gTitle != "" {
				genres = append(genres, client.Genre{
					ID:    gID,
					Title: gTitle,
				})
				genreNames = append(genreNames, gTitle)
			}
		}
	}

	// Extract ALL Episodes
	var episodes []client.EpisodeRef
	epSectionRegex := regexp.MustCompile(`(?s)<div class="eplister"[^>]*>(.*?)</div>\s*</div>`)
	epSecHtml := html
	if epSecMatch := epSectionRegex.FindStringSubmatch(html); len(epSecMatch) > 1 {
		epSecHtml = epSecMatch[1]
	}

	epItemRegex := regexp.MustCompile(`(?s)<li[^>]*>\s*<a\s+href="https?://samehadaku\.[^/]+/([^/"]+)/"[^>]*>.*?<div class="epl-num">([^<]+)</div>.*?<div class="epl-title">([^<]+)</div>`)
	epMatches := epItemRegex.FindAllStringSubmatch(epSecHtml, -1)

	if len(epMatches) == 0 {
		epItemRegex = regexp.MustCompile(`(?s)<a\s+href="https?://samehadaku\.[^/]+/([^/"]+)/"[^>]*><div class="epl-num">([^<]+)</div><div class="epl-title">([^<]+)</div>`)
		epMatches = epItemRegex.FindAllStringSubmatch(html, -1)
	}

	for _, em := range epMatches {
		epSlug := strings.TrimSpace(em[1])
		epNum := strings.TrimSpace(em[2])
		epTitle := strings.TrimSpace(em[3])

		if epSlug != "" {
			if epNum == "" {
				epNum = cleanEpisodeNumber(epTitle)
			}
			episodes = append(episodes, client.EpisodeRef{
				Title:     epTitle,
				DetailEps: epSlug,
				Episode:   epNum,
			})
		}
	}

	// If no structured list, fallback to any episode link
	if len(episodes) == 0 {
		simpleEpRegex := regexp.MustCompile(`<a\s+href="https?://samehadaku\.[^/]+/([^/"]*(?:episode|eps|sub-indo)[^/"]*)/"`)
		simpleMatches := simpleEpRegex.FindAllStringSubmatch(html, -1)
		seen := make(map[string]bool)
		for _, sm := range simpleMatches {
			epSlug := strings.TrimSpace(sm[1])
			if epSlug != "" && !seen[epSlug] {
				seen[epSlug] = true
				num := cleanEpisodeNumber(epSlug)
				episodes = append(episodes, client.EpisodeRef{
					Title:     fmt.Sprintf("%s Episode %s", title, num),
					DetailEps: epSlug,
					Episode:   num,
				})
			}
		}
	}

	anime := &ScrapedAnime{
		Title:      title,
		Slug:       slug,
		Img:        img,
		Poster:     img,
		Score:      score,
		Synopsis:   synopsis,
		Status:     status,
		Type:       animeType,
		Episode:    fmt.Sprintf("%d Episode", len(episodes)),
		GenreNames: genreNames,
		Episodes:   episodes,
	}

	store.SaveAnime(anime, strings.EqualFold(status, "Ongoing"))
	detail := anime.ToAnimeDetailData()
	return &detail, nil
}

// Scrape Episode Detail & Player Video Streams from Samehadaku.li
func (e *ScraperEngine) ScrapeSamehadakuEpisodeDetail(slug string) (*client.EpisodeDetailResponse, error) {
	store := GetStore()

	if cached, ok := store.GetEpisode(slug); ok && (len(cached.Videos) > 0 || cached.VideoURL != "") {
		return cached, nil
	}

	targetURL := fmt.Sprintf("%s/%s/", e.samehadakuBaseURL, slug)
	html, err := e.fetchHTML(targetURL)
	if err != nil {
		return nil, err
	}

	// 1. Title & Episode number
	titleRegex := regexp.MustCompile(`<h1 class="entry-title"[^>]*>([^<]+)</h1>`)
	titleMatch := titleRegex.FindStringSubmatch(html)
	epTitle := slug
	if len(titleMatch) > 1 {
		epTitle = strings.TrimSpace(titleMatch[1])
	}
	epNum := cleanEpisodeNumber(epTitle)

	var defaultVideoURL string
	var videos []client.PlayerOption
	seenURL := make(map[string]bool)

	// 2. Primary embed in #pembed
	pembedRegex := regexp.MustCompile(`(?s)<div[^>]*class="player-embed"[^>]*id="pembed"[^>]*>.*?<iframe[^>]+(?:data-litespeed-src|data-src|src)="([^"]+)"`)
	if pm := pembedRegex.FindStringSubmatch(html); len(pm) > 1 && strings.HasPrefix(pm[1], "http") {
		streamURL := pm[1]
		if !seenURL[streamURL] {
			seenURL[streamURL] = true
			defaultVideoURL = streamURL
			serverTitle := "Blogger HD (Utama)"
			if strings.Contains(streamURL, "mega.nz") {
				serverTitle = "Mega Server HD"
			} else if strings.Contains(streamURL, "wibufile") {
				serverTitle = "Wibufile HD"
			} else if strings.Contains(streamURL, "vidhide") {
				serverTitle = "VidHide HD"
			}

			videos = append(videos, client.PlayerOption{
				ID:    "server-1",
				Title: serverTitle,
				Type:  "embed",
				Video: streamURL,
			})
		}
	}

	// 3. Decode base64 mirror options from <select class="mirror" name="mirror">
	mirrorSelectRegex := regexp.MustCompile(`(?s)<select[^>]*class="mirror"[^>]*>(.*?)</select>`)
	if mSelMatch := mirrorSelectRegex.FindStringSubmatch(html); len(mSelMatch) > 1 {
		optRegex := regexp.MustCompile(`(?s)<option\s+value="([^"]+)"[^>]*>([^<]+)</option>`)
		options := optRegex.FindAllStringSubmatch(mSelMatch[1], -1)

		for _, opt := range options {
			rawBase64 := strings.TrimSpace(opt[1])
			serverLabel := strings.TrimSpace(opt[2])

			if rawBase64 == "" || strings.EqualFold(serverLabel, "Select Video Server") {
				continue
			}

			decodedBytes, err := base64.StdEncoding.DecodeString(rawBase64)
			if err != nil {
				continue
			}

			decodedHTML := string(decodedBytes)
			srcRegex := regexp.MustCompile(`(?i)<iframe[^>]+(?:src|data-src|data-litespeed-src)=["']([^"']+)["']`)
			if srcMatch := srcRegex.FindStringSubmatch(decodedHTML); len(srcMatch) > 1 {
				streamURL := srcMatch[1]
				if streamURL != "" && !seenURL[streamURL] {
					seenURL[streamURL] = true

					formattedTitle := serverLabel
					if strings.EqualFold(serverLabel, "Video") || strings.Contains(streamURL, "blogger.com") {
						formattedTitle = "Blogger HD"
					} else if strings.Contains(streamURL, "mega.nz") {
						formattedTitle = "Mega HD"
					} else if strings.Contains(streamURL, "wibufile") {
						formattedTitle = "Wibufile HD"
					} else if strings.Contains(streamURL, "vidhide") {
						formattedTitle = "VidHide HD"
					}

					videos = append(videos, client.PlayerOption{
						ID:    fmt.Sprintf("server-%d", len(videos)+1),
						Title: formattedTitle,
						Type:  "embed",
						Video: streamURL,
					})

					if defaultVideoURL == "" {
						defaultVideoURL = streamURL
					}
				}
			}
		}
	}

	// 4. Extract Downloads
	var downloads []client.DownloadFormat
	dlSecRegex := regexp.MustCompile(`(?s)<div class="mctnx">(.*?)</div>\s*</div>\s*</div>`)
	if dlSecMatch := dlSecRegex.FindStringSubmatch(html); len(dlSecMatch) > 1 {
		soraRegex := regexp.MustCompile(`(?s)<div class="soraurlx">\s*<strong>([^<]+)</strong>\s*(.*?)\s*</div>`)
		soraMatches := soraRegex.FindAllStringSubmatch(dlSecMatch[1], -1)

		for _, sm := range soraMatches {
			resTitle := strings.TrimSpace(sm[1])
			linksHtml := sm[2]

			linkRegex := regexp.MustCompile(`<a\s+href="([^"]+)"[^>]*>([^<]+)</a>`)
			lMatches := linkRegex.FindAllStringSubmatch(linksHtml, -1)

			var links []client.DownloadLink
			for _, lm := range lMatches {
				links = append(links, client.DownloadLink{
					Title: strings.TrimSpace(lm[2]),
					Link:  strings.TrimSpace(lm[1]),
				})
			}

			if len(links) > 0 {
				downloads = append(downloads, client.DownloadFormat{
					Format: resTitle,
					List: []client.DownloadResolution{
						{
							Resolution: resTitle,
							Links:      links,
						},
					},
				})
			}
		}
	}

	// Single download link fallback (e.g. gofile / mirrored.to)
	if len(downloads) == 0 {
		singleDlRegex := regexp.MustCompile(`<a\s+href="([^"]+)"[^>]*aria-label="Download"`)
		if sm := singleDlRegex.FindStringSubmatch(html); len(sm) > 1 {
			downloads = append(downloads, client.DownloadFormat{
				Format: "720p HD",
				List: []client.DownloadResolution{
					{
						Resolution: "720p HD",
						Links: []client.DownloadLink{
							{
								Title: "Download Server",
								Link:  sm[1],
							},
						},
					},
				},
			})
		}
	}

	// Sort videos by quality & priority (Blogger > Mega > Wibufile > VidHide)
	if len(videos) > 1 {
		sort.SliceStable(videos, func(i, j int) bool {
			return getQualityRankHelper(videos[i].Title, videos[i].Video) > getQualityRankHelper(videos[j].Title, videos[j].Video)
		})
		defaultVideoURL = videos[0].Video
	}

	res := &client.EpisodeDetailResponse{
		Title:         epTitle,
		EpisodeNumber: epNum,
		VideoURL:      defaultVideoURL,
		Videos:        videos,
		Downloads:     downloads,
	}

	store.SaveEpisode(slug, res)
	return res, nil
}

// Helper utility functions
func extractSlugFromURL(rawURL string) string {
	rawURL = strings.TrimSuffix(rawURL, "/")
	parts := strings.Split(rawURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func extractSamehadakuAnimeSlugFromEp(epSlug, title string) string {
	epSlug = strings.ToLower(strings.TrimSpace(epSlug))
	epSlug = strings.TrimSuffix(epSlug, "/")
	
	// Strip "-episode-XYZ-subtitle-indonesia" or similar suffixes
	reSuffix := regexp.MustCompile(`-episode-\d+.*$|-sub-indo.*$|-subtitle-indonesia.*$`)
	clean := reSuffix.ReplaceAllString(epSlug, "")
	if clean != "" && clean != epSlug {
		return clean
	}

	// Fallback to title slugification
	t := strings.ToLower(title)
	if idx := strings.Index(t, "episode"); idx != -1 {
		t = t[:idx]
	}
	t = strings.ReplaceAll(t, ":", "")
	t = strings.ReplaceAll(t, "!", "")
	t = strings.ReplaceAll(t, "?", "")
	t = strings.ReplaceAll(t, ",", "")
	t = strings.ReplaceAll(t, "'", "")
	t = strings.TrimSpace(t)
	words := strings.Fields(t)
	if len(words) > 0 {
		return strings.Join(words, "-")
	}
	return epSlug
}

func cleanAnimeTitleSamehadaku(title string) string {
	t := regexp.MustCompile(`(?i)\s*(?:episode|\bep\b|\beps\b)\s*\d+.*$`).ReplaceAllString(title, "")
	t = regexp.MustCompile(`(?i)\s*(?:subtitle indonesia|sub indo).*$`).ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

func cleanEpisodeNumber(title string) string {
	numRegex := regexp.MustCompile(`(?i)(?:episode|ep|eps\.?)\s*(\d+)`)
	m := numRegex.FindStringSubmatch(title)
	if len(m) > 1 {
		return m[1]
	}
	digits := regexp.MustCompile(`\d+`).FindString(title)
	if digits != "" {
		return digits
	}
	return title
}

func cleanScore(score string) string {
	score = strings.TrimSpace(score)
	if score == "" || score == "0" || score == "N/A" {
		return "8.5"
	}
	return score
}

func getQualityRankHelper(title, videoURL string) int {
	t := strings.ToLower(title + " " + videoURL)
	base := 500

	if strings.Contains(t, "4k") || strings.Contains(t, "2160p") {
		base = 2160
	} else if strings.Contains(t, "1080p") || strings.Contains(t, "fullhd") || strings.Contains(t, "fhd") {
		base = 1080
	} else if strings.Contains(t, "720p") || strings.Contains(t, "hd") {
		base = 720
	} else if strings.Contains(t, "480p") || strings.Contains(t, "sd") {
		base = 480
	} else if strings.Contains(t, "360p") {
		base = 360
	}

	// ⭐ #1 Top Priority: Blogger / Blogspot
	if strings.Contains(t, "blogspot") || strings.Contains(t, "blogger") || strings.Contains(videoURL, "blogger.com") {
		base += 20000
	} else if strings.Contains(t, "mega") || strings.Contains(videoURL, "mega.nz") {
		// ⭐ #2 Priority: Mega Server
		base += 10000
	} else if strings.Contains(t, "wibufile") || strings.Contains(videoURL, "wibufile.com") {
		base += 5000
	} else if strings.Contains(t, "vidhide") || strings.Contains(t, "filedon") || strings.Contains(t, "solidfiles") {
		base += 2000
	}

	return base
}
