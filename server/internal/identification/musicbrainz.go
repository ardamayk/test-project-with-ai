package identification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// ErrRecordingNotFound means MusicBrainz has no Recording with the MBID, for
// example when AcoustID still links a merged or deleted recording.
var ErrRecordingNotFound = errors.New("MusicBrainz recording not found")

const (
	musicBrainzRecordingPath = "/ws/2/recording/"
	musicBrainzISRCPath      = "/ws/2/isrc/"
	// musicBrainzRecordingInc lists everything Recording Identification reads
	// in one call. "media" is deliberately absent: it multiplies the payload
	// (127 KB and 27 s versus 58 KB and 8 s for a recording on 40 releases)
	// and only the release's title, type, status and date are used.
	musicBrainzRecordingInc = "artist-credits+isrcs+releases+release-groups+genres"
)

// Recording is the MusicBrainz metadata Managed Import merges over the
// file's tags (ADR 0017): recording-level fields replace the tags, release
// level fields only complete empty ones.
type Recording struct {
	ID       string
	Title    string
	LengthMs int
	Artists  []Credit
	ISRCs    []string
	// Genres ordered by MusicBrainz vote count, most votes first.
	Genres   []string
	Releases []Release
}

// Credit is one artist credit as printed, with the artist's MBID.
type Credit struct {
	ID   string
	Name string
}

// Release is one MusicBrainz release containing the recording.
type Release struct {
	ID           string
	Title        string
	Status       string
	Date         string
	Year         int
	Country      string
	ReleaseGroup ReleaseGroup
	AlbumArtists []Credit
}

type ReleaseGroup struct {
	ID             string
	PrimaryType    string
	SecondaryTypes []string
}

// MusicBrainzClient reads the MusicBrainz web service (JSON).
type MusicBrainzClient struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string
}

func NewMusicBrainzClient(httpClient *http.Client, baseURL, userAgent string) *MusicBrainzClient {
	return &MusicBrainzClient{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/"), userAgent: userAgent}
}

type musicBrainzCredit struct {
	Name   string `json:"name"`
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type musicBrainzRecording struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Length       int                 `json:"length"`
	ArtistCredit []musicBrainzCredit `json:"artist-credit"`
	ISRCs        []string            `json:"isrcs"`
	Genres       []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"genres"`
	Releases []struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Status       string `json:"status"`
		Date         string `json:"date"`
		Country      string `json:"country"`
		ReleaseGroup struct {
			ID             string   `json:"id"`
			PrimaryType    string   `json:"primary-type"`
			SecondaryTypes []string `json:"secondary-types"`
		} `json:"release-group"`
		ArtistCredit []musicBrainzCredit `json:"artist-credit"`
	} `json:"releases"`
}

// Recording fetches one recording with its credits, ISRCs, genres and
// releases. A 404 maps to ErrRecordingNotFound; every other failure to
// ErrServiceUnavailable.
func (client *MusicBrainzClient) Recording(ctx context.Context, mbid string) (Recording, error) {
	endpoint := client.baseURL + musicBrainzRecordingPath + url.PathEscape(mbid) + "?fmt=json&inc=" + musicBrainzRecordingInc
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Recording{}, fmt.Errorf("%w: build MusicBrainz request: %w", ErrServiceUnavailable, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)

	var parsed musicBrainzRecording
	if err := doJSON(client.httpClient, request, "MusicBrainz", &parsed); err != nil {
		var status *httpStatusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return Recording{}, fmt.Errorf("%w: %s", ErrRecordingNotFound, mbid)
		}
		return Recording{}, err
	}
	return parsed.toRecording(), nil
}

type musicBrainzISRC struct {
	Recordings []struct {
		ID string `json:"id"`
	} `json:"recordings"`
}

// RecordingIDsByISRC lists the recordings MusicBrainz links to an ISRC, in
// MusicBrainz order. An unknown ISRC maps to ErrRecordingNotFound.
func (client *MusicBrainzClient) RecordingIDsByISRC(ctx context.Context, isrc string) ([]string, error) {
	endpoint := client.baseURL + musicBrainzISRCPath + url.PathEscape(isrc) + "?fmt=json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build MusicBrainz ISRC request: %w", ErrServiceUnavailable, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", client.userAgent)

	var parsed musicBrainzISRC
	if err := doJSON(client.httpClient, request, "MusicBrainz", &parsed); err != nil {
		var status *httpStatusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return nil, fmt.Errorf("%w: ISRC %s", ErrRecordingNotFound, isrc)
		}
		return nil, err
	}
	ids := make([]string, 0, len(parsed.Recordings))
	for _, recording := range parsed.Recordings {
		if recording.ID != "" {
			ids = append(ids, recording.ID)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: ISRC %s links no recording", ErrRecordingNotFound, isrc)
	}
	return ids, nil
}

func (parsed musicBrainzRecording) toRecording() Recording {
	recording := Recording{
		ID:       parsed.ID,
		Title:    parsed.Title,
		LengthMs: parsed.Length,
		Artists:  toCredits(parsed.ArtistCredit),
		ISRCs:    parsed.ISRCs,
		Genres:   make([]string, 0, len(parsed.Genres)),
		Releases: make([]Release, 0, len(parsed.Releases)),
	}
	genres := slices.Clone(parsed.Genres)
	slices.SortStableFunc(genres, func(first, second struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}) int {
		return second.Count - first.Count
	})
	for _, genre := range genres {
		recording.Genres = append(recording.Genres, genre.Name)
	}
	for _, release := range parsed.Releases {
		converted := Release{
			ID:      release.ID,
			Title:   release.Title,
			Status:  release.Status,
			Date:    release.Date,
			Year:    yearOf(release.Date),
			Country: release.Country,
			ReleaseGroup: ReleaseGroup{
				ID:             release.ReleaseGroup.ID,
				PrimaryType:    release.ReleaseGroup.PrimaryType,
				SecondaryTypes: release.ReleaseGroup.SecondaryTypes,
			},
			AlbumArtists: toCredits(release.ArtistCredit),
		}
		recording.Releases = append(recording.Releases, converted)
	}
	return recording
}

func toCredits(credits []musicBrainzCredit) []Credit {
	converted := make([]Credit, 0, len(credits))
	for _, credit := range credits {
		name := credit.Name
		if name == "" {
			name = credit.Artist.Name
		}
		converted = append(converted, Credit{ID: credit.Artist.ID, Name: name})
	}
	return converted
}

// yearOf reads the leading year of a MusicBrainz date ("2023-10-27", "2023").
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}
