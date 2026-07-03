package radio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

type fakeSearcher struct {
	result SearchResult
}

func (f fakeSearcher) Search(_ *http.Request) (SearchResultList, error) {
	return SearchResultList{Items: []SearchResult{f.result}, Total: 1}, nil
}

func (f fakeSearcher) LookupStation(_ *http.Request, stationUUID string) (SearchResult, error) {
	result := f.result
	result.StationUUID = stationUUID
	return result, nil
}

func setupHandlers(t *testing.T) (*Handlers, *Store) {
	t.Helper()
	db := testutil.OpenMigratedDB(t)
	store := NewStore(db)
	return NewHandlers(store, nil, nil), store
}

func withStationID(req *http.Request, stationID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("stationId", stationID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandlersCreateListPatchDeleteStation(t *testing.T) {
	h, _ := setupHandlers(t)

	createReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"name": "Radio Paradise",
		"streamUrl": "https://stream.radioparadise.com/mp3-192",
		"tags": ["rock"]
	}`))
	createRec := httptest.NewRecorder()
	h.CreateStation(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created Station
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	listRec := httptest.NewRecorder()
	h.ListStations(listRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	var list StationList
	if err := json.NewDecoder(listRec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	patchReq := withStationID(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"isFavorite": true}`)), created.ID)
	patchRec := httptest.NewRecorder()
	h.PatchStation(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d", patchRec.Code)
	}
	var patched Station
	if err := json.NewDecoder(patchRec.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if !patched.IsFavorite {
		t.Fatalf("patched = %+v", patched)
	}

	deleteReq := withStationID(httptest.NewRequest(http.MethodDelete, "/", nil), created.ID)
	deleteRec := httptest.NewRecorder()
	h.DeleteStation(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteRec.Code)
	}
}

func TestHandlersSearchAndImportStation(t *testing.T) {
	db := testutil.OpenMigratedDB(t)
	store := NewStore(db)
	searcher := fakeSearcher{result: SearchResult{
		StationUUID: "abc",
		Name:        "Radio Paradise",
		StreamURL:   "https://stream.radioparadise.com/mp3-192",
		HomepageURL: "https://radioparadise.com",
		Tags:        []string{"rock"},
		Codec:       "MP3",
		Bitrate:     192,
	}}
	h := NewHandlers(store, searcher, nil)

	searchRec := httptest.NewRecorder()
	h.SearchStations(searchRec, httptest.NewRequest(http.MethodGet, "/?q=paradise", nil))
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search status = %d", searchRec.Code)
	}

	importRec := httptest.NewRecorder()
	h.ImportStation(importRec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"stationUuid": "abc"}`)))
	if importRec.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", importRec.Code, importRec.Body.String())
	}
	var station Station
	if err := json.NewDecoder(importRec.Body).Decode(&station); err != nil {
		t.Fatal(err)
	}
	if station.Source != RadioBrowserSource || station.ExternalID != "abc" {
		t.Fatalf("station = %+v", station)
	}
}

func TestHandlersGetStationReturnsMetadata(t *testing.T) {
	h, store := setupHandlers(t)
	station, err := store.CreateStation(context.Background(), "00000000-0000-0000-0000-000000000001", StationCreate{
		Name:        "Radio Paradise",
		StreamURL:   "https://stream.radioparadise.com/mp3-192",
		HomepageURL: "https://radioparadise.com",
		FaviconURL:  "https://radioparadise.com/favicon.ico",
		Country:     "United States",
		Language:    "english",
		Tags:        []string{"rock", "eclectic"},
		Codec:       "MP3",
		Bitrate:     192,
		Source:      RadioBrowserSource,
		ExternalID:  "rp-main",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.GetStation(rec, withStationID(httptest.NewRequest(http.MethodGet, "/", nil), station.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body Station
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != station.ID || body.HomepageURL != "https://radioparadise.com" || body.Bitrate != 192 {
		t.Fatalf("station = %+v", body)
	}
}

func TestHandlersGetStationNotFound(t *testing.T) {
	h, _ := setupHandlers(t)
	rec := httptest.NewRecorder()
	h.GetStation(rec, withStationID(httptest.NewRequest(http.MethodGet, "/", nil), "missing"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlersGetNowPlaying(t *testing.T) {
	h, store := setupHandlers(t)
	station, err := store.CreateStation(context.Background(), "00000000-0000-0000-0000-000000000001", StationCreate{
		Name:      "Radio Paradise",
		StreamURL: "https://stream.radioparadise.com/mp3-192",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := NowPlaying{Title: "Title", Artist: "Artist", Raw: "Artist - Title"}
	if err := store.UpdateNowPlaying(context.Background(), station.ID, now); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.GetNowPlaying(rec, withStationID(httptest.NewRequest(http.MethodGet, "/", nil), station.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body NowPlaying
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Title != "Title" || body.Artist != "Artist" {
		t.Fatalf("now playing = %+v", body)
	}
}
