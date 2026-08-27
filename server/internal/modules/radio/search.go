package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const RadioBrowserSource = "radio-browser"

const radioBrowserRequestTimeout = 10 * time.Second

type SearchResult struct {
	StationUUID      string   `json:"stationUuid"`
	Name             string   `json:"name"`
	StreamURL        string   `json:"streamUrl"`
	HomepageURL      string   `json:"homepageUrl,omitempty"`
	FaviconURL       string   `json:"faviconUrl,omitempty"`
	Country          string   `json:"country,omitempty"`
	Language         string   `json:"language,omitempty"`
	Tags             []string `json:"tags"`
	Codec            string   `json:"codec,omitempty"`
	Bitrate          int      `json:"bitrate,omitempty"`
	Votes            int      `json:"votes,omitempty"`
	HealthStatus     string   `json:"healthStatus,omitempty"`
	LastCheckedAt    *string  `json:"lastCheckedAt,omitempty"`
	LastSuccessfulAt *string  `json:"lastSuccessfulAt,omitempty"`
}

type SearchResultList struct {
	Items []SearchResult `json:"items"`
	Total int            `json:"total"`
}

type CatalogOption struct {
	Name         string `json:"name"`
	Code         string `json:"code,omitempty"`
	StationCount int    `json:"stationCount,omitempty"`
}

type CatalogOptionList struct {
	Items []CatalogOption `json:"items"`
	Total int             `json:"total"`
}

type RadioBrowserClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRadioBrowserClient(baseURL string, httpClient *http.Client) *RadioBrowserClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	boundedClient := *httpClient
	if boundedClient.Timeout <= 0 || boundedClient.Timeout > radioBrowserRequestTimeout {
		boundedClient.Timeout = radioBrowserRequestTimeout
	}
	return &RadioBrowserClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: &boundedClient}
}

func (c *RadioBrowserClient) Search(r *http.Request) (SearchResultList, error) {
	return c.searchURL(r.Context(), r.URL.String())
}

func (c *RadioBrowserClient) SearchURL(rawURL string) (SearchResultList, error) {
	return c.searchURL(context.Background(), rawURL)
}

func (c *RadioBrowserClient) searchURL(ctx context.Context, rawURL string) (SearchResultList, error) {
	parsed, _ := url.Parse(rawURL)
	rawQuery := url.Values{}
	if parsed != nil {
		rawQuery = parsed.Query()
	}
	params := url.Values{}
	params.Set("limit", boundedQueryInt(rawQuery.Get("limit"), 40, 1, 100))
	params.Set("offset", boundedQueryInt(rawQuery.Get("offset"), 0, 0, 100000))
	params.Set("order", "name")
	params.Set("reverse", "false")
	query := rawQuery.Get("q")
	if strings.TrimSpace(query) != "" {
		params.Set("name", strings.TrimSpace(query))
	}
	for _, key := range []string{"country", "language", "tag", "codec"} {
		if value := rawQuery.Get(key); value != "" {
			params.Set(key, value)
		}
	}
	if value := rawQuery.Get("minBitrate"); value != "" {
		params.Set("bitrateMin", value)
	}
	if strings.EqualFold(rawQuery.Get("codec"), "AAC") || strings.EqualFold(rawQuery.Get("codecGroup"), "aac") {
		params.Del("codec")
		return c.searchAACFamily(ctx, params, rawQuery)
	}
	return c.fetchSearch(ctx, "/json/stations/search?"+params.Encode())
}

func (c *RadioBrowserClient) LookupStation(r *http.Request, stationUUID string) (SearchResult, error) {
	results, err := c.fetchSearch(r.Context(), "/json/stations/byuuid/"+url.PathEscape(stationUUID))
	if err != nil {
		return SearchResult{}, err
	}
	if len(results.Items) == 0 {
		return SearchResult{}, ErrNotFound
	}
	return results.Items[0], nil
}

func (c *RadioBrowserClient) fetchSearch(ctx context.Context, path string) (SearchResultList, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return SearchResultList{}, err
	}
	req.Header.Set("User-Agent", "navidrome-replacement/0.1")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return SearchResultList{}, err
	}
	defer func() {
		_ = res.Body.Close()
	}()
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

