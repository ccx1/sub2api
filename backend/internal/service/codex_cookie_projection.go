package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func (t *openAICodexTicket) cookieProjectionVerified() bool {
	if t == nil || validateCodexCookieMode(normalizeCodexCookieMode(t.CookieMode), false) != nil {
		return false
	}
	return normalizeCodexCookieMode(t.CookieMode) == CodexCookiePreserve || t.CookiePolicyVerified
}

func (s *OpenAIGatewayService) codexCookieModeForRequest(ctx context.Context, account *Account) string {
	if account == nil || !account.IsOpenAIOAuthLike() {
		return CodexCookiePreserve
	}
	policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, codexRequestStrategyConnectionScope(ctx))
	if !enabled {
		return CodexCookiePreserve
	}
	return normalizeCodexCookieMode(policy.CookieMode)
}

// 原票继续由现有库存管理。过滤组合只在真实探测通过后作为本次发送的
// 不可变投影使用，证明不会落库，也不会因策略变化直接改写旧票。
func (s *OpenAIGatewayService) prepareCodexCookieTicket(ctx context.Context, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig) (*openAICodexTicket, error) {
	mode := s.codexCookieModeForRequest(ctx, account)
	if ticket == nil || !ticket.usesCookies() || mode == CodexCookiePreserve {
		return ticket, nil
	}
	// Legacy cookie tickets without a captured session cannot safely prove that
	// the synthetic probe and the subsequent business request use the same
	// upstream session. Wait for a fresh ticket instead of mixing identities.
	if strings.TrimSpace(ticket.SessionID) == "" {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	if !ticket.usable(time.Now(), account, cfg) || s.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket) {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || token == "" {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	key, err := s.codexCookieProjectionKey(account, ticket, cfg, mode, token)
	if err != nil {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	candidate := codexTicketLeaf(ticket)
	candidate.CookieMode, candidate.CookiePolicyVerified, candidate.CookiePolicyProofKey = mode, true, key
	candidate.Verified, candidate.VerificationSkipped = true, false
	if !s.openaiCodexCookieProjections.verified(key, time.Now()) {
		if err := s.verifyCodexCookieProjection(ctx, account, candidate, cfg, token, key); err != nil {
			return nil, err
		}
	}
	if ctx.Err() != nil || mode != s.codexCookieModeForRequest(ctx, account) ||
		!candidate.usable(time.Now(), account, cfg) || s.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket) {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	return candidate, nil
}

func (s *OpenAIGatewayService) codexCookieProjectionKey(account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, mode, token string) (string, error) {
	profile, err := resolveMode1TLSProfile(account)
	if err != nil {
		return "", err
	}
	value := struct {
		AccountID                                                 int64
		Model, Credential, Captured, Binding, Egress, Mode, Token string
		VerificationRounds                                        int
		VerifyBusiness                                            bool
		TLS                                                       any
	}{account.ID, ticket.Model, ticket.credentialIdentity(), ticket.CapturedAt.UTC().Format(time.RFC3339Nano),
		s.codexTicketBindingForConfig(account, cfg), openAICodexTicketEgress(resolveAccountProxyURL(account)), mode, token,
		max(1, cfg.BusinessVerificationRounds), config.CodexTicketBusinessVerificationEnabled(cfg), profile}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return openAICodexTicketEgress(string(encoded)), nil
}

func (s *OpenAIGatewayService) verifyCodexCookieProjection(ctx context.Context, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, token, key string) error {
	result := s.openaiCodexCookieProjections.flights.DoChan(key, func() (any, error) {
		if s.openaiCodexCookieProjections.verified(key, time.Now()) {
			return nil, nil
		}
		policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, codexRequestStrategyConnectionScope(ctx))
		if !enabled || normalizeCodexCookieMode(policy.CookieMode) != normalizeCodexCookieMode(ticket.CookieMode) {
			return nil, ErrOpenAICodexTicketUnavailable
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.ProbeTimeoutSeconds)*time.Second)
		defer cancel()
		if err := s.runCodexCookieProjectionProbe(probeCtx, account, ticket, cfg, token); err != nil {
			// 不回退到未经此模式验证的原始 Cookie，也不暴露上游敏感错误。
			return nil, errors.Join(ErrOpenAICodexTicketUnavailable, errors.New("cookie sending policy verification failed"))
		}
		if ticket.CookieMode != s.codexCookieModeForRequest(ctx, account) ||
			!ticket.usable(time.Now(), account, cfg) || s.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket) {
			return nil, ErrOpenAICodexTicketUnavailable
		}
		expires := earlierCodexTicketExpiry(ticket.hardExpiresAt(), time.Now().Add(time.Duration(cfg.TTLSeconds)*time.Second))
		s.openaiCodexCookieProjections.remember(key, expires)
		return nil, nil
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case completed := <-result:
		return completed.Err
	}
}

func (s *OpenAIGatewayService) runCodexCookieProjectionProbe(ctx context.Context, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, token string) error {
	sessionID := ticket.SessionID
	input := openAICodexTicketProbeInput{
		Account: account, Token: token, Model: ticket.Model, ProxyURL: resolveAccountProxyURL(account),
		Timeout: 60 * time.Second, Config: &cfg, SubscriptionTier: openAICodexTicketSubscriptionTier(account),
		SessionID: &sessionID, State: ticket.State, BusinessCredentialSnapshot: ticket,
		BusinessVerification: true, CheckControls: true, FreezeCredentials: true,
	}
	for round := 0; round < max(1, cfg.BusinessVerificationRounds); round++ {
		if _, _, err := s.probeOpenAICodexTicket(ctx, input); err != nil {
			return err
		}
		if sessionID != ticket.SessionID && ticket.SessionID != "" {
			return ErrOpenAICodexTicketUnavailable
		}
	}
	return nil
}

// 最终 HTTP 发送门禁；Cookie 全被过滤时仍保留 receipt、原票寿命及撤销检查。
func (s *OpenAIGatewayService) validateCodexCookieProjectionSend(req *http.Request, account *Account, receipt *openAICodexTicketReceipt, cfg config.OpenAICodexTicketConfig) error {
	if receipt == nil || !receipt.ticket.usesCookies() {
		return nil
	}
	mode := s.codexCookieModeForRequest(req.Context(), account)
	if mode != normalizeCodexCookieMode(receipt.ticket.CookieMode) {
		return ErrOpenAICodexTicketUnavailable
	}
	if mode == CodexCookiePreserve {
		return nil
	}
	ticket := &receipt.ticket
	if !ticket.usable(time.Now(), account, cfg) ||
		len(ticket.rawCookiesForURL(req.URL)) != len(ticket.Cookies) ||
		s.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket) {
		return ErrOpenAICodexTicketUnavailable
	}
	if err := s.validateCodexCookieProjectionProof(req.Context(), account, ticket, cfg); err != nil {
		return err
	}
	ticket.applyHeaders(req.Header)
	return nil
}

// validateCodexCookieProjectionProof binds the filtered snapshot to the
// current access token and ticket configuration. A receipt can outlive an
// OAuth token refresh or an admin policy update, so mode equality alone is
// insufficient for a new transport handshake.
func (s *OpenAIGatewayService) validateCodexCookieProjectionProof(ctx context.Context, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig) error {
	if ticket == nil || normalizeCodexCookieMode(ticket.CookieMode) == CodexCookiePreserve {
		return nil
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || token == "" {
		return ErrOpenAICodexTicketUnavailable
	}
	mode := normalizeCodexCookieMode(ticket.CookieMode)
	proofKey, err := s.codexCookieProjectionKey(account, ticket, cfg, mode, token)
	if err != nil || ticket.CookiePolicyProofKey == "" || ticket.CookiePolicyProofKey != proofKey || !s.openaiCodexCookieProjections.verified(proofKey, time.Now()) {
		return ErrOpenAICodexTicketUnavailable
	}
	return nil
}
