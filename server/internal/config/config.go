package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string
	DatabasePath string
	CORSOrigins  []string
	Version      string
	MusicPaths   []string
}

func Load() Config {
	cfg := Config{
		Addr:         getEnv("SERVER_ADDR", ":8090"),
		DatabasePath: getEnv("DATABASE_PATH", "./data/app.db"),
		CORSOrigins:  []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		Version:      getEnv("APP_VERSION", "0.1.0"),
		MusicPaths:   parseMusicPaths(getEnv("MUSIC_PATHS", "./music")),
	}
	return cfg
}

func parseMusicPaths(raw string) []string {
	parts := strings.Split(raw, ",")
	paths := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
