package admin

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func optionalStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// importedProxyUpdateInput preserves existing optional proxy settings when a
// legacy backup omits them. Country and group are applied only when their
// pointers are present, so old exports cannot accidentally clear metadata.
func importedProxyUpdateInput(item DataProxy, existing *service.Proxy, status string, proxyNameToID map[string]int64) *service.UpdateProxyInput {
	if existing == nil {
		return nil
	}
	if status == "" {
		status = existing.Status
	}
	expiresAt := existing.ExpiresAt
	if item.ExpiresAt != nil {
		v := time.Unix(*item.ExpiresAt, 0).UTC()
		expiresAt = &v
	}
	fallbackMode := strings.TrimSpace(item.FallbackMode)
	if fallbackMode == "" {
		fallbackMode = existing.FallbackMode
	}
	backupProxyID := existing.BackupProxyID
	if item.BackupProxyName != "" {
		if id, ok := proxyNameToID[item.BackupProxyName]; ok {
			backupProxyID = &id
		}
	}
	warnDays := existing.ExpiryWarnDays
	if item.ExpiryWarnDays > 0 {
		warnDays = item.ExpiryWarnDays
	}
	return &service.UpdateProxyInput{
		GroupID:          item.GroupID,
		ClearGroupID:     item.GroupID != nil && *item.GroupID == 0,
		CountryCode:      item.CountryCode,
		ClearCountryCode: item.CountryCode != nil && strings.TrimSpace(*item.CountryCode) == "",
		Name:             existing.Name,
		Protocol:         existing.Protocol,
		Host:             existing.Host,
		Port:             existing.Port,
		Username:         &existing.Username,
		Password:         &existing.Password,
		Status:           status,
		ExpiresAt:        expiresAt,
		FallbackMode:     fallbackMode,
		BackupProxyID:    backupProxyID,
		ExpiryWarnDays:   &warnDays,
	}
}
