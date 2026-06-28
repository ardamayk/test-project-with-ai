package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ardam/navidrome-replacement/server/internal/api"
	"github.com/ardam/navidrome-replacement/server/internal/config"
	"github.com/ardam/navidrome-replacement/server/internal/db"
	"github.com/ardam/navidrome-replacement/server/internal/modules"
	"github.com/ardam/navidrome-replacement/server/internal/modules/preferences"
	"github.com/ardam/navidrome-replacement/server/internal/staticassets"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	ctx := context.Background()

	migrationsDir := filepath.Join("migrations")
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = filepath.Join("server", "migrations")
	}

	sqlDB, err := db.OpenAndMigrate(ctx, cfg.DatabasePath, migrationsDir)
	if err != nil {
		slog.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	prefStore := preferences.NewStore(sqlDB)
	prefModule := preferences.NewModule(prefStore)
	apiHandler := api.NewHandler(cfg)

	registry := modules.NewRegistry(prefModule)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/api/v1/health", apiHandler.GetHealth)
	r.Get("/api/v1/me", apiHandler.GetMe)
	registry.RegisterAll(r)

	openAPIPath := filepath.Join("..", "packages", "contracts", "openapi.yaml")
	if _, err := os.Stat(openAPIPath); os.IsNotExist(err) {
		openAPIPath = filepath.Join("packages", "contracts", "openapi.yaml")
	}
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, openAPIPath)
	})
	r.Get("/api/docs", api.SwaggerUIHandler())

	r.Handle("/docs/*", staticassets.DocsHandler())
	r.Mount("/", staticassets.WebHandler())

	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server listening", "addr", cfg.Addr, "modules", registry.Names())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
