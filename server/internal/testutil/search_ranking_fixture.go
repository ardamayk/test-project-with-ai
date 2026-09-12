package testutil

import (
	"database/sql"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/auth"
)

// LibrarySearchRankingCase is one labeled Library Search query over the shared
// fixture. Expectations are product examples from the accepted design; the
// integrated quality evaluation reuses them rather than defining new ones.
type LibrarySearchRankingCase struct {
	Query         string
	BestMatchKind string
	BestMatchID   string
	Tracks        []string
	Albums        []string
	Artists       []string
	Genres        []string
	Playlists     []string
}

// SeedLibrarySearchFixture inserts the coherent Library Search library used by
// ranking acceptance tests. Records are visible only through active Tracks, so
// every Artist and Album owns at least one Track.
func SeedLibrarySearchFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	run := func(statement string, arguments ...any) {
		t.Helper()
		if _, err := database.Exec(statement, arguments...); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	artists := [][2]string{
		{"weeknd", "The Weeknd"}, {"beyonce-accent", "Beyoncé"}, {"beyonce-plain", "Beyonce"}, {"sebnem", "Şebnem Ferah"},
		{"acdc", "AC/DC"}, {"nightwish", "Nightwish"}, {"lights", "Lights"}, {"northern-lights", "Northern Lights"},
		{"sameday", "Sameday"}, {"guest-hop", "Guest Hop"},
		{"artist-one", "Artist One"}, {"artist-two", "Artist Two"}, {"artist-three", "Artist Three"}, {"artist-four", "Artist Four"},
		{"artist-five", "Artist Five"}, {"artist-six", "Artist Six"},
	}
	for _, artist := range artists {
		run(`INSERT INTO artists(id,name,name_sort) VALUES (?,?,?)`, artist[0], artist[1], artist[1])
	}
	albums := [][3]string{
		{"after-hours", "weeknd", "After Hours"}, {"blue-album", "beyonce-accent", "Lemonade Blue"}, {"siberia", "lights", "Siberia"},
		{"perdeler", "sebnem", "Perdeler"}, {"aurora", "northern-lights", "Aurora"}, {"highway", "acdc", "Highway"},
		{"decades", "nightwish", "Decades"}, {"sameday-album", "sameday", "Sameday Songs"},
		{"album-one", "artist-one", "Album One"}, {"album-two", "artist-two", "Album Two"}, {"album-three", "artist-three", "Album Three"},
		{"album-four", "artist-four", "Album Four"}, {"album-five", "artist-five", "Album Five"}, {"album-six", "artist-six", "Album Six"},
	}
	for _, album := range albums {
		run(`INSERT INTO albums(id,artist_id,title,title_sort) VALUES (?,?,?,?)`, album[0], album[1], album[2], album[2])
		run(`INSERT INTO album_artists VALUES (?,?,0)`, album[0], album[1])
	}
	tracks := [][4]string{
		{"blinding-lights", "after-hours", "weeknd", "Blinding Lights"}, {"blinding-lights-live", "after-hours", "weeknd", "Blinding Lights (Live)"},
		{"save-your-tears", "after-hours", "weeknd", "Save Your Tears"}, {"someday", "after-hours", "weeknd", "Someday"},
		{"sunflower", "after-hours", "weeknd", "Sunflower"}, {"sunflour", "after-hours", "weeknd", "Sunflour"},
		{"rain", "after-hours", "weeknd", "Rain"}, {"u", "after-hours", "weeknd", "U"}, {"under-pressure", "after-hours", "weeknd", "Under Pressure"},
		{"blue-moon-live", "blue-album", "beyonce-accent", "Blue Moon (Live)"}, {"blue-song", "blue-album", "beyonce-accent", "Blue Song"},
		{"bluebird", "blue-album", "beyonce-accent", "Bluebird"}, {"blue-live-sky", "blue-album", "beyonce-accent", "Blue Live Sky"},
		{"dont-stop", "blue-album", "beyonce-accent", "Don't Stop"}, {"ab-song", "blue-album", "beyonce-accent", "AB Song"},
		{"halo", "blue-album", "beyonce-plain", "Halo"}, {"toes", "siberia", "lights", "Toes"}, {"sil-bastan", "perdeler", "sebnem", "Sil Baştan"},
		{"sky-dance", "aurora", "northern-lights", "Sky Dance"},
		{"thunderstruck", "highway", "acdc", "Thunderstruck"}, {"nemo", "decades", "nightwish", "Nemo"},
		{"sameday-track", "sameday-album", "sameday", "Plain Track"},
		{"echo-one", "album-one", "artist-one", "Echo One"}, {"echo-two", "album-two", "artist-two", "Echo Two"},
		{"echo-three", "album-three", "artist-three", "Echo Three"}, {"echo-four", "album-four", "artist-four", "Echo Four"},
		{"echo-five", "album-five", "artist-five", "Echo Five"}, {"echo-six", "album-six", "artist-six", "Echo Six"},
		{"echo-seven", "album-one", "artist-one", "Echo Seven"},
		{"other-thing", "album-one", "guest-hop", "Other Thing"},
		{"twin-a", "album-one", "artist-one", "Twin"}, {"twin-b", "album-two", "artist-two", "Twin"},
		{"same-coin-b", "album-one", "artist-one", "Same Coin"}, {"same-coin-a", "album-one", "artist-one", "Same Coin"},
	}
	for _, track := range tracks {
		run(`INSERT INTO tracks(id,album_id,title,title_sort,artist_name,format,file_path) VALUES (?,?,?,?,?,'flac',?)`,
			track[0], track[1], track[3], track[3], track[2], "/managed/"+track[0]+".flac")
		run(`INSERT INTO track_artists VALUES (?,?,0)`, track[0], track[2])
	}
	run(`INSERT INTO genres(id,name,name_normalized) VALUES ('pop','Pop','pop'), ('blues','Blues','blues'), ('rnb','R&B','r&b')`)
	run(`INSERT INTO track_genres VALUES ('blinding-lights','pop',0), ('blinding-lights-live','pop',0), ('blue-song','blues',0), ('blue-song','rnb',1)`)
	run(`INSERT INTO playlists(id,user_id,name) VALUES ('blue-vibes',?,'Blue Vibes'), ('blue-other','someone-else','Blue Other')`, auth.DefaultUserID)
}