func (c *RadioBrowserClient) searchAACFamily(ctx context.Context, params url.Values, rawQuery url.Values) (SearchResultList, error) {
	limit, _ := strconv.Atoi(boundedQueryInt(rawQuery.Get("limit"), 40, 1, 100))
	offset, _ := strconv.Atoi(boundedQueryInt(rawQuery.Get("offset"), 0, 0, 100000))
	upstreamLimit := offset + limit
	allItems := make([]SearchResult, 0, upstreamLimit*2)
	for _, codec := range []string{"AAC", "AAC+"} {
		nextParams := cloneValues(params)
		nextParams.Set("codec", codec)
		nextParams.Set("limit", strconv.Itoa(upstreamLimit))
		nextParams.Set("offset", "0")
		results, err := c.fetchSearch(ctx, "/json/stations/search?"+nextParams.Encode())
		if err != nil {
			return SearchResultList{}, err
		}
		allItems = append(allItems, results.Items...)
	}
	sort.SliceStable(allItems, func(i, j int) bool {
		return strings.ToLower(allItems[i].Name) < strings.ToLower(allItems[j].Name)
	})
	if offset >= len(allItems) {
		return SearchResultList{Items: []SearchResult{}, Total: 0}, nil
	}
	end := offset + limit
	if end > len(allItems) {
		end = len(allItems)
	}
	return SearchResultList{Items: allItems[offset:end], Total: end - offset}, nil
}

func (c *RadioBrowserClient) Countries(ctx context.Context) (CatalogOptionList, error) {
	var raw []radioBrowserCatalogOption
	if err := c.fetchJSON(ctx, "/json/countries?order=name&reverse=false", &raw); err != nil {
		return CatalogOptionList{}, err
	}
	items := make([]CatalogOption, 0, len(raw))
	for _, option := range raw {
		items = append(items, option.toCountryOption())
	}
	return CatalogOptionList{Items: items, Total: len(items)}, nil
}

func (c *RadioBrowserClient) Tags(ctx context.Context) (CatalogOptionList, error) {
	var raw []radioBrowserCatalogOption
	if err := c.fetchJSON(ctx, "/json/tags?order=name&reverse=false", &raw); err != nil {
		return CatalogOptionList{}, err
	}
	items := make([]CatalogOption, 0, len(raw))
	for _, option := range raw {
		items = append(items, option.toTagOption())
	}
	return CatalogOptionList{Items: items, Total: len(items)}, nil
}

func (c *RadioBrowserClient) fetchJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "navidrome-replacement/0.1")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("radio browser status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func cloneValues(values url.Values) url.Values {
	next := url.Values{}
	for key, entries := range values {
		next[key] = append([]string(nil), entries...)
	}
	return next
}

type radioBrowserCatalogOption struct {
	Name         string `json:"name"`
	Code         string `json:"iso_3166_1"`
	StationCount any    `json:"stationcount"`
}

func (o radioBrowserCatalogOption) toCountryOption() CatalogOption {
	return CatalogOption{Name: o.Name, Code: strings.ToUpper(o.Code), StationCount: intValue(o.StationCount)}
}

func (o radioBrowserCatalogOption) toTagOption() CatalogOption {
	return CatalogOption{Name: o.Name, StationCount: intValue(o.StationCount)}
}

type radioBrowserStation struct {
	StationUUID            string `json:"stationuuid"`
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	URLResolved            string `json:"url_resolved"`
	Homepage               string `json:"homepage"`
	Favicon                string `json:"favicon"`
	Country                string `json:"country"`
	Language               string `json:"language"`
	Tags                   string `json:"tags"`
	Codec                  string `json:"codec"`
	Bitrate                any    `json:"bitrate"`
	Votes                  any    `json:"votes"`
	LastCheckOK            any    `json:"lastcheckok"`
	LastCheckTimeISO8601   string `json:"lastchecktime_iso8601"`
	LastCheckOKTimeISO8601 string `json:"lastcheckoktime_iso8601"`
}

func (s radioBrowserStation) toSearchResult() SearchResult {
	streamURL := s.URLResolved
	if streamURL == "" {
		streamURL = s.URL
	}
	return SearchResult{
		StationUUID:      s.StationUUID,
		Name:             strings.TrimSpace(s.Name),
		StreamURL:        streamURL,
		HomepageURL:      s.Homepage,
		FaviconURL:       s.Favicon,
		Country:          s.Country,
		Language:         s.Language,
		Tags:             splitTags(s.Tags),
		Codec:            s.Codec,
		Bitrate:          intValue(s.Bitrate),
		Votes:            intValue(s.Votes),
		HealthStatus:     healthStatus(s.LastCheckOK),
		LastCheckedAt:    stringPtr(s.LastCheckTimeISO8601),
		LastSuccessfulAt: stringPtr(s.LastCheckOKTimeISO8601),
	}
}

func boundedQueryInt(raw string, fallback int, minValue int, maxValue int) string {
	value, err := strconv.Atoi(raw)
	if err != nil {
		value = fallback
	}
	if value < minValue {
		value = minValue
	}
	if value > maxValue {
		value = maxValue
	}
	return strconv.Itoa(value)
}

func healthStatus(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "healthy"
		}
		return "broken"
	case float64:
		if v == 1 {
			return "healthy"
		}
		return "broken"
	case int:
		if v == 1 {
			return "healthy"
		}
		return "broken"
	case string:
		if v == "1" || strings.EqualFold(v, "true") {
			return "healthy"
		}
		if v == "0" || strings.EqualFold(v, "false") {
			return "broken"
		}
	}
	return "unknown"
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
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
