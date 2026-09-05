package managedimport

import (
	"slices"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/identification"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
)

func TestMergeRecordingReplacesRecordingFieldsAndCompletesOnlyEmptyYear(t *testing.T) {
	tagged := library.NormalizedMediaMetadata{
		Title: "welcome to new york", Artists: []string{"taylor swift"}, AlbumArtists: []string{"Taylor Swift"},
		Album: "1989 (Taylor's Version)", Genres: []string{"Pop"},
		TrackPosition: library.MediaPosition{Number: 14}, DiscPosition: library.MediaPosition{Number: 1},
	}
	recording := identification.Recording{
		Title:   "Welcome to New York (Taylor’s version)",
		Artists: []identification.Credit{{Name: "Taylor Swift"}},
		Releases: []identification.Release{
			{Title: "1989 (Taylor’s version) (deluxe)", Status: "Official", Date: "2023-10-27", Year: 2023, ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Album"}},
			{Title: "1989 (Taylor’s version)", Status: "Official", Date: "2023-10-27", Year: 2023, ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Album"}},
		},
	}

	merged, changed := mergeRecording(tagged, recording)

	if merged.Title != "Welcome to New York (Taylor’s version)" || !slices.Equal(merged.Artists, []string{"Taylor Swift"}) {
		t.Fatalf("merged = %+v", merged)
	}
	if merged.Album != tagged.Album || merged.Genres[0] != "Pop" || merged.TrackPosition.Number != 14 {
		t.Fatalf("release-level fields changed: %+v", merged)
	}
	if merged.Year != 2023 || !slices.Equal(changed, []string{"title", "artists", "year"}) {
		t.Fatalf("year = %d changed = %v", merged.Year, changed)
	}

	tagged.Year = 2014
	merged, changed = mergeRecording(tagged, recording)
	if merged.Year != 2014 || slices.Contains(changed, "year") {
		t.Fatalf("tagged year must win: year = %d changed = %v", merged.Year, changed)
	}
}

func TestChooseReleaseMatchesTaggedAlbumAcrossApostrophes(t *testing.T) {
	releases := []identification.Release{
		{ID: "single", Title: "Welcome to New York", Status: "Official", Date: "2023-10-27", ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Single"}},
		{ID: "deluxe", Title: "1989 (Taylor’s version) (deluxe)", Status: "Official", Date: "2023-10-27", ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Album"}},
		{ID: "standard", Title: "1989 (Taylor’s version)", Status: "Official", Date: "2023-10-28", ReleaseGroup: identification.ReleaseGroup{PrimaryType: "Album"}},
	}

	if chosen, _ := chooseRelease(releases, "1989 (Taylor's Version) (Deluxe)"); chosen.ID != "deluxe" {
		t.Fatalf("chosen = %s, want the deluxe release despite the straight apostrophe", chosen.ID)
	}
	if chosen, _ := chooseRelease(releases, "Unknown Compilation"); chosen.ID != "deluxe" {
		t.Fatalf("chosen = %s, want the earliest official album", chosen.ID)
	}
	if _, found := chooseRelease(nil, "x"); found {
		t.Fatalf("no releases must not be found")
	}
}
