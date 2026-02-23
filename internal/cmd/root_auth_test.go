package cmd

import "testing"

func TestValidateAPIBaseURLSecurity(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https remote allowed", url: "https://api.prysm.sh/api/v1", wantErr: false},
		{name: "http localhost allowed", url: "http://localhost:8080/api/v1", wantErr: false},
		{name: "http ipv4 loopback allowed", url: "http://127.0.0.1:8080/api/v1", wantErr: false},
		{name: "http ipv6 loopback allowed", url: "http://[::1]:8080/api/v1", wantErr: false},
		{name: "http remote rejected", url: "http://api.prysm.sh/api/v1", wantErr: true},
		{name: "invalid url rejected", url: "://bad url", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAPIBaseURLSecurity(tc.url)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateInsecureTLSUsage(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		insecure bool
		wantErr  bool
	}{
		{name: "disabled insecure always ok", url: "https://api.prysm.sh/api/v1", insecure: false, wantErr: false},
		{name: "insecure localhost allowed", url: "https://localhost:8443/api/v1", insecure: true, wantErr: false},
		{name: "insecure loopback allowed", url: "https://127.0.0.1:8443/api/v1", insecure: true, wantErr: false},
		{name: "insecure remote rejected", url: "https://api.prysm.sh/api/v1", insecure: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInsecureTLSUsage(tc.url, tc.insecure)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
