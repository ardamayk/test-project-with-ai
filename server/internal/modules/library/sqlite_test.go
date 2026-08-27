package library

import (
	"database/sql"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	return testutil.OpenMigratedDB(t)
}
