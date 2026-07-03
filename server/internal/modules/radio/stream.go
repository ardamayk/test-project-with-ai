package radio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nowPlayingStaleAfter = 60 * time.Second

var errUnsafeStreamURL = errors.New("unsafe stream URL")

func ValidateStreamURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: unsupported scheme", errUnsafeStreamURL)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" {
		return fmt.Errorf("%w: blocked host", errUnsafeStreamURL)
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("%w: blocked IP", errUnsafeStreamURL)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

type NowPlayingCache struct {
	mu    sync.RWMutex
	items map[string]NowPlaying
}

func NewNowPlayingCache() *NowPlayingCache {
	return &NowPlayingCache{items: make(map[string]NowPlaying)}
}

func (c *NowPlayingCache) Set(stationID string, now NowPlaying) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[stationID] = now
}

func (c *NowPlayingCache) Get(stationID string) (NowPlaying, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now, ok := c.items[stationID]
	if !ok {
		return NowPlaying{}, false
	}
	if now.UpdatedAt != nil && time.Since(*now.UpdatedAt) > nowPlayingStaleAfter {
		now.Stale = true
	}
	return now, true
}

type ICYReader struct {
	source    *bufio.Reader
	closer    io.Closer
	metaint   int
	remaining int
	onUpdate  func(NowPlaying)
}

func NewICYReader(source io.ReadCloser, metaint int, onUpdate func(NowPlaying)) *ICYReader {
	return &ICYReader{
		source:    bufio.NewReader(source),
		closer:    source,
		metaint:   metaint,
		remaining: metaint,
		onUpdate:  onUpdate,
	}
}

func (r *ICYReader) Read(p []byte) (int, error) {
	if r.metaint <= 0 {
		return r.source.Read(p)
	}
	if r.remaining == 0 {
		if err := r.readMetadata(); err != nil {
			return 0, err
		}
		r.remaining = r.metaint
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.source.Read(p)
	r.remaining -= n
	return n, err
}

func (r *ICYReader) Close() error {
	return r.closer.Close()
}

func (r *ICYReader) readMetadata() error {
	lengthByte, err := r.source.ReadByte()
	if err != nil {
		return err
	}
	length := int(lengthByte) * 16
	if length == 0 {
		return nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r.source, data); err != nil {
		return err
	}
	if now, ok := ParseICYMetadata(string(data)); ok && r.onUpdate != nil {
		r.onUpdate(now)
	}
	return nil
}

func ParseICYMetadata(raw string) (NowPlaying, bool) {
	raw = strings.Trim(raw, "\x00 ")
	if raw == "" {
		return NowPlaying{}, false
	}
	title := parseStreamTitle(raw)
	if title == "" {
		return NowPlaying{Raw: raw}, true
	}
	artist, trackTitle := splitArtistTitle(title)
	now := time.Now().UTC()
	return NowPlaying{
		Title:     trackTitle,
		Artist:    artist,
		Raw:       title,
		UpdatedAt: &now,
	}, true
}

func parseStreamTitle(raw string) string {
	const prefix = "StreamTitle='"
	start := strings.Index(raw, prefix)
	if start < 0 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(raw[start:], "';")
	if end < 0 {
		return strings.TrimSpace(raw[start:])
	}
	return strings.TrimSpace(raw[start : start+end])
}

func splitArtistTitle(raw string) (artist string, title string) {
	parts := strings.SplitN(raw, " - ", 2)
	if len(parts) != 2 {
		return "", raw
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

type StreamProxy struct {
	client *http.Client
	cache  *NowPlayingCache
	store  *Store
}

func NewStreamProxy(store *Store, cache *NowPlayingCache) *StreamProxy {
	client := &http.Client{
		Timeout: 0,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return ValidateStreamURL(req.URL.String())
		},
	}
	return &StreamProxy{client: client, cache: cache, store: store}
}

func (p *StreamProxy) Stream(w http.ResponseWriter, r *http.Request, station Station) {
	if err := ValidateStreamURL(station.StreamURL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, station.StreamURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Header.Set("Icy-MetaData", "1")
	res, err := p.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("stream status %d", res.StatusCode), http.StatusBadGateway)
		return
	}
	copyStreamHeaders(w, res.Header)
	w.WriteHeader(http.StatusOK)
	reader := io.ReadCloser(res.Body)
	if metaint, err := strconv.Atoi(res.Header.Get("icy-metaint")); err == nil && metaint > 0 {
		reader = NewICYReader(res.Body, metaint, func(now NowPlaying) {
			p.cache.Set(station.ID, now)
			_ = p.store.UpdateNowPlaying(r.Context(), station.ID, now)
		})
		defer reader.Close()
	}
	_, _ = io.Copy(w, reader)
}

func copyStreamHeaders(w http.ResponseWriter, headers http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "icy-name", "icy-genre", "icy-br", "icy-url"} {
		if value := headers.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
}
