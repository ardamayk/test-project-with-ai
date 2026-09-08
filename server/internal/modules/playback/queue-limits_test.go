package playback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/auth"
)

func TestQueueSourceLimits(t *testing.T) {
	for _, field := range []string{"albumId", "albumTitle", "artistName", "playlistId", "name"} {
		t.Run(field, func(t *testing.T) {
			source := map[string]any{"kind": "album", "albumId": "a", "albumTitle": "Album", "artistName": "Artist"}
			if field == "playlistId" || field == "name" {
				source = map[string]any{"kind": "playlist", "playlistId": "p", "name": "Playlist"}
			}
			source[field] = strings.Repeat("é", MAX_QUEUE_SOURCE_STRING_LENGTH)
			assertSourceLimit(t, source, false)
			source[field] = strings.Repeat("é", MAX_QUEUE_SOURCE_STRING_LENGTH+1)
			assertSourceLimit(t, source, true)
		})
	}
	trackIDs := make([]string, MAX_QUEUE_SOURCE_BASED_ON)
	for index := range trackIDs {
		trackIDs[index] = "seed"
	}
	assertSourceLimit(t, QueueItemSource{Kind: "suggestion", BasedOn: trackIDs}, false)
	assertSourceLimit(t, QueueItemSource{Kind: "suggestion", BasedOn: append(trackIDs, "seed")}, true)
	assertSourceLimit(t, QueueItemSource{Kind: "suggestion", BasedOn: []string{strings.Repeat("s", MAX_QUEUE_SOURCE_STRING_LENGTH)}}, false)
	assertSourceLimit(t, QueueItemSource{Kind: "suggestion", BasedOn: []string{strings.Repeat("s", MAX_QUEUE_SOURCE_STRING_LENGTH+1)}}, true)
	for index := range trackIDs {
		trackIDs[index] = strings.Repeat("s", MAX_QUEUE_SOURCE_STRING_LENGTH)
	}
	assertSourceLimit(t, QueueItemSource{Kind: "suggestion", BasedOn: trackIDs}, true)
}

func assertSourceLimit(t *testing.T, source any, shouldReject bool) {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseQueueItemSource(data)
	if shouldReject && !errors.Is(err, ErrInvalidQueueSource) || !shouldReject && err != nil {
		t.Fatalf("source validation error = %v, shouldReject = %v", err, shouldReject)
	}
}

func TestQueueLimitsPreserveStateAndEvents(t *testing.T) {
	handlers, store, _, database := setupPlaybackHandlers(t)
	trackID := seedPlaybackTrack(t, database)
	trackIDs := make([]string, MAX_QUEUE_ITEMS)
	for index := range trackIDs {
		trackIDs[index] = trackID
	}
	ctx := context.Background()
	before, err := store.ReplaceQueue(ctx, auth.DefaultUserID, trackIDs, "0")
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := handlers.queueEvents.Subscribe(auth.DefaultUserID)
	defer unsubscribe()
	if _, replaceErr := store.ReplaceQueue(ctx, auth.DefaultUserID, append(trackIDs, trackID), before.Revision); !errors.Is(replaceErr, ErrQueueLimitExceeded) {
		t.Fatalf("replace error = %v", replaceErr)
	}
	if _, appendErr := store.AppendItem(ctx, auth.DefaultUserID, trackID, before.Revision); !errors.Is(appendErr, ErrQueueLimitExceeded) {
		t.Fatalf("append error = %v", appendErr)
	}
	body, err := json.Marshal(map[string]any{"trackIds": append(trackIDs, trackID), "revision": before.Revision})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handlers.ReplaceQueue(response, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(string(body))))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("replace status = %d: %s", response.Code, response.Body)
	}
	response = requestQueueSource(handlers, http.MethodPost, trackID, before.Revision, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("append status = %d: %s", response.Code, response.Body)
	}
	after, err := store.GetQueue(ctx, auth.DefaultUserID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected mutation changed queue: err = %v", err)
	}
	select {
	case event := <-events:
		t.Fatalf("rejected mutation published event: %+v", event)
	default:
	}
	// Removing an item must allow another append at the boundary.
	after, err = store.RemoveItem(ctx, auth.DefaultUserID, before.Items[0].ID, before.Revision)
	if err != nil {
		t.Fatal(err)
	}
	after, err = store.AppendItem(ctx, auth.DefaultUserID, trackID, after.Revision)
	if err != nil || len(after.Items) != MAX_QUEUE_ITEMS {
		t.Fatalf("append at boundary: err = %v, items = %d", err, len(after.Items))
	}
}

func TestQueueRequestBodyLimit(t *testing.T) {
	handlers, store, _, database := setupPlaybackHandlers(t)
	trackID := seedPlaybackTrack(t, database)
	events, unsubscribe := handlers.queueEvents.Subscribe(auth.DefaultUserID)
	defer unsubscribe()
	for _, handler := range []http.HandlerFunc{handlers.ReplaceQueue, handlers.AppendItem, handlers.ReorderQueue} {
		// A valid leading JSON object must not hide an oversized trailing body.
		body := `{"trackIds":["` + trackID + `"],"trackId":"` + trackID + `","itemIds":[],"revision":"0"}`
		body += strings.Repeat(" ", MAX_QUEUE_REQUEST_BYTES)
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("oversized request status = %d: %s", response.Code, response.Body)
		}
	}
	queue, err := store.GetQueue(context.Background(), auth.DefaultUserID)
	if err != nil || queue.Revision != "0" || queue.EventSequence != "0" || len(queue.Items) != 0 {
		t.Fatalf("oversized request changed queue: %+v, err = %v", queue, err)
	}
	select {
	case event := <-events:
		t.Fatalf("oversized request published event: %+v", event)
	default:
	}
	for _, size := range []int{MAX_QUEUE_REQUEST_BYTES, MAX_QUEUE_REQUEST_BYTES + 1} {
		body := "{}" + strings.Repeat(" ", size-2)
		var value any
		err := decodeQueueRequest(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), &value)
		if (err != nil) != (size > MAX_QUEUE_REQUEST_BYTES) {
			t.Fatalf("request size %d: %v", size, err)
		}
	}
}
