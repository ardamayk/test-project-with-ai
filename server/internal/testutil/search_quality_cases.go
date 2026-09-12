package testutil

import (
	"database/sql"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/auth"
)

// SearchIdentity includes type: unrelated entities may share the same ID.
type SearchIdentity struct {
	Kind string
	ID   string
}

// LibrarySearchQualityCase labels intent independently of search output. Targets
// are alternatives, not an ordered ranking: any visible identity satisfies intent.
// Metric is best (unique full name), words, typo, or negative; category partitions
// the samples so easy full names cannot conceal failures in another category.
type LibrarySearchQualityCase struct {
	Label    string
	Category string
	Metric   string
	Query    string
	Targets  []SearchIdentity
}

// SeedLibrarySearchQualityFixture extends, rather than replaces, the ranking
// fixture. Additional records cover the version allowlist and Unicode boundaries.
func SeedLibrarySearchQualityFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	SeedLibrarySearchFixture(t, database)
	run := func(statement string, args ...any) {
		t.Helper()
		if _, err := database.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, track := range [][2]string{
		{"signal", "Signal"}, {"signal-live", "Signal (Live)"},
		{"signal-remix", "Signal - Remix"}, {"signal-acoustic", "Signal (Acoustic)"},
		{"signal-instrumental", "Signal - Instrumental"}, {"signal-demo", "Signal (Demo)"},
		{"signal-remaster", "Signal (2011 Remaster)"}, {"signal-remastered", "Signal - Remastered"},
		{"signal-radio", "Signal (Radio Edit)"}, {"signal-midnight", "Signal (Midnight)"},
		{"live-forever", "Live Forever"}, {"daybreak", "Daybreak"},
	} {
		run(`INSERT INTO tracks(id,album_id,title,title_sort,artist_name,format,file_path) VALUES (?,'album-one',?,?,'Artist One','flac',?)`, track[0], track[1], track[1], "/managed/"+track[0]+".flac")
		run(`INSERT INTO track_artists VALUES (?,'artist-one',0)`, track[0])
	}
	for _, playlist := range [][2]string{
		{"isik", "Işık"}, {"izmir", "İzmir"}, {"islik", "ıslık"}, {"letter-i", "İ"},
		{"tokyo", "東京 １２3"},
	} {
		run(`INSERT INTO playlists(id,user_id,name) VALUES (?,?,?)`, playlist[0], auth.DefaultUserID, playlist[1])
	}
}

