package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr         string
	DatabasePath string
	CORSOrigins  []string
	Version      string
}

func Load() Config {
	cfg := Config{
		Addr:         getEnv("SERVER_ADDR", ":8090"),
		DatabasePath: getEnv("DATABASE_PATH", "./data/app.db"),
		CORSOrigins:  []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		Version:      getEnv("APP_VERSION", "0.1.0"),
	}
	return cfg
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
