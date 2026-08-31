package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ServeRequest(t testing.TB, handler http.Handler, method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, body)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func DecodeJSON(t testing.TB, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func AssertErrorCode(t testing.TB, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, status, response.Body.String())
	}
	var failure struct {
		Code string `json:"code"`
	}
	DecodeJSON(t, response, &failure)
	if failure.Code != code {
		t.Fatalf("error code = %q, want %q", failure.Code, code)
	}
}

func CreateResourceID(t testing.TB, handler http.Handler, path string) string {
	t.Helper()
	response := ServeRequest(t, handler, http.MethodPost, path, nil, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create resource status = %d, body = %s", response.Code, response.Body.String())
	}
	var resource struct {
		ID string `json:"id"`
	}
	DecodeJSON(t, response, &resource)
	if resource.ID == "" {
		t.Fatal("created resource ID is empty")
	}
	return resource.ID
}
