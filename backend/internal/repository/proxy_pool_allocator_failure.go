package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	proxyPoolFailureTTL       = time.Minute
	proxyPoolFailureWindow    = 5 * time.Minute
	proxyPoolFailureThreshold = 3
)

func proxyPoolFailureKey(member string) string {
	return "proxy:{pool}:failures:" + member
}

func (a *ProxyPoolAllocator) ReportFailure(ctx context.Context, accountID, proxyID int64) error {
	if accountID <= 0 || proxyID <= 0 {
		return nil
	}
	if a == nil || a.rdb == nil {
		return fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	member, id := strconv.FormatInt(accountID, 10), strconv.FormatInt(proxyID, 10)
	keys := []string{proxyPoolFailureKey(member), proxyPoolAffinityKey(member), proxyPoolLeaseKey(id), proxyPoolBoundAccountsKey(id)}
	if err := proxyPoolFailureScript.Run(ctx, a.rdb, keys, member, id, int(proxyPoolFailureTTL.Seconds()),
		int(proxyPoolFailureWindow.Seconds()), proxyPoolFailureThreshold).Err(); err != nil {
		return fmt.Errorf("report account proxy failure: %w", err)
	}
	return nil
}

func (a *ProxyPoolAllocator) ReportSuccess(ctx context.Context, accountID, proxyID int64) error {
	if accountID <= 0 || proxyID <= 0 {
		return nil
	}
	if a == nil || a.rdb == nil {
		return fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	keys := []string{proxyPoolAffinityKey(strconv.FormatInt(accountID, 10))}
	if err := proxyPoolSuccessScript.Run(ctx, a.rdb, keys, strconv.FormatInt(proxyID, 10)).Err(); err != nil {
		return fmt.Errorf("report account proxy success: %w", err)
	}
	return nil
}

// 同一账号当前出口连续失败才换绑；旧出口迟到回调不能影响新关联。
var proxyPoolFailureScript = redis.NewScript(`
if redis.call('HGET', KEYS[2], 'proxy_id') ~= ARGV[2] then return 0 end
local clock = redis.call('TIME')
local now = tonumber(clock[1])
local since = tonumber(redis.call('HGET', KEYS[2], 'failure_since')) or now
local count = tonumber(redis.call('HGET', KEYS[2], 'failure_count')) or 0
if now - since >= tonumber(ARGV[4]) then count, since = 0, now end
count = count + 1
redis.call('HSET', KEYS[2], 'failure_count', count, 'failure_since', since)
if count < tonumber(ARGV[5]) then return count end
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[3]), ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
redis.call('DEL', KEYS[2])
redis.call('ZREM', KEYS[3], ARGV[1])
redis.call('SREM', KEYS[4], ARGV[1])
return count
`)

var proxyPoolSuccessScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'proxy_id') ~= ARGV[1] then return 0 end
return redis.call('HDEL', KEYS[1], 'failure_count', 'failure_since')
`)
