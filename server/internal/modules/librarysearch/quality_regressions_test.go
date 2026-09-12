package librarysearch_test

import (
	"fmt"
	"reflect"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
)

func TestLibrarySearchMixedTypoKeepsPrimaryContributionWithinBudget(t *testing.T) {
	handler, database := newSearchServer(t)
	execute(t, database, `UPDATE tracks SET title = 'Blue Moon Star' WHERE id = 'rain'`)
	execute(t, database, `UPDATE albums SET title = 'Moons' WHERE id = 'after-hours'`)
	execute(t, database, `UPDATE artists SET name = 'Blues' WHERE id = 'weeknd'`)
	execute(t, database, `UPDATE genres SET name = 'Stars' WHERE id = 'pop'`)
	execute(t, database, `INSERT INTO track_genres VALUES ('rain','pop',0)`)
	// All three title words need one edit. Credits can cover the other words,
	// leaving one title contribution within the two-edit query budget.
	result := search(t, handler, "blues moons stars")
	assertIDs(t, "mixed typo with overlapping related words", result.Tracks, "rain")
	if result.BestMatch != nil || result.Tracks[0].Match != apigen.Corrected {
		t.Fatalf("mixed typo result = %+v", result)
	}
}

func TestLibrarySearchBestMatchIsFirstWithinFiveCategoryResults(t *testing.T) {
	handler, _ := newSearchServer(t)
	result := search(t, handler, "echo")
	assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "echo-five")
	assertIDs(t, "Best Match consumes first Track slot", result.Tracks, "echo-five", "echo-four", "echo-one", "echo-seven", "echo-six")
}

func TestLibrarySearchRequiresOwnNameContribution(t *testing.T) {
	handler, database := newSearchServer(t)
	execute(t, database, `UPDATE artists SET name = 'Ariana Grande' WHERE id = 'guest-hop'`)
	execute(t, database, `UPDATE artists SET name = 'Taylor Swift' WHERE id = 'artist-one'`)
	execute(t, database, `UPDATE tracks SET title = 'Bang Bang' WHERE id = 'other-thing'`)
	for _, query := range []string{"ar", "ariana", "arinaa"} {
		result := search(t, handler, query)
		if query == "arinaa" {
			assertIDs(t, query+" artists", result.Artists, "guest-hop")
		} else {
			assertBestMatch(t, result, apigen.LibrarySearchResultTypeArtist, "guest-hop")
		}
		assertIDs(t, query+" tracks", result.Tracks)
		assertIDs(t, query+" albums", result.Albums)
	}
	result := search(t, handler, "bang bang")
	assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "other-thing")
	assertIDs(t, "no Album expansion", result.Albums)
	assertIDs(t, "no Artist expansion", result.Artists)
	for _, query := range []string{"ariana bang", "arinaa bang"} {
		result := search(t, handler, query)
		if query == "arinaa bang" {
			assertIDs(t, "mixed typo Track", result.Tracks, "other-thing")
			if result.BestMatch != nil || result.Tracks[0].Match != apigen.Corrected {
				t.Fatalf("mixed typo result = %+v", result)
			}
		} else {
			assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "other-thing")
		}
		assertIDs(t, "mixed query Albums", result.Albums)
		assertIDs(t, "mixed query Artists", result.Artists)
	}
}

