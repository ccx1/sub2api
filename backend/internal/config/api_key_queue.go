package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type APIKeyQueueConfig struct {
	MaxWaiting     int `mapstructure:"max_waiting"`
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
}

func (c APIKeyQueueConfig) Timeout() time.Duration {
	return time.Duration(c.TimeoutSeconds) * time.Second
}

const (
	APIKeyQueueMaxWaitingEnv         = "GATEWAY_API_KEY_QUEUE_MAX_WAITING"
	APIKeyQueueTimeoutSecondsEnv     = "GATEWAY_API_KEY_QUEUE_TIMEOUT_SECONDS"
	defaultAPIKeyQueueMaxWaiting     = 5
	defaultAPIKeyQueueTimeoutSeconds = 30
	maxAPIKeyQueueTimeoutSeconds     = math.MaxInt64 / int64(time.Second)
)

// 严格解析，避免 Viper 将小数或非法配置静默转换为有效策略。
func loadAPIKeyQueueConfig() (APIKeyQueueConfig, error) {
	maxWaiting, err := strictConfigInt(viper.Get("gateway.api_key_queue.max_waiting"))
	if err != nil {
		return APIKeyQueueConfig{}, fmt.Errorf("gateway.api_key_queue.max_waiting: %w", err)
	}
	if maxWaiting < 0 {
		return APIKeyQueueConfig{}, fmt.Errorf("gateway.api_key_queue.max_waiting must be non-negative")
	}
	timeoutSeconds, err := strictConfigInt(viper.Get("gateway.api_key_queue.timeout_seconds"))
	if err != nil {
		return APIKeyQueueConfig{}, fmt.Errorf("gateway.api_key_queue.timeout_seconds: %w", err)
	}
	if timeoutSeconds <= 0 {
		return APIKeyQueueConfig{}, fmt.Errorf("gateway.api_key_queue.timeout_seconds must be a positive integer")
	}
	if int64(timeoutSeconds) > maxAPIKeyQueueTimeoutSeconds {
		return APIKeyQueueConfig{}, fmt.Errorf("gateway.api_key_queue.timeout_seconds exceeds the supported duration range")
	}
	return APIKeyQueueConfig{MaxWaiting: maxWaiting, TimeoutSeconds: timeoutSeconds}, nil
}

func strictConfigInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		if v < math.MinInt || v > math.MaxInt {
			return 0, fmt.Errorf("value %d overflows int", v)
		}
		return int(v), nil
	case uint:
		return strictConfigUnsigned(uint64(v))
	case uint64:
		return strictConfigUnsigned(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0, fmt.Errorf("must be a whole number, got %v", v)
		}
		return strictConfigInt(strconv.FormatFloat(v, 'f', 0, 64))
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("must be an integer")
		}
		n, err := strconv.ParseInt(trimmed, 10, strconv.IntSize)
		if err != nil {
			return 0, fmt.Errorf("must be an integer, got %q", v)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("unsupported value type %T", value)
	}
}

func strictConfigUnsigned(value uint64) (int, error) {
	if value > uint64(math.MaxInt) {
		return 0, fmt.Errorf("value %d overflows int", value)
	}
	return int(value), nil
}
