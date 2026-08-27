package config

import "testing"

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
