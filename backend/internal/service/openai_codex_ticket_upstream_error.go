package service

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type codexTicketProbeClassifiedError struct {
	err    error
	detail *CodexTicketUpstreamError
}

func (e *codexTicketProbeClassifiedError) Error() string { return e.err.Error() }
func (e *codexTicketProbeClassifiedError) Unwrap() error { return e.err }

func codexTicketProbeUpstreamError(err error) *CodexTicketUpstreamError {
	var classified *codexTicketProbeClassifiedError
	if errors.As(err, &classified) {
		return classified.detail
	}
	return nil
}

func classifyCodexTicketUpstreamError(data []byte, wireStatus int, retryAfter string) *CodexTicketUpstreamError {
	if !gjson.ValidBytes(data) {
		data = nil
	}
	root := gjson.ParseBytes(data)
	node, source := root.Get("response.error"), "response.error"
	if !node.IsObject() {
		node, source = root.Get("error"), "error"
	}
	if !node.IsObject() && root.Get("type").String() == "error" {
		node, source = root, "error_event"
	}
	if !node.IsObject() && wireStatus < 400 {
		return nil
	}
	d := &CodexTicketUpstreamError{WireHTTPStatus: wireStatus, EffectiveStatus: wireStatus, Scope: "request", ClassificationSource: source}
	d.Type, d.Code = codexTicketErrorToken(node.Get("type")), codexTicketErrorToken(node.Get("code"))
	if source == "error_event" {
		d.Type = codexTicketErrorToken(node.Get("error_type"))
	}
	if !node.IsObject() {
		d.ClassificationSource = "http_status"
	}
	if wireStatus < 400 {
		d.EffectiveStatus = http.StatusBadGateway
		for _, v := range []gjson.Result{node.Get("status_code"), node.Get("status"), root.Get("status_code"), root.Get("status")} {
			if status, ok := codexTicketBoundedInteger(v, 400, 599); ok {
				d.EffectiveStatus = int(status)
				break
			}
		}
	}
	classifyCodexTicketErrorScope(d)
	d.RetryAt = codexTicketErrorRetryAt(node, retryAfter)
	return d
}

func classifyCodexTicketErrorScope(d *CodexTicketUpstreamError) {
	switch {
	case d.Type == "usage_limit_reached" || d.Code == "usage_limit_reached":
		d.EffectiveStatus, d.Scope = http.StatusTooManyRequests, "credential"
	case d.Type == "authentication_error" || d.Code == "invalid_api_key" || d.Code == "unauthorized":
		d.EffectiveStatus, d.Scope = http.StatusUnauthorized, "credential"
	case d.Type == "permission_error" || d.Code == "permission_denied" || d.Code == "forbidden":
		d.EffectiveStatus, d.Scope = http.StatusForbidden, "credential"
	case d.Code == "model_at_capacity" || d.Code == "model_is_at_capacity":
		d.EffectiveStatus, d.Scope = http.StatusTooManyRequests, "model"
	case d.Type == "rate_limit_error" || d.Code == "rate_limit_exceeded":
		d.EffectiveStatus = http.StatusTooManyRequests
	case d.EffectiveStatus == 401 || d.EffectiveStatus == 403 || d.EffectiveStatus == 429:
		d.Scope = "credential"
	}
}

func codexTicketErrorToken(value gjson.Result) string {
	if value.Type != gjson.String {
		return ""
	}
	text := strings.TrimSpace(value.String())
	if len(text) > 96 {
		return ""
	}
	for _, c := range text {
		if c < 32 || c == 127 {
			return ""
		}
	}
	return strings.ToLower(text)
}

func codexTicketBoundedInteger(value gjson.Result, min, max int64) (int64, bool) {
	if value.Type == gjson.Number {
		if math.IsNaN(value.Num) || math.IsInf(value.Num, 0) || value.Num != math.Trunc(value.Num) || value.Num < float64(min) || value.Num > float64(max) {
			return 0, false
		}
		return int64(value.Num), true
	}
	if value.Type == gjson.String {
		parsed, err := strconv.ParseInt(value.String(), 10, 64)
		return parsed, err == nil && parsed >= min && parsed <= max
	}
	return 0, false
}

func codexTicketErrorRetryAt(node gjson.Result, retryAfter string) *time.Time {
	now := time.Now().UTC()
	const maxWait = int64(365 * 24 * 60 * 60)
	kind := codexTicketErrorToken(node.Get("type"))
	if kind == "" || kind == "error" {
		kind = codexTicketErrorToken(node.Get("error_type"))
	}
	if code := codexTicketErrorToken(node.Get("code")); code == "usage_limit_reached" || code == "rate_limit_exceeded" {
		kind = code
	}
	clean := map[string]any{"type": kind}
	if value, ok := codexTicketBoundedInteger(node.Get("resets_at"), now.Unix()+1, now.Unix()+maxWait); ok {
		clean["resets_at"] = value
	}
	if value, ok := codexTicketBoundedInteger(node.Get("resets_in_seconds"), 1, maxWait); ok {
		clean["resets_in_seconds"] = value
	}
	encoded, _ := json.Marshal(map[string]any{"error": clean})
	var until time.Time
	if reset := parseOpenAIRateLimitResetTime(encoded); reset != nil {
		until = time.Unix(*reset, 0).UTC()
	}
	if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil && seconds > 0 && seconds <= maxWait {
		if candidate := now.Add(time.Duration(seconds) * time.Second); candidate.After(until) {
			until = candidate
		}
	} else if candidate, err := http.ParseTime(retryAfter); err == nil && candidate.After(now) && candidate.Before(now.Add(time.Duration(maxWait)*time.Second)) && candidate.After(until) {
		until = candidate.UTC()
	}
	if until.IsZero() {
		return nil
	}
	return &until
}
