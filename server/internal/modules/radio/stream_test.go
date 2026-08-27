package radio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestStreamProxyErrorsUseAPIErrorContract(t *testing.T) {
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("unexpected upstream request")
		return nil, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream?url=https%3A%2F%2Fpublic.example%2Farbitrary", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var response struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v; body = %q", err, recorder.Body.String())
	}
	if response.Error != "bad_request" || response.Code != "bad_request" || response.Message != "invalid HLS resource token" {
		t.Fatalf("response = %+v", response)
	}
}

func TestValidateStreamURLBlocksLocalTargets(t *testing.T) {
	for _, rawURL := range []string{
		"file:///tmp/song.mp3",
		"http://localhost:8000/stream",
		"http://localhost.:8000/stream",
		"http://127.0.0.1:8000/stream",
		"http://10.0.0.1/stream",
		"http://169.254.1.1/stream",
	} {
		if err := ValidateStreamURL(rawURL); err == nil {
			t.Fatalf("ValidateStreamURL(%q) = nil, want error", rawURL)
		}
	}
	if err := ValidateStreamURL("https://stream.radioparadise.com/mp3-192"); err != nil {
		t.Fatalf("public URL rejected: %v", err)
	}
}

func TestICYReaderStripsMetadataAndReportsNowPlaying(t *testing.T) {
	var updates []NowPlaying
	reader := NewICYReader(
		io.NopCloser(strings.NewReader("abcd\x02StreamTitle='Artist - Title';\x00\x00\x00")),
		4,
		func(now NowPlaying) {
			updates = append(updates, now)
		},
	)

	audio, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "abcd" {
		t.Fatalf("audio = %q", string(audio))
	}
	if len(updates) != 1 || updates[0].Artist != "Artist" || updates[0].Title != "Title" {
		t.Fatalf("updates = %+v", updates)
	}
}

func TestStreamProxyRewritesMixedMasterPlaylistReferences(t *testing.T) {
	var fetchedResources []string
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://radio.example/live/master.m3u8" {
			fetchedResources = append(fetchedResources, request.URL.String())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"audio/aac"}},
				Body:       io.NopCloser(strings.NewReader("resource")),
				Request:    request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
			Body: io.NopCloser(strings.NewReader("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aac\",URI=\"audio/index.m3u8\"\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=128000\n" +
				"low/index.m3u8\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=256000\n" +
				"https://cdn.example/high/index.m3u8\n")),
			Request: request,
		}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/live/master.m3u8"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/vnd.apple.mpegurl" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	resourceURLs := rewrittenResourceURLs(t, recorder.Body.String())
	if len(resourceURLs) != 3 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}
	for _, resourceURL := range resourceURLs {
		resourceRequest := httptest.NewRequest(http.MethodGet, resourceURL, nil)
		proxy.Stream(httptest.NewRecorder(), resourceRequest, Station{ID: "station-1", StreamURL: "https://radio.example/live/master.m3u8"})
	}
	wantFetched := []string{
		"https://radio.example/live/audio/index.m3u8",
		"https://radio.example/live/low/index.m3u8",
		"https://cdn.example/high/index.m3u8",
	}
	if strings.Join(fetchedResources, "|") != strings.Join(wantFetched, "|") {
		t.Fatalf("fetched resources = %q, want %q", fetchedResources, wantFetched)
	}
}

