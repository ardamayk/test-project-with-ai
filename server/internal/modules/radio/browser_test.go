package radio

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

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
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[{
			"stationuuid": "abc",
			"name": "Radio Paradise",
			"url_resolved": "https://stream.radioparadise.com/mp3-192",
			"homepage": "https://radioparadise.com",
			"favicon": "https://radioparadise.com/favicon.ico",
			"country": "United States",
			"language": "english",
			"tags": "rock,eclectic",
			"codec": "MP3",
			"bitrate": 192,
			"votes": 100
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
