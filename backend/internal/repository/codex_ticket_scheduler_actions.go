package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type codexSchedulerResult struct {
	SessionEpoch string                            `json:"session_epoch"`
	Token        string                            `json:"token"`
	ProxyID      string                            `json:"proxy_id"`
	Status       *service.CodexTicketRuntimeStatus `json:"status"`
	Waiting      bool                              `json:"waiting"`
	RetryMS      int64                             `json:"retry_ms"`
	CooldownMS   int64                             `json:"cooldown_ms"`
	SilenceMS    int64                             `json:"silence_ms"`
}

func (a *ProxyPoolAllocator) runCodexScheduler(ctx context.Context, id int64, q map[string]any) (*codexSchedulerResult, error) {
	if a == nil || a.rdb == nil {
		return nil, errors.New("codex ticket shared scheduler unavailable")
	}
	encoded, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	raw, err := codexTicketSchedulerScript.Run(ctx, a.rdb, []string{codexSchedulerAccountKey(id)}, string(encoded)).Text()
	if err != nil {
		return nil, fmt.Errorf("codex ticket shared scheduler unavailable: %w", err)
	}
	var result codexSchedulerResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result.Status == nil {
		return nil, errors.New("invalid codex ticket scheduler response")
	}
	if result.RetryMS > 0 {
		at := time.UnixMilli(result.RetryMS)
		result.Status.RetryAt = &at
	}
	if result.CooldownMS > 0 {
		at := time.UnixMilli(result.CooldownMS)
		result.Status.CooldownUntil = &at
	}
	if result.SilenceMS > 0 {
		at := time.UnixMilli(result.SilenceMS)
		result.Status.SilenceUntil = &at
	}
	if result.Waiting {
		return &result, &service.CodexTicketWaitError{Status: result.Status}
	}
	return &result, nil
}

func (a *ProxyPoolAllocator) reservationInput(ctx context.Context, action string, r *service.CodexTicketReservation) (map[string]any, error) {
	if r == nil || r.AccountID <= 0 || r.Token == "" {
		return nil, errors.New("invalid codex ticket reservation")
	}
	cfg, err := a.codexSchedulerConfig(ctx, r.Config)
	if err != nil {
		return nil, err
	}
	q, err := a.codexSchedulerInput(ctx, action, cfg)
	if err != nil {
		return nil, err
	}
	q["account"], q["token"], q["generation"], q["manual"] = strconv.FormatInt(r.AccountID, 10), r.Token, r.Generation, r.Manual
	return q, nil
}

func (a *ProxyPoolAllocator) StartCodexTicket(ctx context.Context, r *service.CodexTicketReservation) error {
	q, err := a.reservationInput(ctx, "start", r)
	if err != nil {
		return err
	}
	if current, err := a.codexSchedulerCurrentReservationProxy(ctx, r); err != nil {
		return err
	} else if current != nil {
		candidate, err := a.codexSchedulerCurrentCandidate(ctx, current, true)
		if err != nil {
			return err
		}
		q["current_harvest"] = candidate
	}
	_, err = a.runCodexScheduler(ctx, r.AccountID, q)
	return err
}

func (a *ProxyPoolAllocator) ValidateCodexTicket(ctx context.Context, r *service.CodexTicketReservation) error {
	q, err := a.reservationInput(ctx, "publish", r)
	if err != nil {
		return err
	}
	if current, err := a.codexSchedulerCurrentReservationProxy(ctx, r); err != nil {
		return err
	} else if current != nil {
		candidate, err := a.codexSchedulerCurrentCandidate(ctx, current, false)
		if err != nil {
			return err
		}
		q["current_harvest"] = candidate
	}
	if business, err := a.codexSchedulerBusinessProxy(ctx, r.AccountID); err != nil {
		return err
	} else if business != nil {
		candidate, err := a.codexSchedulerCurrentCandidate(ctx, business, false)
		if err != nil {
			return err
		}
		q["current_business"] = candidate
	}
	_, err = a.runCodexScheduler(ctx, r.AccountID, q)
	return err
}

