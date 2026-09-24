package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type codexModelQualityDiagnosticContextKey struct{}

func withCodexModelQualityDiagnostic(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexModelQualityDiagnosticContextKey{}, true)
}

func isCodexModelQualityDiagnostic(ctx context.Context) bool {
	value, _ := ctx.Value(codexModelQualityDiagnosticContextKey{}).(bool)
	return value
}

// CodexModelQualityDiagnosticItem describes one administrator-triggered
// quality check. The actual probe runs asynchronously and can be observed via
// GetCodexModelQualityStatusSnapshot.
type CodexModelQualityDiagnosticItem struct {
	Model     string                   `json:"model"`
	Scheduled bool                     `json:"scheduled"`
	Reason    string                   `json:"reason"`
	Current   *CodexModelQualityStatus `json:"current,omitempty"`
}

// CodexModelQualityDiagnosticResult is intentionally a scheduling response.
// It does not expose ticket, cookie, proxy or probe contents.
type CodexModelQualityDiagnosticResult struct {
	AccountID int64                             `json:"account_id"`
	Items     []CodexModelQualityDiagnosticItem `json:"items"`
}

// DiagnoseCodexModelQuality schedules a bounded, administrator-triggered
// quality check for one or more configured Codex models. It reuses the normal
// model-quality worker and lease/CAS fencing; this entry point only changes
// the source label and selection scope. Diagnostic failures are persisted for
// review but do not quarantine the tested ticket.
func (s *OpenAIGatewayService) DiagnoseCodexModelQuality(ctx context.Context, account *Account, requested []string) (CodexModelQualityDiagnosticResult, error) {
	if err := validateCodexModelQualityDiagnosticModels(requested); err != nil {
		return CodexModelQualityDiagnosticResult{}, err
	}
	if s == nil || s.settingService == nil || s.accountRepo == nil {
		return CodexModelQualityDiagnosticResult{}, infraerrors.ServiceUnavailable("MODEL_QUALITY_UNAVAILABLE", "Model quality service unavailable")
	}
	if account == nil || account.ID <= 0 {
		return CodexModelQualityDiagnosticResult{}, infraerrors.BadRequest("INVALID_ACCOUNT", "Invalid account")
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return CodexModelQualityDiagnosticResult{}, err
	}
	policyHash := codexModelQualityPolicyHash(policy)
	// A manual diagnosis is an explicit operator action. It is available even
	// when the background checker is disabled. The context marker lets the
	// normal snapshot and policy hash remain unchanged while prepare/finish
	// enforce the diagnostic-only behavior.
	ctx = withCodexModelQualityDiagnostic(ctx)
	// A diagnostic is allowed to run while the background policy is disabled,
	// but its result must still belong to the persisted policy generation. Keep
	// the original hash for the ticket fence and disable quarantine for this
	// operator initiated run only.
	policy.Enabled = true
	policy.QuarantineOnFailure = false
	models := codexModelQualityDiagnosticModels(s.openAICodexTicketConfigForAccount(ctx, account).Models, requested)
	models = prioritizedCodexModelQualityModels(models, policy.ModelPriorities)
	if len(models) > 32 {
		return CodexModelQualityDiagnosticResult{}, infraerrors.BadRequest("TOO_MANY_MODELS", "Select at most 32 models")
	}
	result := CodexModelQualityDiagnosticResult{AccountID: account.ID, Items: make([]CodexModelQualityDiagnosticItem, 0, len(models))}
	current, _ := s.GetCodexModelQualityStatusSnapshot(ctx, account)
	currentByModel := make(map[string]*CodexModelQualityStatus, len(current))
	for i := range current {
		status := current[i]
		currentByModel[status.Model] = &status
	}
	for _, model := range models {
		if ctx.Err() != nil {
			break
		}
		scheduled := s.scheduleCodexModelQualityWithPolicyHash(ctx, account, model, "diagnostic", policy, policyHash)
		result.Items = append(result.Items, CodexModelQualityDiagnosticItem{Model: model,
			Scheduled: scheduled.Scheduled, Reason: scheduled.Reason, Current: currentByModel[model]})
	}
	if refreshed, err := s.GetCodexModelQualityStatusSnapshot(ctx, account); err == nil {
		for i := range result.Items {
			for j := range refreshed {
				if refreshed[j].Model == result.Items[i].Model {
					status := refreshed[j]
					result.Items[i].Current = &status
					break
				}
			}
		}
	}
	return result, nil
}

func codexModelQualityDiagnosticModels(configured, requested []string) []string {
	if len(requested) == 0 {
		requested = configured
	}
	seen := make(map[string]struct{}, len(requested))
	models := make([]string, 0, len(requested))
	for _, model := range requested {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func validateCodexModelQualityDiagnosticModels(models []string) error {
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 160 {
			return infraerrors.BadRequest("INVALID_MODEL", "Invalid model")
		}
		seen[model] = struct{}{}
	}
	if len(seen) > 32 {
		return infraerrors.BadRequest("TOO_MANY_MODELS", "Select at most 32 models")
	}
	return nil
}
