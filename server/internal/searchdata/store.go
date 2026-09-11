package searchdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Field struct {
	Name      string
	RelatedID string
	Position  int
	Text      Text
}

type Document struct {
	Kind   string
	ID     string
	Fields []Field
}

type Store struct{ database *sql.DB }

func NewStore(database *sql.DB) *Store { return &Store{database: database} }

// GetDocument reads prepared fields with live visibility and ownership rules.
// Relationships retain their source identity and credit order; no name splitting
// or query-time normalization of library records occurs here.
func (store *Store) GetDocument(ctx context.Context, kind, id, userID string) (Document, error) {
	rows, err := store.database.QueryContext(ctx, `
 SELECT field, related_id, position, text_json FROM library_search_fields
 WHERE kind = ? AND id = ? AND (user_id = '' OR user_id = ?)
 ORDER BY CASE field WHEN 'primary' THEN 0 ELSE 1 END, field, position, related_id`, kind, id, userID)
	if err != nil {
		return Document{}, fmt.Errorf("read Library Search %s %q: %w", kind, id, err)
	}
	defer func() { _ = rows.Close() }()
	document := Document{Kind: kind, ID: id, Fields: []Field{}}
	for rows.Next() {
		field, err := readField(rows)
		if err != nil {
			return Document{}, fmt.Errorf("read Library Search %s %q field: %w", kind, id, err)
		}
		document.Fields = append(document.Fields, field)
	}
	if err := rows.Err(); err != nil {
		return Document{}, fmt.Errorf("iterate Library Search %s %q: %w", kind, id, err)
	}
	if len(document.Fields) == 0 {
		return Document{}, sql.ErrNoRows
	}
	return document, nil
}

func readField(rows *sql.Rows) (Field, error) {
	var field Field
	var encoded string
	if err := rows.Scan(&field.Name, &field.RelatedID, &field.Position, &encoded); err != nil {
		return Field{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &field.Text); err != nil {
		return Field{}, fmt.Errorf("decode prepared field: %w", err)
	}
	return field, nil
}
