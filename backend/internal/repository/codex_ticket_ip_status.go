package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const codexSchedulerIPStatusPattern = "proxy:{pool}:codex:ip:*"

type codexSchedulerIPState struct {
	IP             string             `json:"ip"`
	Failed         map[string]float64 `json:"failed"`
	FailedAccounts float64            `json:"failed_accounts"`
	Rounds         float64            `json:"rounds"`
	UntilAt        float64            `json:"until_at"`
	Disabled       bool               `json:"disabled"`
	LastFailureAt  float64            `json:"last_failure_at"`
}

// ListCodexIPStatus returns only IPs that are currently cooling or permanently
// disabled. It scans the scheduler state keys without writing, refreshing, or
// deleting any Redis data. Legacy state records that do not include the clear
// text IP are skipped because the hashed key cannot be reversed safely.
func (a *ProxyPoolAllocator) ListCodexIPStatus(ctx context.Context, status service.CodexIPStatusKind, page, pageSize int) ([]service.CodexIPStatus, int64, error) {
	if a == nil || a.rdb == nil {
		return nil, 0, fmt.Errorf("codex IP status storage unavailable")
	}
	if status != "" && status != service.CodexIPStatusCooling && status != service.CodexIPStatusDisabled {
		return nil, 0, fmt.Errorf("invalid codex IP status: %q", status)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	keys, err := scanCodexIPStatusKeys(ctx, a.rdb)
	if err != nil {
		return nil, 0, fmt.Errorf("scan codex IP status: %w", err)
	}
	items := make([]service.CodexIPStatus, 0, len(keys))
	now := time.Now().UnixMilli()
	for _, key := range keys {
		raw, err := a.rdb.Get(ctx, key).Bytes()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, 0, fmt.Errorf("read codex IP status: %w", err)
		}
		var state codexSchedulerIPState
		if err := json.Unmarshal(raw, &state); err != nil {
			return nil, 0, fmt.Errorf("decode codex IP status: %w", err)
		}
		ip := strings.TrimSpace(state.IP)
		if ip == "" || net.ParseIP(strings.Trim(ip, "[]")) == nil {
			continue
		}
		kind := service.CodexIPStatusKind("")
		switch {
		case state.Disabled:
			kind = service.CodexIPStatusDisabled
		case state.UntilAt > float64(now):
			kind = service.CodexIPStatusCooling
		default:
			continue
		}
		if status != "" && status != kind {
			continue
		}
		failedAccounts := int(state.FailedAccounts)
		if failedAccounts <= 0 {
			failedAccounts = len(state.Failed)
		}
		item := service.CodexIPStatus{
			IP:             ip,
			Status:         kind,
			Rounds:         max(0, int(state.Rounds)),
			FailedAccounts: failedAccounts,
		}
		if state.UntilAt > float64(now) {
			until := time.UnixMilli(int64(state.UntilAt)).UTC()
			item.UntilAt = &until
		}
		lastFailure := state.LastFailureAt
		if lastFailure <= 0 {
			for _, at := range state.Failed {
				if at > lastFailure {
					lastFailure = at
				}
			}
		}
		if lastFailure > 0 {
			at := time.UnixMilli(int64(lastFailure)).UTC()
			item.LastFailureAt = &at
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Status != items[j].Status {
			return items[i].Status < items[j].Status
		}
		if items[i].UntilAt == nil || items[j].UntilAt == nil {
			return items[i].UntilAt != nil
		}
		if !items[i].UntilAt.Equal(*items[j].UntilAt) {
			return items[i].UntilAt.Before(*items[j].UntilAt)
		}
		return items[i].IP < items[j].IP
	})
	total := int64(len(items))
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []service.CodexIPStatus{}, total, nil
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}

func scanCodexIPStatusKeys(ctx context.Context, rdb *redis.Client) ([]string, error) {
	keys := make([]string, 0)
	iter := rdb.Scan(ctx, 0, codexSchedulerIPStatusPattern, 256).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}
