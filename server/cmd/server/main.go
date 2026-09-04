package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, configErr := config.Load()
	if configErr != nil {
		slog.Error("configuration failed", "error", configErr)
		os.Exit(1)
	}
	if err := config.ValidateServerAddress(cfg.Addr); err != nil {
		slog.Error("unsafe server address", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	migrationsDir := filepath.Join("migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("server", "migrations")
	}

	sqlDB, err := db.OpenAndMigrate(ctx, cfg.DatabasePath, migrationsDir)
	if err != nil {
		slog.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			slog.Error("database close failed", "error", err)
		}
	}()

	assembled := newAssembledServer(cfg, sqlDB)
	registry := assembled.registry

	server := newHTTPServer(cfg.Addr, assembled.router)

	if err := assembled.importModule.Start(ctx); err != nil {
		slog.Error("Managed Import startup cleanup failed", "error", err)
	}

	go func() {
		slog.Info("server listening", "addr", cfg.Addr, "modules", registry.Names())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

func corsHandler(allowedOrigins []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "If-Match", "Last-Event-ID", "Range", "X-Import-Filename", "X-Import-Filename-Encoding", "X-Permanent-Delete", "X-Track-Replacement"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}

func streamAwareTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	timeoutMiddleware := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		timed := timeoutMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isUnboundedRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}

func isUnboundedRequest(request *http.Request) bool {
	if isStreamPath(request.URL.Path) {
		return true
	}
	if request.Method == http.MethodDelete &&
		request.Header.Get("X-Permanent-Delete") == "1" &&
		strings.HasPrefix(request.URL.Path, "/api/v1/library/tracks/") {
		return true
	}
	return request.Method == http.MethodPost &&
		request.Header.Get("X-Track-Replacement") == "1" &&
		strings.HasPrefix(request.URL.Path, "/api/v1/imports/") &&
		strings.HasSuffix(request.URL.Path, "/replacement")
}

func isStreamPath(path string) bool {
	if path == "/api/v1/playback/queue/events" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/imports/") && strings.HasSuffix(path, "/file") {
		return true
	}
	return strings.HasSuffix(path, "/stream") &&
		(strings.HasPrefix(path, "/api/v1/tracks/") ||
			strings.HasPrefix(path, "/api/v1/radio/stations/") ||
			strings.HasPrefix(path, "/api/v1/radio/preview/"))
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: 15 * time.Second,
		// Streaming audio responses can run for the full track duration.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
}
