package librarysearch_test

import (
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/modules/librarysearch"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func qualityGroups(result apigen.LibrarySearchResponse) map[string][]apigen.LibrarySearchResult {
	return map[string][]apigen.LibrarySearchResult{
		"track": result.Tracks, "album": result.Albums, "artist": result.Artists,
		"genre": result.Genres, "playlist": result.Playlists,
	}
}

func qualityIdentity(result apigen.LibrarySearchResult) testutil.SearchIdentity {
	return testutil.SearchIdentity{Kind: string(result.Type), ID: result.Id}
}

// Visibility is Best Match OR the first five in the target's own category, not
// the first five flattened dialog rows. Targets are a predeclared acceptable set.
func qualityTargetVisible(result apigen.LibrarySearchResponse, targets []testutil.SearchIdentity) bool {
	if result.BestMatch != nil && slices.Contains(targets, qualityIdentity(*result.BestMatch)) {
		return true
	}
	for _, group := range qualityGroups(result) {
		for _, item := range group[:min(5, len(group))] {
			if slices.Contains(targets, qualityIdentity(item)) {
				return true
			}
		}
	}
	return false
}

func TestLibrarySearchQuality(t *testing.T) {
	database := testutil.OpenMigratedDB(t)
	testutil.SeedLibrarySearchQualityFixture(t, database)
	handler := chi.NewRouter()
	librarysearch.NewModule(database).RegisterRoutes(handler)
	cases := testutil.LibrarySearchQualityCases()
	if len(cases) < 100 {
		t.Fatalf("quality corpus has %d queries; need at least 100", len(cases))
	}
	type tally struct{ passed, total int }
	categories, metrics, categoryMetrics := map[string]tally{}, map[string]tally{}, map[string]tally{}
	thresholds := map[string]int{"best": 100, "words": 95, "typo": 90, "negative": 100}
	labels, queries := map[string]bool{}, map[string]bool{}
	violations := 0
	durations := make([]time.Duration, 0, len(cases))
	var total time.Duration
	for _, item := range cases {
		if item.Label == "" || labels[item.Label] || item.Category == "" || item.Query == "" {
			t.Fatalf("invalid/duplicate corpus label: %+v", item)
		}
		labels[item.Label], queries[item.Query] = true, true
		if item.Metric == "negative" && len(item.Targets) != 0 || item.Metric != "negative" && len(item.Targets) == 0 || item.Metric == "best" && len(item.Targets) != 1 {
			t.Fatalf("invalid target set: %+v", item)
		}
		for _, target := range item.Targets {
			if target.ID == "" || !slices.Contains([]string{"track", "album", "artist", "genre", "playlist"}, target.Kind) {
				t.Fatalf("invalid identity: %+v", target)
			}
		}
		started := time.Now()
		result := search(t, handler, item.Query)
		elapsed := time.Since(started)
		durations = append(durations, elapsed)
		total += elapsed
		seen := map[testutil.SearchIdentity]bool{}
		hard := func(format string, args ...any) {
			violations++
			t.Errorf("%s (%q): %s", item.Label, item.Query, fmt.Sprintf(format, args...))
		}
		if result.BestMatch != nil {
			group := qualityGroups(result)[string(result.BestMatch.Type)]
			if result.BestMatch.Match != apigen.Direct {
				hard("Best Match is not direct: %+v", result.BestMatch)
			}
			if len(group) == 0 || qualityIdentity(group[0]) != qualityIdentity(*result.BestMatch) {
				hard("Best Match must be first in its category: %+v", result.BestMatch)
			}
		}
		for kind, group := range qualityGroups(result) {
			if len(group) > 5 {
				hard("%s group has %d entries; cap is five", kind, len(group))
			}
			for _, entry := range group {
				identity := qualityIdentity(entry)
				if identity.Kind != kind || seen[identity] {
					hard("wrong group or duplicate identity: %+v", identity)
				}
				seen[identity] = true
				if result.BestMatch != nil && entry.Match == apigen.Corrected {
					hard("strong result supplemented with typo-only %s", entry.Id)
				}
				if item.Metric == "typo" && entry.Match != apigen.Corrected {
					hard("typo-only query returned non-corrected result: %+v", entry)
				}
				if entry.Match == apigen.Related {
					hard("relationship-only result returned in %s", kind)
				}
			}
		}
		passed := qualityTargetVisible(result, item.Targets)
		switch item.Metric {
		case "best":
			passed = result.BestMatch != nil && qualityIdentity(*result.BestMatch) == item.Targets[0]
		case "words":
		case "typo":
			if result.BestMatch != nil {
				hard("typo-only query received Best Match: %+v", result.BestMatch)
			}
		case "negative":
			passed = len(seen) == 0
			if !passed {
				hard("nonmatching query returned %v", seen)
			}
		default:
			t.Fatalf("unknown quality metric %q", item.Metric)
		}
		for _, group := range []struct {
			counts map[string]tally
			key    string
		}{{categories, item.Category}, {metrics, item.Metric}, {categoryMetrics, item.Category + "/" + item.Metric}} {
			count := group.counts[group.key]
			count.total++
			if passed {
				count.passed++
			}
			group.counts[group.key] = count
		}
		if !passed {
			t.Logf("MISS %s (%q): targets=%v best=%+v groups=%v", item.Label, item.Query, item.Targets, result.BestMatch, qualityGroups(result))
		}
	}
	if len(queries) < 100 {
		t.Errorf("only %d distinct query strings; need at least 100", len(queries))
	}
	keys := make([]string, 0, len(categories))
	for category := range categories {
		keys = append(keys, category)
	}
	sort.Strings(keys)
	for _, category := range keys {
		count := categories[category]
		t.Logf("CATEGORY %-16s %d/%d (%.2f%%)", category, count.passed, count.total, 100*float64(count.passed)/float64(count.total))
		for _, metric := range []string{"best", "words", "typo", "negative"} {
			count := categoryMetrics[category+"/"+metric]
			if count.total == 0 {
				continue
			}
			t.Logf("CATEGORY-METRIC %s/%s %d/%d, threshold %d%%", category, metric, count.passed, count.total, thresholds[metric])
			if count.passed*100 < count.total*thresholds[metric] {
				t.Errorf("%s/%s acceptance failed: %d/%d; need %d%%", category, metric, count.passed, count.total, thresholds[metric])
			}
		}
	}
	for _, metric := range []string{"best", "words", "typo", "negative"} {
		count := metrics[metric]
		t.Logf("METRIC %-8s %d/%d (%.2f%%), threshold %d%%", metric, count.passed, count.total, 100*float64(count.passed)/float64(count.total), thresholds[metric])
		if count.total == 0 || count.passed*100 < count.total*thresholds[metric] {
			t.Errorf("%s acceptance failed: %d/%d; need %d%%", metric, count.passed, count.total, thresholds[metric])
		}
	}
	t.Logf("CORPUS %d labels, %d distinct strings; hard violations %d", len(cases), len(queries), violations)
	// ponytail: fixture HTTP calls including decode, not network/browser latency; profile real load only when needed.
	slices.Sort(durations)
	n := len(durations)
	t.Logf("SEARCH count=%d total=%s p50=%s p95=%s", n, total, durations[(n*50+99)/100-1], durations[(n*95+99)/100-1])
}

