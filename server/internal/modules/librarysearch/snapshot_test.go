package librarysearch_test

import (
	"net/http"
	"sync"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestSearchConcurrentReadersObserveCommittedNames(t *testing.T) {
	handler, database := newSearchServer(t)
	assertBestMatch(t, search(t, handler, "rain"), "track", "rain")
	for _, name := range []string{"Fresh Signal", "Fresh Lantern"} {
		execute(t, database, `UPDATE tracks SET title=? WHERE id='rain'`, name)
		var readers sync.WaitGroup
		for range 8 {
			readers.Go(func() {
				assertBestMatch(t, search(t, handler, name), "track", "rain")
			})
		}
		readers.Wait()
	}
}

func TestSearchSnapshotTracksCommittedChanges(t *testing.T) {
	handler, database := newSearchServer(t)
	assertBestMatch(t, search(t, handler, "Blinding Lights"), "track", "blinding-lights")
	for _, change := range []struct{ statement, query, kind, id string }{
		{`UPDATE tracks SET title='Northern Signal' WHERE id='blinding-lights'`, "Northern Signal", "track", "blinding-lights"},
		{`UPDATE artists SET name='New Credit' WHERE id='weeknd'`, "New Credit", "artist", "weeknd"},
		{`UPDATE albums SET title='New Collection' WHERE id='after-hours'`, "New Collection", "album", "after-hours"},
		{`UPDATE genres SET name='New Style' WHERE id='pop'`, "New Style", "genre", "pop"},
		{`UPDATE playlists SET name='New Mix' WHERE id='blue-vibes'`, "New Mix", "playlist", "blue-vibes"},
		{`UPDATE track_artists SET artist_id='nightwish' WHERE track_id='blinding-lights'`, "nightwish northern signal", "track", "blinding-lights"},
		{`UPDATE tracks SET album_id='decades' WHERE id='blinding-lights'`, "decades northern signal", "track", "blinding-lights"},
		{`UPDATE track_genres SET genre_id='blues' WHERE track_id='blinding-lights'`, "blues northern signal", "track", "blinding-lights"},
		{`UPDATE album_artists SET artist_id='acdc' WHERE album_id='decades'`, "acdc northern signal", "track", "blinding-lights"},
	} {
		execute(t, database, change.statement)
		assertBestMatch(t, search(t, handler, change.query), apigen.LibrarySearchResultType(change.kind), change.id)
	}
	for _, statement := range []string{
		`UPDATE tracks SET is_pending_commit=1 WHERE id='blinding-lights'`,
		`UPDATE tracks SET is_pending_commit=0, missing_at=CURRENT_TIMESTAMP WHERE id='blinding-lights'`,
		`DELETE FROM tracks WHERE id='blinding-lights'`,
	} {
		execute(t, database, statement)
		result := search(t, handler, "Northern Signal")
		if result.BestMatch != nil || len(result.Tracks) != 0 {
			t.Fatalf("invisible Track survived %s: %+v", statement, result)
		}
	}
	execute(t, database, `UPDATE playlists SET user_id='other-user' WHERE id='blue-vibes'`)
	if result := search(t, handler, "New Mix"); result.BestMatch != nil || len(result.Playlists) != 0 {
		t.Fatalf("foreign Playlist survived ownership change: %+v", result)
	}
	// A warmed snapshot must not turn storage failure into a stale success.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	testutil.AssertErrorCode(t, testutil.ServeRequest(t, handler, http.MethodGet, "/api/v1/library/search?q=New+Mix", nil, nil), http.StatusInternalServerError, "internal_error")
}
