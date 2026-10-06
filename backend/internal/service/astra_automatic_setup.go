package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type AstraSetupStatus struct {
	Revision   string     `json:"revision"`
	State      string     `json:"state"`
	Phase      string     `json:"phase"`
	AccountID  int64      `json:"account_id"`
	Reason     string     `json:"reason"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// A save schedules one bounded serial preparation run. Saving again cancels
// the previous run, and revision checks prevent stale work publishing readiness.
func (s *AccountTestService) StartAstraAutomaticSetup(settings config.AstraRoutingSettings) {
	if !s.astraSetupSettingsCurrent(context.Background(), settings) {
		return
	}
	s.astraSetupMu.Lock()
	if !s.astraSetupSettingsCurrent(context.Background(), settings) {
		s.astraSetupMu.Unlock()
		return
	}
	if s.astraSetupCancel != nil {
		s.astraSetupCancel()
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ctx = context.WithValue(ctx, astraSetupStartedKey{}, started)
	s.astraSetupCancel = cancel
	state := "queued"
	if !settings.CookiePool.Enabled && !settings.WSSession.Enabled {
		state = "disabled"
	}
	s.astraSetupStatus = AstraSetupStatus{Revision: settings.Revision, State: state, StartedAt: started}
	if settings.SelectionError != "" {
		s.astraSetupStatus.State = "failed"
		s.astraSetupStatus.Reason = settings.SelectionError
		s.astraSetupStatus.FinishedAt = &started
		state = "failed"
	}
	s.astraSetupMu.Unlock()
	if state == "disabled" || state == "failed" {
		cancel()
		return
	}
	go s.runAstraAutomaticSetup(ctx, cancel, settings)
}
func (s *AccountTestService) runAstraAutomaticSetup(ctx context.Context, cancel context.CancelFunc, settings config.AstraRoutingSettings) {
	defer cancel()
	set := func(state, phase string, id int64, reason string) {
		s.astraSetupMu.Lock()
		defer s.astraSetupMu.Unlock()
		started, owned := ctx.Value(astraSetupStartedKey{}).(time.Time)
		if s.astraSetupStatus.Revision != settings.Revision || (owned && !s.astraSetupStatus.StartedAt.Equal(started)) {
			return
		}
		s.astraSetupStatus.State = state
		s.astraSetupStatus.Phase = phase
		s.astraSetupStatus.AccountID = id
		s.astraSetupStatus.Reason = reason
		if state == "failed" || state == "ready" {
			now := time.Now()
			s.astraSetupStatus.FinishedAt = &now
		}
	}
	// Share serialization with manually requested tests without discarding a save
	// just because an older, cancelled task has not released its lock yet.
	for !s.astraGatewayActionMu.TryLock() {
		select {
		case <-ctx.Done():
			set("failed", "", 0, "setup_cancelled")
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer s.astraGatewayActionMu.Unlock()
	if ctx.Err() != nil || !s.astraSetupSettingsCurrent(ctx, settings) {
		set("failed", "", 0, "setup_cancelled")
		return
	}
	if settings.SelectionError != "" {
		set("failed", "source", 0, settings.SelectionError)
		return
	}
	set("running", "source", 0, "")
	provider, ok := s.httpUpstream.(AstraGatewayRuntimeProvider)
	if !ok {
		set("failed", "source", 0, "runtime_unavailable")
		return
	}
	if err := prepareAstraForSetup(ctx, provider); err != nil {
		set("failed", "source", 0, astraSetupError(err))
		return
	}
	var failedID int64
	var failure string
	for _, id := range settings.CookiePool.TargetAccountIDs {
		if ctx.Err() != nil || !s.astraSetupSettingsCurrent(ctx, settings) {
			set("failed", "target", id, "configuration_changed")
			return
		}
		// 来源预热后仍有效的证据直接复用；重复验证旧 Cookie 不会延长寿命。
		if astraAccountSchedulingReady(settings, provider.AstraGatewaySnapshot(ctx), id, time.Now()) {
			continue
		}
		set("running", "target", id, "")
		// The shared state probe verifies target credentials on the borrowed route.
		err := s.verifyAstraGatewayTarget(context.WithValue(ctx, astraTargetValidationCacheKey{}, true), id)
		if err != nil && failure == "" {
			failedID, failure = id, astraSetupError(err)
		}
	}
	if failure != "" {
		set("failed", "target", failedID, failure)
		return
	}
	if settings.CookiePool.Enabled {
		if id, ready := astraTargetsReady(ctx, provider, settings.CookiePool.TargetAccountIDs); !ready {
			set("failed", "target", id, "target_not_verified")
			return
		}
	}
	set("ready", "complete", 0, "")
}

func (s *AccountTestService) astraSetupSettingsCurrent(ctx context.Context, settings config.AstraRoutingSettings) bool {
	current := s.cfg.AstraRouting(ctx)
	return current.Revision == settings.Revision && config.AstraRouteSettingsEqual(current, settings)
}

type astraSetupStartedKey struct{}
type astraTargetValidationCacheKey struct{}

// AstraTargetValidationCacheAllowed 仅让自动准备复用缓存，手工验证仍强制重跑。
func AstraTargetValidationCacheAllowed(ctx context.Context) bool {
	allowed, _ := ctx.Value(astraTargetValidationCacheKey{}).(bool)
	return allowed
}
func astraSetupError(err error) string {
	switch err.Error() {
	case "astra_rotation_cooling", "astra_rotation_unavailable", "astra_rotation_use_once", "astra_rotation_no_nodes", "astra_rotation_node_unavailable", "astra_rotation_exhausted", "answer_mismatch", "upstream_test_failed", "no_qualified_source_route", "source_probe_cooldown", "configuration_changed", "cookie_pool_disabled", "target_probe_degraded", "target_route_changed", "target_probe_failed", "target_validation_in_progress", "preparation_in_progress":
		return err.Error()
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return "setup_cancelled"
	}
	return "setup_failed"
}

func (s *AccountTestService) StopAstraAutomaticSetup() {
	s.astraSetupMu.Lock()
	defer s.astraSetupMu.Unlock()
	if s.astraSetupCancel != nil {
		s.astraSetupCancel()
	}
}

// A later target may reject and rotate away from an earlier target's route.
// Only report setup success when all selected targets still match current routes.
func astraTargetsReady(ctx context.Context, provider AstraGatewayRuntimeProvider, ids []int64) (int64, bool) {
	snapshot := provider.AstraGatewaySnapshot(ctx)
	for _, id := range ids {
		ready := false
		for _, row := range snapshot.Targets {
			if row.AccountID == id && row.State == "ready" {
				ready = true
				break
			}
		}
		if !ready {
			return id, false
		}
	}
	return 0, true
}

// Saving may overlap an on-demand preparation. Wait for it rather than
// incorrectly publishing a terminal failure while the source is still running.
func prepareAstraForSetup(ctx context.Context, provider AstraGatewayRuntimeProvider) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := provider.PrepareAstraGateway(ctx)
		if err == nil || err.Error() != "preparation_in_progress" {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
