package testutil

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

var (
	contractOnce   sync.Once
	contractDoc    *openapi3.T
	contractRouter routers.Router
	contractErr    error
)

// Contract returns the embedded OpenAPI document that ships inside the Music
// Server binary. It is the same document served at /api/openapi.yaml, so tests
// validate against exactly what clients are generated from.
func Contract(t testing.TB) *openapi3.T {
	t.Helper()
	loadContract()
	if contractErr != nil {
		t.Fatalf("load embedded OpenAPI contract: %v", contractErr)
	}
	return contractDoc
}

func loadContract() {
	contractOnce.Do(func() {
		doc, err := apigen.GetSwagger()
		if err != nil {
			contractErr = err
			return
		}
		// Route matching is path-based; the documented development server URL
		// must not exclude httptest requests that carry no host.
		doc.Servers = nil
		if validateErr := doc.Validate(context.Background()); validateErr != nil {
			contractErr = validateErr
			return
		}
		router, err := legacy.NewRouter(doc)
		if err != nil {
			contractErr = err
			return
		}
		contractDoc = doc
		contractRouter = router
	})
}

// ContractRequest describes one HTTP exchange against the versioned contract.
type ContractRequest struct {
	Method  string
	Path    string
	Body    []byte
	Headers map[string]string
}

// ServeContractRequest serves one request and fails the test unless the
// request resolves to a documented operation and the response status, media
// type, and body satisfy the embedded OpenAPI contract.
func ServeContractRequest(t testing.TB, handler http.Handler, request ContractRequest) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if request.Body != nil {
		body = bytes.NewReader(request.Body)
	}
	httpRequest := httptest.NewRequest(request.Method, request.Path, body)
	for name, value := range request.Headers {
		httpRequest.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httpRequest)
	AssertContractResponse(t, httpRequest, response)
	return response
}

// AssertContractResponse validates an already-served response against the
// documented operation for its request.
func AssertContractResponse(t testing.TB, request *http.Request, response *httptest.ResponseRecorder) {
	t.Helper()
	loadContract()
	if contractErr != nil {
		t.Fatalf("load embedded OpenAPI contract: %v", contractErr)
	}
	route, pathParams, err := contractRouter.FindRoute(request)
	if err != nil {
		t.Fatalf("%s %s is not a documented operation: %v", request.Method, request.URL.Path, err)
	}
	validationErr := openapi3filter.ValidateResponse(request.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    request,
			PathParams: pathParams,
			Route:      route,
			Options:    &openapi3filter.Options{MultiError: true},
		},
		Status:  response.Code,
		Header:  response.Header(),
		Body:    io.NopCloser(bytes.NewReader(response.Body.Bytes())),
		Options: &openapi3filter.Options{IncludeResponseStatus: true, MultiError: true},
	})
	if validationErr != nil {
		t.Fatalf("%s %s -> %d violates the OpenAPI contract (%s):\n%v\nbody = %s",
			request.Method, request.URL.Path, response.Code, route.Operation.OperationID, validationErr, truncate(response.Body.String()))
	}
}

// AssertStructuredError checks the documented structured error shape: JSON
// media type, a stable machine-readable code separated from the human message,
// and the legacy error alias kept equal to the code.
func AssertStructuredError(t testing.TB, response *httptest.ResponseRecorder, status int, code string) ErrorBody {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, status, response.Body.String())
	}
	if mediaType := response.Header().Get("Content-Type"); !strings.HasPrefix(mediaType, "application/json") {
		t.Fatalf("error Content-Type = %q, want application/json", mediaType)
	}
	var failure ErrorBody
	DecodeJSON(t, response, &failure)
	if failure.Code != code {
		t.Fatalf("error code = %q, want %q (body = %s)", failure.Code, code, response.Body.String())
	}
	if failure.Error != failure.Code {
		t.Fatalf("error alias = %q, want it equal to code %q", failure.Error, failure.Code)
	}
	if strings.TrimSpace(failure.Message) == "" {
		t.Fatalf("error %q has no human-readable message", failure.Code)
	}
	return failure
}

// ErrorBody mirrors the documented ErrorResponse schema.
type ErrorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field"`
	Reason  string `json:"reason"`
}

func truncate(value string) string {
	const limit = 2000
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