// LibrarySearchQualityCases is deliberately literal, not generated from fixture
// names, normalization helpers, or observed rankings. Labels describe user intent
// before evaluation. Hard ordering has a separate exact-outcome oracle, shared
// with LibrarySearchRankingCases; ambiguity here must not weaken that oracle.
func LibrarySearchQualityCases() []LibrarySearchQualityCase {
	targets := func(kind string, ids ...string) []SearchIdentity {
		out := make([]SearchIdentity, 0, len(ids))
		for _, id := range ids {
			out = append(out, SearchIdentity{Kind: kind, ID: id})
		}
		return out
	}
	track := func(ids ...string) []SearchIdentity { return targets("track", ids...) }
	album := func(ids ...string) []SearchIdentity { return targets("album", ids...) }
	artist := func(ids ...string) []SearchIdentity { return targets("artist", ids...) }
	genre := func(ids ...string) []SearchIdentity { return targets("genre", ids...) }
	playlist := func(ids ...string) []SearchIdentity { return targets("playlist", ids...) }
	return []LibrarySearchQualityCase{
		{"track-full-title", "full-name", "best", "Blinding Lights", track("blinding-lights")},
		{"artist-full-name", "full-name", "best", "the weeknd", artist("weeknd")},
		{"album-full-title", "full-name", "best", "After Hours", album("after-hours")},
		{"genre-full-name", "full-name", "best", "pop", genre("pop")},
		{"playlist-full-name", "full-name", "best", "blue vibes", playlist("blue-vibes")},
		{"track-tears", "full-name", "best", "Save Your Tears", track("save-your-tears")},
		{"album-siberia", "full-name", "best", "Siberia", album("siberia")},
		{"album-perdeler", "full-name", "best", "Perdeler", album("perdeler")},
		{"album-decades", "full-name", "best", "Decades", album("decades")},
		{"artist-nightwish", "full-name", "best", "Nightwish", artist("nightwish")},
		{"artist-track-credit-only", "full-name", "best", "guest hop", artist("guest-hop")},
		{"artist-exact-over-partial", "full-name", "best", "lights", artist("lights")},
		{"track-disables-artist-typo", "full-name", "best", "someday", track("someday")},
		{"track-nemo-album-context", "full-name", "best", "Nemo", track("nemo")},
		{"track-thunderstruck", "full-name", "best", "Thunderstruck", track("thunderstruck")},
		{"track-credit-halo", "full-name", "best", "Halo", track("halo")},
		{"track-toes", "full-name", "best", "Toes", track("toes")},
		{"album-lemonade", "full-name", "best", "Lemonade Blue", album("blue-album")},
		{"genre-blues", "full-name", "best", "Blues", genre("blues")},
		{"artist-northern-lights", "full-name", "best", "Northern Lights", artist("northern-lights")},
		{"ambiguous-recordings", "ambiguous", "words", "twin", track("twin-a", "twin-b")},
		{"ambiguous-identical-metadata", "ambiguous", "words", "same coin", track("same-coin-a", "same-coin-b")},
		{"credit-and-title", "combined-fields", "words", "weeknd blinding lights", track("blinding-lights", "blinding-lights-live")},
		{"album-and-title", "combined-fields", "words", "after hours blinding", track("blinding-lights", "blinding-lights-live")},
		{"genre-and-title", "combined-fields", "words", "pop blinding lights", track("blinding-lights", "blinding-lights-live")},
		{"track-and-album-artist", "combined-fields", "words", "beyonce halo", track("halo")},
		{"album-artist-and-title", "combined-fields", "words", "weeknd after hours", album("after-hours")},
		{"album-title-and-artist", "combined-fields", "words", "nightwish decades", album("decades")},
		{"track-credit-not-album-credit", "combined-fields", "words", "guest hop other", track("other-thing")},
		{"title-and-album-across-words", "combined-fields", "words", "toes siberia", track("toes")},
		{"two-genres-and-title", "combined-fields", "words", "blues r&b blue song", track("blue-song")},
		{"four-searchable-fields", "combined-fields", "words", "weeknd after hours pop blinding", track("blinding-lights", "blinding-lights-live")},
		{"title-and-credit-reversed", "word-order", "words", "lights blinding weeknd", track("blinding-lights", "blinding-lights-live")},
		{"title-words-reversed", "word-order", "words", "lights blinding", track("blinding-lights", "blinding-lights-live")},
		{"tears-first", "word-order", "words", "tears your save", track("save-your-tears")},
		{"album-words-reversed", "word-order", "words", "hours after", album("after-hours")},
		{"artist-words-reversed", "word-order", "words", "ferah sebnem", artist("sebnem")},
		{"track-then-album", "word-order", "words", "nemo decades nightwish", track("nemo")},
		{"version-first", "word-order", "words", "live blinding lights", track("blinding-lights-live")},
		{"playlist-words-reversed", "word-order", "words", "vibes blue", playlist("blue-vibes")},
		{"whole-word-over-prefix", "prefix", "words", "blue", track("blue-live-sky", "blue-song", "blue-moon-live")},
		{"one-letter-final-word", "prefix", "words", "blue m", track("blue-moon-live")},
		{"single-title-prefix", "prefix", "words", "blind", track("blinding-lights", "blinding-lights-live")},
		{"final-word-only-prefix", "prefix", "words", "blinding li", track("blinding-lights", "blinding-lights-live")},
		{"credit-and-final-prefix", "prefix", "words", "weeknd blind", track("blinding-lights", "blinding-lights-live")},
		{"album-prefix", "prefix", "words", "after h", album("after-hours")},
		{"artist-prefix", "prefix", "words", "nightw", artist("nightwish")},
		{"genre-prefix", "prefix", "words", "blu", genre("blues")},
		{"playlist-prefix", "prefix", "words", "blue v", playlist("blue-vibes")},
		{"punctuation-slashed", "punctuation", "best", "AC/DC", artist("acdc")},
		{"punctuation-spaced", "punctuation", "best", "ac dc", artist("acdc")},
		{"punctuation-compact", "punctuation", "best", "acdc", artist("acdc")},
		{"apostrophe-straight", "punctuation", "best", "don't stop", track("dont-stop")},
		{"apostrophe-curly", "punctuation", "best", "don’t stop", track("dont-stop")},
		{"apostrophe-omitted", "punctuation", "best", "dont stop", track("dont-stop")},
		{"case-and-redundant-space", "punctuation", "best", "  BLINDING   LIGHTS  ", track("blinding-lights")},
		{"genre-ampersand", "punctuation", "best", "R&B", genre("rnb")},
		{"genre-compact", "punctuation", "best", "rb", genre("rnb")},
		{"faithful-plain-artist", "accents", "best", "beyonce", artist("beyonce-plain")},
		{"faithful-accented-artist", "accents", "best", "Beyoncé", artist("beyonce-accent")},
		{"decomposed-accent-artist", "accents", "best", "Beyoncé", artist("beyonce-accent")},
		{"accented-credit-and-title", "accents", "words", "Beyoncé blue song", track("blue-song")},
		{"folded-credit-and-title", "accents", "words", "beyonce dont stop", track("dont-stop")},
		{"turkish-folded-full-name", "turkish", "best", "sebnem ferah", artist("sebnem")},
		{"turkish-faithful-full-name", "turkish", "best", "ŞEBNEM FERAH", artist("sebnem")},
		{"turkish-folded-given-name", "turkish", "words", "sebnem", artist("sebnem")},
		{"turkish-faithful-given-name", "turkish", "words", "Şebnem", artist("sebnem")},
		{"turkish-folded-track", "turkish", "best", "sil bastan", track("sil-bastan")},
		{"turkish-faithful-track", "turkish", "best", "Sil Baştan", track("sil-bastan")},
		{"turkish-dotless-folding", "turkish", "best", "isik", playlist("isik")},
		{"turkish-dotted-uppercase", "turkish", "best", "İZMİR", playlist("izmir")},
		{"turkish-dotless-query", "turkish", "best", "ızmır", playlist("izmir")},
		{"turkish-dotless-stored", "turkish", "best", "islik", playlist("islik")},
		{"turkish-uppercase-ascii-i", "turkish", "best", "I", playlist("letter-i")},
		{"turkish-uppercase-dotted-i", "turkish", "best", "İ", playlist("letter-i")},
		{"turkish-lowercase-dotless-i", "turkish", "best", "ı", playlist("letter-i")},
		{"turkish-lowercase-dotted-i", "turkish", "best", "i", playlist("letter-i")},
		{"non-latin-and-digits", "non-latin", "best", "東京 １２3", playlist("tokyo")},
		{"non-latin-word", "non-latin", "words", "東京", playlist("tokyo")},
		{"non-latin-digit-prefix", "non-latin", "words", "東京 １２", playlist("tokyo")},
		{"version-request-over-ordinary-word", "versions", "words", "blue live", track("blue-moon-live")},
		{"plain-over-live", "versions", "best", "blinding lights", track("blinding-lights")},
		{"full-live-title", "versions", "best", "Blinding Lights (Live)", track("blinding-lights-live")},
		{"plain-over-all-versions", "versions", "best", "Signal", track("signal")},
		{"allowlist-live", "versions", "best", "signal live", track("signal-live")},
		{"allowlist-remix-suffix", "versions", "best", "signal remix", track("signal-remix")},
		{"allowlist-acoustic", "versions", "best", "signal acoustic", track("signal-acoustic")},
		{"allowlist-instrumental-suffix", "versions", "best", "signal instrumental", track("signal-instrumental")},
		{"allowlist-demo", "versions", "best", "signal demo", track("signal-demo")},
		{"allowlist-dated-remaster", "versions", "words", "signal remaster", track("signal-remaster", "signal-remastered")},
		{"allowlist-remastered", "versions", "best", "signal remastered", track("signal-remastered")},
		{"allowlist-radio-edit", "versions", "best", "signal radio edit", track("signal-radio")},
		{"dated-remaster-full-title", "versions", "best", "signal 2011 remaster", track("signal-remaster")},
		{"ordinary-parenthetical-not-stripped", "versions", "best", "signal midnight", track("signal-midnight")},
		{"ordinary-live-title", "versions", "best", "Live Forever", track("live-forever")},
		{"short-one-exact-only", "short", "best", "u", track("u")},
		{"short-two-enables-prefix", "short", "words", "un", track("under-pressure")},
		{"short-two-word-prefix", "short", "words", "ab", track("ab-song")},
		{"short-three-word-prefix", "short", "words", "rai", track("rain")},
		{"short-three-full-word", "short", "words", "sky", track("sky-dance")},
		{"transposition-title", "typos", "typo", "blidning lights", track("blinding-lights", "blinding-lights-live")},
		{"different-words-correction-and-prefix", "typos", "typo", "weknd blind", track("blinding-lights", "blinding-lights-live")},
		{"two-one-edit-words", "typos", "typo", "blidning lihgts", track("blinding-lights", "blinding-lights-live")},
		{"cross-type-fallback", "typos", "typo", "sxmeday", track("someday")},
		{"four-characters-one-edit", "typos", "typo", "rian", track("rain")},
		{"seven-characters-one-edit", "typos", "typo", "somedya", track("someday")},
		{"nine-characters-one-edit", "typos", "typo", "sunflwoer", track("sunflower")},
		{"nine-characters-two-edits", "typos", "typo", "snuflwoer", track("sunflower")},
		{"fewer-edits-before-more", "typos", "typo", "sunflowar", track("sunflower")},
		{"eight-characters-two-edits", "typos", "typo", "dyabreka", track("daybreak")},
		{"insertion-error", "typos", "typo", "blindingg lights", track("blinding-lights", "blinding-lights-live")},
		{"deletion-error", "typos", "typo", "blnding lights", track("blinding-lights", "blinding-lights-live")},
		{"substitution-error", "typos", "typo", "blinding lughts", track("blinding-lights", "blinding-lights-live")},
		{"artist-correction", "typos", "typo", "nigthwish", artist("nightwish")},
		{"album-correction", "typos", "typo", "sibreia", album("siberia")},
		{"playlist-correction", "typos", "typo", "blue vibse", playlist("blue-vibes")},
		{"genre-correction", "typos", "typo", "bluse", genre("blues")},
		{"four-characters-substitution", "typos", "typo", "raim", track("rain")},
		{"unknown-word", "negative", "negative", "quasarzzzz", nil},
		{"extra-query-word", "negative", "negative", "blinding lights quasarzzzz", nil},
		{"no-stop-word-dropping", "negative", "negative", "blinding lights and", nil},
		{"internal-substring", "negative", "negative", "inding", nil},
		{"ordinary-spaces-not-deleted", "negative", "negative", "a b", nil},
		{"three-characters-no-correction", "negative", "negative", "ran", nil},
		{"seven-characters-two-edits-forbidden", "negative", "negative", "smoedya", nil},
		{"whole-query-three-edits-forbidden", "negative", "negative", "blidnign ligths", nil},
		{"same-word-correction-plus-prefix-forbidden", "negative", "negative", "blid", nil},
		{"earlier-word-prefix-forbidden", "negative", "negative", "blind lights", nil},
		{"one-character-prefix-forbidden", "negative", "negative", "b", nil},
		{"two-characters-no-correction", "negative", "negative", "ux", nil},
		{"fields-on-different-records", "negative", "negative", "nemo weeknd", nil},
		{"missing-version-word", "negative", "negative", "blinding lights acoustic", nil},
		{"unknown-parenthetical-not-removed", "negative", "negative", "signal midnight live", nil},
		{"no-transliteration", "negative", "negative", "tokyo", nil},
		{"no-translation", "negative", "negative", "Tokyo Japan", nil},
		{"foreign-user-playlist", "negative", "negative", "blue other", nil},
		{"file-path-out-of-scope", "negative", "negative", "managed flac", nil},
		{"no-alias-expansion", "negative", "negative", "Abel Tesfaye", nil},
		{"five-primary-results", "own-name", "words", "echo", track("echo-five", "echo-four", "echo-one", "echo-seven", "echo-six")},
		{"primary-title-prefix", "own-name", "words", "nem", track("nemo")},
		{"primary-title-not-credit-expansion", "own-name", "words", "other thing", track("other-thing")},
		{"primary-title-and-album", "own-name", "words", "halo blue", track("halo")},
	}
}