func TestLibrarySearchQualityVisibilityDefinition(t *testing.T) {
	// Guard the evaluator itself: category position, typed identity, ambiguous
	// target sets and Best Match highlighting must not inflate or deflate quality.
	result := apigen.LibrarySearchResponse{
		Tracks: []apigen.LibrarySearchResult{{Type: "track", Id: "same"}},
		Playlists: []apigen.LibrarySearchResult{
			{Type: "playlist", Id: "one"}, {Type: "playlist", Id: "two"},
			{Type: "playlist", Id: "three"}, {Type: "playlist", Id: "four"},
			{Type: "playlist", Id: "five"}, {Type: "playlist", Id: "six"},
		},
	}
	for _, item := range []struct {
		targets []testutil.SearchIdentity
		visible bool
	}{
		{[]testutil.SearchIdentity{{Kind: "playlist", ID: "five"}}, true},
		{[]testutil.SearchIdentity{{Kind: "playlist", ID: "six"}}, false},
		{[]testutil.SearchIdentity{{Kind: "artist", ID: "same"}}, false},
		{[]testutil.SearchIdentity{{Kind: "track", ID: "absent"}, {Kind: "track", ID: "same"}}, true},
	} {
		if got := qualityTargetVisible(result, item.targets); got != item.visible {
			t.Errorf("targets=%v: visible=%t, want %t", item.targets, got, item.visible)
		}
	}
	result.BestMatch = &apigen.LibrarySearchResult{Type: "album", Id: "best", Match: apigen.Direct}
	if !qualityTargetVisible(result, []testutil.SearchIdentity{{Kind: "album", ID: "best"}}) {
		t.Fatal("Best Match must count as visible outside groups")
	}
}
