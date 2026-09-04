package config

import (
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	DEFAULT_MANAGED_STORAGE_RESERVE_BYTES    int64 = 2 * 1024 * 1024 * 1024
	DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES  int64 = 2 * 1024 * 1024 * 1024
	DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES int64 = 4 * 1024 * 1024 * 1024
)

type Config struct {
	Addr                         string
	DatabasePath                 string
	CORSOrigins                  []string
	Version                      string
	ManagedStoragePath           string
	ManagedStorageReserveBytes   int64
	ManagedImportFileLimitBytes  int64
	ManagedImportBatchLimitBytes int64
	RadioBrowserBaseURL          string
}

type managedStorageSettings struct {
	reserveBytes    int64
	fileLimitBytes  int64
	batchLimitBytes int64
}

func Load() (Config, error) {
	managedStorage, err := loadManagedStorageSettings()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Addr:         getEnv("SERVER_ADDR", "127.0.0.1:8090"),
		DatabasePath: getEnv("DATABASE_PATH", "./data/app.db"),
		CORSOrigins:  []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		Version:      getEnv("APP_VERSION", "0.1.0"),
		ManagedStoragePath: getEnv(
			"MANAGED_STORAGE_PATH",
			"./data/managed",
		),
		ManagedStorageReserveBytes:   managedStorage.reserveBytes,
		ManagedImportFileLimitBytes:  managedStorage.fileLimitBytes,
		ManagedImportBatchLimitBytes: managedStorage.batchLimitBytes,
		RadioBrowserBaseURL: getEnv(
			"RADIO_BROWSER_BASE_URL",
			"https://de1.api.radio-browser.info",
		),
	}
	return cfg, nil
}

func loadManagedStorageSettings() (managedStorageSettings, error) {
	reserveBytes, err := getEnvInt64("MANAGED_STORAGE_RESERVE_BYTES", DEFAULT_MANAGED_STORAGE_RESERVE_BYTES, true)
	if err != nil {
		return managedStorageSettings{}, err
	}
	fileLimitBytes, err := getEnvInt64("MANAGED_IMPORT_FILE_LIMIT_BYTES", DEFAULT_MANAGED_IMPORT_FILE_LIMIT_BYTES, false)
	if err != nil {
		return managedStorageSettings{}, err
	}
	batchLimitBytes, err := getEnvInt64("MANAGED_IMPORT_BATCH_LIMIT_BYTES", DEFAULT_MANAGED_IMPORT_BATCH_LIMIT_BYTES, false)
	if err != nil {
		return managedStorageSettings{}, err
	}
	return managedStorageSettings{
		reserveBytes:    reserveBytes,
		fileLimitBytes:  fileLimitBytes,
		batchLimitBytes: batchLimitBytes,
	}, nil
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt64(key string, fallback int64, isZeroAllowed bool) (int64, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil || value < 0 || (!isZeroAllowed && value == 0) || value == math.MaxInt64 {
		description := "positive"
		if isZeroAllowed {
			description = "non-negative"
		}
		return 0, fmt.Errorf("%s must be a valid %s supported byte count", key, description)
	}
	return value, nil
}
