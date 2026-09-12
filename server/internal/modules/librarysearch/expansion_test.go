package librarysearch_test

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestSearchTotalAndExpansion(t *testing.T) {
	handler, database := newSearchServer(t)
	var expected []string
	for i := range 7 {
		id, name := fmt.Sprintf("expansion-%02d", i), fmt.Sprintf("Expansion %02d", i)
		expected = append(expected, id)
		// Shared IDs across types remain distinct matches; credits cannot multiply them.
		execute(t, database, `INSERT INTO artists(id,name,name_sort) VALUES (?,?,?)`, id, name, name)
		execute(t, database, `INSERT INTO albums(id,artist_id,title,title_sort) VALUES (?,?,?,?)`, id, id, name, name)
		execute(t, database, `INSERT INTO album_artists VALUES (?,?,0)`, id, id)
		execute(t, database, `INSERT INTO tracks(id,album_id,title,title_sort,artist_name,format,file_path) VALUES (?,?,?,?,?,'flac',?)`, id, id, name, name, name, "/managed/"+id+".flac")
		execute(t, database, `INSERT INTO track_artists VALUES (?,?,0)`, id, id)
		execute(t, database, `INSERT INTO genres(id,name,name_normalized) VALUES (?,?,?)`, id, name, name)
		execute(t, database, `INSERT INTO track_genres VALUES (?,?,0)`, id, id)
		execute(t, database, `INSERT INTO playlists(id,user_id,name) VALUES (?,?,?)`, id, auth.DefaultUserID, name)
	}

	for _, query := range []string{"expansion", "expnasion", "zzzzzzzzzz"} {
		t.Run(query, func(t *testing.T) {
			capped := search(t, handler, query)
			wantTotal := 35
			if query == "zzzzzzzzzz" {
				wantTotal = 0
			}
			if capped.Total == nil || *capped.Total != wantTotal {
				t.Fatalf("total = %v, want %d unique matches before cap", capped.Total, wantTotal)
			}
			if (capped.BestMatch != nil) != (query == "expansion") {
				t.Fatalf("unexpected Best Match: %+v", capped.BestMatch)
			}
			for _, all := range []string{"false", "true"} {
				response := testutil.ServeContractRequest(t, handler, testutil.ContractRequest{
					Method: http.MethodGet, Path: "/api/v1/library/search?q=" + url.QueryEscape(query) + "&all=" + all,
				})
				if response.Code != http.StatusOK {
					t.Fatalf("all=%s status = %d: %s", all, response.Code, response.Body.String())
				}
				var result apigen.LibrarySearchResponse
				testutil.DecodeJSON(t, response, &result)
				if result.Total == nil || *result.Total != wantTotal || !reflect.DeepEqual(result.BestMatch, capped.BestMatch) {
					t.Fatalf("all=%s changed total or Best Match: %+v", all, result)
				}
				if all == "false" && !reflect.DeepEqual(result, capped) {
					t.Fatal("all=false differs from default")
				}
				count := 0
				for kind, group := range qualityGroups(result) {
					wantIDs := expected
					if wantTotal == 0 {
						wantIDs = nil
					} else if all == "false" {
						wantIDs = expected[:5]
					}
					assertIDs(t, kind, group, wantIDs...)
					prefix := qualityGroups(capped)[kind]
					if !reflect.DeepEqual(group[:len(prefix)], prefix) {
						t.Fatalf("all=%s changed ranked prefix for %s", all, kind)
					}
					for _, entry := range group {
						wantMatch := apigen.Direct
						if query == "expnasion" {
							wantMatch = apigen.Corrected
						}
						if entry.Match != wantMatch {
							t.Fatalf("unexpected match label: %+v", entry)
						}
					}
					count += len(group)
				}
				if all == "true" && count != wantTotal {
					t.Fatalf("expanded count = %d, total = %d", count, wantTotal)
				}
			}
		})
	}
}

func TestSearchRejectsMalformedAll(t *testing.T) {
	handler, _ := newSearchServer(t)
	for _, value := range []string{"", "yes", "1", "0", "TRUE", "null", "true&all=false", "false&all=false", "%20true"} {
		t.Run(value, func(t *testing.T) {
			response := testutil.ServeRequest(t, handler, http.MethodGet, "/api/v1/library/search?q=echo&all="+value, nil, nil)
			testutil.AssertErrorCode(t, response, http.StatusBadRequest, "bad_request")
		})
	}
}
