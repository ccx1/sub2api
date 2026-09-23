package service

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

func codexTicketSignalHeaderAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "x-codex-safety-buffering-enabled", "x-codex-safety-buffering-faster-model", "x-codex-active-limit", "x-codex-plan-type":
		return true
	}
	_, _, ok := codexTicketQuotaHeader(name)
	return ok
}

func codexTicketQuotaHeader(name string) (string, string, bool) {
	name = strings.ToLower(name)
	if !strings.HasPrefix(name, "x-codex-") {
		return "", "", false
	}
	name = strings.TrimPrefix(name, "x-codex-")
	for _, field := range []string{"used-percent", "reset-at", "reset-after-seconds", "window-minutes", "limit-reached"} {
		if !strings.HasSuffix(name, "-"+field) {
			continue
		}
		base := strings.TrimSuffix(name, "-"+field)
		for _, window := range []string{"primary", "secondary"} {
			id := window
			if base != window {
				if !strings.HasSuffix(base, "-"+window) {
					continue
				}
				namespace := strings.TrimSuffix(base, "-"+window)
				if !codexTicketQuotaID(namespace) {
					continue
				}
				id = namespace + ":" + window
			}
			if len(id) > 64 {
				return "", "", false
			}
			return id, strings.ReplaceAll(field, "-", "_"), true
		}
	}
	return "", "", false
}

func collectCodexTicketHeaderQuotas(signals *CodexTicketSignals, headers http.Header) {
	values := map[string]map[string]any{}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		id, field, ok := codexTicketQuotaHeader(key)
		if !ok {
			continue
		}
		if values[id] == nil {
			if len(values) >= 8 {
				signals.Truncated = true
				continue
			}
			values[id] = map[string]any{}
		}
		value := headers.Get(key)
		if field == "limit_reached" {
			if value == "true" || value == "false" {
				values[id][field] = value == "true"
			}
		} else {
			values[id][field] = value
		}
	}
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		encoded, _ := json.Marshal(values[id])
		signals.addQuota(id, gjson.ParseBytes(encoded))
	}
}
