package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type SharedPoolService struct {
	repo        SharedPoolRepository
	accounts    AccountRepository
	groups      GroupRepository
	admin       AdminService
	protection  *AntiDegradeService
	earnings    SharedPoolEarningsRepository
	invalidator TokenCacheInvalidator
}

func NewSharedPoolService(repo SharedPoolRepository, accounts AccountRepository, groups GroupRepository, admin AdminService,
	protection *AntiDegradeService, earnings SharedPoolEarningsRepository, invalidator TokenCacheInvalidator) *SharedPoolService {
	return &SharedPoolService{repo: repo, accounts: accounts, groups: groups, admin: admin, protection: protection, earnings: earnings, invalidator: invalidator}
}

func (s *SharedPoolService) OwnedAccount(ctx context.Context, ownerID, id int64) (*SharedPoolAccountRecord, *Account, error) {
	record, err := s.repo.GetSharedAccount(ctx, ownerID, id)
	if err != nil {
		return nil, nil, err
	}
	account, err := s.accounts.GetByID(ctx, id)
	return record, account, err
}

func (s *SharedPoolService) Create(ctx context.Context, userID int64, in SharedPoolAccountInput) (*SharedPoolAccountView, error) {
	if in.Enabled && !in.DispatchConsent {
		return nil, infraerrors.BadRequest("SHARED_DISPATCH_CONSENT_REQUIRED", "启用账号前，请明确授权账号参与平台调度")
	}
	dailyCooldown, err := normalizeSharedDailyCooldown(in.DailyCooldown)
	if err != nil {
		return nil, err
	}
	cfg, err := s.repo.SharedSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateSharedAccountInput(in, cfg); err != nil {
		return nil, err
	}
	credentials, err := sanitizeSharedCredentials(in.Platform, in.Type, in.Credentials)
	if err != nil {
		return nil, err
	}
	fingerprint, err := sharedCredentialFingerprint(in.Platform, in.Type, credentials)
	if err != nil {
		return nil, err
	}
	extra := map[string]any{SharedPoolOwnerKey: userID, SharedPoolEnabledKey: in.Enabled, SharedPoolAdminDisabledKey: false, SharedPoolDispatchConsentKey: in.DispatchConsent}
	if dailyCooldown != nil {
		extra[DailyCooldownExtraKey] = dailyCooldown.ExtraValue()
	}
	if in.CodexTicketEnabled != nil {
		extra[OpenAICodexTicketEnabledExtraKey] = *in.CodexTicketEnabled
	}
	if in.ExcelBPSEnabled != nil && *in.ExcelBPSEnabled {
		if !(&Account{Platform: in.Platform, Type: in.Type, Credentials: credentials, Extra: map[string]any{excelBPSExtraKey: true}}).IsExcelBPSEnabled() {
			return nil, infraerrors.BadRequest("EXCEL_BPS_UNSUPPORTED_ACCOUNT", "Excel / BPS 协议仅支持 OpenAI OAuth 账号")
		}
		extra[excelBPSExtraKey] = true
		delete(extra, OpenAICodexTicketEnabledExtraKey)
	}
	proxyID, err := s.applyProxy(ctx, userID, in.ProxyURL, extra)
	if err != nil {
		return nil, err
	}
	a, err := buildAccountForCreate(&CreateAccountInput{Name: strings.TrimSpace(in.Name), Platform: in.Platform, Type: in.Type,
		Concurrency: in.Concurrency, Priority: cfg.DefaultPriority, Credentials: credentials, ProxyID: proxyID}, extra)
	if err != nil {
		return nil, err
	}
	normalizeSharedCodexTicket(a)
	if err := s.prepareSharedDispatch(ctx, cfg, a, in.DispatchConsent); err != nil {
		return nil, err
	}
	persist := func() error { return s.repo.CreateSharedAccount(ctx, a, userID, fingerprint) }
	if in.ProtectionEnabled {
		err = s.protection.CreateProtectedAccount(ctx, a, persist)
	} else {
		a.Extra[AntiDegradationExtraKey] = false
		err = persist()
	}
	if err != nil {
		return nil, err
	}
	// 与管理员创建一致，授权信息由现有服务设置上游隐私，禁止客户端伪造 privacy_mode。
	s.ensureSharedPrivacy(ctx, a)
	return s.Get(ctx, userID, a.ID)
}

