package config

import (
	"context"
	"fmt"
	"reflect"
)

type AstraRoutingSettings struct {
	SchedulingMode     string                `json:"scheduling_mode"`
	SchedulingGroupIDs []int64               `json:"scheduling_group_ids"`
	AccountScheduling  bool                  `json:"account_scheduling"`
	CookiePool         CodexGatewayPinConfig `json:"cookie_pool"`
	WSSession          CodexWSAnchorConfig   `json:"ws_session"`
	Revision           string                `json:"revision"`
}
type astraRoutingLoader struct {
	load func(context.Context) AstraRoutingSettings
}

// CodexGatewayPinConfig shares only a qualified gateway route between
// explicitly selected source and target accounts.
type CodexGatewayPinConfig struct {
	NodeCooldownSeconds int     `mapstructure:"node_cooldown_seconds" json:"node_cooldown_seconds"`
	RotateNodes         bool    `mapstructure:"rotate_nodes" json:"rotate_nodes"`
	MaxNodeAttempts     int     `mapstructure:"max_node_attempts" json:"max_node_attempts"`
	IPAffinity          bool    `mapstructure:"ip_affinity" json:"ip_affinity"`
	TTLSeconds          int     `mapstructure:"ttl_seconds" json:"ttl_seconds"`
	Enabled             bool    `mapstructure:"enabled" json:"enabled"`
	SourceAccountIDs    []int64 `mapstructure:"source_account_ids" json:"source_account_ids"`
	TargetAccountIDs    []int64 `mapstructure:"target_account_ids" json:"target_account_ids"`
}

func (c CodexGatewayPinConfig) Validate() error {
	if c.NodeCooldownSeconds != 0 && (c.NodeCooldownSeconds < 60 || c.NodeCooldownSeconds > 86400) {
		return fmt.Errorf("astra node cooldown must be 60-86400 seconds")
	}
	if c.MaxNodeAttempts < 0 || c.MaxNodeAttempts > 10 {
		return fmt.Errorf("astra node attempts must be 1-10 (0 uses default 3)")
	}
	if c.TTLSeconds != 0 && (c.TTLSeconds < 30 || c.TTLSeconds > 240) {
		return fmt.Errorf("cookie TTL must be 30-240 seconds")
	}
	if !c.Enabled {
		return nil
	}
	if len(c.SourceAccountIDs) == 0 || len(c.TargetAccountIDs) == 0 || len(c.SourceAccountIDs) > 64 || len(c.TargetAccountIDs) > 64 {
		return fmt.Errorf("gateway.codex_gateway_pin requires 1-64 source_account_ids and target_account_ids")
	}
	seen := map[int64]bool{}
	for _, ids := range [][]int64{c.SourceAccountIDs, c.TargetAccountIDs} {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return fmt.Errorf("gateway.codex_gateway_pin account IDs must be positive, unique and disjoint")
			}
			seen[id] = true
		}
	}
	return nil
}

type CodexWSAnchorConfig struct {
	TTLSeconds int     `mapstructure:"ttl_seconds" json:"ttl_seconds"`
	Enabled    bool    `mapstructure:"enabled" json:"enabled"`
	AccountIDs []int64 `mapstructure:"account_ids" json:"account_ids"`
}

func (c CodexWSAnchorConfig) Validate() error {
	if c.TTLSeconds != 0 && (c.TTLSeconds < 60 || c.TTLSeconds > 3600) {
		return fmt.Errorf("WS TTL must be 60-3600 seconds")
	}
	if !c.Enabled {
		return nil
	}
	if len(c.AccountIDs) == 0 || len(c.AccountIDs) > 64 {
		return fmt.Errorf("gateway.codex_ws_anchor requires 1-64 account_ids")
	}
	seen := map[int64]bool{}
	for _, id := range c.AccountIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("gateway.codex_ws_anchor account IDs must be positive and unique")
		}
		seen[id] = true
	}
	return nil
}

func (c *Config) SetAstraRoutingLoader(load func(context.Context) AstraRoutingSettings) {
	c.astraRoutingLoader.Store(&astraRoutingLoader{load: load})
}
func (c *Config) AstraRouting(ctx context.Context) AstraRoutingSettings {
	if c == nil {
		return AstraRoutingSettings{}
	}
	if loader := c.astraRoutingLoader.Load(); loader != nil {
		return loader.load(ctx)
	}
	return AstraRoutingSettings{CookiePool: c.Gateway.CodexGatewayPin, WSSession: c.Gateway.CodexWSAnchor}
}
func (s AstraRoutingSettings) Validate() error {
	switch s.SchedulingMode {
	case "", "account", "model", "groups":
	default:
		return fmt.Errorf("invalid scheduling mode")
	}
	if len(s.SchedulingGroupIDs) > 100 {
		return fmt.Errorf("select at most 100 groups")
	}
	seen := map[int64]bool{}
	for _, id := range s.SchedulingGroupIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("invalid scheduling groups")
		}
		seen[id] = true
	}
	if s.AccountScheduling && s.SchedulingMode == "groups" && len(s.SchedulingGroupIDs) == 0 {
		return fmt.Errorf("select scheduling groups")
	}
	if err := s.CookiePool.Validate(); err != nil {
		return err
	}
	return s.WSSession.Validate()
}

func (c *Config) HasAstraRoutingLoader() bool { return c != nil && c.astraRoutingLoader.Load() != nil }

func (s AstraRoutingSettings) EffectiveSchedulingMode() string {
	if s.SchedulingMode == "" {
		return "account"
	}
	return s.SchedulingMode
}
func AstraRouteSettingsEqual(a, b AstraRoutingSettings) bool {
	a.AccountScheduling, b.AccountScheduling = false, false
	a.SchedulingMode, b.SchedulingMode = "", ""
	a.SchedulingGroupIDs, b.SchedulingGroupIDs = nil, nil
	a.Revision, b.Revision = "", ""
	return reflect.DeepEqual(a, b)
}
