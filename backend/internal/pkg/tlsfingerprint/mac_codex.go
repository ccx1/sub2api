package tlsfingerprint

import utls "github.com/refraction-networking/utls"

// MacCodexProfileName is the stable name of the built-in macOS Codex profile.
// This is an adapted TLS profile, not a verified capture of the native Codex
// client. Application identities remain scoped to the Codex account/window.
const MacCodexProfileName = "Mac Codex (macOS arm64)"

// NewMacCodexProfile returns a fresh macOS-style Codex TLS profile.
//
// The slices are copied so callers cannot mutate shared process defaults or
// affect another account/window that selected this profile.
func NewMacCodexProfile() *Profile {
	return &Profile{
		Name:         MacCodexProfileName,
		CipherSuites: append([]uint16(nil), defaultCipherSuites...),
		Curves:       []uint16{uint16(utls.X25519), uint16(utls.CurveP256), uint16(utls.CurveP384)},
		PointFormats: []uint16{0},
		SignatureAlgorithms: []uint16{
			0x0403, 0x0804, 0x0401,
			0x0503, 0x0805, 0x0501,
			0x0806, 0x0601, 0x0201,
		},
		ALPNProtocols:     []string{"http/1.1"},
		SupportedVersions: []uint16{utls.VersionTLS13, utls.VersionTLS12},
		KeyShareGroups:    []uint16{uint16(utls.X25519)},
		PSKModes:          []uint16{uint16(utls.PskModeDHE)},
		// Keep pre_shared_key (41) as the final extension as required by TLS 1.3.
		Extensions: []uint16{0, 23, 65281, 10, 11, 35, 16, 5, 13, 18, 51, 45, 43, 41},
	}
}
