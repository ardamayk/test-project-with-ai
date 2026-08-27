package radio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ManualSource = "manual"

var ErrNotFound = errors.New("radio station not found")

type NowPlaying struct {
	Title     string     `json:"title,omitempty"`
	Artist    string     `json:"artist,omitempty"`
	Raw       string     `json:"raw,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Stale     bool       `json:"stale"`
}

type Station struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	StreamURL      string      `json:"streamUrl"`
	HomepageURL    string      `json:"homepageUrl,omitempty"`
	FaviconURL     string      `json:"faviconUrl,omitempty"`
	Country        string      `json:"country,omitempty"`
	Language       string      `json:"language,omitempty"`
	Tags           []string    `json:"tags"`
	Codec          string      `json:"codec,omitempty"`
	Bitrate        int         `json:"bitrate,omitempty"`
	Source         string      `json:"source"`
	ExternalID     string      `json:"externalId,omitempty"`
	IsFavorite     bool        `json:"isFavorite"`
	Position       int         `json:"position"`
	LastNowPlaying *NowPlaying `json:"lastNowPlaying,omitempty"`
}

type StationList struct {
	Items []Station `json:"items"`
	Total int       `json:"total"`
}

type StationCreate struct {
	Name        string   `json:"name"`
	StreamURL   string   `json:"streamUrl"`
	HomepageURL string   `json:"homepageUrl,omitempty"`
	FaviconURL  string   `json:"faviconUrl,omitempty"`
	Country     string   `json:"country,omitempty"`
	Language    string   `json:"language,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Codec       string   `json:"codec,omitempty"`
	Bitrate     int      `json:"bitrate,omitempty"`
	Source      string   `json:"source,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	IsFavorite  bool     `json:"isFavorite"`
}

type StationPatch struct {
	Name        *string  `json:"name,omitempty"`
	StreamURL   *string  `json:"streamUrl,omitempty"`
	HomepageURL *string  `json:"homepageUrl,omitempty"`
	FaviconURL  *string  `json:"faviconUrl,omitempty"`
	Country     *string  `json:"country,omitempty"`
	Language    *string  `json:"language,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Codec       *string  `json:"codec,omitempty"`
	Bitrate     *int     `json:"bitrate,omitempty"`
	IsFavorite  *bool    `json:"isFavorite,omitempty"`
	Position    *int     `json:"position,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) ListStations(ctx context.Context, userID string) (StationList, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, stream_url, homepage_url, favicon_url, country, language, tags, codec, bitrate,
			source, external_id, is_favorite, position, last_now_playing_title, last_now_playing_artist,
			last_now_playing_raw, last_now_playing_updated_at
		FROM radio_stations
		WHERE user_id = ?
		ORDER BY is_favorite DESC, position ASC, name COLLATE NOCASE`, userID)
	if err != nil {
		return StationList{}, err
	}
	defer func() {
		_ = rows.Close()
	}()

	items := []Station{}
	for rows.Next() {
		station, err := scanStation(rows)
		if err != nil {
			return StationList{}, err
		}
		items = append(items, station)
	}
	return StationList{Items: items, Total: len(items)}, rows.Err()
}

