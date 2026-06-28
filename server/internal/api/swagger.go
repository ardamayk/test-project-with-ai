package api

import (
	_ "embed"
	"net/http"
)

//go:embed swagger.html
var swaggerHTML []byte

func SwaggerUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(swaggerHTML)
	}
}
