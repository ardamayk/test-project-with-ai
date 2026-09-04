package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHandlersGetAlbumReturnsNormalizedRelationshipsAndDiscOrder(t *testing.T) {
	handlers, database := setupHandlerFixture(t)
	albumID, trackID := seedTrack(t, database)
	seedExpandedReadFixture(t, database, albumID, trackID)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("albumId", albumID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	handlers.GetAlbum(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var album AlbumDetail
	if err := json.NewDecoder(response.Body).Decode(&album); err != nil {
		t.Fatal(err)
	}
	assertExpandedAlbum(t, album)
}

func TestHandlersListTracksSearchesNormalizedCreditsAndGenres(t *testing.T) {
	handlers, database := setupHandlerFixture(t)
	albumID, trackID := seedTrack(t, database)
	seedExpandedReadFixture(t, database, albumID, trackID)

	for _, query := range []string{"Guest, Artist", "Electronic / Ambient"} {
		request := httptest.NewRequest(http.MethodGet, "/?q="+url.QueryEscape(query), nil)
		response := httptest.NewRecorder()
		handlers.ListTracks(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("search %q status = %d, want 200", query, response.Code)
		}
		var tracks TrackList
		if err := json.NewDecoder(response.Body).Decode(&tracks); err != nil {
			t.Fatal(err)
		}
		if tracks.Total != 1 || len(tracks.Items) != 1 || tracks.Items[0].ID != trackID {
			t.Fatalf("search %q tracks = %#v, want normalized Track %q", query, tracks, trackID)
		}
	}
}

func TestHandlersListAlbumsFilterByArtistUsesAlbumCreditsOnly(t *testing.T) {
	handlers, database := setupHandlerFixture(t)
	albumID, trackID := seedTrack(t, database)
	seedExpandedReadFixture(t, database, albumID, trackID)

	listAlbumsFor := func(artistID string) AlbumList {
		request := httptest.NewRequest(http.MethodGet, "/?artistId="+artistID, nil)
		response := httptest.NewRecorder()
		handlers.ListAlbums(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", response.Code)
		}
		var albums AlbumList
		if err := json.NewDecoder(response.Body).Decode(&albums); err != nil {
			t.Fatal(err)
		}
		return albums
	}

	// A guest credited on one track does not own the album.
	if albums := listAlbumsFor("guest-artist"); albums.Total != 0 || len(albums.Items) != 0 {
		t.Fatalf("track-only guest surfaced Albums = %#v, want none", albums)
	}
	// An Album Artist credit does, even as a secondary credit.
	if albums := listAlbumsFor("album-guest"); albums.Total != 1 || len(albums.Items) != 1 || albums.Items[0].ID != albumID {
		t.Fatalf("album credit Albums = %#v, want %q", albums, albumID)
	}
}

func TestHandlersListArtistsCountsDistinctArtistsAndPreservesAlbumGenreSummary(t *testing.T) {
	handlers, database := setupHandlerFixture(t)
	firstAlbumID, firstTrackID := seedTrack(t, database)
	seedExpandedReadFixture(t, database, firstAlbumID, firstTrackID)
	executeFixtureStatement(t, database, `UPDATE albums SET genres = '["Album Genre Summary"]' WHERE id = ?`, firstAlbumID)
	executeFixtureStatement(t, database, `
		INSERT INTO albums (id, artist_id, title, title_sort, genres)
		SELECT 'second-album', artist_id, 'Second Album', 'second album', '[]'
		FROM albums WHERE id = ?`, firstAlbumID)
	executeFixtureStatement(t, database, `
		INSERT INTO album_artists (album_id, artist_id, position)
		SELECT 'second-album', artist_id, position FROM album_artists WHERE album_id = ?`, firstAlbumID)
	executeFixtureStatement(t, database, `
		INSERT INTO tracks (id, album_id, title, title_sort, artist_name, duration_ms, format, file_path)
		VALUES ('second-track', 'second-album', 'Second Track', 'second track', 'Credited Artist', 1000, 'flac', '/music/second.flac')`)

	artistsRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	artistsResponse := httptest.NewRecorder()
	handlers.ListArtists(artistsResponse, artistsRequest)
	// An error body also decodes into an empty ArtistList, so the status and
	// the seeded Artist must be asserted explicitly.
	if artistsResponse.Code != http.StatusOK {
		t.Fatalf("list Artists status = %d, body = %s", artistsResponse.Code, artistsResponse.Body.String())
	}
	var artists ArtistList
	if err := json.NewDecoder(artistsResponse.Body).Decode(&artists); err != nil {
		t.Fatal(err)
	}
	if artists.Total != len(artists.Items) {
		t.Fatalf("Artist total = %d, items = %d", artists.Total, len(artists.Items))
	}
	if artists.Total == 0 {
		t.Fatal("list Artists returned no Artists for the seeded library")
	}

	albumsRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	albumsResponse := httptest.NewRecorder()
	handlers.ListAlbums(albumsResponse, albumsRequest)
	var albums AlbumList
	if err := json.NewDecoder(albumsResponse.Body).Decode(&albums); err != nil {
		t.Fatal(err)
	}
	for _, album := range albums.Items {
		if album.ID == firstAlbumID && (len(album.Genres) != 1 || album.Genres[0] != "Album Genre Summary") {
			t.Fatalf("Album Genre summary = %#v", album.Genres)
		}
	}
}

func TestHandlersGetAlbumCoverReadsNormalizedArtworkFile(t *testing.T) {
	handlers, database := setupHandlerFixture(t)
	albumID, trackID := seedTrack(t, database)
	artworkPath := filepath.Join(t.TempDir(), "cover.png")
	artworkData := []byte("normalized artwork")
	if err := os.WriteFile(artworkPath, artworkData, 0o600); err != nil {
		t.Fatal(err)
	}
	executeFixtureStatement(t, database, `
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type, width, height,
			encoded_size_bytes, file_path
		) VALUES (
			'artwork-file', ?, ?,
			'1111111111111111111111111111111111111111111111111111111111111111',
			'image/png', 1, 1, ?, ?
		)`, albumID, trackID, len(artworkData), artworkPath)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("albumId", albumID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	handlers.GetAlbumCover(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("cover response = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if response.Body.String() != string(artworkData) {
		t.Fatalf("cover body = %q, want normalized artwork", response.Body.String())
	}
}

func seedExpandedReadFixture(t *testing.T, database *sql.DB, albumID, trackID string) {
	t.Helper()
	executeFixtureStatement(t, database, `
		INSERT INTO artists (id, name, name_sort, name_normalized) VALUES
			('guest-artist', 'Guest, Artist', 'guest artist', 'guest, artist'),
			('album-guest', 'Album & Guest', 'album guest', 'album & guest')`)
	executeFixtureStatement(t, database, `DELETE FROM track_artists WHERE track_id = ?`, trackID)
	executeFixtureStatement(t, database, `
		INSERT INTO track_artists (track_id, artist_id, position)
		SELECT ?, 'guest-artist', 0
		UNION ALL
		SELECT ?, artist_id, 1 FROM album_artists WHERE album_id = ? AND position = 0`,
		trackID, trackID, albumID,
	)
	executeFixtureStatement(t, database, `
		INSERT INTO album_artists (album_id, artist_id, position) VALUES (?, 'album-guest', 1)`, albumID)
	executeFixtureStatement(t, database, `
		INSERT INTO genres (id, name, name_normalized) VALUES
			('electronic-ambient', 'Electronic / Ambient', 'electronic / ambient')`)
	executeFixtureStatement(t, database, `
		INSERT INTO track_genres (track_id, genre_id, position) VALUES
			(?, 'electronic-ambient', 1)`, trackID)
	executeFixtureStatement(t, database, `
		UPDATE tracks SET artist_name = 'Credited Artist', genre = 'Summary, Guess', disc_no = 2,
			track_total = 9, disc_total = 2, channel_count = 2, bitrate_bps = 2304000,
			codec = 'flac', container = 'flac', sample_format = 's24'
		WHERE id = ?`, trackID)
	executeFixtureStatement(t, database, `
		UPDATE albums SET release_date = '2024-02-03' WHERE id = ?`, albumID)
	executeFixtureStatement(t, database, `
		INSERT INTO album_release_identifiers (album_id, scheme, value)
		VALUES (?, 'musicbrainz-release', 'release-1')`, albumID)
	executeFixtureStatement(t, database, `
		INSERT INTO album_artwork (
			id, album_id, source_track_id, content_sha256, media_type, width, height,
			encoded_size_bytes, file_path
		) VALUES (
			'artwork-1', ?, ?,
			'0000000000000000000000000000000000000000000000000000000000000000',
			'image/png', 1200, 1200, 4096, '/managed/cover.png'
		)`, albumID, trackID)
}

func executeFixtureStatement(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := database.Exec(query, args...)
	if err != nil {
		t.Fatalf("seed expanded read fixture: %v", err)
	}
}

func assertExpandedAlbum(t *testing.T, album AlbumDetail) {
	t.Helper()
	if len(album.AlbumArtists) != 2 || album.AlbumArtists[1].Name != "Album & Guest" {
		t.Fatalf("Album Artists = %#v", album.AlbumArtists)
	}
	if album.ReleaseDate == nil || *album.ReleaseDate != "2024-02-03" {
		t.Fatalf("release date = %#v", album.ReleaseDate)
	}
	if len(album.ReleaseIdentifiers) != 1 || album.ReleaseIdentifiers[0].Value != "release-1" {
		t.Fatalf("release identifiers = %#v", album.ReleaseIdentifiers)
	}
	if album.Artwork == nil || album.Artwork.MediaType != "image/png" || album.Artwork.Width != 1200 {
		t.Fatalf("Album Artwork = %#v", album.Artwork)
	}
	if len(album.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(album.Tracks))
	}
	track := album.Tracks[0]
	if track.DiscNo != 2 || len(track.Artists) != 2 || track.Artists[0].Name != "Guest, Artist" {
		t.Fatalf("Track position or Artists = %#v", track)
	}
	if len(track.Genres) != 2 || track.Genres[1].Name != "Electronic / Ambient" {
		t.Fatalf("Track Genres = %#v", track.Genres)
	}
}
