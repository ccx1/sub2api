package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const SettingKeyUpstreamErrorRetry = "upstream_error_retry"

// UpstreamErrorRetrySettings controls request-scoped retries for explicitly
// configured upstream errors. MaxRetries is the number of additional attempts.
type UpstreamErrorRetrySettings struct {
	Enabled    bool   `json:"enabled"`
	MaxRetries int    `json:"max_retries"`
	DelayMS    int    `json:"delay_ms"`
	Errors     string `json:"errors"`
}

func defaultUpstreamErrorRetrySettings() UpstreamErrorRetrySettings {
	return UpstreamErrorRetrySettings{MaxRetries: 3, DelayMS: 1000}
}

func normalizeUpstreamErrorRetrySettings(v UpstreamErrorRetrySettings) (UpstreamErrorRetrySettings, error) {
	bad := func(message string) (UpstreamErrorRetrySettings, error) {
		return v, infraerrors.BadRequest("INVALID_UPSTREAM_ERROR_RETRY", message)
	}
	if v.MaxRetries < 1 || v.MaxRetries > 10 {
		return bad("upstream_error_retry.max_retries must be between 1 and 10")
	}
	if v.DelayMS < 100 || v.DelayMS > 10000 {
		return bad("upstream_error_retry.delay_ms must be between 100 and 10000")
	}
	if len(v.Errors) > 32*1024 {
		return bad("upstream_error_retry.errors must not exceed 32 KiB")
	}
	seen := make(map[string]struct{})
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(v.Errors, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		key := strings.ToLower(line)
		if line == "" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if len(line) > 512 || strings.ContainsAny(line, "\r\x00") {
			return bad("each error rule must be at most 512 bytes and occupy one line")
		}
		if code, err := strconv.Atoi(line); err == nil && (code < 400 || code > 599 || code == http.StatusTooManyRequests) {
			return bad("HTTP error codes must be 400-599, excluding 429")
		}
		seen[key] = struct{}{}
		lines = append(lines, line)
	}
	if len(lines) > 100 {
		return bad("at most 100 upstream error rules are allowed")
	}
	if v.Enabled && len(lines) == 0 {
		return bad("at least one error rule is required when retry is enabled")
	}
	v.Errors = strings.Join(lines, "\n")
	return v, nil
}

func parseUpstreamErrorRetrySettings(raw string) UpstreamErrorRetrySettings {
	v := defaultUpstreamErrorRetrySettings()
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &v) != nil {
		return v
	}
	if normalized, err := normalizeUpstreamErrorRetrySettings(v); err == nil {
		return normalized
	}
	return defaultUpstreamErrorRetrySettings()
}

type upstreamErrorRetryPolicy struct {
	settings UpstreamErrorRetrySettings
	statuses map[int]struct{}
	keywords []string
}

func compileUpstreamErrorRetryPolicy(v UpstreamErrorRetrySettings) *upstreamErrorRetryPolicy {
	p := &upstreamErrorRetryPolicy{settings: v, statuses: make(map[int]struct{})}
	for _, raw := range strings.Split(v.Errors, "\n") {
		line := strings.ToLower(strings.TrimSpace(raw))
		if code, err := strconv.Atoi(line); err == nil {
			p.statuses[code] = struct{}{}
		} else if line != "" {
			p.keywords = append(p.keywords, line)
		}
	}
	return p
}

func (p *upstreamErrorRetryPolicy) matches(status int, body []byte) bool {
	if p == nil || !p.settings.Enabled || status < 400 || status > 599 || status == http.StatusTooManyRequests || upstreamErrorRetryHasUsage(body) {
		return false
	}
	if _, ok := p.statuses[status]; ok {
		return true
	}
	if len(p.keywords) == 0 {
		return false
	}
	texts := make([]string, 0, 8)
	if gjson.ValidBytes(body) {
		for _, path := range []string{"error.code", "error.type", "error.message", "response.error.code", "response.error.type", "response.error.message", "detail.code", "detail.message", "code", "type", "message"} {
			if value := gjson.GetBytes(body, path); value.Type == gjson.String {
				texts = append(texts, strings.ToLower(value.String()))
			}
		}
	} else {
		texts = append(texts, strings.ToLower(string(body)))
	}
	for _, text := range texts {
		for _, keyword := range p.keywords {
			if strings.Contains(text, keyword) {
				return true
			}
		}
	}
	return false
}

type cachedUpstreamErrorRetry struct {
	policy    *upstreamErrorRetryPolicy
	expiresAt time.Time
}

type upstreamErrorRetryContextKey struct{}

type upstreamErrorRetryState struct {
	clientCtx context.Context
	settings  *SettingService
	once      sync.Once
	policy    *upstreamErrorRetryPolicy
	mu        sync.Mutex
	used      int
}