func (a *ProxyPoolAllocator) CheckCodexTicketStage(ctx context.Context, r *service.CodexTicketReservation, proxy *service.Proxy) error {
	q, err := a.reservationInput(ctx, "stage", r)
	if err != nil {
		return err
	}
	if proxy != nil && a.client != nil {
		current, err := a.client.Proxy.Get(ctx, proxy.ID)
		if err != nil {
			return codexSchedulerWait("business_proxy_unavailable")
		}
		latest := proxyEntityToService(current)
		if !latest.IsActive() || latest.IsExpired(time.Now()) || latest.URL() != proxy.URL() {
			return codexSchedulerWait("proxy_changed")
		}
	}
	items, _, err := a.codexSchedulerCandidates(ctx, service.CodexTicketReserveRequest{FixedProxy: proxy})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return codexSchedulerWait("business_proxy_unavailable")
	}
	limit, err := a.settings.GetProxyPoolMaxAccounts(ctx)
	if err != nil || limit < 0 || limit > 10000 {
		return errors.New("read codex ticket proxy capacity failed")
	}
	q["candidate"], q["limit"] = items[0], limit
	_, err = a.runCodexScheduler(ctx, r.AccountID, q)
	return err
}

func (a *ProxyPoolAllocator) ReportCodexTicketHarvest(ctx context.Context, r *service.CodexTicketReservation, accepted, silence, neutral bool) error {
	q, err := a.reservationInput(ctx, "report", r)
	if err != nil {
		return err
	}
	q["accepted"], q["silence"], q["neutral"] = accepted, silence, neutral
	_, err = a.runCodexScheduler(ctx, r.AccountID, q)
	return err
}

func (a *ProxyPoolAllocator) FinishCodexTicket(ctx context.Context, req service.CodexTicketFinishRequest) error {
	q, err := a.reservationInput(ctx, "finish", req.Reservation)
	if err != nil {
		return err
	}
	q["accepted"], q["silence"], q["outcome"] = req.HarvestAccepted, req.Silence, req.Outcome
	q["harvest_failed"], q["business_failed"], q["business_succeeded"] = req.HarvestProxyFailed, req.BusinessProxyFailed, req.BusinessProxySucceeded
	q["quality_failed"] = req.QualityProxyFailed
	q["retry_ms"], q["retry_scope"], q["complete"] = codexSchedulerMillis(req.RetryAt), req.RetryScope, req.TargetsComplete
	q["deferred_proxy"], q["deferred_until"] = strconv.FormatInt(req.DeferredProxyID, 10), codexSchedulerMillis(req.DeferredUntil)
	result, err := a.runCodexScheduler(ctx, req.Reservation.AccountID, q)
	if result != nil && result.Status.Reason != "stale_completion" {
		req.Reservation.Status = result.Status
	}
	return err
}

func codexSchedulerMillis(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

func (a *ProxyPoolAllocator) codexSchedulerTemporaryProxyWait(ctx context.Context, reason string) error {
	cfg, err := a.codexSchedulerConfig(ctx, config.OpenAICodexTicketConfig{})
	if err != nil {
		return err
	}
	now, err := a.rdb.Time(ctx).Result()
	if err != nil {
		return errors.New("codex ticket shared scheduler unavailable")
	}
	at := now.Add(time.Duration(max(1, cfg.HarvestProbeIntervalSeconds)) * time.Second)
	return &service.CodexTicketWaitError{Status: &service.CodexTicketRuntimeStatus{State: "waiting", Reason: reason, RetryAt: &at}}
}

func (a *ProxyPoolAllocator) GetCodexTicketRuntimeStatus(ctx context.Context, accountID int64) (*service.CodexTicketRuntimeStatus, error) {
	if accountID <= 0 {
		return nil, errors.New("invalid codex ticket account")
	}
	if a == nil || a.rdb == nil {
		return nil, errors.New("codex ticket shared scheduler unavailable")
	}
	q := map[string]any{"action": "status"}
	if source, ok := a.settings.(codexTicketSettingsReader); ok {
		cfg, err := source.GetCodexTicketSettings(ctx)
		if err != nil {
			return nil, err
		}
		q["max_attempts"] = max(1, cfg.TicketProtection().MaxAccountAttempts)
	}
	// status 只投影当前上限，不写入状态、不创建周期或收回租约。
	result, err := a.runCodexScheduler(ctx, accountID, q)
	if err != nil {
		return nil, err
	}
	return result.Status, nil
}
