package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// NormalizeProxyCountryCode accepts an optional ISO 3166-1 alpha-2 code.
// Empty values mean that the proxy country is unknown and should be inferred
// from the latest egress probe when one is available.
func NormalizeProxyCountryCode(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return "", nil
	}
	if len(value) != 2 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' {
		return "", infraerrors.BadRequest("INVALID_PROXY_COUNTRY_CODE", "代理国家必须是两位国家代码")
	}
	return value, nil
}
