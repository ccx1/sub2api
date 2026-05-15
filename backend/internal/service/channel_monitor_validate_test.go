//go:build unit

package service

import "testing"

func TestValidateEndpoint_AllowsPublicHTTPAndHTTPSOrigins(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{
			name:     "http origin",
			endpoint: "http://8.8.8.8",
		},
		{
			name:     "https origin",
			endpoint: "https://8.8.8.8",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateEndpoint(tc.endpoint); err != nil {
				t.Fatalf("validateEndpoint(%q) returned error: %v", tc.endpoint, err)
			}
		})
	}
}

func TestValidateEndpoint_RejectsNonHTTPSSchemes(t *testing.T) {
	err := validateEndpoint("ftp://example.com")
	if err != ErrChannelMonitorEndpointScheme {
		t.Fatalf("expected ErrChannelMonitorEndpointScheme, got %v", err)
	}
}