func TestStreamProxyRejectsDRMProtectedPlaylist(t *testing.T) {
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
			Body: io.NopCloser(strings.NewReader("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"https://keys.example/license\"\n" +
				"segment.ts\n")),
			Request: request,
		}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/live/index.m3u8"})

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusUnsupportedMediaType, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "DRM-protected HLS is unsupported") {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyPreservesSegmentRangeResponse(t *testing.T) {
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://radio.example/live/index.m3u8" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nhttps://cdn.example/live/segment.ts\n")),
				Request:    request,
			}, nil
		}
		if request.URL.String() != "https://cdn.example/live/segment.ts" {
			t.Fatalf("upstream URL = %q", request.URL.String())
		}
		if rangeHeader := request.Header.Get("Range"); rangeHeader != "bytes=4-7" {
			t.Fatalf("Range = %q", rangeHeader)
		}
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Type":  []string{"video/mp2t"},
				"Content-Range": []string{"bytes 4-7/12"},
				"Accept-Ranges": []string{"bytes"},
			},
			Body:    io.NopCloser(strings.NewReader("data")),
			Request: request,
		}, nil
	})}}
	manifestRequest := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	manifestRecorder := httptest.NewRecorder()
	station := Station{ID: "station-1", StreamURL: "https://radio.example/live/index.m3u8"}
	proxy.Stream(manifestRecorder, manifestRequest, station)
	resourceURLs := rewrittenResourceURLs(t, manifestRecorder.Body.String())
	if len(resourceURLs) != 1 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}
	request := httptest.NewRequest(http.MethodGet, resourceURLs[0], nil)
	request.Header.Set("Range", "bytes=4-7")
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, station)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusPartialContent)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "video/mp2t" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if contentRange := recorder.Header().Get("Content-Range"); contentRange != "bytes 4-7/12" {
		t.Fatalf("Content-Range = %q", contentRange)
	}
	if body := recorder.Body.String(); body != "data" {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyRewritesMediaPlaylistResources(t *testing.T) {
	var fetchedResources []string
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://media.example/nested/index.m3u8" {
			fetchedResources = append(fetchedResources, request.URL.String())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:       io.NopCloser(strings.NewReader("resource")),
				Request:    request,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/x-mpegurl; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=AES-128,URI=\"keys/live.key\"\n" +
				"#EXT-X-MAP:URI=\"init.mp4\"\n" +
				"#EXTINF:6,\n" +
				"segments/first.m4s\n")),
			Request: request,
		}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()
	station := Station{ID: "station-1", StreamURL: "https://media.example/nested/index.m3u8"}

	proxy.Stream(recorder, request, station)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	resourceURLs := rewrittenResourceURLs(t, recorder.Body.String())
	if len(resourceURLs) != 3 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}
	for _, resourceURL := range resourceURLs {
		proxy.Stream(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, resourceURL, nil), station)
	}
	wantFetched := []string{
		"https://media.example/nested/keys/live.key",
		"https://media.example/nested/init.mp4",
		"https://media.example/nested/segments/first.m4s",
	}
	if strings.Join(fetchedResources, "|") != strings.Join(wantFetched, "|") {
		t.Fatalf("fetched resources = %q, want %q", fetchedResources, wantFetched)
	}
}

func TestStreamProxyRejectsMalformedHLSPlaylists(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "missing header", body: "segment.ts\n"},
		{name: "malformed URL", body: "#EXTM3U\nhttp://example.com/%gh\n"},
		{name: "unterminated URI", body: "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
					Body:       io.NopCloser(strings.NewReader(testCase.body)),
					Request:    request,
				}, nil
			})}}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
			recorder := httptest.NewRecorder()

			proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusBadGateway, recorder.Body.String())
			}
			if body := recorder.Body.String(); !strings.Contains(body, "invalid HLS playlist") {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

func TestStreamProxyRejectsPrivateNestedResourceBeforeFetch(t *testing.T) {
	fetchCount := 0
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		fetchCount++
		return nil, errors.New("unexpected fetch")
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream?url=http%3A%2F%2F127.0.0.1%2Fsecret", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if fetchCount != 0 {
		t.Fatalf("upstream fetch count = %d", fetchCount)
	}
}

func TestStreamProxyRejectsCallerSuppliedPublicResourceURL(t *testing.T) {
	fetchCount := 0
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		fetchCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
			Body:       io.NopCloser(strings.NewReader("audio")),
			Request:    request,
		}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream?url=https%3A%2F%2Fpublic.example%2Farbitrary", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if fetchCount != 0 {
		t.Fatalf("upstream fetch count = %d", fetchCount)
	}
}

