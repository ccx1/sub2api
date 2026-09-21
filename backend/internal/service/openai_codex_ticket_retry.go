package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// CodexTicketRetryResult describes a manual retry request. Scheduled probes
// run asynchronously so the admin request does not wait for upstream timeouts.
type CodexTicketRetryResult struct {
	Scheduled int      `json:"scheduled"`
	Skipped   int      `json:"skipped"`
	Models    []string `json:"models"`
}

var (
	ErrCodexTicketRetryUnavailable = errors.New("codex ticket retry is unavailable")
	ErrCodexTicketRetryModel       = errors.New("codex ticket retry model is not configured")
)

type codexTicketManualRetryContextKey struct{}

func withCodexTicketManualRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexTicketManualRetryContextKey{}, true)
}

func isCodexTicketManualRetry(ctx context.Context) bool {
	value, _ := ctx.Value(codexTicketManualRetryContextKey{}).(bool)
	return value
}

// RetryOpenAICodexTicket schedules one probe for each configured model whose
// ticket is missing or within the configured refresh window. A healthy ticket
// is deliberately left untouched, and a manual retry only clears the model's
// in-memory backoff once so a failed click cannot create an endless loop.
func (s *OpenAIGatewayService) RetryOpenAICodexTicket(ctx context.Context, accountID int64, requestedModel string) (CodexTicketRetryResult, error) {
	result := CodexTicketRetryResult{Models: []string{}}
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return result, ErrCodexTicketRetryUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return result, err
	}
	if account == nil || account.Status != StatusActive || !OpenAICodexTicketAccountEnabled(account) ||
		!s.openAICodexTicketEnabledContext(ctx) {
		return result, ErrCodexTicketRetryUnavailable
	}
	cfg := s.openAICodexTicketConfigContext(ctx)
	models, err := codexTicketRetryModels(cfg, requestedModel)
	if err != nil {
		return result, err
	}
	now := time.Now()
	refreshBefore := time.Duration(cfg.RefreshBeforeSeconds) * time.Second
	token := account.GetCredential("access_token")
	if account.IsInDailyCooldown(now) || s.openAICodexTicketCooling(account, token) {
		result.Skipped = len(models)
		return result, nil
	}
	for _, model := range models {
		ticket := s.lookupOpenAICodexTicket(account, model)
		if ticket.usable(now, account, cfg) && !ticket.needsRefresh(now, refreshBefore) {
			result.Skipped++
			continue
		}
		// The button is an explicit, one-shot retry. The next failure starts a
		// fresh configured backoff sequence instead of inheriting the old one.
		if key := codexTicketBackoffKey(account, token, model); key != "" {
			s.openaiCodexTicketBackoff.Delete(key)
		}
		acc := *account
		acc.Extra = cloneAnyMap(account.Extra)
		acc.Credentials = cloneAnyMap(account.Credentials)
		result.Scheduled++
		result.Models = append(result.Models, model)
		manualCtx := withCodexTicketManualRetry(context.WithoutCancel(ctx))
		go s.probeOnceOpenAICodexTicket(manualCtx, &acc, model)
	}
	return result, nil
}

func codexTicketRetryModels(cfg config.OpenAICodexTicketConfig, requested string) ([]string, error) {
	requested = normalizeOpenAICodexTicketModel(requested)
	seen := make(map[string]struct{}, len(cfg.Models))
	models := make([]string, 0, len(cfg.Models))
	for _, raw := range cfg.Models {
		model := normalizeOpenAICodexTicketModel(raw)
		if model == "" || !codexTicketConfigGatesModel(cfg, model) {
			continue
		}
		if requested != "" && model != requested {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	if requested != "" && len(models) == 0 {
		return nil, ErrCodexTicketRetryModel
	}
	return models, nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