func (s *Store) GetStation(ctx context.Context, userID, stationID string) (Station, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, stream_url, homepage_url, favicon_url, country, language, tags, codec, bitrate,
			source, external_id, is_favorite, position, last_now_playing_title, last_now_playing_artist,
			last_now_playing_raw, last_now_playing_updated_at
		FROM radio_stations
		WHERE user_id = ? AND id = ?`, userID, stationID)
	station, err := scanStation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Station{}, ErrNotFound
	}
	return station, err
}

func (s *Store) CreateStation(ctx context.Context, userID string, input StationCreate) (Station, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.StreamURL = strings.TrimSpace(input.StreamURL)
	input.HomepageURL = strings.TrimSpace(input.HomepageURL)
	input.FaviconURL = strings.TrimSpace(input.FaviconURL)
	if input.Name == "" {
		return Station{}, fmt.Errorf("station name is required")
	}
	if input.StreamURL == "" {
		return Station{}, fmt.Errorf("streamUrl is required")
	}
	if err := validateStationURLs(input.StreamURL, input.HomepageURL, input.FaviconURL); err != nil {
		return Station{}, err
	}
	if input.Source == "" {
		input.Source = ManualSource
	}
	position, err := s.nextPosition(ctx, userID)
	if err != nil {
		return Station{}, err
	}
	id := uuid.NewString()
	tags, err := encodeTags(input.Tags)
	if err != nil {
		return Station{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO radio_stations (
			id, user_id, name, stream_url, homepage_url, favicon_url, country, language, tags, codec, bitrate,
			source, external_id, is_favorite, position
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, input.Name, input.StreamURL, input.HomepageURL,
		input.FaviconURL, strings.TrimSpace(input.Country), strings.TrimSpace(input.Language),
		tags, strings.TrimSpace(input.Codec), input.Bitrate, strings.TrimSpace(input.Source),
		strings.TrimSpace(input.ExternalID), boolInt(input.IsFavorite), position,
	)
	if err != nil {
		return Station{}, err
	}
	return s.GetStation(ctx, userID, id)
}

func (s *Store) ImportStation(ctx context.Context, userID string, result SearchResult) (Station, error) {
	input := StationCreate{
		Name:        result.Name,
		StreamURL:   result.StreamURL,
		HomepageURL: result.HomepageURL,
		FaviconURL:  result.FaviconURL,
		Country:     result.Country,
		Language:    result.Language,
		Tags:        result.Tags,
		Codec:       result.Codec,
		Bitrate:     result.Bitrate,
		Source:      RadioBrowserSource,
		ExternalID:  result.StationUUID,
	}
	if existing, err := s.getStationByExternalID(ctx, userID, input.Source, input.ExternalID); err == nil {
		return s.UpdateStation(ctx, userID, existing.ID, StationPatch{
			Name:        &input.Name,
			StreamURL:   &input.StreamURL,
			HomepageURL: &input.HomepageURL,
			FaviconURL:  &input.FaviconURL,
			Country:     &input.Country,
			Language:    &input.Language,
			Tags:        input.Tags,
			Codec:       &input.Codec,
			Bitrate:     &input.Bitrate,
		})
	} else if !errors.Is(err, ErrNotFound) {
		return Station{}, err
	}
	return s.CreateStation(ctx, userID, input)
}

func (s *Store) UpdateStation(ctx context.Context, userID, stationID string, patch StationPatch) (Station, error) {
	current, err := s.GetStation(ctx, userID, stationID)
	if err != nil {
		return Station{}, err
	}
	if patch.Name != nil {
		current.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.StreamURL != nil {
		current.StreamURL = strings.TrimSpace(*patch.StreamURL)
	}
	if patch.HomepageURL != nil {
		current.HomepageURL = strings.TrimSpace(*patch.HomepageURL)
	}
	if patch.FaviconURL != nil {
		current.FaviconURL = strings.TrimSpace(*patch.FaviconURL)
	}
	if patch.Country != nil {
		current.Country = strings.TrimSpace(*patch.Country)
	}
	if patch.Language != nil {
		current.Language = strings.TrimSpace(*patch.Language)
	}
	if patch.Tags != nil {
		current.Tags = patch.Tags
	}
	if patch.Codec != nil {
		current.Codec = strings.TrimSpace(*patch.Codec)
	}
	if patch.Bitrate != nil {
		current.Bitrate = *patch.Bitrate
	}
	if patch.IsFavorite != nil {
		current.IsFavorite = *patch.IsFavorite
	}
	if patch.Position != nil {
		current.Position = *patch.Position
	}
	if current.Name == "" {
		return Station{}, fmt.Errorf("station name is required")
	}
	if current.StreamURL == "" {
		return Station{}, fmt.Errorf("streamUrl is required")
	}
	if validationErr := validateStationURLs(current.StreamURL, current.HomepageURL, current.FaviconURL); validationErr != nil {
		return Station{}, validationErr
	}
	tags, err := encodeTags(current.Tags)
	if err != nil {
		return Station{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE radio_stations
		SET name = ?, stream_url = ?, homepage_url = ?, favicon_url = ?, country = ?, language = ?,
			tags = ?, codec = ?, bitrate = ?, is_favorite = ?, position = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND id = ?`,
		current.Name, current.StreamURL, current.HomepageURL, current.FaviconURL, current.Country,
		current.Language, tags, current.Codec, current.Bitrate, boolInt(current.IsFavorite), current.Position,
		userID, stationID,
	)
	if err != nil {
		return Station{}, err
	}
	return s.GetStation(ctx, userID, stationID)
}

