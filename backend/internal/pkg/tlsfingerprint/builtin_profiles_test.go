package tlsfingerprint

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestBuiltinCodexCLIProfileNegotiatesTLS12Only(t *testing.T) {
	profile := BuiltinProfile("codex_cli")
	if profile == nil {
		t.Fatal("codex_cli builtin profile missing")
	}
	spec := buildClientHelloSpecFromProfile(profile)
	if spec.TLSVersMax != utls.VersionTLS12 || spec.TLSVersMin != utls.VersionTLS10 {
		t.Fatalf("unexpected version bounds %#x-%#x", spec.TLSVersMin, spec.TLSVersMax)
	}
	if len(spec.Extensions) != len(profile.Extensions) {
		t.Fatalf("expected %d extensions, got %d", len(profile.Extensions), len(spec.Extensions))
	}
	for _, ext := range spec.Extensions {
		switch ext.(type) {
		case *utls.ALPNExtension, *utls.SupportedVersionsExtension, *utls.KeyShareExtension:
			t.Fatalf("codex_cli must not send %T", ext)
		}
	}
	if spec.CipherSuites[0] != 255 || len(spec.CipherSuites) != 22 {
		t.Fatalf("unexpected cipher suites %v", spec.CipherSuites)
	}
}

func TestBuiltinTLS13ProfilesKeepTLS13Bounds(t *testing.T) {
	for _, name := range []string{"nodejs24", "nodejs22"} {
		spec := buildClientHelloSpecFromProfile(BuiltinProfile(name))
		if spec.TLSVersMax != utls.VersionTLS13 {
			t.Fatalf("%s max version %#x", name, spec.TLSVersMax)
		}
	}
	if spec := buildClientHelloSpecFromProfile(nil); spec.TLSVersMax != utls.VersionTLS13 {
		t.Fatalf("default max version %#x", spec.TLSVersMax)
	}
}
