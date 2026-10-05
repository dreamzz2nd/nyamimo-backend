package debrid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ToshoSearchResult struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Link      string `json:"link"`
	InfoHash  string `json:"info_hash"`
	MagnetURI string `json:"magnet_uri"`
	Seeders   int    `json:"seeders"`
	TotalSize int64  `json:"total_size"`
}

type ToshoClient struct {
	httpClient *http.Client
}

func NewToshoClient() *ToshoClient {
	return &ToshoClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SearchAnimeEpisode searches AnimeTosho for a specific anime title and episode
func (c *ToshoClient) SearchAnimeEpisode(animeTitle, epNum string) ([]ToshoSearchResult, error) {
	cleanTitle := cleanTitleForSearch(animeTitle)
	epInt, _ := strconv.Atoi(strings.TrimSpace(epNum))
	formattedEp := fmt.Sprintf("%02d", epInt)
	if epInt == 0 {
		formattedEp = epNum
	}

	queries := []string{
		fmt.Sprintf("%s %s 1080p", cleanTitle, formattedEp),
		fmt.Sprintf("%s %s", cleanTitle, formattedEp),
		fmt.Sprintf("%s %s 720p", cleanTitle, formattedEp),
		fmt.Sprintf("%s episode %s", cleanTitle, epNum),
	}

	var allResults []ToshoSearchResult
	seenHashes := make(map[string]bool)

	for _, q := range queries {
		endpoint := fmt.Sprintf("https://feed.animetosho.org/json?q=%s", url.QueryEscape(q))
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 NyamimoServer/1.0")

		resp, err := c.httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var results []ToshoSearchResult
		if err := json.Unmarshal(body, &results); err == nil && len(results) > 0 {
			for _, r := range results {
				if r.InfoHash != "" && !seenHashes[r.InfoHash] {
					seenHashes[r.InfoHash] = true
					allResults = append(allResults, r)
				}
			}
			if len(allResults) >= 5 {
				break
			}
		}
	}

	// Sort by seeders descending
	sort.SliceStable(allResults, func(i, j int) bool {
		return allResults[i].Seeders > allResults[j].Seeders
	})

	return allResults, nil
}

func cleanTitleForSearch(t string) string {
	t = regexp.MustCompile(`(?i)\b(?:Sub\s*Indo|Subtitle\s*Indonesia|Season\s*\d+|S\d+|TV)\b`).ReplaceAllString(t, "")
	t = regexp.MustCompile(`[^a-zA-Z0-9\s]`).ReplaceAllString(t, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(t, " "))
}
