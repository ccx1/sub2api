package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

const SettingKeyProxyQualityGuardSettings = "proxy_quality_guard_settings"

const (
	ProxyQualityGuardCheckModeBasic = "basic"
	ProxyQualityGuardCheckModeFull  = "full"

	// 使用前检测：off 保持原选择；prefer 让未检测/未达标代理降为后备；strict 直接排除，可能触发空池策略。
	ProxyQualityGuardPreUseOff    = "off"
	ProxyQualityGuardPreUsePrefer = "prefer"
	ProxyQualityGuardPreUseStrict = "strict"
)

// ProxyQualityGuardSettings 控制代理池质量巡检：检测失败先禁用，冷却后恢复复检，超过轮数后删除。
type ProxyQualityGuardSettings struct {
	Enabled                 bool   `json:"enabled"`
	IntervalSeconds         int    `json:"interval_seconds"`
	CheckIntervalMinutes    int    `json:"check_interval_minutes"`
	CheckMode               string `json:"check_mode"`
	MinScore                int    `json:"min_score"`
	FailOnChallenge         bool   `json:"fail_on_challenge"`
	FailureThreshold        int    `json:"failure_threshold"`
	RuntimeFailureThreshold int    `json:"runtime_failure_threshold"`
	DisableMinutes          int    `json:"disable_minutes"`
	MaxRounds               int    `json:"max_rounds"`
	AutoDelete              bool   `json:"auto_delete"`
	PreUseCheck             string `json:"pre_use_check"`
	IncludeFixedBound       bool   `json:"include_fixed_bound"`
	StableResetHours        int    `json:"stable_reset_hours"`
	MaxChecksPerRun         int    `json:"max_checks_per_run"`
	Concurrency             int    `json:"concurrency"`
}

func DefaultProxyQualityGuardSettings() ProxyQualityGuardSettings {
	return ProxyQualityGuardSettings{
		Enabled:                 false,
		IntervalSeconds:         60,
		CheckIntervalMinutes:    30,
		CheckMode:               ProxyQualityGuardCheckModeFull,
		MinScore:                60,
		FailOnChallenge:         false,
		FailureThreshold:        1,
		RuntimeFailureThreshold: 3,
		DisableMinutes:          10,
		MaxRounds:               3,
		AutoDelete:              true,
		PreUseCheck:             ProxyQualityGuardPreUsePrefer,
		IncludeFixedBound:       false,
		StableResetHours:        24,
		MaxChecksPerRun:         20,
		Concurrency:             4,
	}
}

func normalizeProxyQualityGuardSettings(in ProxyQualityGuardSettings) ProxyQualityGuardSettings {
	def := DefaultProxyQualityGuardSettings()
	if in.IntervalSeconds < 15 || in.IntervalSeconds > 24*60*60 {
		in.IntervalSeconds = def.IntervalSeconds
	}
	if in.CheckIntervalMinutes < 1 || in.CheckIntervalMinutes > 7*24*60 {
		in.CheckIntervalMinutes = def.CheckIntervalMinutes
	}
	in.CheckMode = strings.ToLower(strings.TrimSpace(in.CheckMode))
	if in.CheckMode != ProxyQualityGuardCheckModeBasic && in.CheckMode != ProxyQualityGuardCheckModeFull {
		in.CheckMode = def.CheckMode
	}
	in.PreUseCheck = strings.ToLower(strings.TrimSpace(in.PreUseCheck))
	switch in.PreUseCheck {
	case ProxyQualityGuardPreUseOff, ProxyQualityGuardPreUsePrefer, ProxyQualityGuardPreUseStrict:
	default:
		in.PreUseCheck = def.PreUseCheck
	}
	if in.MinScore < 0 || in.MinScore > 100 {
		in.MinScore = def.MinScore
	}
	if in.FailureThreshold < 1 || in.FailureThreshold > 100 {
		in.FailureThreshold = def.FailureThreshold
	}
	// 0 表示运行时错误不触发提前复检，仍按周期巡检。
	if in.RuntimeFailureThreshold < 0 || in.RuntimeFailureThreshold > 1000 {
		in.RuntimeFailureThreshold = def.RuntimeFailureThreshold
	}
	if in.DisableMinutes < 1 || in.DisableMinutes > 7*24*60 {
		in.DisableMinutes = def.DisableMinutes
	}
	if in.MaxRounds < 1 || in.MaxRounds > 100 {
		in.MaxRounds = def.MaxRounds
	}
	if in.StableResetHours < 0 || in.StableResetHours > 30*24 {
		in.StableResetHours = def.StableResetHours
	}
	if in.MaxChecksPerRun < 1 || in.MaxChecksPerRun > 500 {
		in.MaxChecksPerRun = def.MaxChecksPerRun
	}
	if in.Concurrency < 1 || in.Concurrency > 32 {
		in.Concurrency = def.Concurrency
	}
	return in
}

// ParseProxyQualityGuardSettings 解析持久化配置；空值或损坏的 JSON 回落到默认值。
func ParseProxyQualityGuardSettings(raw string) ProxyQualityGuardSettings {
	cfg := DefaultProxyQualityGuardSettings()
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("proxy_quality_guard.bad_settings", "error", err)
		return DefaultProxyQualityGuardSettings()
	}
	return normalizeProxyQualityGuardSettings(cfg)
}

// PassesPreUseCheck 判断代理是否已通过当前配置的检测。
func (cfg ProxyQualityGuardSettings) PassesPreUseCheck(proxy *Proxy, info *ProxyLatencyInfo) bool {
	if proxy == nil || !ProxyLatencyMatchesProxy(info, proxy) || !info.Success {
		return false
	}
	if cfg.CheckMode != ProxyQualityGuardCheckModeFull {
		return true
	}
	if info.QualityCheckedAt == nil || info.QualityScore == nil || *info.QualityScore < cfg.MinScore {
		return false
	}
	return !(cfg.FailOnChallenge && info.QualityStatus == "challenge")
}

// ApplyPoolGate 按使用前检测策略调整候选：prefer 降级，strict 排除；未开启巡检时不改变原有结论。
func (cfg ProxyQualityGuardSettings) ApplyPoolGate(proxy *Proxy, info *ProxyLatencyInfo, degraded, valid bool) (bool, bool) {
	if !valid || !cfg.Enabled || cfg.PreUseCheck == ProxyQualityGuardPreUseOff || cfg.PassesPreUseCheck(proxy, info) {
		return degraded, valid
	}
	if cfg.PreUseCheck == ProxyQualityGuardPreUseStrict {
		return degraded, false
	}
	return true, valid
}

// GetProxyQualityGuardSettings 复用代理池设置的短缓存，供随机代理分配热路径读取。
func (s *SettingService) GetProxyQualityGuardSettings(ctx context.Context) (ProxyQualityGuardSettings, error) {
	values, err := s.getProxyPoolSettingValues(ctx)
	if err != nil {
		return DefaultProxyQualityGuardSettings(), err
	}
	return ParseProxyQualityGuardSettings(values[SettingKeyProxyQualityGuardSettings]), nil
}

func (s *ProxyQualityGuardService) currentSettings() ProxyQualityGuardSettings {
	if s == nil || s.settings == nil {
		return DefaultProxyQualityGuardSettings()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := s.settings.GetValue(ctx, SettingKeyProxyQualityGuardSettings)
	if err != nil {
		return DefaultProxyQualityGuardSettings()
	}
	return ParseProxyQualityGuardSettings(raw)
}
