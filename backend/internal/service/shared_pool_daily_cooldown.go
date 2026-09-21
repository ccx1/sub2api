package service

// SharedPoolDailyCooldown 限定用户可编辑和查看的每日冷却字段。
type SharedPoolDailyCooldown struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start,omitempty"`
	End      string `json:"end,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// ExtraValue 仅生成冷却对象，不能借此写入其它账号 Extra 字段。
func (c *SharedPoolDailyCooldown) ExtraValue() map[string]any {
	if c == nil {
		return nil
	}
	if !c.Enabled {
		return map[string]any{"enabled": false}
	}
	zone := c.Timezone
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	return map[string]any{"enabled": true, "start": c.Start, "end": c.End, "timezone": zone}
}

func normalizeSharedDailyCooldown(input *SharedPoolDailyCooldown) (*SharedPoolDailyCooldown, error) {
	if input == nil {
		return nil, nil
	}
	value := *input
	if !value.Enabled {
		return &SharedPoolDailyCooldown{}, nil
	}
	if value.Timezone == "" {
		value.Timezone = "Asia/Shanghai"
	}
	if err := ValidateDailyCooldownExtra(map[string]any{DailyCooldownExtraKey: value.ExtraValue()}); err != nil {
		return nil, err
	}
	return &value, nil
}

func sharedDailyCooldownView(extra map[string]any) *SharedPoolDailyCooldown {
	raw, ok := extra[DailyCooldownExtraKey].(map[string]any)
	if !ok || ValidateDailyCooldownExtra(map[string]any{DailyCooldownExtraKey: raw}) != nil {
		return nil
	}
	enabled, _ := raw["enabled"].(bool)
	start, _ := raw["start"].(string)
	end, _ := raw["end"].(string)
	zone, _ := raw["timezone"].(string)
	value, _ := normalizeSharedDailyCooldown(&SharedPoolDailyCooldown{Enabled: enabled, Start: start, End: end, Timezone: zone})
	return value
}
