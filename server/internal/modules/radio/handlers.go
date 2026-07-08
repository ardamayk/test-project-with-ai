package radio

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/api/respond"
	"github.com/ardam/navidrome-replacement/server/internal/auth"
	"github.com/go-chi/chi/v5"
)

type Searcher interface {
	Search(r *http.Request) (SearchResultList, error)
	LookupStation(r *http.Request, stationUUID string) (SearchResult, error)
	Countries() (CatalogOptionList, error)
	Tags() (CatalogOptionList, error)
}

type Streamer interface {
	Stream(w http.ResponseWriter, r *http.Request, station Station)
}

type Handlers struct {
	store    *Store
	searcher Searcher
	streamer Streamer
	cache    *NowPlayingCache
}

func NewHandlers(store *Store, searcher Searcher, streamer Streamer, cache ...*NowPlayingCache) *Handlers {
	nowPlayingCache := NewNowPlayingCache()
	if len(cache) > 0 && cache[0] != nil {
		nowPlayingCache = cache[0]
	}
	return &Handlers{store: store, searcher: searcher, streamer: streamer, cache: nowPlayingCache}
}

func (h *Handlers) ListStations(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	list, err := h.store.ListStations(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, list)
}

func (h *Handlers) CreateStation(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body StationCreate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	station, err := h.store.CreateStation(r.Context(), userID, body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, station)
}

func (h *Handlers) GetStation(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	station, err := h.store.GetStation(r.Context(), userID, chi.URLParam(r, "stationId"))
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "radio station not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, station)
}

func (h *Handlers) PatchStation(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body StationPatch
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	station, err := h.store.UpdateStation(r.Context(), userID, chi.URLParam(r, "stationId"), body)
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "radio station not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, station)
}

func (h *Handlers) DeleteStation(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	err = h.store.DeleteStation(r.Context(), userID, chi.URLParam(r, "stationId"))
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "radio station not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) SearchStations(w http.ResponseWriter, r *http.Request) {
	if h.searcher == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio search unavailable")
		return
	}
	results, err := h.searcher.Search(r)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bad_gateway", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, results)
}

func (h *Handlers) ListCatalogCountries(w http.ResponseWriter, _ *http.Request) {
	if h.searcher == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio search unavailable")
		return
	}
	results, err := h.searcher.Countries()
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bad_gateway", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, results)
}

func (h *Handlers) ListCatalogTags(w http.ResponseWriter, _ *http.Request) {
	if h.searcher == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio search unavailable")
		return
	}
	results, err := h.searcher.Tags()
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bad_gateway", err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, results)
}

type importRequest struct {
	StationUUID string        `json:"stationUuid"`
	Result      *SearchResult `json:"result,omitempty"`
}

func (h *Handlers) ImportStation(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var body importRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	result, err := h.importResult(r, body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	station, err := h.store.ImportStation(r.Context(), userID, result)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	respond.JSON(w, http.StatusCreated, station)
}

func (h *Handlers) PreviewStation(w http.ResponseWriter, r *http.Request) {
	if h.streamer == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio streaming unavailable")
		return
	}
	var body importRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	result, err := h.importResult(r, body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.streamer.Stream(w, r, previewStation(result))
}

func (h *Handlers) StreamPreviewStation(w http.ResponseWriter, r *http.Request) {
	if h.streamer == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio streaming unavailable")
		return
	}
	if h.searcher == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio search unavailable")
		return
	}
	stationUUID := chi.URLParam(r, "stationUuid")
	result, err := h.searcher.LookupStation(r, stationUUID)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	h.streamer.Stream(w, r, previewStation(result))
}

func (h *Handlers) importResult(r *http.Request, body importRequest) (SearchResult, error) {
	if body.StationUUID != "" && h.searcher != nil {
		result, err := h.searcher.LookupStation(r, body.StationUUID)
		if err == nil {
			return result, nil
		}
		if body.Result == nil {
			return SearchResult{}, err
		}
	}
	if body.Result != nil {
		return *body.Result, nil
	}
	return SearchResult{}, errors.New("stationUuid or result is required")
}

func previewStation(result SearchResult) Station {
	return Station{
		ID:          "preview:" + result.StationUUID,
		Name:        result.Name,
		StreamURL:   result.StreamURL,
		HomepageURL: result.HomepageURL,
		FaviconURL:  result.FaviconURL,
		Country:     result.Country,
		Language:    result.Language,
		Tags:        result.Tags,
		Codec:       result.Codec,
		Bitrate:     result.Bitrate,
		Source:      RadioBrowserSource,
		ExternalID:  result.StationUUID,
	}
}

func (h *Handlers) StreamStation(w http.ResponseWriter, r *http.Request) {
	if h.streamer == nil {
		respond.Error(w, http.StatusServiceUnavailable, "unavailable", "radio streaming unavailable")
		return
	}
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	station, err := h.store.GetStation(r.Context(), userID, chi.URLParam(r, "stationId"))
	if errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "radio station not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	h.streamer.Stream(w, r, station)
}

func (h *Handlers) GetNowPlaying(w http.ResponseWriter, r *http.Request) {
	userID, err := auth.CurrentUserID(r)
	if err != nil {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	stationID := chi.URLParam(r, "stationId")
	if strings.HasPrefix(stationID, "preview:") {
		if now, ok := h.cache.Get(stationID); ok {
			respond.JSON(w, http.StatusOK, now)
			return
		}
		respond.JSON(w, http.StatusOK, NowPlaying{})
		return
	}
	if _, err := h.store.GetStation(r.Context(), userID, stationID); errors.Is(err, ErrNotFound) {
		respond.Error(w, http.StatusNotFound, "not_found", "radio station not found")
		return
	} else if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if now, ok := h.cache.Get(stationID); ok {
		respond.JSON(w, http.StatusOK, now)
		return
	}
	station, err := h.store.GetStation(r.Context(), userID, stationID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if station.LastNowPlaying != nil {
		respond.JSON(w, http.StatusOK, station.LastNowPlaying)
		return
	}
	respond.JSON(w, http.StatusOK, NowPlaying{})
}
