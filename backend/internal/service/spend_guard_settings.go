package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"
)

const SettingKeySpendGuardSettings = "spend_guard_settings"

type SpendGuardSettings struct {
	Enabled         bool    `json:"enabled"`
	WindowMinutes   int     `json:"window_minutes"`
	TokensPerMinute float64 `json:"tokens_per_minute"`
	MinRequests     int     `json:"min_requests"`
	MaxErrorRate    float64 `json:"max_error_rate"`
	IntervalSeconds int     `json:"interval_seconds"`
}

func DefaultSpendGuardSettings() SpendGuardSettings {
	return SpendGuardSettings{Enabled: false, WindowMinutes: 5, TokensPerMinute: 2000000,
		MinRequests: 5, MaxErrorRate: 0.8, IntervalSeconds: 60}
}

func (s *SpendGuardService) currentSettings() SpendGuardSettings {
	cfg := DefaultSpendGuardSettings()
	if s == nil || s.settings == nil {
		return cfg
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := s.settings.GetValue(ctx, SettingKeySpendGuardSettings)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("spend_guard.bad_settings", "error", err)
		return DefaultSpendGuardSettings()
	}
	return normalizeSpendGuardSettings(cfg)
}

func normalizeSpendGuardSettings(in SpendGuardSettings) SpendGuardSettings {
	def := DefaultSpendGuardSettings()
	if in.WindowMinutes <= 0 || in.WindowMinutes > 24*60 {
		in.WindowMinutes = def.WindowMinutes
	}
	if in.TokensPerMinute < 0 || math.IsNaN(in.TokensPerMinute) || math.IsInf(in.TokensPerMinute, 0) {
		in.TokensPerMinute = def.TokensPerMinute
	}
	if in.MinRequests <= 0 {
		in.MinRequests = def.MinRequests
	}
	if in.MaxErrorRate <= 0 || in.MaxErrorRate > 1 || math.IsNaN(in.MaxErrorRate) {
		in.MaxErrorRate = def.MaxErrorRate
	}
	if in.IntervalSeconds < 10 || in.IntervalSeconds > 24*60*60 {
		in.IntervalSeconds = def.IntervalSeconds
	}
	return in
}

func (s *SpendGuardService) GetSettings(_ context.Context) SpendGuardSettings {
	return s.currentSettings()
}

func (s *SpendGuardService) UpdateSettings(ctx context.Context, in SpendGuardSettings) (SpendGuardSettings, error) {
	if s == nil || s.settings == nil {
		return DefaultSpendGuardSettings(), errors.New("spend guard settings unavailable")
	}
	norm := normalizeSpendGuardSettings(in)
	raw, err := json.Marshal(norm)
	if err != nil {
		return norm, err
	}
	if err := s.settings.Set(ctx, SettingKeySpendGuardSettings, string(raw)); err != nil {
		return norm, err
	}
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
	return norm, nil
}