// LibrarySearchRankingCases lists queries whose complete outcome over the shared
// fixture is fixed by the accepted design: own-name contribution required,
// Best Match first in its category and counted within five results.
func LibrarySearchRankingCases() []LibrarySearchRankingCase {
	return []LibrarySearchRankingCase{
		// Own names must contribute; credits alone never return Tracks or Albums.
		{Query: "Blinding Lights", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "the weeknd", BestMatchKind: "artist", BestMatchID: "weeknd", Artists: []string{"weeknd"}},
		// Combined fields in any word order.
		{Query: "weeknd blinding lights", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "lights blinding weeknd", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		// Full words before prefixes; plain titles before versions; no Album-title-only Tracks.
		{Query: "blue", BestMatchKind: "track", BestMatchID: "blue-live-sky", Tracks: []string{"blue-live-sky", "blue-song", "blue-moon-live", "bluebird"}, Albums: []string{"blue-album"}, Genres: []string{"blues"}, Playlists: []string{"blue-vibes"}},
		{Query: "blue m", BestMatchKind: "track", BestMatchID: "blue-moon-live", Tracks: []string{"blue-moon-live"}},
		// A requested allowlisted version is preferred within the class.
		{Query: "blue live", BestMatchKind: "track", BestMatchID: "blue-moon-live", Tracks: []string{"blue-moon-live", "blue-live-sky"}},
		// One-character exact names and two-character prefixes never expand relationships.
		{Query: "u", BestMatchKind: "track", BestMatchID: "u", Tracks: []string{"u"}},
		{Query: "un", BestMatchKind: "track", BestMatchID: "under-pressure", Tracks: []string{"under-pressure"}},
		// Typo correction only without strong results; corrected Tracks expand nothing.
		{Query: "blidning lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "weknd blind", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "blidning lihgts", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		// A strong result in one type disables correction in every type.
		{Query: "someday", BestMatchKind: "track", BestMatchID: "someday", Tracks: []string{"someday"}},
		{Query: "sxmeday", Tracks: []string{"someday"}, Albums: []string{"sameday-album"}, Artists: []string{"sameday"}},
		// Edit limits at every word-length threshold and the total budget.
		{Query: "ran"}, {Query: "rian", Tracks: []string{"rain"}},
		{Query: "somedya", Tracks: []string{"someday"}}, {Query: "smoedya"},
		{Query: "sunflwoer", Tracks: []string{"sunflower", "sunflour"}}, {Query: "snuflwoer", Tracks: []string{"sunflower"}},
		{Query: "blidnign ligths"}, {Query: "blid"},
		// Fewer edits precede more edits within a class, ahead of alphabetical order.
		{Query: "sunflowar", Tracks: []string{"sunflower", "sunflour"}},
		// Written form: faithful spellings first, then folded alternatives.
		{Query: "beyonce", BestMatchKind: "artist", BestMatchID: "beyonce-plain", Artists: []string{"beyonce-plain", "beyonce-accent"}},
		{Query: "Beyoncé", BestMatchKind: "artist", BestMatchID: "beyonce-accent", Artists: []string{"beyonce-accent", "beyonce-plain"}},
		{Query: "a b"},
		// Every Artist matches its own name, not another Track's relationships.
		{Query: "lights", BestMatchKind: "artist", BestMatchID: "lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}, Artists: []string{"lights", "northern-lights"}},
		// Best Match consumes the first of five slots, not a sixth result.
		{Query: "echo", BestMatchKind: "track", BestMatchID: "echo-five", Tracks: []string{"echo-five", "echo-four", "echo-one", "echo-seven", "echo-six"}},
		// Deterministic ties: Artist/Album text, then stable identity.
		{Query: "twin", BestMatchKind: "track", BestMatchID: "twin-a", Tracks: []string{"twin-a", "twin-b"}},
		{Query: "same coin", BestMatchKind: "track", BestMatchID: "same-coin-a", Tracks: []string{"same-coin-a", "same-coin-b"}},
		// Track-only credits keep Artists searchable; Genres and Playlists match their names.
		{Query: "guest hop", BestMatchKind: "artist", BestMatchID: "guest-hop", Artists: []string{"guest-hop"}},
		{Query: "pop", BestMatchKind: "genre", BestMatchID: "pop", Genres: []string{"pop"}},
		{Query: "blue vibes", BestMatchKind: "playlist", BestMatchID: "blue-vibes", Playlists: []string{"blue-vibes"}},
	}
}
