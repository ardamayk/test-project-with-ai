package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

type Config struct {
	Addr                string
	DatabasePath        string
	CORSOrigins         []string
	Version             string
	MusicPaths          []string
	ManagedStoragePath  string
	RadioBrowserBaseURL string
}

func Load() Config {
	cfg := Config{
		Addr:         getEnv("SERVER_ADDR", "127.0.0.1:8090"),
		DatabasePath: getEnv("DATABASE_PATH", "./data/app.db"),
		CORSOrigins:  []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		Version:      getEnv("APP_VERSION", "0.1.0"),
		MusicPaths:   parseMusicPaths(getEnv("MUSIC_PATHS", "./music")),
		ManagedStoragePath: getEnv(
			"MANAGED_STORAGE_PATH",
			"./data/managed",
		),
		RadioBrowserBaseURL: getEnv(
			"RADIO_BROWSER_BASE_URL",
			"https://de1.api.radio-browser.info",
		),
	}
	return cfg
}

func ValidateServerAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid SERVER_ADDR %q: %w", address, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("SERVER_ADDR %q must bind to a loopback address while authentication is disabled", address)
	}
	return nil
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
