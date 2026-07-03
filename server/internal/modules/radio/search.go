package radio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const RadioBrowserSource = "radio-browser"

type SearchResult struct {
	StationUUID string   `json:"stationUuid"`
	Name        string   `json:"name"`
	StreamURL   string   `json:"streamUrl"`
	HomepageURL string   `json:"homepageUrl,omitempty"`
	FaviconURL  string   `json:"faviconUrl,omitempty"`
	Country     string   `json:"country,omitempty"`
	Language    string   `json:"language,omitempty"`
	Tags        []string `json:"tags"`
	Codec       string   `json:"codec,omitempty"`
	Bitrate     int      `json:"bitrate,omitempty"`
	Votes       int      `json:"votes,omitempty"`
}

type SearchResultList struct {
	Items []SearchResult `json:"items"`
	Total int            `json:"total"`
}

type RadioBrowserClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRadioBrowserClient(baseURL string, httpClient *http.Client) *RadioBrowserClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &RadioBrowserClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *RadioBrowserClient) Search(r *http.Request) (SearchResultList, error) {
	return c.SearchURL(r.URL.String())
}

func (c *RadioBrowserClient) SearchURL(rawURL string) (SearchResultList, error) {
	query := ""
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed != nil {
		query = parsed.Query().Get("q")
	}
	rawQuery := url.Values{}
	if parsed != nil {
		rawQuery = parsed.Query()
	}
	params := url.Values{}
	params.Set("hidebroken", "true")
	params.Set("limit", "25")
	params.Set("order", "votes")
	params.Set("reverse", "true")
	if strings.TrimSpace(query) != "" {
		params.Set("name", strings.TrimSpace(query))
	}
	for _, key := range []string{"country", "language", "tag"} {
		if value := rawQuery.Get(key); value != "" {
			params.Set(key, value)
		}
	}
	return c.fetchSearch("/json/stations/search?" + params.Encode())
}

func (c *RadioBrowserClient) LookupStation(r *http.Request, stationUUID string) (SearchResult, error) {
	results, err := c.fetchSearch("/json/stations/byuuid/" + url.PathEscape(stationUUID))
	if err != nil {
		return SearchResult{}, err
	}
	if len(results.Items) == 0 {
		return SearchResult{}, ErrNotFound
	}
	return results.Items[0], nil
}

func (c *RadioBrowserClient) fetchSearch(path string) (SearchResultList, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return SearchResultList{}, err
	}
	req.Header.Set("User-Agent", "navidrome-replacement/0.1")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return SearchResultList{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return SearchResultList{}, fmt.Errorf("radio browser status %d", res.StatusCode)
	}
	var raw []radioBrowserStation
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return SearchResultList{}, err
	}
	items := make([]SearchResult, 0, len(raw))
	for _, station := range raw {
		items = append(items, station.toSearchResult())
	}
	return SearchResultList{Items: items, Total: len(items)}, nil
}

type radioBrowserStation struct {
	StationUUID string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	URLResolved string `json:"url_resolved"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Country     string `json:"country"`
	Language    string `json:"language"`
	Tags        string `json:"tags"`
	Codec       string `json:"codec"`
	Bitrate     any    `json:"bitrate"`
	Votes       any    `json:"votes"`
}

func (s radioBrowserStation) toSearchResult() SearchResult {
	streamURL := s.URLResolved
	if streamURL == "" {
		streamURL = s.URL
	}
	return SearchResult{
		StationUUID: s.StationUUID,
		Name:        s.Name,
		StreamURL:   streamURL,
		HomepageURL: s.Homepage,
		FaviconURL:  s.Favicon,
		Country:     s.Country,
		Language:    s.Language,
		Tags:        splitTags(s.Tags),
		Codec:       s.Codec,
		Bitrate:     intValue(s.Bitrate),
		Votes:       intValue(s.Votes),
	}
}

func splitTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}
