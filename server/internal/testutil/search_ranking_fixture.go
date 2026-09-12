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
// fixture is fixed by the accepted design: Best Match, then the five groups in
// order (with the Best Match already excluded from its group).
func LibrarySearchRankingCases() []LibrarySearchRankingCase {
	return []LibrarySearchRankingCase{
		// Full names win across types; a strong Track expands its Album and credited Artists.
		{Query: "Blinding Lights", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights-live"}, Albums: []string{"after-hours"}, Artists: []string{"weeknd"}},
		{Query: "the weeknd", BestMatchKind: "artist", BestMatchID: "weeknd", Tracks: []string{"blinding-lights", "rain", "save-your-tears", "someday", "sunflour"}, Albums: []string{"after-hours"}},
		// Combined fields in any word order.
		{Query: "weeknd blinding lights", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights-live"}, Artists: []string{"weeknd"}, Albums: []string{"after-hours"}},
		{Query: "lights blinding weeknd", BestMatchKind: "track", BestMatchID: "blinding-lights", Tracks: []string{"blinding-lights-live"}, Artists: []string{"weeknd"}, Albums: []string{"after-hours"}},
		// Final-word prefix; full words before prefixes; plain titles before versions; Album-title-only matches last, capped at five.
		{Query: "blue", BestMatchKind: "track", BestMatchID: "blue-live-sky", Tracks: []string{"blue-song", "blue-moon-live", "bluebird", "ab-song", "dont-stop"}, Albums: []string{"blue-album"}, Artists: []string{"beyonce-accent"}, Genres: []string{"blues"}, Playlists: []string{"blue-vibes"}},
		{Query: "blue m", BestMatchKind: "track", BestMatchID: "blue-moon-live", Albums: []string{"blue-album"}, Artists: []string{"beyonce-accent"}},
		// A requested allowlisted version is preferred within the class.
		{Query: "blue live", BestMatchKind: "track", BestMatchID: "blue-moon-live", Tracks: []string{"blue-live-sky"}, Albums: []string{"blue-album"}, Artists: []string{"beyonce-accent"}},
		// One-character queries match exact names only; two characters enable prefixes.
		{Query: "u", BestMatchKind: "track", BestMatchID: "u", Albums: []string{"after-hours"}, Artists: []string{"weeknd"}},
		{Query: "un", BestMatchKind: "track", BestMatchID: "under-pressure", Albums: []string{"after-hours"}, Artists: []string{"weeknd"}},
		// Typo correction only without strong results; corrected Tracks expand nothing.
		{Query: "blidning lights", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "weknd blind", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "blidning lihgts", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		// A strong result in one type disables correction in every type.
		{Query: "someday", BestMatchKind: "track", BestMatchID: "someday", Albums: []string{"after-hours"}, Artists: []string{"weeknd"}},
		{Query: "sxmeday", Tracks: []string{"someday", "sameday-track"}, Albums: []string{"sameday-album"}, Artists: []string{"sameday"}},
		// Edit limits at every word-length threshold and the total budget.
		{Query: "ran"}, {Query: "rian", Tracks: []string{"rain"}},
		{Query: "somedya", Tracks: []string{"someday"}}, {Query: "smoedya"},
		{Query: "sunflwoer", Tracks: []string{"sunflower", "sunflour"}}, {Query: "snuflwoer", Tracks: []string{"sunflower"}},
		{Query: "blidnign ligths"}, {Query: "blid"},
		// Fewer edits precede more edits within a class, ahead of alphabetical order.
		{Query: "sunflowar", Tracks: []string{"sunflower", "sunflour"}},
		// Written form: faithful spellings first, then folded alternatives (Tracks credited via Album Artist are class 4).
		{Query: "beyonce", BestMatchKind: "artist", BestMatchID: "beyonce-plain", Tracks: []string{"halo", "ab-song", "blue-live-sky", "blue-song", "bluebird"}, Albums: []string{"blue-album"}, Artists: []string{"beyonce-accent"}},
		{Query: "Beyoncé", BestMatchKind: "artist", BestMatchID: "beyonce-accent", Tracks: []string{"ab-song", "blue-live-sky", "blue-song", "bluebird", "dont-stop"}, Albums: []string{"blue-album"}, Artists: []string{"beyonce-plain"}},
		{Query: "a b"},
		// Exact names precede related records, which precede weaker direct matches.
		{Query: "lights", BestMatchKind: "artist", BestMatchID: "lights", Tracks: []string{"blinding-lights", "blinding-lights-live", "sky-dance", "toes"}, Albums: []string{"after-hours", "aurora", "siberia"}, Artists: []string{"weeknd", "northern-lights"}},
		// Five source Tracks, deduplicated by their strongest source, one hop only.
		{Query: "echo", BestMatchKind: "track", BestMatchID: "echo-five", Tracks: []string{"echo-four", "echo-one", "echo-seven", "echo-six", "echo-three"}, Albums: []string{"album-five", "album-four", "album-one", "album-six"}, Artists: []string{"artist-five", "artist-four", "artist-one", "artist-six"}},
		// Deterministic ties: Artist/Album text, then stable identity.
		{Query: "twin", BestMatchKind: "track", BestMatchID: "twin-a", Tracks: []string{"twin-b"}, Albums: []string{"album-one", "album-two"}, Artists: []string{"artist-one", "artist-two"}},
		{Query: "same coin", BestMatchKind: "track", BestMatchID: "same-coin-a", Tracks: []string{"same-coin-b"}, Albums: []string{"album-one"}, Artists: []string{"artist-one"}},
		// Track credits alone make an Artist searchable; Genres and Playlists match by their own names.
		{Query: "guest hop", BestMatchKind: "artist", BestMatchID: "guest-hop", Tracks: []string{"other-thing"}},
		{Query: "pop", BestMatchKind: "genre", BestMatchID: "pop", Tracks: []string{"blinding-lights", "blinding-lights-live"}},
		{Query: "blue vibes", BestMatchKind: "playlist", BestMatchID: "blue-vibes"},
	}
}
