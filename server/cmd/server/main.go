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

	"github.com/ardam/navidrome-replacement/server/internal/api"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules"
	docsmodule "github.com/ardam/navidrome-replacement/server/internal/modules/docs"
	"github.com/ardam/navidrome-replacement/server/internal/modules/library"
	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playback"
	"github.com/ardam/navidrome-replacement/server/internal/modules/playlists"
	"github.com/ardam/navidrome-replacement/server/internal/modules/preferences"
	"github.com/ardam/navidrome-replacement/server/internal/modules/radio"
	"github.com/ardam/navidrome-replacement/server/internal/staticassets"
	"github.com/go-chi/chi/v5"
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

	if len(cfg.MusicPaths) == 0 {
		slog.Warn("MUSIC_PATHS is empty; library scan will fail until configured")
	} else {
		slog.Info("music paths configured", "paths", cfg.MusicPaths)
	}

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

	prefStore := preferences.NewStore(sqlDB)
	prefModule := preferences.NewModule(prefStore)
	libModule := library.NewModule(sqlDB, cfg)
	importModule := managedimport.NewModule(sqlDB, cfg, library.NewMediaInspector())
	trackAccess := libModule.TrackAccess()
	playModule := playback.NewModule(sqlDB, trackAccess)
	playlistModule := playlists.NewModule(sqlDB, trackAccess)
	radioModule := radio.NewModule(sqlDB, cfg)
	docsModule := docsmodule.NewModule()
	apiHandler := api.NewHandler(cfg)

	registry := modules.NewRegistry(libModule, importModule, playModule, playlistModule, radioModule, prefModule, docsModule)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromRemoteAddr)
	r.Use(middleware.Recoverer)
	r.Use(streamAwareTimeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Last-Event-ID", "Range", "X-Import-Filename", "X-Migration-Preview"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/api/v1/health", apiHandler.GetHealth)
	r.Get("/api/v1/me", apiHandler.GetMe)
	registry.RegisterAll(r)

	r.Mount("/", staticassets.WebHandler())

	server := newHTTPServer(cfg.Addr, r)

	if err := libModule.Start(ctx); err != nil {
		slog.Error("library startup failed", "error", err)
	}
	if err := importModule.Start(ctx); err != nil {
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

func streamAwareTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	timeoutMiddleware := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		timed := timeoutMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStreamPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}

func isStreamPath(path string) bool {
	if path == "/api/v1/playback/queue/events" || path == "/api/v1/library-migrations/preview" {
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
