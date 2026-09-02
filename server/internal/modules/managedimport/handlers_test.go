package managedimport

import (
	"net/http/httptest"
	"testing"
)

func TestImportFilenameDecodesNativeURLHeader(t *testing.T) {
	request := httptest.NewRequest("PUT", "/api/v1/imports/job-1/file", nil)
	request.Header.Set("X-Import-Filename", "Beyonc%C3%A9.flac")
	request.Header.Set("X-Import-Filename-Encoding", "url")

	filename, err := importFilename(request)
	if err != nil {
		t.Fatalf("decode filename: %v", err)
	}
	if filename != "Beyoncé.flac" {
		t.Fatalf("filename = %q, want Beyoncé.flac", filename)
	}
}
