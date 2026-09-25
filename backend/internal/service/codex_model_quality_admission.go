package service

import (
	"context"
	"time"
)

// CodexModelQualityAdmissionEnabled reports whether a persisted quality
// failure is allowed to gate ticket admission. Callers that expose status to
// administrators can combine this with the ticket configuration's
// fail-closed setting to mirror the request path.
func (s *OpenAIGatewayService) CodexModelQualityAdmissionEnabled(ctx context.Context) bool {
	if s == nil {
		return false
	}
	if s.settingService == nil {
		return true
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return true
	}
	return policy.Enabled
}

// CodexModelQualityFailure reports a confirmed quality failure. Pending,
// running and inconclusive results deliberately remain schedulable and do not
// block normal traffic.
func CodexModelQualityFailure(status CodexModelQualityStatus) bool {
	// Numeric fingerprints are an auxiliary similarity signal, not an identity
	// proof.  A fingerprint mismatch remains visible as a suspected anomaly,
	// but cannot pause a model or revoke a ticket by itself.
	confirmed := codexQualityAnswerFailure(status.Reason) || status.Reason == "model_mismatch" ||
		status.Reason == "quarantine_persist_failed"
	return confirmed && (status.Status == "suspect" || status.Status == "quarantined")
}

// codexModelQualityCircuitPaused is the account/model quality circuit. A
// single bad ticket still gets revoked, while only a configured burst of bad
// tickets pauses new harvesting and background checks.
func (s *OpenAIGatewayService) codexModelQualityCircuitPaused(ctx context.Context, account *Account, model string) bool {
	if s == nil || s.accountRepo == nil || account == nil || account.ID <= 0 {
		return false
	}
	store, ok := s.accountRepo.(CodexModelQualityStore)
	if !ok {
		return false
	}
	if s.settingService == nil {
		return false
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return true
	}
	if !policy.Enabled {
		return false
	}
	record, err := store.LoadCodexModelQuality(ctx, account.ID, normalizeOpenAICodexTicketModel(model))
	if err != nil {
		return true
	}
	if record == nil || record.QualityPausedUntil == nil {
		return false
	}
	return time.Now().Before(*record.QualityPausedUntil)
}

func (s *OpenAIGatewayService) codexModelQualityPaused(ctx context.Context, account *Account, model string) bool {
	if s == nil || s.accountRepo == nil || account == nil || account.ID <= 0 {
		return false
	}
	store, ok := s.accountRepo.(CodexModelQualityStore)
	if !ok {
		return false
	}
	record, err := store.LoadCodexModelQuality(ctx, account.ID, normalizeOpenAICodexTicketModel(model))
	if err != nil || record == nil || !CodexModelQualityFailure(record.Status) {
		return false
	}
	if s.settingService == nil {
		return true
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return true
	}
	if !policy.Enabled || record.Policy != codexModelQualityPolicyHash(policy) {
		return false
	}
	// A quality result belongs to the ticket that was actually tested.  When a
	// replacement ticket is harvested, leave the model schedulable while its
	// replacement check is pending; the old failure must not quarantine every
	// future ticket indefinitely.  A missing ticket still blocks as before.
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	currentAccount := cloneOpenAICodexTicketAccount(account)
	if !s.readCodexModelQualityProxy(ctx, currentAccount) {
		return true
	}
	ticket := s.lookupOpenAICodexTicketForConfig(currentAccount, model, cfg)
	if ticket == nil {
		return true
	}
	if record.Status.TicketCapturedAt != nil && !ticket.lineageCapturedAt().Equal(*record.Status.TicketCapturedAt) {
		return false
	}
	return record.Scope == codexModelQualityScope(currentAccount, ticket, cfg)
}

// codexQualityAnswerFailure reports failures proven by wrong answers (capability
// recheck or administrator canary). These always revoke the ticket and count
// toward the low-quality circuit.
func codexQualityAnswerFailure(reason string) bool {
	return reason == "capability_failed" || reason == "canary_failed"
}
