package service

import (
	"context"
	"math"
	"sort"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *SharedPoolService) Get(ctx context.Context, ownerID, id int64) (*SharedPoolAccountView, error) {
	record, a, err := s.OwnedAccount(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	page, err := s.accountViews(ctx, ownerID, []SharedPoolAccountRecord{*record}, []*Account{a})
	if err != nil {
		return nil, err
	}
	return &page[0], nil
}

func (s *SharedPoolService) List(ctx context.Context, ownerID int64, page, size int) (*SharedPoolAccountPage, error) {
	records, total, err := s.repo.ListSharedAccounts(ctx, ownerID, page, size)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.AccountID)
	}
	accounts, err := s.accounts.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	items, err := s.accountViews(ctx, ownerID, records, accounts)
	return &SharedPoolAccountPage{Items: items, Total: total, Page: page, PageSize: size}, err
}

func (s *SharedPoolService) accountViews(ctx context.Context, ownerID int64, records []SharedPoolAccountRecord, accounts []*Account) ([]SharedPoolAccountView, error) {
	settings, err := s.repo.SharedSettings(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := s.repo.SharedUserRates(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*Account{}
	ids := make([]int64, 0, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
		ids = append(ids, a.ID)
	}
	totals, err := s.earnings.AccountTotals(ctx, ownerID, ids)
	if err != nil {
		return nil, err
	}
	result := make([]SharedPoolAccountView, 0, len(records))
	for _, r := range records {
		a := byID[r.AccountID]
		if a == nil {
			continue
		}
		p, q := effectiveSharedRates(settings, overrides, r.OwnerUserID)
		v := sharedAccountView(a, r, p, q, ownerID == 0)
		if v.DispatchConsent {
			rate := effectiveSharedSettlementMultiplierForTier(settings, overrides, r.OwnerUserID, a.Platform, SharedPoolOverviewTierForAccount(a))
			v.SettlementMultiplier = &rate
		}
		v.TodayEarnings = totals[a.ID].TodayEarnings
		v.TotalEarnings = totals[a.ID].TotalEarnings
		v.EstimatedEarnings = s.estimatedSharedEarnings(ctx, r.OwnerUserID, a)
		result = append(result, v)
	}
	return result, nil
}

func sharedAccountView(a *Account, r SharedPoolAccountRecord, p, q int, admin bool) SharedPoolAccountView {
	v := SharedPoolAccountView{ID: a.ID, Name: a.Name, Platform: a.Platform, Type: a.Type, Concurrency: a.Concurrency,
		SubscriptionTier: SharedPoolOverviewTierForAccount(a),
		Enabled:          r.Enabled, AdminDisabled: r.AdminDisabled, Status: a.Status, ProxyMode: "custom", HasCustomProxy: a.ProxyID != nil,
		ProtectionEnabled: a.AntiDegradationEnabled(), DailyCooldown: sharedDailyCooldownView(a.Extra), GroupIDs: []int64{}, Groups: []SharedPoolGroupView{},
		PlatformRateBPS: p, ProxyRateBPS: q, LastUsedAt: a.LastUsedAt, CreatedAt: a.CreatedAt}
	v.DispatchConsent = SharedPoolDispatchConsented(a)
	if isOpenAICodexTicketAccount(a) {
		enabled := OpenAICodexTicketAccountEnabled(a)
		v.CodexTicketEnabled = &enabled
		v.CodexTicketRequired = SharedPoolCodexTicketRequired(a)
	}
	if admin {
		v.Priority = a.Priority
		v.OwnerUserID = r.OwnerUserID
		v.OwnerEmail = r.OwnerEmail
		v.GroupIDs = a.GroupIDs
		if raw, ok := a.Extra[SharedPoolSubscriptionTierKey].(string); ok {
			v.SubscriptionTierOverride, _ = NormalizeSharedPoolTierOverride(a.Platform, a.Type, raw)
		}
	}
	if a.IsRandomProxy() {
		v.ProxyMode = "random"
		v.HasCustomProxy = false
	}
	if !a.IsSchedulable() && r.Enabled {
		v.Status = "unavailable"
	}
	if a.ErrorMessage != "" {
		v.ErrorMessage = "上游账号暂不可用，请测试连接或重新授权"
	}
	for _, g := range a.Groups {
		if admin && g != nil {
			v.Groups = append(v.Groups, SharedPoolGroupView{ID: g.ID, Name: g.Name})
		}
	}
	if v.GroupIDs == nil {
		v.GroupIDs = []int64{}
	}
	return v
}

func effectiveSharedRates(s *SharedPoolSettings, rates []SharedPoolUserRate, userID int64) (int, int) {
	p, q := s.PlatformRateBPS, s.ProxyRateBPS
	for _, r := range rates {
		if r.UserID == userID {
			if r.PlatformRateBPS != nil {
				p = *r.PlatformRateBPS
			}
			if r.ProxyRateBPS != nil {
				q = *r.ProxyRateBPS
			}
			break
		}
	}
	return p, q
}

func (s *SharedPoolService) estimatedSharedEarnings(ctx context.Context, ownerID int64, a *Account) *float64 {
	if a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth {
		return nil
	}
	// 使用已有额度快照，列表不主动对每个账号发起上游探测。
	usage := buildCodexUsageProgressFromExtra(a.Extra, "7d", time.Now())
	if usage == nil || usage.Utilization <= 0 || math.IsNaN(usage.Utilization) || math.IsInf(usage.Utilization, 0) {
		return nil
	}
	since := codexWindowStatsStart(usage, 7*24*time.Hour, time.Now())
	window, err := s.earnings.AccountWindow(ctx, ownerID, a.ID, since)
	if err != nil || window == nil || window.OwnerAmount <= 0 {
		return nil
	}
	estimate := window.OwnerAmount * 100 / usage.Utilization
	if math.IsInf(estimate, 0) || math.IsNaN(estimate) {
		return nil
	}
	return &estimate
}

func (s *SharedPoolService) Settings(ctx context.Context) (*SharedPoolSettings, error) {
	return s.repo.SharedSettings(ctx)
}
func (s *SharedPoolService) UserRates(ctx context.Context) ([]SharedPoolUserRate, error) {
	return s.repo.SharedUserRates(ctx)
}

func (s *SharedPoolService) SaveSettings(ctx context.Context, cfg *SharedPoolSettings) error {
	if err := ValidateSharedSettlementSettings(cfg); err != nil {
		return err
	}
	if cfg.DefaultPriority < 0 || cfg.DefaultPriority > 100 {
		return infraerrors.BadRequest("INVALID_SHARED_PRIORITY", "共享账号默认优先级须为0至100")
	}
	if cfg.PlatformRateBPS < 0 || cfg.ProxyRateBPS < 0 || cfg.PlatformRateBPS > 10000-cfg.ProxyRateBPS || cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > 100 {
		return infraerrors.BadRequest("INVALID_SHARED_SETTINGS", "分成总比例须为0至100%，最大并发须为1至100")
	}
	if err := s.validateSharedGroupSettings(ctx, cfg); err != nil {
		return err
	}
	return s.repo.SaveSharedSettings(ctx, cfg)
}

func (s *SharedPoolService) SaveUserRate(ctx context.Context, rate SharedPoolUserRate) error {
	if rate.SettlementMultiplier != nil && !ValidSharedPoolSettlementMultiplier(*rate.SettlementMultiplier) {
		return infraerrors.BadRequest("INVALID_SETTLEMENT_MULTIPLIER", "结算倍率须为0至100之间的有限数值")
	}
	if rate.UserID <= 0 {
		return infraerrors.BadRequest("INVALID_USER", "用户ID无效")
	}
	for _, value := range []*int{rate.PlatformRateBPS, rate.ProxyRateBPS} {
		if value != nil && (*value < 0 || *value > 10000) {
			return infraerrors.BadRequest("INVALID_SHARED_RATES", "分成比例须为0至100%")
		}
	}
	return s.repo.SaveSharedUserRate(ctx, rate)
}

func (s *SharedPoolService) UserConfig(ctx context.Context, userID int64) (map[string]any, error) {
	cfg, err := s.repo.SharedSettings(ctx)
	if err != nil {
		return nil, err
	}
	rates, err := s.repo.SharedUserRates(ctx)
	if err != nil {
		return nil, err
	}
	p, q := effectiveSharedRates(cfg, rates, userID)
	platforms := []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity}
	sort.Strings(platforms)
	return map[string]any{"platforms": platforms, "max_concurrency": cfg.MaxConcurrency, "platform_rate_bps": p, "proxy_rate_bps": q,
		"settlement_multiplier": effectiveSharedSettlementMultiplier(cfg, rates, userID)}, nil
}
