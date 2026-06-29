package library

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
