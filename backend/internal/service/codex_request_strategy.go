package service

import (
	"fmt"
	"strings"
)

// CodexRequestStrategyPolicy controls optional request bootstrap behavior.
// It is deliberately disabled by default because cross-transport previous IDs
// still require validation against the upstream implementation.
type CodexRequestStrategyPolicy struct {
	Enabled             bool   `json:"enabled"`
	Strategy            string `json:"strategy"`
	Scope               string `json:"scope"`
	FailureMode         string `json:"failure_mode"`
	ProbeTimeoutSeconds int    `json:"probe_timeout_seconds"`
	// RouteAffinityMode controls whether WS connections prefer or require the
	// route fingerprint observed during the handshake.
	RouteAffinityMode           string `json:"route_affinity_mode"`
	RoutePrewarmConnections     int    `json:"route_prewarm_connections"`
	RouteFailureCooldownSeconds int    `json:"route_failure_cooldown_seconds"`
	// 仅过滤出站 Cookie 中选定的基础设施字段，默认保留现有行为。
	CookieMode string `json:"cookie_mode"`
	// RegionMode controls account routing/residency headers emitted by the
	// gateway. The default strips client supplied region headers; account
	// routing can be explicitly preserved or overridden by an administrator.
	RegionMode             string `json:"region_mode"`
	AccountRoutingOverride string `json:"account_routing_override"`
	Residency              string `json:"residency"`
	// TimeContextMode controls timezone metadata already present in the JSON
	// request. It does not synthesize current-date or current-time prompts.
	TimeContextMode string `json:"time_context_mode"`
	Timezone        string `json:"timezone"`
	// ComplianceMode controls account-derived FedRAMP signaling. Manual
	// enabling is intentionally unsupported to avoid spoofing compliance.
	ComplianceMode string `json:"compliance_mode"`
}

const (
	CodexRequestStrategyNative           = "native"
	CodexRequestStrategyCookiePreviousWS = "cookie_previous_ws"
	CodexRequestStrategyScopeDedicated   = "dedicated"
	CodexRequestStrategyScopePassthrough = "passthrough"
	CodexRequestStrategyScopeAll         = "all"
	CodexRequestStrategyFallback         = "fallback"
	CodexRequestStrategyReject           = "reject"
	CodexRouteAffinityOff                = "off"
	CodexRouteAffinityPrefer             = "prefer"
	CodexRouteAffinityStrict             = "strict"
	CodexRequestRegionPreserve           = "preserve"
	CodexRequestRegionStrip              = "strip"
	CodexRequestRegionOverride           = "override"
	CodexRequestTimeContextPreserve      = "preserve"
	CodexRequestTimeContextStrip         = "strip"
	CodexRequestTimeContextOverride      = "override"
	CodexRequestComplianceAccount        = "account"
	CodexRequestCompliancePreserve       = "preserve"
	CodexRequestComplianceStrip          = "strip"
)

func DefaultCodexRequestStrategyPolicy() CodexRequestStrategyPolicy {
	return CodexRequestStrategyPolicy{
		Strategy:                    CodexRequestStrategyNative,
		Scope:                       CodexRequestStrategyScopeDedicated,
		FailureMode:                 CodexRequestStrategyFallback,
		ProbeTimeoutSeconds:         10,
		RouteAffinityMode:           CodexRouteAffinityOff,
		RoutePrewarmConnections:     4,
		RouteFailureCooldownSeconds: 120,
		CookieMode:                  CodexCookiePreserve,
		// When the policy is enabled, remove client supplied region and time
		// context by default. Account-derived compliance is re-applied below.
		RegionMode:      CodexRequestRegionStrip,
		TimeContextMode: CodexRequestTimeContextStrip,
		ComplianceMode:  CodexRequestComplianceAccount,
	}
}

