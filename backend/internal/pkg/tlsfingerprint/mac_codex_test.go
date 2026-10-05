package tlsfingerprint

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestNewMacCodexProfileIsStableAndIndependent(t *testing.T) {
	first := NewMacCodexProfile()
	second := NewMacCodexProfile()
	if first == nil || second == nil {
		t.Fatal("NewMacCodexProfile returned nil")
	}
	if first.Name != MacCodexProfileName || second.Name != MacCodexProfileName {
		t.Fatalf("profile names = %q, %q; want %q", first.Name, second.Name, MacCodexProfileName)
	}
	if len(first.Extensions) != 14 || first.Extensions[len(first.Extensions)-1] != 41 {
		t.Fatalf("unexpected macOS extension order: %#v", first.Extensions)
	}
	first.CipherSuites[0] = 0
	first.Extensions[0] = 999
	if second.CipherSuites[0] == 0 || second.Extensions[0] == 999 {
		t.Fatal("profiles share mutable slices")
	}
}

func TestMacCodexProfileBuildsTLS13HelloWithFinalPSK(t *testing.T) {
	profile := BuiltinProfile("mac_codex")
	if profile == nil {
		t.Fatal("mac_codex builtin profile missing")
	}
	spec := buildClientHelloSpecFromProfile(profile)
	if spec.TLSVersMax != utls.VersionTLS13 {
		t.Fatalf("TLS max = %#x, want TLS 1.3", spec.TLSVersMax)
	}
	if profile.ALPNProtocols[0] != "http/1.1" {
		t.Fatalf("ALPN = %#v, want [http/1.1]", profile.ALPNProtocols)
	}
	if len(spec.Extensions) == 0 {
		t.Fatal("mac_codex profile has no extensions")
	}
	if _, ok := spec.Extensions[len(spec.Extensions)-1].(*utls.UtlsPreSharedKeyExtension); !ok {
		t.Fatalf("last extension = %T, want *utls.UtlsPreSharedKeyExtension", spec.Extensions[len(spec.Extensions)-1])
	}
	var hasALPN, hasVersions, hasKeyShare bool
	for _, ext := range spec.Extensions {
		switch typed := ext.(type) {
		case *utls.ALPNExtension:
			hasALPN = len(typed.AlpnProtocols) == 1 && typed.AlpnProtocols[0] == "http/1.1"
		case *utls.SupportedVersionsExtension:
			hasVersions = true
		case *utls.KeyShareExtension:
			hasKeyShare = len(typed.KeyShares) == 1 && typed.KeyShares[0].Group == utls.X25519
		}
	}
	if !hasALPN || !hasVersions || !hasKeyShare {
		t.Fatalf("mac_codex extensions missing ALPN=%v versions=%v key_share=%v", hasALPN, hasVersions, hasKeyShare)
	}
}
