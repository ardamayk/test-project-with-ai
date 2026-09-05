package identification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrServiceUnavailable wraps every transport, HTTP, rate-limit or malformed
// response failure from AcoustID or MusicBrainz. Managed Import treats it as
// "identification could not run" and falls back to the file's tags.
var ErrServiceUnavailable = errors.New("identification service unavailable")

const (
	acoustIDLookupPath     = "/v2/lookup"
	acoustIDMeta           = "recordings sources"
	responseBodyLimitBytes = 4 << 20
	errorBodyPreviewBytes  = 200
)

// AcoustIDResult is one fingerprint match with the recordings AcoustID users
// have linked to it. Sources counts how many submissions linked a recording,
// which breaks ties between recordings sharing one fingerprint.
type AcoustIDResult struct {
	ID         string
	Score      float64
	Recordings []AcoustIDRecording
}

type AcoustIDRecording struct {
	ID      string
	Sources int
}

// AcoustIDClient calls the AcoustID lookup API with the application key.
type AcoustIDClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
}

func NewAcoustIDClient(httpClient *http.Client, baseURL, apiKey, userAgent string) *AcoustIDClient {
	return &AcoustIDClient{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, userAgent: userAgent}
}

type acoustIDResponse struct {
	Status string `json:"status"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID      string `json:"id"`
			Sources int    `json:"sources"`
		} `json:"recordings"`
	} `json:"results"`
}

// Lookup posts the fingerprint as a form so long fingerprints never hit URL
// length limits. Results keep AcoustID's order.
func (client *AcoustIDClient) Lookup(ctx context.Context, fingerprint Fingerprint) ([]AcoustIDResult, error) {
	form := url.Values{
		"client":      {client.apiKey},
		"duration":    {strconv.Itoa(int(fingerprint.DurationSeconds))},
		"fingerprint": {fingerprint.Value},
		"meta":        {acoustIDMeta},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+acoustIDLookupPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: build AcoustID request: %w", ErrServiceUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)

	var parsed acoustIDResponse
	if err := doJSON(client.httpClient, request, "AcoustID", &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "ok" {
		message := "unknown error"
		if parsed.Error != nil {
			message = fmt.Sprintf("code %d: %s", parsed.Error.Code, parsed.Error.Message)
		}
		return nil, fmt.Errorf("%w: AcoustID %s", ErrServiceUnavailable, message)
	}
	results := make([]AcoustIDResult, 0, len(parsed.Results))
	for _, result := range parsed.Results {
		recordings := make([]AcoustIDRecording, 0, len(result.Recordings))
		for _, recording := range result.Recordings {
			recordings = append(recordings, AcoustIDRecording{ID: recording.ID, Sources: recording.Sources})
		}
		results = append(results, AcoustIDResult{ID: result.ID, Score: result.Score, Recordings: recordings})
	}
	return results, nil
}

// doJSON performs the request and decodes a JSON body, mapping every failure
// onto ErrServiceUnavailable with the service name for the Preview reason.
func doJSON(httpClient *http.Client, request *http.Request, service string, target any) error {
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s request: %w", ErrServiceUnavailable, service, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, responseBodyLimitBytes))
	if err != nil {
		return fmt.Errorf("%w: read %s response: %w", ErrServiceUnavailable, service, err)
	}
	if response.StatusCode != http.StatusOK {
		preview := string(body)
		if len(preview) > errorBodyPreviewBytes {
			preview = preview[:errorBodyPreviewBytes]
		}
		return fmt.Errorf("%w: %w", ErrServiceUnavailable, &httpStatusError{service: service, code: response.StatusCode, preview: strings.TrimSpace(preview)})
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("%w: decode %s response: %w", ErrServiceUnavailable, service, err)
	}
	return nil
}

// httpStatusError carries the status code so callers can distinguish a
// definitive "not found" from a transient failure.
type httpStatusError struct {
	service string
	code    int
	preview string
}

func (err *httpStatusError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d: %s", err.service, err.code, err.preview)
}