func ValidateCodexRequestStrategyPolicy(p CodexRequestStrategyPolicy) error {
	if p.Strategy != CodexRequestStrategyNative && p.Strategy != CodexRequestStrategyCookiePreviousWS {
		return fmt.Errorf("strategy must be native or cookie_previous_ws")
	}
	if p.Scope != CodexRequestStrategyScopeDedicated && p.Scope != CodexRequestStrategyScopePassthrough && p.Scope != CodexRequestStrategyScopeAll {
		return fmt.Errorf("scope must be dedicated, passthrough or all")
	}
	if p.FailureMode != CodexRequestStrategyFallback && p.FailureMode != CodexRequestStrategyReject {
		return fmt.Errorf("failure_mode must be fallback or reject")
	}
	if p.ProbeTimeoutSeconds < 5 || p.ProbeTimeoutSeconds > 60 {
		return fmt.Errorf("probe_timeout_seconds must be between 5 and 60")
	}
	if p.RouteAffinityMode != CodexRouteAffinityOff && p.RouteAffinityMode != CodexRouteAffinityPrefer && p.RouteAffinityMode != CodexRouteAffinityStrict {
		return fmt.Errorf("route_affinity_mode must be off, prefer or strict")
	}
	if p.RoutePrewarmConnections < 0 || p.RoutePrewarmConnections > 12 {
		return fmt.Errorf("route_prewarm_connections must be between 0 and 12")
	}
	if p.RouteFailureCooldownSeconds < 30 || p.RouteFailureCooldownSeconds > 900 {
		return fmt.Errorf("route_failure_cooldown_seconds must be between 30 and 900")
	}
	if p.RegionMode != CodexRequestRegionPreserve && p.RegionMode != CodexRequestRegionStrip && p.RegionMode != CodexRequestRegionOverride {
		return fmt.Errorf("region_mode must be preserve, strip or override")
	}
	if p.AccountRoutingOverride != "" && p.AccountRoutingOverride != "NO_CONSTRAINT" && p.AccountRoutingOverride != "us" && p.AccountRoutingOverride != "us_cr" {
		return fmt.Errorf("account_routing_override must be empty, NO_CONSTRAINT, us or us_cr")
	}
	if p.Residency != "" && p.Residency != "us" {
		return fmt.Errorf("residency must be empty or us")
	}
	if p.RegionMode == CodexRequestRegionOverride && p.AccountRoutingOverride == "" && p.Residency == "" {
		return fmt.Errorf("region override requires account_routing_override or residency")
	}
	if p.TimeContextMode != CodexRequestTimeContextPreserve && p.TimeContextMode != CodexRequestTimeContextStrip && p.TimeContextMode != CodexRequestTimeContextOverride {
		return fmt.Errorf("time_context_mode must be preserve, strip or override")
	}
	if p.TimeContextMode == CodexRequestTimeContextOverride {
		if _, err := NormalizeOpenAIRequestTimezone(p.Timezone); err != nil {
			return fmt.Errorf("timezone: %w", err)
		}
	} else if strings.TrimSpace(p.Timezone) != "" {
		if _, err := NormalizeOpenAIRequestTimezone(p.Timezone); err != nil {
			return fmt.Errorf("timezone: %w", err)
		}
	}
	if p.ComplianceMode != CodexRequestComplianceAccount && p.ComplianceMode != CodexRequestCompliancePreserve && p.ComplianceMode != CodexRequestComplianceStrip {
		return fmt.Errorf("compliance_mode must be account, preserve or strip")
	}
	return validateCodexCookieMode(p.CookieMode, p.RouteAffinityMode == CodexRouteAffinityStrict)
}

func normalizeCodexRequestStrategyPolicy(p CodexRequestStrategyPolicy) CodexRequestStrategyPolicy {
	d := DefaultCodexRequestStrategyPolicy()
	if strings.TrimSpace(p.Strategy) == "" {
		p.Strategy = d.Strategy
	}
	if strings.TrimSpace(p.Scope) == "" {
		p.Scope = d.Scope
	}
	if strings.TrimSpace(p.FailureMode) == "" {
		p.FailureMode = d.FailureMode
	}
	if p.ProbeTimeoutSeconds == 0 {
		p.ProbeTimeoutSeconds = d.ProbeTimeoutSeconds
	}
	if strings.TrimSpace(p.RouteAffinityMode) == "" {
		p.RouteAffinityMode = d.RouteAffinityMode
	}
	if p.RouteFailureCooldownSeconds == 0 {
		p.RouteFailureCooldownSeconds = d.RouteFailureCooldownSeconds
	}
	if strings.TrimSpace(p.RegionMode) == "" {
		p.RegionMode = d.RegionMode
	}
	if strings.TrimSpace(p.TimeContextMode) == "" {
		p.TimeContextMode = d.TimeContextMode
	}
	if strings.TrimSpace(p.ComplianceMode) == "" {
		p.ComplianceMode = d.ComplianceMode
	}
	p.AccountRoutingOverride = strings.TrimSpace(p.AccountRoutingOverride)
	p.Residency = strings.TrimSpace(strings.ToLower(p.Residency))
	p.Timezone = strings.TrimSpace(p.Timezone)
	p.CookieMode = normalizeCodexCookieMode(p.CookieMode)
	return p
}
