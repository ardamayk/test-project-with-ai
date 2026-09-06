package managedimport_test

import (
	"bytes"
	"net/http"
	"reflect"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/testutil"
)

func TestManagedImportRetainsAllValidationIssues(t *testing.T) {
	router, _, _ := newMP3ManagedImportRouter(t)
	jobID := createManagedImportJob(t, router)
	fixture := bytes.ReplaceAll(testutil.StrictMP3Fixture(), []byte("TIT2"), []byte("XXXX"))
	fixture = bytes.ReplaceAll(fixture, []byte("TPE1"), []byte("YYYY"))
	response := testutil.ServeRequest(t, router, http.MethodPut, "/api/v1/imports/"+jobID+"/file", bytes.NewReader(fixture), map[string]string{"Content-Type": "application/octet-stream", "X-Import-Filename": "multiple.mp3"})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("upload status = %d: %s", response.Code, response.Body.String())
	}
	var failure struct {
		Issues []struct {
			Code   string
			Field  string
			Reason string
		}
	}
	testutil.DecodeJSON(t, response, &failure)
	fields := []string{}
	for _, issue := range failure.Issues {
		fields = append(fields, issue.Field)
	}
	if !reflect.DeepEqual(fields, []string{"TITLE", "ARTIST"}) {
		t.Fatalf("response issues = %+v", failure.Issues)
	}
	historyResponse := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/import-history", nil, nil)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history status = %d: %s", historyResponse.Code, historyResponse.Body.String())
	}
	var history struct {
		Items []struct {
			Files []struct {
				Issues []struct {
					Code   string
					Field  string
					Reason string
				}
			}
		}
	}
	testutil.DecodeJSON(t, historyResponse, &history)
	if len(history.Items) != 1 || len(history.Items[0].Files) != 1 || !reflect.DeepEqual(history.Items[0].Files[0].Issues, failure.Issues) {
		t.Fatalf("history lost validation issues: %s", historyResponse.Body.String())
	}
}

func TestManagedImportBatchKeepsIssuesAfterCancellation(t *testing.T) {
	router, _, _ := newMP3ManagedImportRouter(t)
	batch := createHistoryTestBatch(t, router)
	job := createHistoryTestJob(t, router, batch.ID, "00000000-0000-4000-8000-000000000123")
	fixture := bytes.ReplaceAll(testutil.StrictMP3Fixture(), []byte("TIT2"), []byte("XXXX"))
	fixture = bytes.ReplaceAll(fixture, []byte("TPE1"), []byte("YYYY"))
	uploadHistoryTestFile(t, router, job.ID, "multiple.mp3", fixture, http.StatusUnprocessableEntity)
	response := testutil.ServeRequest(t, router, http.MethodGet, "/api/v1/import-batches/"+batch.ID, nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("batch status = %d", response.Code)
	}
	testutil.DecodeJSON(t, response, &batch)
	if len(batch.Files) != 1 || len(batch.Files[0].Issues) != 2 {
		t.Fatalf("batch lost issues: %s", response.Body.String())
	}
	canceled := testutil.ServeRequest(t, router, http.MethodDelete, "/api/v1/import-batches/"+batch.ID, nil, nil)
	if canceled.Code != http.StatusNoContent {
		t.Fatalf("cancel status = %d: %s", canceled.Code, canceled.Body.String())
	}
	history := getImportHistory(t, router)
	if len(history.Items) != 1 || len(history.Items[0].Files) != 1 || !reflect.DeepEqual(history.Items[0].Files[0].Issues, batch.Files[0].Issues) {
		t.Fatalf("history lost issues: %+v", history)
	}
}
