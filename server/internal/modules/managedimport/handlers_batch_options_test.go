package managedimport_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestCreateBatchAcceptsRecordingIdentificationChoice(t *testing.T) {
	module := managedimport.NewModule(testutil.OpenMigratedDB(t), config.Config{ManagedStoragePath: t.TempDir()}, library.NewMediaInspector())
	router := chi.NewRouter()
	module.RegisterRoutes(router)

	cases := map[string]struct {
		body string
		want bool
	}{
		"no body keeps the switch off": {"", false},
		"explicit true turns it on":    {`{"recordingIdentification":true}`, true},
		"explicit false keeps it off":  {`{"recordingIdentification":false}`, false},
		"empty object keeps it off":    {`{}`, false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			var body *strings.Reader
			headers := map[string]string{}
			if testCase.body != "" {
				body = strings.NewReader(testCase.body)
				headers["Content-Type"] = "application/json"
			}
			var reader io.Reader
			if body != nil {
				reader = body
			}
			response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/import-batches", reader, headers)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var batch managedimport.Batch
			testutil.DecodeJSON(t, response, &batch)
			if batch.RecordingIdentification != testCase.want {
				t.Fatalf("recordingIdentification = %v, want %v", batch.RecordingIdentification, testCase.want)
			}
		})
	}

	response := testutil.ServeRequest(t, router, http.MethodPost, "/api/v1/import-batches", strings.NewReader(`{"recordingIdentification":"yes"}`), map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400", response.Code)
	}
}