func WithUpstreamErrorRetry(ctx context.Context, settings *SettingService) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if settings == nil || upstreamErrorRetryFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, upstreamErrorRetryContextKey{}, &upstreamErrorRetryState{clientCtx: ctx, settings: settings})
}

func upstreamErrorRetryFromContext(ctx context.Context) *upstreamErrorRetryState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(upstreamErrorRetryContextKey{}).(*upstreamErrorRetryState)
	return state
}

func (s *upstreamErrorRetryState) getPolicy() *upstreamErrorRetryPolicy {
	if s == nil {
		return nil
	}
	s.once.Do(func() { s.policy = s.settings.upstreamErrorRetryPolicy(s.clientCtx) })
	return s.policy
}

func (s *upstreamErrorRetryState) claim(status int, body []byte) (time.Duration, bool) {
	policy := s.getPolicy()
	if s == nil || policy == nil || s.clientCtx.Err() != nil || !policy.matches(status, body) {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used >= policy.settings.MaxRetries {
		return 0, false
	}
	s.used++
	slog.Info("gateway.upstream_error_retry", "upstream_status", status, "retry", s.used, "max_retries", policy.settings.MaxRetries)
	return time.Duration(policy.settings.DelayMS) * time.Millisecond, true
}

func (s *upstreamErrorRetryState) wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.clientCtx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.clientCtx.Done():
		return s.clientCtx.Err()
	case <-timer.C:
		return nil
	}
}

// TryConfiguredUpstreamErrorRetry claims the shared retry budget for a
// failover error. Existing credential, 429 and output/usage guards remain in
// control; callers should continue their existing failover loop on success.
func TryConfiguredUpstreamErrorRetry(ctx context.Context, failure *UpstreamFailoverError) (bool, error) {
	if ctx == nil || failure == nil || !failure.ShouldRetryNextAccount() || failure.IsCredentialFailure() || failure.StatusCode == http.StatusTooManyRequests || ctx.Err() != nil {
		return false, nil
	}
	state := upstreamErrorRetryFromContext(ctx)
	if delay, ok := state.claim(failure.StatusCode, failure.ResponseBody); ok {
		return true, state.wait(ctx, delay)
	}
	return false, nil
}

func marshalUpstreamErrorRetrySettings(v *UpstreamErrorRetrySettings) (string, error) {
	if v == nil {
		return "", infraerrors.BadRequest("INVALID_UPSTREAM_ERROR_RETRY", "settings are required")
	}
	normalized, err := normalizeUpstreamErrorRetrySettings(*v)
	if err != nil {
		return "", err
	}
	*v = normalized
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal upstream error retry: %w", err)
	}
	return string(data), nil
}

func upstreamErrorRetryHasUsage(body []byte) bool {
	for _, path := range []string{"usage.input_tokens", "usage.output_tokens", "usage.prompt_tokens", "usage.completion_tokens", "usage.total_tokens", "response.usage.input_tokens", "response.usage.output_tokens", "response.output.#", "usageMetadata.totalTokenCount"} {
		if gjson.GetBytes(body, path).Int() > 0 {
			return true
		}
	}
	return false
}

func (s *SettingService) upstreamErrorRetryPolicy(ctx context.Context) *upstreamErrorRetryPolicy {
	if s == nil || s.settingRepo == nil {
		return compileUpstreamErrorRetryPolicy(defaultUpstreamErrorRetrySettings())
	}
	if cached := s.upstreamErrorRetryCache.Load(); cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.policy
	}
	s.upstreamErrorRetryMu.Lock()
	defer s.upstreamErrorRetryMu.Unlock()
	if cached := s.upstreamErrorRetryCache.Load(); cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.policy
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyUpstreamErrorRetry)
	ttl := time.Minute
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		ttl = 5 * time.Second
		slog.Warn("upstream_error_retry.settings_read_failed", "error", err)
		raw = ""
	}
	policy := compileUpstreamErrorRetryPolicy(parseUpstreamErrorRetrySettings(raw))
	s.upstreamErrorRetryCache.Store(&cachedUpstreamErrorRetry{policy: policy, expiresAt: time.Now().Add(ttl)})
	return policy
}

func (s *SettingService) publishUpstreamErrorRetrySettings(v *UpstreamErrorRetrySettings) {
	if s == nil || v == nil {
		return
	}
	s.upstreamErrorRetryMu.Lock()
	defer s.upstreamErrorRetryMu.Unlock()
	s.upstreamErrorRetryCache.Store(&cachedUpstreamErrorRetry{policy: compileUpstreamErrorRetryPolicy(*v), expiresAt: time.Now().Add(time.Minute)})
}
