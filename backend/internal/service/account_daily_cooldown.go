package service

import (
	"sync"
	"time"
	_ "time/tzdata"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const DailyCooldownExtraKey = "daily_cooldown"

var dailyCooldownLocations sync.Map

type dailyCooldownConfig struct {
	enabled bool
	start   int
	end     int
	zone    *time.Location
}

// IsInDailyCooldown 按账号时区判断每日冷却，不更改手动禁用和其它运行状态。
func (a *Account) IsInDailyCooldown(now time.Time) bool {
	if a == nil {
		return false
	}
	config, err := parseDailyCooldownExtra(a.Extra)
	if err != nil || !config.enabled {
		return false
	}
	local := now.In(config.zone)
	minute := local.Hour()*60 + local.Minute()
	if config.start < config.end {
		return minute >= config.start && minute < config.end
	}
	return minute >= config.start || minute < config.end
}

func ValidateDailyCooldownExtra(extra map[string]any) error {
	_, err := parseDailyCooldownExtra(extra)
	return err
}

func parseDailyCooldownExtra(extra map[string]any) (dailyCooldownConfig, error) {
	var config dailyCooldownConfig
	raw, exists := extra[DailyCooldownExtraKey]
	if !exists || raw == nil {
		return config, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return config, invalidDailyCooldown("每日冷却配置必须是对象")
	}
	if enabled, exists := values["enabled"]; exists {
		config.enabled, ok = enabled.(bool)
		if !ok {
			return config, invalidDailyCooldown("每日冷却开关必须是布尔值")
		}
	}
	var err error
	config.start, err = dailyCooldownMinute(values, "start", config.enabled)
	if err != nil {
		return config, err
	}
	config.end, err = dailyCooldownMinute(values, "end", config.enabled)
	if err != nil {
		return config, err
	}
	if config.start >= 0 && config.end >= 0 && config.start == config.end {
		return config, invalidDailyCooldown("每日冷却的开始时间和结束时间不能相同")
	}
	if !config.enabled && values["timezone"] == nil {
		return config, nil
	}
	config.zone, err = dailyCooldownLocation(values)
	return config, err
}

func dailyCooldownMinute(values map[string]any, key string, required bool) (int, error) {
	raw, exists := values[key]
	if !exists && !required {
		return -1, nil
	}
	value, ok := raw.(string)
	if !ok || len(value) != 5 || value[2] != ':' {
		return 0, invalidDailyCooldown("每日冷却时间必须使用 HH:mm 格式")
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return 0, invalidDailyCooldown("每日冷却时间必须使用 HH:mm 格式")
		}
	}
	hour, minute := int(value[0]-'0')*10+int(value[1]-'0'), int(value[3]-'0')*10+int(value[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, invalidDailyCooldown("每日冷却时间必须在 00:00 至 23:59 范围内")
	}
	return hour*60 + minute, nil
}

func dailyCooldownLocation(values map[string]any) (*time.Location, error) {
	name := "Asia/Shanghai"
	if raw, exists := values["timezone"]; exists {
		value, ok := raw.(string)
		if !ok {
			return nil, invalidDailyCooldown("每日冷却时区必须是有效的 IANA 时区")
		}
		if value != "" {
			name = value
		}
	}
	if name == "Local" {
		return nil, invalidDailyCooldown("每日冷却不能使用服务器本地时区，请指定 IANA 时区")
	}
	if cached, ok := dailyCooldownLocations.Load(name); ok {
		return cached.(*time.Location), nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalidDailyCooldown("每日冷却时区必须是有效的 IANA 时区")
	}
	dailyCooldownLocations.Store(name, location)
	return location, nil
}

func invalidDailyCooldown(message string) error {
	return infraerrors.BadRequest("INVALID_DAILY_COOLDOWN", message)
}
