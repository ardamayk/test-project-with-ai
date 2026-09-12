package librarysearch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/ardam/navidrome-replacement/server/internal/searchdata"
)

// preparedText is one stored representation with its word lists computed once
// per load; queries never normalize library names again.
type preparedText struct {
	searchdata.Text
	faithfulWords []string
	foldedWords   []string
	compactWords  []string
}

type fieldRef struct {
	name      string
	relatedID string
	text      *preparedText
}

type entity struct {
	kind    string
	id      string
	primary *preparedText
	related []fieldRef
}

func (e *entity) fields(name string) []fieldRef {
	var out []fieldRef
	for _, field := range e.related {
		if field.name == name {
			out = append(out, field)
		}
	}
	return out
}

// Index is a visibility-filtered snapshot of every searchable entity. Candidate
// collection covers the whole eligible library so ranking, deduplication, and
// group limits are applied globally rather than to truncated pages.
type Index struct {
	entities []*entity
	byKey    map[entityKey]*entity
}

type entityKey struct{ kind, id string }

// Result types in group order, matching the prepared-data kinds.
const (
	KIND_TRACK    = "track"
	KIND_ALBUM    = "album"
	KIND_ARTIST   = "artist"
	KIND_GENRE    = "genre"
	KIND_PLAYLIST = "playlist"
)

var resultKinds = []string{KIND_TRACK, KIND_ALBUM, KIND_ARTIST, KIND_GENRE, KIND_PLAYLIST}

// relatedKinds maps a Track's relationship fields to the kind they reference;
// Genres are searchable by their own name only and are never expanded.
var relatedKinds = map[string]string{"album": KIND_ALBUM, "track_artist": KIND_ARTIST, "album_artist": KIND_ARTIST}

func (index *Index) lookup(kind, id string) *entity {
	return index.byKey[entityKey{kind, id}]
}

type IndexLoader struct{ database *sql.DB }

func NewIndexLoader(database *sql.DB) *IndexLoader { return &IndexLoader{database: database} }

func (loader *IndexLoader) Load(ctx context.Context, userID string) (*Index, error) {
	texts, err := loader.loadTexts(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := loader.database.QueryContext(ctx, `
 SELECT source.kind, source.id, source.field, source.related_kind, source.related_id
 FROM library_search_field_sources source
 JOIN library_search_entities entity ON entity.kind = source.kind AND entity.id = source.id
 WHERE entity.user_id = '' OR entity.user_id = ?
 ORDER BY source.kind, source.id, CASE source.field WHEN 'primary' THEN 0 ELSE 1 END, source.field, source.position, source.related_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("read Library Search fields: %w", err)
	}
	defer func() { _ = rows.Close() }()
	index := &Index{byKey: map[entityKey]*entity{}}
	var current *entity
	for rows.Next() {
		var kind, id, field, relatedKind, relatedID string
		if err := rows.Scan(&kind, &id, &field, &relatedKind, &relatedID); err != nil {
			return nil, fmt.Errorf("scan Library Search field: %w", err)
		}
		text, prepared := texts[entityKey{relatedKind, relatedID}]
		if !prepared {
			return nil, fmt.Errorf("prepared Library Search data for %s %q is missing; rebuild derived data", relatedKind, relatedID)
		}
		if current == nil || current.kind != kind || current.id != id {
			current = &entity{kind: kind, id: id}
			index.entities = append(index.entities, current)
			index.byKey[entityKey{kind, id}] = current
		}
		if field == "primary" {
			current.primary = text
			continue
		}
		current.related = append(current.related, fieldRef{name: field, relatedID: relatedID, text: text})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Library Search fields: %w", err)
	}
	for _, item := range index.entities {
		if item.primary == nil {
			return nil, fmt.Errorf("searchable %s %q has no primary name", item.kind, item.id)
		}
	}
	return index, nil
}

// splitFaithful keeps accents and inner punctuation (don't, ac/dc) as spelling
// evidence while ignoring enclosing punctuation such as the parentheses of a
// version section.
func splitFaithful(faithful string) []string {
	words := strings.Fields(faithful)
	out := words[:0]
	for _, word := range words {
		if word = strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }); word != "" {
			out = append(out, word)
		}
	}
	return out
}

func (loader *IndexLoader) loadTexts(ctx context.Context) (map[entityKey]*preparedText, error) {
	rows, err := loader.database.QueryContext(ctx, `SELECT kind, id, text_json FROM library_search_texts`)
	if err != nil {
		return nil, fmt.Errorf("read Library Search texts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	texts := map[entityKey]*preparedText{}
	for rows.Next() {
		var kind, id, encoded string
		if err := rows.Scan(&kind, &id, &encoded); err != nil {
			return nil, fmt.Errorf("scan Library Search text: %w", err)
		}
		text := &preparedText{}
		if err := json.Unmarshal([]byte(encoded), &text.Text); err != nil {
			return nil, fmt.Errorf("decode Library Search %s %q text: %w", kind, id, err)
		}
		text.faithfulWords = splitFaithful(text.Faithful)
		text.foldedWords = strings.Fields(text.Folded)
		text.compactWords = strings.Fields(text.Compact)
		texts[entityKey{kind, id}] = text
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Library Search texts: %w", err)
	}
	return texts, nil
}