func TestLibrarySearchQualityHardBoundaries(t *testing.T) {
	t.Run("three classes preserve primary intent and reject secondary-only", func(t *testing.T) {
		handler, database := newSearchServer(t)
		// One query, three classes plus an ineligible credit-only Track. Secondary
		// evidence cross a class. Best Match takes the exact primary title.
		execute(t, database, `UPDATE tracks SET title = 'Blue Lights' WHERE id = 'blue-song'`)
		execute(t, database, `UPDATE tracks SET title = 'Blue Lights Tonight' WHERE id = 'bluebird'`)
		execute(t, database, `UPDATE tracks SET title = 'Blue Arrival' WHERE id = 'toes'`)
		execute(t, database, `UPDATE albums SET title = 'Blue Lights Collection' WHERE id = 'siberia'`)
		execute(t, database, `INSERT INTO tracks(id,album_id,title,title_sort,artist_name,format,file_path) VALUES ('secondary-only','siberia','Aardvark','Aardvark','Lights','flac','/managed/secondary.flac')`)
		execute(t, database, `INSERT INTO track_artists VALUES ('secondary-only','lights',0)`)
		result := search(t, handler, "blue lights")
		assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "blue-song")
		assertIDs(t, "three classes", result.Tracks, "blue-song", "bluebird", "toes")
	})

	for _, kind := range []string{"track", "album", "artist", "genre", "playlist"} {
		t.Run("strong "+kind+" suppresses correction everywhere", func(t *testing.T) {
			handler, database := newSearchServer(t)
			// Every type has a one-edit 'Sameday' target. A unique exact
			// 'Someday' result in any one type must suppress all five.
			execute(t, database, `UPDATE tracks SET title = 'Sameday' WHERE id = 'sameday-track'`)
			execute(t, database, `UPDATE albums SET title = 'Sameday' WHERE id = 'sameday-album'`)
			execute(t, database, `UPDATE genres SET name = 'Sameday', name_normalized = 'sameday' WHERE id = 'pop'`)
			execute(t, database, `UPDATE playlists SET name = 'Sameday' WHERE id = 'blue-vibes'`)
			execute(t, database, `UPDATE tracks SET title = 'Away' WHERE id = 'someday'`)
			id := map[string]string{"track": "someday", "album": "aurora", "artist": "guest-hop", "genre": "blues", "playlist": "blue-vibes"}[kind]
			switch kind {
			case "track":
				execute(t, database, `UPDATE tracks SET title = 'Someday' WHERE id = ?`, id)
			case "album":
				execute(t, database, `UPDATE albums SET title = 'Someday' WHERE id = ?`, id)
			case "artist":
				execute(t, database, `UPDATE artists SET name = 'Someday' WHERE id = ?`, id)
			case "genre":
				execute(t, database, `UPDATE genres SET name = 'Someday', name_normalized = 'someday' WHERE id = ?`, id)
			case "playlist":
				execute(t, database, `INSERT INTO playlists(id,user_id,name) VALUES ('near-playlist',?,'Sameday Nearby')`, auth.DefaultUserID)
				execute(t, database, `UPDATE playlists SET name = 'Someday' WHERE id = ?`, id)
			}
			result := search(t, handler, "someday")
			assertBestMatch(t, result, apigen.LibrarySearchResultType(kind), id)
			for _, group := range qualityGroups(result) {
				for _, item := range group {
					if item.Match == apigen.Corrected || item.Id == "sameday-track" || item.Id == "sameday-album" || item.Id == "sameday" || item.Id == "near-playlist" {
						t.Fatalf("strong %s permitted typo-only result: %+v", kind, item)
					}
				}
			}
		})
	}

	t.Run("related names do not gate fallback or become Best Match", func(t *testing.T) {
		handler, database := newSearchServer(t)
		execute(t, database, `UPDATE tracks SET title = 'Lantern' WHERE id = 'rain'`)
		execute(t, database, `UPDATE albums SET title = 'Lanterns' WHERE id = 'after-hours'`)
		result := search(t, handler, "lantarn")
		if result.BestMatch != nil {
			t.Fatalf("typo-only Best Match: %+v", result.BestMatch)
		}
		assertIDs(t, "corrected track", result.Tracks, "rain")
		assertIDs(t, "no related expansion", result.Albums)
		assertIDs(t, "no related expansion", result.Artists)
		if result.Tracks[0].Match != apigen.Corrected {
			t.Fatalf("track must be corrected: %+v", result.Tracks)
		}
	})

	t.Run("exact Album then weaker direct without Track expansion", func(t *testing.T) {
		handler, database := newSearchServer(t)
		execute(t, database, `UPDATE albums SET title = 'Nemo' WHERE id = 'aurora'`)
		execute(t, database, `UPDATE albums SET title = 'Nemo Collection' WHERE id = 'siberia'`)
		result := search(t, handler, "nemo")
		// Same primary name: the Track's Nightwish credit breaks the tie.
		assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "nemo")
		assertIDs(t, "album precedence", result.Albums, "aurora", "siberia")
		if !reflect.DeepEqual(matches(result.Albums), []string{"direct", "direct"}) {
			t.Fatalf("Album match labels: %v", matches(result.Albums))
		}
	})

	t.Run("independent categories never fill slots from Track relationships", func(t *testing.T) {
		handler, database := newSearchServer(t)
		// Seven qualifying Tracks must not add their nonmatching Albums or Artists.
		execute(t, database, `UPDATE tracks SET title = 'Echo Zero' WHERE id = 'echo-seven'`)
		execute(t, database, `UPDATE albums SET title = 'Echo' WHERE id = 'aurora'`)
		execute(t, database, `UPDATE artists SET name = 'Echo' WHERE id = 'northern-lights'`)
		result := search(t, handler, "echo")
		assertIDs(t, "only matching Album", result.Albums, "aurora")
		assertIDs(t, "only matching Artist", result.Artists, "northern-lights")
		assertBestMatch(t, result, apigen.LibrarySearchResultTypeArtist, "northern-lights")
		assertIDs(t, "Track cap after other-type Best Match", result.Tracks, "echo-five", "echo-four", "echo-one", "echo-six", "echo-three")
	})

	t.Run("playlist contents and radio stay outside search scope", func(t *testing.T) {
		handler, database := newSearchServer(t)
		execute(t, database, `INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES ('blue-vibes','nemo',0)`)
		execute(t, database, `INSERT INTO radio_stations(id,user_id,name,stream_url) VALUES ('nemo-radio',?,'Nemo','https://example.invalid/radio')`, auth.DefaultUserID)
		result := search(t, handler, "nemo")
		assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "nemo")
		assertIDs(t, "Playlist contents must not match", result.Playlists)
		for _, group := range qualityGroups(result) {
			for _, entry := range group {
				if entry.Id == "nemo-radio" {
					t.Fatal("Radio leaked into Library Search")
				}
			}
		}
	})

	t.Run("version preference cannot cross classes", func(t *testing.T) {
		handler, database := newSearchServer(t)
		execute(t, database, `UPDATE tracks SET title = 'Blue Live' WHERE id = 'blue-live-sky'`)
		result := search(t, handler, "blue live")
		assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "blue-live-sky")
		assertIDs(t, "exact ordinary title over qualified partial", result.Tracks, "blue-live-sky", "blue-moon-live")
	})

	t.Run("allowlisted versions beat ordinary words only within a class", func(t *testing.T) {
		handler, database := newSearchServer(t)
		for _, version := range []struct{ query, suffix string }{
			{"live", "(Live)"}, {"remix", " - Remix"}, {"acoustic", "(Acoustic)"},
			{"instrumental", " - Instrumental"}, {"demo", "(Demo)"},
			{"remaster", "(2011 Remaster)"}, {"remastered", " - Remastered"}, {"radio edit", "(Radio Edit)"},
		} {
			execute(t, database, `UPDATE tracks SET title = ? WHERE id = 'rain'`, "Signal Zulu "+version.suffix)
			execute(t, database, `UPDATE tracks SET title = ? WHERE id = 'u'`, "Signal "+version.query+" Aardvark")
			result := search(t, handler, "signal "+version.query)
			assertBestMatch(t, result, apigen.LibrarySearchResultTypeTrack, "rain")
			assertIDs(t, version.query+" ordinary word", result.Tracks, "rain", "u")
		}
		execute(t, database, `UPDATE tracks SET title = 'Signal Aardvark (Midnight)' WHERE id = 'rain'`)
		execute(t, database, `UPDATE tracks SET title = 'Signal Zulu' WHERE id = 'u'`)
		assertBestMatch(t, search(t, handler, "signal"), apigen.LibrarySearchResultTypeTrack, "rain")
	})

	t.Run("stable repeated identities and ordering", func(t *testing.T) {
		handler, _ := newSearchServer(t)
		for _, query := range []string{"twin", "same coin", "echo", "sunflowar"} {
			first := search(t, handler, query)
			for repeat := 0; repeat < 3; repeat++ {
				if got := search(t, handler, query); !reflect.DeepEqual(first, got) {
					t.Fatalf("query %q changed on repeat %d", query, repeat)
				}
			}
		}
	})

	t.Run("every edit-length boundary", func(t *testing.T) {
		handler, database := newSearchServer(t)
		execute(t, database, `UPDATE tracks SET title = 'Daybreak' WHERE id = 'sunflower'`)
		for _, query := range []string{"dyabreka", "daybreka"} {
			result := search(t, handler, query)
			assertIDs(t, query, result.Tracks, "sunflower")
			if result.BestMatch != nil || result.Tracks[0].Match != apigen.Corrected {
				t.Fatalf("eight-letter correction: %+v", result)
			}
		}
		for _, query := range []string{"b", "ux", "ran", "rxxn", "smoedya", "dxabrxka", "blidnign ligths", "blid"} {
			result := search(t, handler, query)
			if result.BestMatch != nil {
				t.Fatalf("out-of-budget %q has Best Match", query)
			}
			for kind, group := range qualityGroups(result) {
				assertIDs(t, fmt.Sprintf("%q %s", query, kind), group)
			}
		}
	})
}