func validateStationURLs(streamURL, homepageURL, faviconURL string) error {
	if err := ValidateStreamURL(streamURL); err != nil {
		return fmt.Errorf("invalid streamUrl: %w", err)
	}
	if err := validateOptionalHTTPURL("homepageUrl", homepageURL); err != nil {
		return err
	}
	return validateOptionalHTTPURL("faviconUrl", faviconURL)
}

func validateOptionalHTTPURL(field, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return fmt.Errorf("invalid %s: must be an absolute HTTP(S) URI", field)
	}
	return nil
}

func (s *Store) DeleteStation(ctx context.Context, userID, stationID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM radio_stations WHERE user_id = ? AND id = ?`, userID, stationID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) getStationByExternalID(ctx context.Context, userID, source, externalID string) (Station, error) {
	if externalID == "" {
		return Station{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, stream_url, homepage_url, favicon_url, country, language, tags, codec, bitrate,
			source, external_id, is_favorite, position, last_now_playing_title, last_now_playing_artist,
			last_now_playing_raw, last_now_playing_updated_at
		FROM radio_stations
		WHERE user_id = ? AND source = ? AND external_id = ?`, userID, source, externalID)
	station, err := scanStation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Station{}, ErrNotFound
	}
	return station, err
}

func (s *Store) UpdateNowPlaying(ctx context.Context, stationID string, now NowPlaying) error {
	var updatedAt any
	if now.UpdatedAt != nil {
		updatedAt = *now.UpdatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE radio_stations
		SET last_now_playing_title = ?, last_now_playing_artist = ?, last_now_playing_raw = ?,
			last_now_playing_updated_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		now.Title, now.Artist, now.Raw, updatedAt, stationID,
	)
	return err
}

func (s *Store) nextPosition(ctx context.Context, userID string) (int, error) {
	var maxPosition sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(position) FROM radio_stations WHERE user_id = ?`, userID,
	).Scan(&maxPosition); err != nil {
		return 0, err
	}
	if !maxPosition.Valid {
		return 0, nil
	}
	return int(maxPosition.Int64) + 1, nil
}

type stationScanner interface {
	Scan(dest ...any) error
}

func scanStation(scanner stationScanner) (Station, error) {
	var station Station
	var tagsRaw string
	var isFavorite int
	var title, artist, raw string
	var updatedAt sql.NullTime
	if err := scanner.Scan(
		&station.ID,
		&station.Name,
		&station.StreamURL,
		&station.HomepageURL,
		&station.FaviconURL,
		&station.Country,
		&station.Language,
		&tagsRaw,
		&station.Codec,
		&station.Bitrate,
		&station.Source,
		&station.ExternalID,
		&isFavorite,
		&station.Position,
		&title,
		&artist,
		&raw,
		&updatedAt,
	); err != nil {
		return Station{}, err
	}
	station.IsFavorite = isFavorite != 0
	station.Tags = decodeTags(tagsRaw)
	if title != "" || artist != "" || raw != "" || updatedAt.Valid {
		station.LastNowPlaying = &NowPlaying{
			Title:  title,
			Artist: artist,
			Raw:    raw,
		}
		if updatedAt.Valid {
			station.LastNowPlaying.UpdatedAt = &updatedAt.Time
		}
	}
	return station, nil
}

func encodeTags(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeTags(raw string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	return tags
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
