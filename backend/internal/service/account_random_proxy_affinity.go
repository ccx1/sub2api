package service

import (
	"encoding/json"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const RandomProxyMaxReuseMinutesExtraKey = "random_proxy_max_reuse_minutes"

func (a *Account) RandomProxyMaxReuseDuration() time.Duration {
	if a == nil {
		return 0
	}
	minutes, _ := randomProxyReuseMinutes(a.Extra[RandomProxyMaxReuseMinutesExtraKey])
	return time.Duration(minutes) * time.Minute
}

// 兼容已有配置值；健康出口持续复用，实际切换由失败、失效或容量约束触发。
func ValidateRandomProxyReuseExtra(extra map[string]any) error {
	if _, valid := randomProxyReuseMinutes(extra[RandomProxyMaxReuseMinutesExtraKey]); !valid {
		return infraerrors.BadRequest("INVALID_RANDOM_PROXY_REUSE_MINUTES", "代理最长复用时间必须是 0 至 525600 的整数分钟，0 表示不限时")
	}
	return nil
}

func randomProxyReuseMinutes(raw any) (int64, bool) {
	if raw == nil {
		return 0, true
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return 0, false
	}
	minutes, err := strconv.ParseInt(string(encoded), 10, 64)
	if err != nil || minutes < 0 || minutes > 525600 {
		return 0, false
	}
	return minutes, true
}
