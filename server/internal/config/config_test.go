package config

import "testing"

func TestLoadConfiguresManagedStoragePath(t *testing.T) {
	t.Setenv("MANAGED_STORAGE_PATH", "/srv/music-managed")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.ManagedStoragePath != "/srv/music-managed" {
		t.Fatalf("ManagedStoragePath = %q", configuration.ManagedStoragePath)
	}
}

func TestLoadConfiguresManagedStorageSafetyLimits(t *testing.T) {
	t.Setenv("MANAGED_STORAGE_RESERVE_BYTES", "4096")
	t.Setenv("MANAGED_IMPORT_FILE_LIMIT_BYTES", "8192")
	t.Setenv("MANAGED_IMPORT_BATCH_LIMIT_BYTES", "16384")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.ManagedStorageReserveBytes != 4096 {
		t.Fatalf("ManagedStorageReserveBytes = %d", configuration.ManagedStorageReserveBytes)
	}
	if configuration.ManagedImportFileLimitBytes != 8192 {
		t.Fatalf("ManagedImportFileLimitBytes = %d", configuration.ManagedImportFileLimitBytes)
	}
	if configuration.ManagedImportBatchLimitBytes != 16384 {
		t.Fatalf("ManagedImportBatchLimitBytes = %d", configuration.ManagedImportBatchLimitBytes)
	}
}

func TestLoadUsesTwoGiBManagedStorageReserveByDefault(t *testing.T) {
	t.Setenv("MANAGED_STORAGE_RESERVE_BYTES", "")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.ManagedStorageReserveBytes != 2*1024*1024*1024 {
		t.Fatalf("ManagedStorageReserveBytes = %d", configuration.ManagedStorageReserveBytes)
	}
}

func TestLoadUsesFourGiBManagedImportBatchLimitByDefault(t *testing.T) {
	t.Setenv("MANAGED_IMPORT_BATCH_LIMIT_BYTES", "")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if configuration.ManagedImportBatchLimitBytes != 4*1024*1024*1024 {
		t.Fatalf("ManagedImportBatchLimitBytes = %d", configuration.ManagedImportBatchLimitBytes)
	}
}

func TestLoadRejectsInvalidManagedStorageSafetyLimit(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "non-numeric file limit", key: "MANAGED_IMPORT_FILE_LIMIT_BYTES", value: "unlimited"},
		{name: "zero batch limit", key: "MANAGED_IMPORT_BATCH_LIMIT_BYTES", value: "0"},
		{name: "negative reserve", key: "MANAGED_STORAGE_RESERVE_BYTES", value: "-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() succeeded with %s=%q", test.key, test.value)
			}
		})
	}
}

func TestValidateServerAddressRequiresLoopback(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "IPv4 loopback", address: "127.0.0.1:8090"},
		{name: "IPv6 loopback", address: "[::1]:8090"},
		{name: "localhost", address: "localhost:8090"},
		{name: "wildcard", address: ":8090", wantErr: true},
		{name: "IPv4 wildcard", address: "0.0.0.0:8090", wantErr: true},
		{name: "LAN address", address: "192.168.1.20:8090", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateServerAddress(test.address)
			if test.wantErr && err == nil {
				t.Fatalf("ValidateServerAddress(%q) succeeded, want error", test.address)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidateServerAddress(%q) = %v", test.address, err)
			}
		})
	}
}