func (s *SharedPoolService) Update(ctx context.Context, userID, id int64, in SharedPoolAccountInput) (*SharedPoolAccountView, error) {
	record, a, err := s.OwnedAccount(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	dailyCooldown, err := normalizeSharedDailyCooldown(in.DailyCooldown)
	if err != nil {
		return nil, err
	}
	if in.CodexTicketEnabled != nil {
		return nil, infraerrors.BadRequest("SHARED_SWITCH_SEPARATE", "请在账号卡片上单独操作打票开关，刷新后重试")
	}
	if in.ExcelBPSEnabled != nil {
		return nil, infraerrors.BadRequest("SHARED_EXCEL_BPS_CREATE_ONLY", "Excel / BPS 协议仅能在添加账号时设置")
	}
	if in.Platform != a.Platform || in.Type != a.Type {
		return nil, infraerrors.BadRequest("SHARED_IDENTITY_IMMUTABLE", "平台和认证类型不能修改，请重新创建账号")
	}
	cfg, err := s.repo.SharedSettings(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateSharedAccountInput(in, cfg); err != nil {
		return nil, err
	}
	if in.ProtectionEnabled != a.AntiDegradationEnabled() || in.Enabled != record.Enabled || (in.DispatchConsent && !SharedPoolDispatchConsented(a)) {
		return nil, infraerrors.BadRequest("SHARED_SWITCH_SEPARATE", "请在账号卡片上单独操作共享和保护开关，刷新后重试")
	}
	input := SharedPoolAccountUpdate{Name: strings.TrimSpace(in.Name), Concurrency: in.Concurrency,
		ProxyChanged: in.ProxyURL != nil, DailyCooldown: dailyCooldown}
	if input.ProxyChanged {
		input.ProxyID, err = s.applyProxy(ctx, userID, in.ProxyURL, map[string]any{})
		if err != nil {
			return nil, err
		}
	}
	if len(in.Credentials) > 0 {
		input.Credentials, err = sanitizeSharedCredentials(in.Platform, in.Type, in.Credentials)
		if err != nil {
			return nil, err
		}
		input.Fingerprint, err = sharedCredentialFingerprint(in.Platform, in.Type, input.Credentials)
		if err != nil {
			return nil, err
		}
	}
	updated := *a
	if input.Credentials != nil {
		updated.Credentials = input.Credentials
	}
	input.ForceCodexTicket = SharedPoolCodexTicketRequired(&updated)
	if err = s.repo.UpdateSharedAccount(ctx, userID, id, input); err != nil {
		return nil, err
	}
	if input.Credentials != nil {
		updated, e := s.accounts.GetByID(ctx, id)
		if s.invalidator != nil {
			cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = s.invalidator.InvalidateToken(cacheCtx, a)
			if e == nil {
				_ = s.invalidator.InvalidateToken(cacheCtx, updated)
			}
			cancel()
		}
		if e == nil {
			s.ensureSharedPrivacy(ctx, updated)
		}
	}
	return s.Get(ctx, userID, id)
}

func (s *SharedPoolService) ensureSharedPrivacy(ctx context.Context, a *Account) {
	if a.Type != AccountTypeOAuth || (a.Platform != PlatformOpenAI && a.Platform != PlatformAntigravity) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ResolveRandomProxyFromSource(ctx, a, s.accounts); err != nil {
		return
	}
	if a.Platform == PlatformOpenAI {
		s.admin.EnsureOpenAIPrivacy(ctx, a)
	} else {
		s.admin.EnsureAntigravityPrivacy(ctx, a)
	}
}

func (s *SharedPoolService) SetEnabled(ctx context.Context, userID, id int64, enabled bool, consent *bool) error {
	return s.setSharedEnabled(ctx, userID, id, enabled, consent)
}

func (s *SharedPoolService) SetProtection(ctx context.Context, userID, id int64, enabled, confirm bool) error {
	if _, _, err := s.OwnedAccount(ctx, userID, id); err != nil {
		return err
	}
	_, err := s.protection.SetProtection(ctx, id, enabled, confirm)
	return err
}

func (s *SharedPoolService) Assign(ctx context.Context, id int64, groups *[]int64, disabled *bool) error {
	return s.AdminSetAccountState(ctx, id, SharedPoolAccountState{GroupIDs: groups, AdminDisabled: disabled})
}

func (s *SharedPoolService) AdminSetAccountState(ctx context.Context, id int64, state SharedPoolAccountState) error {
	if state.Priority != nil && (*state.Priority < 0 || *state.Priority > 100) {
		return infraerrors.BadRequest("INVALID_SHARED_PRIORITY", "共享账号优先级须为0至100")
	}
	if state.OwnerID != 0 || state.DispatchConsent != nil || state.DefaultGroupIDs != nil {
		return infraerrors.Forbidden("SHARED_STATE_FORBIDDEN", "管理员不能代替账号所有者授权")
	}
	if state.SubscriptionTier == nil && (state.Enabled == nil || !*state.Enabled) {
		return s.repo.SetSharedAccountState(ctx, id, state)
	}
	record, account, err := s.OwnedAccount(ctx, 0, id)
	if err != nil {
		return err
	}
	if state.Enabled != nil && *state.Enabled {
		if _, modern := account.Extra[SharedPoolDispatchConsentKey]; modern && !SharedPoolDispatchConsented(account) {
			return infraerrors.Forbidden("SHARED_DISPATCH_CONSENT_REQUIRED", "启用账号前，请账号所有者先授权参与平台调度")
		}
		if record.AdminDisabled && (state.AdminDisabled == nil || *state.AdminDisabled) {
			return infraerrors.Forbidden("SHARED_ADMIN_DISABLED", "请先取消管理员停用状态")
		}
	}
	if state.SubscriptionTier != nil {
		value, err := NormalizeSharedPoolTierOverride(account.Platform, account.Type, *state.SubscriptionTier)
		if err != nil {
			return err
		}
		state.SubscriptionTier = &value
		copy := *account
		copy.Extra = make(map[string]any, len(account.Extra)+1)
		for key, value := range account.Extra {
			copy.Extra[key] = value
		}
		copy.Extra[SharedPoolSubscriptionTierKey] = value
		account = &copy
	}
	if state.Enabled != nil && *state.Enabled && state.GroupIDs == nil && !record.Assigned && len(account.GroupIDs) == 0 && SharedPoolDispatchConsented(account) {
		settings, err := s.repo.SharedSettings(ctx)
		if err != nil {
			return err
		}
		groupIDs, err := s.initialSharedGroups(ctx, settings, account)
		if err != nil {
			return err
		}
		state.DefaultGroupIDs = groupIDs
	}
	return s.repo.SetSharedAccountState(ctx, id, state)
}

func (s *SharedPoolService) Remove(ctx context.Context, userID, id int64) error {
	return s.repo.RemoveSharedAccount(ctx, userID, id)
}

func validateSharedAccountInput(in SharedPoolAccountInput, cfg *SharedPoolSettings) error {
	if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 100 {
		return infraerrors.BadRequest("INVALID_NAME", "账号名称需为1至100个字符")
	}
	if !sharedPlatformSupported(in.Platform) || (in.Type != AccountTypeOAuth && in.Type != AccountTypeAPIKey) {
		return infraerrors.BadRequest("INVALID_PLATFORM", "该平台或认证方式暂不支持共享")
	}
	if in.Concurrency < 1 || in.Concurrency > cfg.MaxConcurrency {
		return infraerrors.BadRequest("INVALID_CONCURRENCY", "并发数超出平台允许范围")
	}
	if in.CodexTicketEnabled != nil {
		return validateSharedCodexTicketAccount(&Account{Platform: in.Platform, Type: in.Type})
	}
	return nil
}

func sharedPlatformSupported(platform string) bool {
	switch platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity:
		return true
	}
	return false
}
