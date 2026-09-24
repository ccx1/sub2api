package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/modelquality"
)

type codexModelQualityJob struct {
	account                                    *Account
	ticket                                     *openAICodexTicket
	config                                     config.OpenAICodexTicketConfig
	policy                                     CodexModelQualityPolicy
	diagnostic                                 bool
	leaseExpiresAt                             time.Time
	scope, policyHash, proxyURL, lease, source string
}

type codexModelQualityBoundProxyReader interface {
	GetCodexModelQualityBoundProxy(context.Context, int64) (*Proxy, error)
}

func codexModelQualityHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *OpenAIGatewayService) prepareCodexModelQuality(ctx context.Context, account *Account, model string, policy CodexModelQualityPolicy) (*codexModelQualityJob, string) {
	if !policy.Enabled && !isCodexModelQualityDiagnostic(ctx) {
		return nil, "disabled"
	}
	if !openAICodexTicketHarvestEnabled(account) || account.IsInDailyCooldown(time.Now()) {
		return nil, "account_unavailable"
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if !codexTicketConfigGatesModel(cfg, model) {
		return nil, "model_not_configured"
	}
	account = cloneOpenAICodexTicketAccount(account)
	if !s.readCodexModelQualityProxy(ctx, account) {
		return nil, "proxy_unavailable"
	}
	ticket := s.lookupOpenAICodexTicketForConfig(account, model, cfg)
	if !ticket.usable(time.Now(), account, cfg) {
		return nil, "no_ticket"
	}
	if strings.TrimSpace(ticket.SessionID) == "" {
		return nil, "request_unavailable"
	}
	proxyURL := resolveAccountProxyURL(account)
	if ticket.Egress == "" || ticket.Egress != openAICodexTicketEgress(proxyURL) {
		return nil, "egress_changed"
	}
	job := &codexModelQualityJob{account: account, ticket: codexTicketLeaf(ticket), config: cfg, policy: policy, proxyURL: proxyURL,
		diagnostic: isCodexModelQualityDiagnostic(ctx)}
	job.policyHash = codexModelQualityPolicyHash(policy)
	job.scope = codexModelQualityScope(account, ticket, cfg)
	return job, ""
}

// 检测只能跟随已存在的出口，不能因轮询而分配容量、换绑或解除代理冷却。
func (s *OpenAIGatewayService) readCodexModelQualityProxy(ctx context.Context, account *Account) bool {
	if account.IsRandomProxy() {
		reader, ok := s.accountRepo.(codexModelQualityBoundProxyReader)
		if !ok {
			return false
		}
		proxy, err := reader.GetCodexModelQualityBoundProxy(ctx, account.ID)
		if err != nil || !codexTicketProxyAvailable(proxy) {
			return false
		}
		selection, err := ResolveAccountProxyPoolSelection(ctx, account, s.accountRepo)
		if err != nil || selection.Restricted && !slices.Contains(selection.IDs, proxy.ID) ||
			(!proxy.RegionFallback && validateProxyRegion(ctx, proxy, selection.CountryCode, s.accountRepo) != nil) {
			return false
		}
		id := proxy.ID
		account.ProxyID, account.Proxy = &id, proxy
		return true
	}
	if account.ProxyID != nil {
		reader, ok := s.accountRepo.(codexTicketProxyLoader)
		if !ok {
			return false
		}
		proxy, err := reader.GetCodexTicketProxy(ctx, *account.ProxyID)
		if err != nil || !codexTicketProxyAvailable(proxy) || proxy.ID != *account.ProxyID {
			return false
		}
		account.Proxy = proxy
	}
	country, err := account.ProxyRegionCountry()
	return err == nil && validateProxyRegion(ctx, account.Proxy, country, s.accountRepo) == nil
}

func codexModelQualityPolicyHash(policy CodexModelQualityPolicy) string {
	// Toggling ticket revocation changes only the side effect of a confirmed
	// result. It must not invalidate the result or lift the model pause.
	policy.QuarantineOnFailure = false
	return codexModelQualityHash(struct {
		Policy CodexModelQualityPolicy
		Bank   string
	}{policy, modelquality.BankVersion})
}

func codexModelQualityScope(account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig) string {
	if ticket == nil {
		return ""
	}
	// OAuth access/refresh tokens are transport credentials, not the ticket
	// under test. A new standby ticket is commonly harvested while the OAuth
	// access token rotates; including those tokens here made an unchanged
	// primary ticket look stale and caused the background checker to loop.
	// AccountBinding already fences account identity and the ticket's egress;
	// the selected ticket itself is fenced separately by CapturedAt and
	// credentialIdentity in the runtime checks.
	return codexModelQualityHash([]string{ticket.Model, ticket.Egress, openAICodexTicketAccountBinding(account), codexModelQualityHash(cfg)})
}

func codexModelQualityDisplayStatus(record *CodexModelQualityRecord, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, policy CodexModelQualityPolicy) CodexModelQualityStatus {
	status := record.Status
	status.ConsecutiveLowQuality = record.ConsecutiveLowQuality
	status.QualityPausedUntil = record.QualityPausedUntil
	if status.QualityPausedUntil != nil && !time.Now().Before(*status.QualityPausedUntil) {
		status.ConsecutiveLowQuality, status.QualityPausedUntil = 0, nil
	}
	if record.Policy != codexModelQualityPolicyHash(policy) {
		status.Status, status.Reason = "stale", "stale"
		return status
	}
	if ticket == nil || !ticket.usable(time.Now(), account, cfg) {
		if status.Status != "quarantined" {
			status.Status, status.Reason = "stale", "no_ticket"
		}
		return status
	}
	if record.Scope != codexModelQualityScope(account, ticket, cfg) {
		status.Status, status.Reason = "stale", "stale"
		return status
	}
	changed := status.TicketCapturedAt == nil || !ticket.CapturedAt.Equal(*status.TicketCapturedAt)
	status.BaselineReused = false
	if changed {
		next := ticket.CapturedAt.Add(time.Duration(policy.ReplacementCheckDelaySeconds) * time.Second)
		if status.Status != "passed" && status.NextCheckAt != nil && status.NextCheckAt.After(next) {
			next = *status.NextCheckAt
		}
		captured, expires := ticket.CapturedAt, ticket.effectiveExpiresAt(cfg)
		status.Status, status.Reason, status.NextCheckAt = "pending", "not_checked", &next
		status.TicketCapturedAt, status.TicketExpiresAt = &captured, &expires
		status.CapabilityScore, status.FingerprintCandidate, status.FingerprintProbability, status.FingerprintSimilarity = nil, "", nil, nil
		status.SampleCount, status.Requests, status.ModelIdentity = 0, 0, "unknown"
	}
	if status.Status == "running" && status.NextCheckAt != nil && time.Now().After(*status.NextCheckAt) {
		status.Status, status.Reason = "inconclusive", "timeout"
	}
	return status
}

func codexModelQualityBudget(now, expires time.Time, policy CodexModelQualityPolicy) time.Duration {
	remaining := expires.Sub(now)
	budget := min(time.Duration(policy.TimeoutSeconds)*time.Second, remaining*time.Duration(policy.MaxTTLPercent)/100)
	budget = min(budget, remaining-time.Duration(policy.ReserveSeconds)*time.Second)
	if budget < 2*time.Second {
		return 0
	}
	return budget
}

func (s *OpenAIGatewayService) codexModelQualityCurrent(ctx context.Context, job *codexModelQualityJob) bool {
	if job != nil && job.diagnostic {
		// Final and watchdog fences use fresh contexts, so carry the diagnostic
		// allowance from the job rather than relying on the worker parent context.
		ctx = withCodexModelQualityDiagnostic(ctx)
	}
	account := s.codexModelQualityCurrentAccount(ctx, job)
	if account == nil {
		return false
	}
	nowJob, _ := s.prepareCodexModelQuality(ctx, account, job.ticket.Model, job.policy)
	return nowJob != nil && nowJob.scope == job.scope && sameCodexTicket(nowJob.ticket, job.ticket)
}

func (s *OpenAIGatewayService) codexModelQualityCurrentAccount(ctx context.Context, job *codexModelQualityJob) *Account {
	if ctx.Err() != nil {
		return nil
	}
	if job.diagnostic {
		ctx = withCodexModelQualityDiagnostic(ctx)
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil || codexModelQualityPolicyHash(policy) != job.policyHash {
		return nil
	}
	account, err := s.accountRepo.GetByID(ctx, job.account.ID)
	if err != nil {
		return nil
	}
	return account
}

// 持有票据锁时只核对控制项和出口，不能再次查票递归获取同一把锁。
func (s *OpenAIGatewayService) codexModelQualityControlsCurrent(ctx context.Context, job *codexModelQualityJob) bool {
	account := s.codexModelQualityCurrentAccount(ctx, job)
	if !openAICodexTicketHarvestEnabled(account) || account.IsInDailyCooldown(time.Now()) {
		return false
	}
	account = cloneOpenAICodexTicketAccount(account)
	if !s.readCodexModelQualityProxy(ctx, account) {
		return false
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	return ctx.Err() == nil && job.ticket.usable(time.Now(), account, cfg) &&
		job.proxyURL == resolveAccountProxyURL(account) && codexModelQualityScope(account, job.ticket, cfg) == job.scope
}

func qualityStatusForJob(job *codexModelQualityJob) CodexModelQualityStatus {
	expires := job.ticket.effectiveExpiresAt(job.config)
	captured := job.ticket.CapturedAt
	return CodexModelQualityStatus{AccountID: job.account.ID, Model: job.ticket.Model, Status: "running",
		Reason: "checking", ModelIdentity: "unknown", Source: job.source, TicketCapturedAt: &captured, TicketExpiresAt: &expires}
}
