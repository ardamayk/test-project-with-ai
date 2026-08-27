package radio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRadioBrowserSearchUsesRequestContext(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !errors.Is(request.Context().Err(), context.Canceled) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[]`)),
			}, nil
		}
		return nil, request.Context().Err()
	})}
	client := NewRadioBrowserClient("https://radio.test", httpClient)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://server.test/api/v1/radio/search", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Search(request)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want context.Canceled", err)
	}
}

func TestRadioBrowserCatalogUsesRequestContext(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	client := NewRadioBrowserClient("https://radio.test", httpClient)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Countries(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Countries() error = %v, want context.Canceled", err)
	}
}

func TestRadioBrowserClientBoundsUnconfiguredHTTPClient(t *testing.T) {
	var requestTimeout time.Duration
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, hasDeadline := request.Context().Deadline()
		if !hasDeadline {
			t.Fatal("outbound request has no deadline")
		}
		requestTimeout = time.Until(deadline)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}
	client := NewRadioBrowserClient("https://radio.test", httpClient)

	if _, err := client.SearchURL("/"); err != nil {
		t.Fatal(err)
	}
	if requestTimeout <= 9*time.Second || requestTimeout > 10*time.Second {
		t.Fatalf("outbound request timeout = %s, want approximately 10s", requestTimeout)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRadioBrowserSearchMapsResults(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/json/stations/search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("name") != "paradise" {
			t.Fatalf("name query = %q", r.URL.Query().Get("name"))
		}
		if r.URL.Query().Get("order") != "name" {
			t.Fatalf("order query = %q", r.URL.Query().Get("order"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[{
			"stationuuid": "abc",
			"name": "  Radio Paradise\t",
			"url_resolved": "https://stream.radioparadise.com/mp3-192",
			"homepage": "https://radioparadise.com",
			"favicon": "https://radioparadise.com/favicon.ico",
			"country": "United States",
			"language": "english",
			"tags": "rock,eclectic",
			"codec": "MP3",
			"bitrate": 192,
			"votes": 100,
			"lastcheckok": 1,
			"lastchecktime_iso8601": "2026-07-04T20:00:00Z",
			"lastcheckoktime_iso8601": "2026-07-04T20:00:00Z"
		}]`)),
		}, nil
	})}

	client := NewRadioBrowserClient("https://radio.test", httpClient)
	results, err := client.SearchURL("/?q=paradise")
	if err != nil {
		t.Fatal(err)
	}
	if results.Total != 1 {
		t.Fatalf("total = %d", results.Total)
	}
	item := results.Items[0]
	if item.StationUUID != "abc" || item.Name != "Radio Paradise" || item.StreamURL == "" {
		t.Fatalf("item = %+v", item)
	}
	if len(item.Tags) != 2 || item.Tags[0] != "rock" || item.Tags[1] != "eclectic" {
		t.Fatalf("tags = %#v", item.Tags)
	}
	if item.HealthStatus != "healthy" || item.LastCheckedAt == nil {
		t.Fatalf("health = %+v", item)
	}
}

func TestRadioBrowserSearchUsesCatalogFiltersAndPagination(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		if query.Get("limit") != "40" || query.Get("offset") != "80" {
			t.Fatalf("pagination query = %s", query.Encode())
		}
		if query.Get("country") != "Switzerland" || query.Get("tag") != "jazz" {
			t.Fatalf("filter query = %s", query.Encode())
		}
		if query.Get("codec") != "MP3" || query.Get("bitrateMin") != "128" {
			t.Fatalf("quality query = %s", query.Encode())
		}
		if query.Get("order") != "name" || query.Get("reverse") != "false" {
			t.Fatalf("sort query = %s", query.Encode())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}

	client := NewRadioBrowserClient("https://radio.test", httpClient)
	if _, err := client.SearchURL("/?country=Switzerland&tag=jazz&codec=MP3&minBitrate=128&limit=40&offset=80"); err != nil {
		t.Fatal(err)
	}
}

func TestRadioBrowserSearchTreatsAACAsCodecFamily(t *testing.T) {
	var codecs []string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		codec := r.URL.Query().Get("codec")
		codecs = append(codecs, codec)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[{
				"stationuuid": "` + codec + `",
				"name": "` + codec + ` Station",
				"url": "https://example.com/` + codec + `",
				"codec": "` + codec + `",
				"tags": ""
			}]`)),
		}, nil
	})}

	client := NewRadioBrowserClient("https://radio.test", httpClient)
	results, err := client.SearchURL("/?codec=AAC")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(codecs, ",") != "AAC,AAC+" {
		t.Fatalf("codecs = %#v", codecs)
	}
	if results.Total != 2 {
		t.Fatalf("total = %d", results.Total)
	}
}

func TestRadioBrowserCatalogOptions(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/json/countries":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`[{"name":"Switzerland","iso_3166_1":"CH","stationcount":12}]`)),
			}, nil
		case "/json/tags":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`[{"name":"jazz","stationcount":7}]`)),
			}, nil
		default:
			t.Fatalf("path = %s", r.URL.Path)
			return nil, nil
		}
	})}

	client := NewRadioBrowserClient("https://radio.test", httpClient)
	countries, err := client.Countries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countries.Total != 1 || countries.Items[0].Code != "CH" {
		t.Fatalf("countries = %+v", countries)
	}
	tags, err := client.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tags.Total != 1 || tags.Items[0].Name != "jazz" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestRadioBrowserSearchHandlesMalformedSourceURL(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("name") != "" {
			t.Fatalf("name query = %q", r.URL.Query().Get("name"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}

	client := NewRadioBrowserClient("https://radio.test", httpClient)
	results, err := client.SearchURL("%")
	if err != nil {
		t.Fatal(err)
	}
	if results.Total != 0 {
		t.Fatalf("total = %d", results.Total)
	}
}
