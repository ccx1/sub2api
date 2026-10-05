package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type qualityProbeContextKey struct{}

func WithQualityProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, qualityProbeContextKey{}, true)
}

type quality5xxRepository interface {
	ApplyQuality5xx(context.Context, int64) error
	RecordQualityRecoverySuccess(context.Context, int64, int) error
}

func qualityRequestError(body []byte) bool {
	if isGrokContentPolicyRejection(403, body) {
		return true
	}
	for _, path := range []string{"response.error.code", "error.code", "code", "response.error.type", "error.type"} {
		code := strings.ToLower(gjson.GetBytes(body, path).String())
		for _, marker := range []string{"invalid_prompt", "invalid_request", "content_policy", "permission", "authentication", "unauthorized", "invalid_api_key", "invalid_token", "cancel", "quota", "rate_limit"} {
			if strings.Contains(code, marker) {
				return true
			}
		}
	}
	return false
}

// Call only with the actual upstream HTTP status, before protocol mapping.
func QualityUpstreamFailureStatus(status int, body []byte, err error) int {
	if err != nil || status < 500 || status > 599 || qualityRequestError(body) {
		return 0
	}
	return status
}

// A semantic error with no server evidence is not an upstream 5xx.
func QualitySemanticFailureStatus(body []byte) int {
	if !gjson.ValidBytes(body) || qualityRequestError(body) {
		return 0
	}
	for _, path := range []string{"response.error.status", "response.error.status_code", "error.status", "error.status_code", "status_code", "status"} {
		value := gjson.GetBytes(body, path)
		if !value.Exists() {
			continue
		}
		if value.Int() == 0 {
			continue
		}
		if status := int(value.Int()); status >= 500 && status <= 599 {
			return status
		}
		return 0
	}
	for _, path := range []string{"response.error.code", "error.code", "code", "response.error.type", "error.type"} {
		switch strings.ToLower(gjson.GetBytes(body, path).String()) {
		case "server_error", "internal_server_error":
			return 500
		case "server_is_overloaded", "overloaded_error", "service_unavailable":
			return 503
		}
	}
	return 0
}

func (s *RateLimitService) ObserveQualityUpstreamFailure(ctx context.Context, account *Account, status int, body []byte, err error) {
	if s == nil || account == nil || account.Type != AccountTypeOAuth || account.ID <= 0 || ctx.Err() != nil || ctx.Value(qualityProbeContextKey{}) != nil || QualityUpstreamFailureStatus(status, body, err) == 0 {
		return
	}
	repo, ok := s.accountRepo.(quality5xxRepository)
	if !ok {
		return
	}
	actionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := repo.ApplyQuality5xx(actionCtx, account.ID); err != nil {
		slog.Warn("quality_5xx_immediate_failed", "account_id", account.ID, "error", err)
	}
}

func (s *RateLimitService) ObserveQualityUpstreamSuccess(ctx context.Context, account *Account) {
	if s == nil || account == nil || account.Type != AccountTypeOAuth || account.ID <= 0 || ctx.Err() != nil || ctx.Value(qualityProbeContextKey{}) != nil {
		return
	}
	repo, ok := s.accountRepo.(quality5xxRepository)
	if !ok {
		return
	}
	actionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := repo.RecordQualityRecoverySuccess(actionCtx, account.ID, account.Concurrency); err != nil {
		slog.Warn("quality_concurrency_recovery_failed", "account_id", account.ID, "error", err)
	}
}
