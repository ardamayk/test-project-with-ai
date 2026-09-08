package playback

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidQueueSource = errors.New("invalid queue source")

// QueueItemSource records the origin at insertion time, independently of track metadata.
type QueueItemSource struct {
	Kind       string   `json:"kind"`
	AlbumID    string   `json:"albumId,omitempty"`
	AlbumTitle string   `json:"albumTitle,omitempty"`
	ArtistName string   `json:"artistName,omitempty"`
	PlaylistID string   `json:"playlistId,omitempty"`
	Name       string   `json:"name,omitempty"`
	BasedOn    []string `json:"basedOn,omitempty"`
}

func (source QueueItemSource) MarshalJSON() ([]byte, error) {
	type sourceFields QueueItemSource
	if source.Kind == "suggestion" {
		return json.Marshal(struct {
			sourceFields
			BasedOn []string `json:"basedOn"`
		}{sourceFields(source), source.BasedOn})
	}
	return json.Marshal(sourceFields(source))
}

func parseQueueItemSource(data json.RawMessage) (QueueItemSource, error) {
	if len(data) == 0 {
		return QueueItemSource{Kind: "user"}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return QueueItemSource{}, fmt.Errorf("%w: source must be an object", ErrInvalidQueueSource)
	}
	var source QueueItemSource
	if err := json.Unmarshal(data, &source); err != nil {
		return QueueItemSource{}, fmt.Errorf("%w: source fields have invalid types", ErrInvalidQueueSource)
	}
	if err := validateQueueSourceFields(source.Kind, fields); err != nil {
		return QueueItemSource{}, err
	}
	return source, nil
}

func validateQueueSourceFields(kind string, fields map[string]json.RawMessage) error {
	var required []string
	switch kind {
	case "album":
		required = []string{"kind", "albumId", "albumTitle", "artistName"}
	case "playlist":
		required = []string{"kind", "playlistId", "name"}
	case "user":
		required = []string{"kind"}
	case "suggestion":
		required = []string{"kind", "basedOn"}
	default:
		return fmt.Errorf("%w: unknown kind", ErrInvalidQueueSource)
	}
	if len(fields) != len(required) {
		return fmt.Errorf("%w: fields do not match source kind %q", ErrInvalidQueueSource, kind)
	}
	for _, field := range required {
		if err := validateQueueSourceField(field, fields[field]); err != nil {
			return err
		}
	}
	return nil
}

func validateQueueSourceField(field string, value json.RawMessage) error {
	if field == "basedOn" {
		var trackIDs []string
		if err := json.Unmarshal(value, &trackIDs); err != nil || trackIDs == nil {
			return fmt.Errorf("%w: basedOn must be an array of track IDs", ErrInvalidQueueSource)
		}
		for _, trackID := range trackIDs {
			if strings.TrimSpace(trackID) == "" {
				return fmt.Errorf("%w: basedOn track IDs must not be blank", ErrInvalidQueueSource)
			}
		}
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil || strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: %s must be a nonblank string", ErrInvalidQueueSource, field)
	}
	return nil
}

func encodeQueueItemSource(sources []QueueItemSource) (string, error) {
	source := QueueItemSource{Kind: "user"}
	if len(sources) > 1 {
		return "", fmt.Errorf("%w: only one source is allowed", ErrInvalidQueueSource)
	}
	if len(sources) == 1 {
		source = sources[0]
	}
	data, err := json.Marshal(source)
	if err != nil {
		return "", fmt.Errorf("encode queue source: %w", err)
	}
	if _, err := parseQueueItemSource(data); err != nil {
		return "", err
	}
	return string(data), nil
}
