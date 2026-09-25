package tlsfingerprint

// BuiltinProfile returns a complete, non-empty ClientHello profile for the
// supported compatibility presets. The names describe template families, not
// verified captures of the official Codex client or demonstrated risk controls.
func BuiltinProfile(name string) *Profile {
	var p Profile
	switch name {
	case "nodejs24":
		p = Profile{
			Name:         "Node.js 24 compatibility",
			CipherSuites: []uint16{0x1301, 0x1302, 0x1303, 0xc02b, 0xc02f, 0xc02c, 0xc030, 0xcca9, 0xcca8, 0xc009, 0xc013, 0xc00a, 0xc014, 0x009c, 0x009d, 0x002f, 0x0035},
			Curves:       []uint16{29, 23, 24}, PointFormats: []uint16{0},
			SignatureAlgorithms: []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601, 0x0201},
			ALPNProtocols:       []string{"http/1.1"}, SupportedVersions: []uint16{0x0304, 0x0303},
			KeyShareGroups: []uint16{29}, PSKModes: []uint16{1},
			Extensions: []uint16{0, 65037, 23, 65281, 10, 11, 35, 16, 5, 13, 18, 51, 45, 43},
		}
	case "nodejs22":
		p = Profile{
			Name:         "Node.js 22 compatibility",
			CipherSuites: []uint16{4866, 4867, 4865, 49199, 49195, 49200, 49196, 158, 49191, 103, 49192, 107, 163, 159, 52393, 52392, 52394, 49327, 49325, 49315, 49311, 49245, 49249, 49239, 49235, 162, 49326, 49324, 49314, 49310, 49244, 49248, 49238, 49234, 49188, 106, 49187, 64, 49162, 49172, 57, 56, 49161, 49171, 51, 50, 157, 49313, 49309, 49233, 156, 49312, 49308, 49232, 61, 60, 53, 47, 255},
			Curves:       []uint16{29, 23, 30, 25, 24, 256, 257, 258, 259, 260}, PointFormats: []uint16{0, 1, 2},
			SupportedVersions: []uint16{0x0304, 0x0303}, KeyShareGroups: []uint16{29}, PSKModes: []uint16{1},
			Extensions: []uint16{0, 11, 10, 35, 16, 22, 23, 13, 43, 45, 51},
		}
	case "codex_cli":
		// Codex macOS CLI 0.155.1 capture from the codex-tls-transport plugin:
		// TLS 1.2 only, no ALPN/supported_versions/key_share extensions.
		p = Profile{
			Name:         "Codex macOS CLI 0.155.1 compatibility",
			CipherSuites: []uint16{255, 49196, 49195, 49188, 49187, 49162, 49161, 49160, 49200, 49199, 49192, 49191, 49172, 49171, 49170, 157, 156, 61, 60, 53, 47, 10},
			Curves:       []uint16{23, 24, 25}, PointFormats: []uint16{0},
			SignatureAlgorithms: []uint16{1025, 513, 1281, 1537, 1027, 515, 1283, 1539},
			Extensions:          []uint16{0, 10, 11, 13, 5, 18, 23},
		}
	default:
		return nil
	}
	return &p
}
