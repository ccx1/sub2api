package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ProxyProbeIdentity returns a stable identity for the egress-affecting
// fields. Metadata changes such as name/group/expiry do not invalidate a
// successful probe, while endpoint or credential changes do.
func ProxyProbeIdentity(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%s",
		strings.TrimSpace(proxy.Protocol), strings.TrimSpace(proxy.Host), proxy.Port,
		proxy.Username, proxy.Password, proxy.Status)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// ProxyLatencyMatchesProxy validates that cached probe data still belongs to
// the current proxy configuration. Legacy entries without an identity remain
// valid while their observation timestamp is at least as new as the proxy.
func ProxyLatencyMatchesProxy(info *ProxyLatencyInfo, proxy *Proxy) bool {
	if info == nil || proxy == nil || info.UpdatedAt.IsZero() {
		return false
	}
	if info.ProxyIdentity == "" {
		return !info.UpdatedAt.Before(proxy.UpdatedAt)
	}
	return info.ProxyIdentity == ProxyProbeIdentity(proxy)
}