func TestStreamProxyRejectsOversizedHLSPlaylist(t *testing.T) {
	proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
			Body:       io.NopCloser(strings.NewReader("#EXTM3U\n#" + strings.Repeat("x", 2*1024*1024))),
			Request:    request,
		}, nil
	})}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "HLS playlist exceeds size limit") {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyResourceTokenIsBoundToStation(t *testing.T) {
	segmentFetchCount := 0
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://radio.example/live/index.m3u8" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n")),
				Request:    request,
			}, nil
		}
		segmentFetchCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp2t"}},
			Body:       io.NopCloser(strings.NewReader("segment")),
			Request:    request,
		}, nil
	})
	station := Station{ID: "station-1", StreamURL: "https://radio.example/live/index.m3u8"}
	manifestRequest := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	manifestRecorder := httptest.NewRecorder()
	proxy.Stream(manifestRecorder, manifestRequest, station)

	lines := strings.Split(strings.TrimSpace(manifestRecorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("manifest = %q", manifestRecorder.Body.String())
	}
	resourceURL, err := url.Parse(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	if token := resourceURL.Query().Get("resource"); token == "" {
		t.Fatalf("rewritten resource URL = %q", resourceURL.String())
	}
	if upstreamURL := resourceURL.Query().Get("url"); upstreamURL != "" {
		t.Fatalf("rewritten URL exposes caller-controlled url parameter = %q", upstreamURL)
	}
	tamperedURL := *resourceURL
	tamperedQuery := tamperedURL.Query()
	tamperedQuery.Set("resource", tamperedQuery.Get("resource")+"tampered")
	tamperedURL.RawQuery = tamperedQuery.Encode()
	tamperedRecorder := httptest.NewRecorder()
	proxy.Stream(tamperedRecorder, httptest.NewRequest(http.MethodGet, tamperedURL.String(), nil), station)
	if tamperedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("tampered token status = %d, want %d", tamperedRecorder.Code, http.StatusBadRequest)
	}
	if segmentFetchCount != 0 {
		t.Fatalf("segment fetch count after tampered token = %d", segmentFetchCount)
	}

	wrongStationRequest := httptest.NewRequest(http.MethodGet, resourceURL.String(), nil)
	wrongStationRecorder := httptest.NewRecorder()
	proxy.Stream(wrongStationRecorder, wrongStationRequest, Station{ID: "station-2", StreamURL: "https://other.example/live/index.m3u8"})
	if wrongStationRecorder.Code != http.StatusBadRequest {
		t.Fatalf("wrong station status = %d, want %d", wrongStationRecorder.Code, http.StatusBadRequest)
	}
	if segmentFetchCount != 0 {
		t.Fatalf("segment fetch count after wrong station = %d", segmentFetchCount)
	}

	segmentRequest := httptest.NewRequest(http.MethodGet, resourceURL.String(), nil)
	segmentRecorder := httptest.NewRecorder()
	proxy.Stream(segmentRecorder, segmentRequest, station)
	if segmentRecorder.Code != http.StatusOK || segmentRecorder.Body.String() != "segment" {
		t.Fatalf("segment status = %d, body = %q", segmentRecorder.Code, segmentRecorder.Body.String())
	}
	if segmentFetchCount != 1 {
		t.Fatalf("segment fetch count = %d", segmentFetchCount)
	}
}

func TestStreamProxyResourceTokenIsBoundToCatalogPreview(t *testing.T) {
	segmentFetchCount := 0
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://catalog.example/live/index.m3u8" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n")),
				Request:    request,
			}, nil
		}
		segmentFetchCount++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp2t"}}, Body: io.NopCloser(strings.NewReader("segment")), Request: request}, nil
	})
	preview := Station{ID: "preview:catalog-1", StreamURL: "https://catalog.example/live/index.m3u8"}
	manifestRecorder := httptest.NewRecorder()
	proxy.Stream(manifestRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/radio/preview/catalog-1/stream", nil), preview)
	resourceURLs := rewrittenResourceURLs(t, manifestRecorder.Body.String())
	if len(resourceURLs) != 1 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}

	wrongPreviewRecorder := httptest.NewRecorder()
	proxy.Stream(wrongPreviewRecorder, httptest.NewRequest(http.MethodGet, resourceURLs[0], nil), Station{ID: "preview:catalog-2", StreamURL: "https://catalog.example/other.m3u8"})
	if wrongPreviewRecorder.Code != http.StatusBadRequest {
		t.Fatalf("wrong preview status = %d, want %d", wrongPreviewRecorder.Code, http.StatusBadRequest)
	}
	if segmentFetchCount != 0 {
		t.Fatalf("segment fetch count after wrong preview = %d", segmentFetchCount)
	}

	segmentRecorder := httptest.NewRecorder()
	proxy.Stream(segmentRecorder, httptest.NewRequest(http.MethodGet, resourceURLs[0], nil), preview)
	if segmentRecorder.Code != http.StatusOK || segmentFetchCount != 1 {
		t.Fatalf("segment status = %d, fetch count = %d", segmentRecorder.Code, segmentFetchCount)
	}
}

