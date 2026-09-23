package service

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func codexTicketSignalsFromHeaders(headers http.Header) *CodexTicketSignals {
	s := &CodexTicketSignals{}
	if value := headers.Get("X-Codex-Safety-Buffering-Enabled"); value == "true" || value == "false" {
		parsed := value == "true"
		s.SafetyBufferingEnabled = &parsed
	}
	s.FasterModel = codexTicketSignalText(headers.Get("X-Codex-Safety-Buffering-Faster-Model"), s)
	s.ActiveLimit = codexTicketSignalText(headers.Get("X-Codex-Active-Limit"), s)
	s.PlanType = codexTicketSignalText(headers.Get("X-Codex-Plan-Type"), s)
	collectCodexTicketHeaderQuotas(s, headers)
	if !codexTicketHasSignals(s) {
		return nil
	}
	return s
}

func observeCodexTicketSignals(s *CodexTicketSignals, root gjson.Result, kind string) *CodexTicketSignals {
	if kind != "codex.response.metadata" && kind != "codex.rate_limits" && kind != "error" {
		return s
	}
	if s == nil {
		s = &CodexTicketSignals{}
	}
	if kind == "codex.response.metadata" || kind == "error" {
		headers := http.Header{}
		root.Get("headers").ForEach(func(name, value gjson.Result) bool {
			if codexTicketSignalHeaderAllowed(name.String()) && value.Type == gjson.String {
				headers.Set(name.String(), value.String())
			}
			return true
		})
		if update := codexTicketSignalsFromHeaders(headers); update != nil {
			s.merge(update)
		}
	} else {
		if plan := codexTicketSignalField(root, "plan_type", "planType"); plan.Type == gjson.String {
			s.PlanType = codexTicketSignalText(plan.String(), s)
		}
		if active := codexTicketSignalField(root, "metered_limit_name", "meteredLimitName"); active.Type == gjson.String && codexTicketQuotaID(active.String()) {
			s.ActiveLimit = active.String()
		}
		s.addQuotaWindows("", codexTicketSignalField(root, "rate_limits", "rateLimit"))
		s.addQuotaWindows("code_review:", codexTicketSignalField(root, "code_review_rate_limits", "codeReviewRateLimits"))
		additional := codexTicketSignalField(root, "additional_rate_limits", "additionalRateLimits")
		additional.ForEach(func(key, value gjson.Result) bool {
			id := key.String()
			if additional.IsArray() {
				id = codexTicketSignalField(value, "limit_name", "limitName").String()
			}
			if !codexTicketQuotaID(id) || len(id) > 54 {
				return true
			}
			rate := codexTicketSignalField(value, "rate_limit", "rateLimit")
			if !rate.IsObject() {
				rate = value
			}
			s.addQuotaWindows(id+":", rate)
			return true
		})
	}
	if !codexTicketHasSignals(s) {
		return nil
	}
	return s
}

func (s *CodexTicketSignals) merge(other *CodexTicketSignals) {
	if other.SafetyBufferingEnabled != nil {
		s.SafetyBufferingEnabled = other.SafetyBufferingEnabled
	}
	if other.FasterModel != "" {
		s.FasterModel = other.FasterModel
	}
	if other.ActiveLimit != "" {
		s.ActiveLimit = other.ActiveLimit
	}
	if other.PlanType != "" {
		s.PlanType = other.PlanType
	}
	s.Truncated = s.Truncated || other.Truncated
	for _, q := range other.Quota {
		s.storeQuota(q)
	}
}

func codexTicketSignalText(value string, s *CodexTicketSignals) string {
	for _, c := range value {
		if c < 32 || c == 127 {
			return ""
		}
	}
	if len(value) > 160 {
		s.Truncated = true
	}
	return codexTicketUTF8Prefix(value, 160)
}

func codexTicketQuotaID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.", c)) {
			return false
		}
	}
	return true
}

func (s *CodexTicketSignals) addQuota(id string, value gjson.Result) {
	if !value.IsObject() {
		return
	}
	q := CodexTicketQuotaSignal{ID: id}
	if raw := codexTicketSignalField(value, "used_percent", "usedPercent"); raw.Exists() {
		n, err := strconv.ParseFloat(raw.String(), 64)
		if (raw.Type == gjson.Number || raw.Type == gjson.String) && err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 100 {
			q.UsedPercent = &n
		}
	}
	if n, ok := codexTicketBoundedInteger(codexTicketSignalField(value, "reset_at", "resetAt"), 1, 253402300799); ok {
		timestamp := time.Unix(n, 0).UTC()
		q.ResetAt = &timestamp
	}
	if n, ok := codexTicketBoundedInteger(codexTicketSignalField(value, "reset_after_seconds", "resetAfterSeconds"), 0, 365*24*3600); ok {
		q.ResetAfterSeconds = &n
	}
	if n, ok := codexTicketBoundedInteger(codexTicketSignalField(value, "window_minutes", "windowMinutes"), 1, 365*24*60); ok {
		q.WindowMinutes = &n
	}
	if raw := codexTicketSignalField(value, "limit_reached", "limitReached"); raw.Type == gjson.True || raw.Type == gjson.False {
		n := raw.Bool()
		q.LimitReached = &n
	}
	if q.UsedPercent == nil && q.ResetAt == nil && q.ResetAfterSeconds == nil && q.WindowMinutes == nil && q.LimitReached == nil {
		return
	}
	s.storeQuota(q)
}

func (s *CodexTicketSignals) storeQuota(q CodexTicketQuotaSignal) {
	for i := range s.Quota {
		if s.Quota[i].ID == q.ID {
			s.Quota[i] = q
			return
		}
	}
	if len(s.Quota) >= 8 {
		s.Truncated = true
		return
	}
	s.Quota = append(s.Quota, q)
	sort.Slice(s.Quota, func(i, j int) bool { return s.Quota[i].ID < s.Quota[j].ID })
}

func (s *CodexTicketSignals) addQuotaWindows(prefix string, rate gjson.Result) {
	for _, window := range []string{"primary", "secondary"} {
		s.addQuota(prefix+window, rate.Get(window))
	}
	if reached := codexTicketSignalField(rate, "limit_reached", "limitReached"); reached.Type == gjson.True || reached.Type == gjson.False {
		for i := range s.Quota {
			if s.Quota[i].ID == prefix+"primary" || s.Quota[i].ID == prefix+"secondary" {
				value := reached.Bool()
				s.Quota[i].LimitReached = &value
			}
		}
	}
}

func codexTicketSignalField(root gjson.Result, names ...string) gjson.Result {
	for _, name := range names {
		if value := root.Get(name); value.Exists() && value.Type != gjson.Null {
			return value
		}
	}
	return gjson.Result{}
}

func codexTicketHasSignals(s *CodexTicketSignals) bool {
	return s != nil && (s.SafetyBufferingEnabled != nil || s.FasterModel != "" || s.ActiveLimit != "" || s.PlanType != "" || len(s.Quota) > 0 || s.Truncated)
}