func TestStreamProxyRejectsExpiredResourceToken(t *testing.T) {
	currentTime := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	resourceFetchCount := 0
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.now = func() time.Time { return currentTime }
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == "https://radio.example/index.m3u8" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n")),
				Request:    request,
			}, nil
		}
		resourceFetchCount++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("segment")), Request: request}, nil
	})
	station := Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"}
	manifestRecorder := httptest.NewRecorder()
	proxy.Stream(manifestRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil), station)
	resourceURLs := rewrittenResourceURLs(t, manifestRecorder.Body.String())
	if len(resourceURLs) != 1 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}

	currentTime = currentTime.Add(hlsResourceTokenTTL + time.Second)
	expiredRecorder := httptest.NewRecorder()
	proxy.Stream(expiredRecorder, httptest.NewRequest(http.MethodGet, resourceURLs[0], nil), station)
	if expiredRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expired token status = %d, want %d", expiredRecorder.Code, http.StatusBadRequest)
	}
	if resourceFetchCount != 0 {
		t.Fatalf("resource fetch count = %d", resourceFetchCount)
	}
}

func TestStreamProxyRejectsPrivateResourceReferencedByPlaylist(t *testing.T) {
	fetchCount := 0
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		fetchCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
			Body:       io.NopCloser(strings.NewReader("#EXTM3U\nhttp://127.0.0.1/segment.ts\n")),
			Request:    request,
		}, nil
	})
	recorder := httptest.NewRecorder()
	proxy.Stream(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil), Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if fetchCount != 1 {
		t.Fatalf("upstream fetch count = %d", fetchCount)
	}
}

func TestStreamProxyRejectsPublicHostnameResolvingToPrivateAddress(t *testing.T) {
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = newSecureStreamTransportWithResolver(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "resolved to blocked IP") {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyRejectsRedirectToPrivateTarget(t *testing.T) {
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://127.0.0.1/secret"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    request,
		}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if body := recorder.Body.String(); !strings.Contains(body, "unsafe stream URL") {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyResolvesPlaylistReferencesFromRedirectTarget(t *testing.T) {
	segmentFetchCount := 0
	proxy := NewStreamProxy(nil, NewNowPlayingCache())
	proxy.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "radio.example" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://cdn.example/live/index.m3u8"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    request,
			}, nil
		}
		if request.URL.String() == "https://cdn.example/live/index.m3u8" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n")),
				Request:    request,
			}, nil
		}
		segmentFetchCount++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp2t"}}, Body: io.NopCloser(strings.NewReader("segment")), Request: request}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
	recorder := httptest.NewRecorder()

	proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/start"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	resourceURLs := rewrittenResourceURLs(t, recorder.Body.String())
	if len(resourceURLs) != 1 {
		t.Fatalf("rewritten resource URLs = %q", resourceURLs)
	}
	proxy.Stream(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, resourceURLs[0], nil), Station{ID: "station-1", StreamURL: "https://radio.example/start"})
	if segmentFetchCount != 1 {
		t.Fatalf("segment fetch count = %d", segmentFetchCount)
	}
}

func rewrittenResourceURLs(t *testing.T, playlist string) []string {
	t.Helper()
	pattern := regexp.MustCompile(`/api/v1/radio/[^"\r\n]+\?resource=[^"\r\n]+`)
	return pattern.FindAllString(playlist, -1)
}

func TestStreamProxyReportsUpstreamFailures(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		fetchError error
	}{
		{name: "upstream status", statusCode: http.StatusServiceUnavailable},
		{name: "transport error", fetchError: errors.New("connection refused")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			proxy := &StreamProxy{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if testCase.fetchError != nil {
					return nil, testCase.fetchError
				}
				return &http.Response{
					StatusCode: testCase.statusCode,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("unavailable")),
					Request:    request,
				}, nil
			})}}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/radio/stations/station-1/stream", nil)
			recorder := httptest.NewRecorder()

			proxy.Stream(recorder, request, Station{ID: "station-1", StreamURL: "https://radio.example/index.m3u8"})

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d; body = %q", recorder.Code, http.StatusBadGateway, recorder.Body.String())
			}
		})
	}
}
